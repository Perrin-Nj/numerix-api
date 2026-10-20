package app

// Go for Spring people: game_*.go files implement the play operations (worlds, journey, sessions, beats,
// answers, narration, progress) on the same Server type. Files of one package see each other's lowercase
// names, so game_* reuses helpers from platform_common.go (plCaller, plOwnedChild ...) with no import.
//
// Rules this module keeps:
//   - the correct answer never leaves the server before grading (questions.json is read only inside the app);
//   - question selection is ONE write transaction (pick an unserved question, else generate and insert,
//     then record "served") so two requests can never serve the same pair (SQLite has no SKIP LOCKED);
//   - the narrator never calculates and never receives the answer in phase 1.

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"math/rand/v2"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/Perrin-Nj/numerix-api/api"
	"github.com/Perrin-Nj/numerix-api/internal/auth"
	"github.com/Perrin-Nj/numerix-api/internal/engine"
	"github.com/Perrin-Nj/numerix-api/internal/httpapi"
	"github.com/Perrin-Nj/numerix-api/internal/narrator"
)

// chapterLength is the number of beats in a chapter (contract: ChapterLength, same as the mock).
const chapterLength = 3

// gameState holds the game collaborators of one Server. It is created lazily on first use, so the
// Server struct (owned by the platform module) needs no new field.
type gameState struct {
	graph *engine.Graph      // skill graph, loaded once from the embedded JSON
	narr  *narrator.Narrator // story text with leak check and fallback
	seed  func() uint64      // source of generation seeds (tests replace it)
	now   func() time.Time   // clock (tests replace it)
}

// games maps a Server to its gameState (Spring: a singleton bean per application context).
var games sync.Map

// game returns the Server's gameState, building it on first call.
func (s *Server) game() *gameState {
	if v, ok := games.Load(s); ok {
		return v.(*gameState)
	}
	graph, err := engine.LoadGraph()
	if err != nil { // the JSON is embedded, so this is a build mistake: fail loudly at first use
		panic("game: load curriculum: " + err.Error())
	}
	log := s.Log
	if log == nil {
		log = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	gs := &gameState{graph: graph, seed: rand.Uint64, now: func() time.Time { return time.Now().UTC() }}
	gs.narr = narrator.New(narrator.FromEnv(os.Getenv, log), narrationCache{s}, log)
	v, _ := games.LoadOrStore(s, gs) // another request may have won the race: use its value
	return v.(*gameState)
}

// narrationCache stores LLM narrations in the `narrations` table (implements narrator.Cache).
type narrationCache struct{ s *Server }

// Get reads one cached narration.
func (c narrationCache) Get(ctx context.Context, key string) (string, bool, error) {
	var text string
	err := c.s.Store.R.QueryRowContext(ctx, `SELECT text FROM narrations WHERE cache_key = ?`, key).Scan(&text)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	return text, err == nil, err
}

// Put stores one narration (first writer wins).
func (c narrationCache) Put(ctx context.Context, key, text, source string) error {
	parts := strings.Split(key, "|") // key layout: question|world|lang|phase|outcome|version
	get := func(i int) string {
		if i < len(parts) {
			return parts[i]
		}
		return ""
	}
	return c.s.Store.WithWriteTx(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO narrations (cache_key, world, lang, phase, text, source) VALUES (?, ?, ?, ?, ?, ?)`,
			key, get(1), get(2), get(3), text, source)
		return err
	})
}

// gameLang reduces an Accept-Language value to "fr" or "en" (default fr).
func gameLang(accept *string, childLocale string) string {
	if plLocaleOf(accept, childLocale) == "en-CA" {
		return "en"
	}
	return "fr"
}

// gameChild resolves the caller and loads the child they may act on (parent: own child; child: itself).
// requirePlay also enforces the play rules: active consent, parent not blocked, subscription allows play.
func (s *Server) gameChild(ctx context.Context, childID string, requirePlay bool) (plChild, auth.Principal, error) {
	p, err := plCaller(ctx)
	if err != nil {
		return plChild{}, p, err
	}
	c, err := s.plOwnedChild(ctx, p, childID)
	if err != nil {
		return plChild{}, p, err
	}
	if requirePlay {
		if !c.Consented {
			return c, p, httpapi.NewError(http.StatusForbidden, "consent_missing", "A parent must give consent before this child can play.")
		}
		if err := s.gamePlayAllowed(ctx, c.ParentID); err != nil {
			return c, p, err
		}
	}
	return c, p, nil
}

// gamePlayAllowed refuses a blocked parent (403) and an inactive subscription unless the platform policy
// is "allow" (402). The start gate is can_play OR unpaid_policy allow (contract SubscriptionView).
func (s *Server) gamePlayAllowed(ctx context.Context, parentID string) error {
	var blocked sql.NullString
	if err := s.Store.R.QueryRowContext(ctx, `SELECT blocked_at FROM parents WHERE id = ?`, parentID).Scan(&blocked); err != nil {
		return s.plInternalErr(ctx, "load parent", err)
	}
	if blocked.Valid && blocked.String != "" {
		return httpapi.NewError(http.StatusForbidden, "account_blocked", "This account is blocked.")
	}
	sub, err := s.plLoadSub(ctx, parentID)
	if err != nil {
		return err
	}
	if plView(sub, s.game().now()).CanPlay || s.plConfig(ctx).UnpaidPolicy == "allow" {
		return nil
	}
	return httpapi.NewError(http.StatusPaymentRequired, "subscription_inactive", "Your family's subscription is paused.")
}

// sessData is the JSON kept in sessions.json. The feed module reads $.kind from it.
type sessData struct {
	Kind     string `json:"kind"`
	Skill    string `json:"skill_id"`
	Seed     uint32 `json:"seed"`
	Length   int    `json:"length"`
	TitleIdx int    `json:"title_idx"`
	Reason   string `json:"reason"`
	Lang     string `json:"lang"`           // language of the story title stored on the card
	Idem     string `json:"idem,omitempty"` // Idempotency-Key of the start request (a retry returns the same session)
	// Variants are the client's recent scene ids (last maxVariantHistory), kept so a reload keeps the no-repeat rule.
	Variants []string `json:"variants,omitempty"`
}

// sessRow is one sessions row with its parsed JSON and the owning child.
type sessRow struct {
	ID, ChildID, Status, World string
	Started                    string
	Paused, Finished           sql.NullString
	Data                       sessData
}

// loadSession reads a session and checks the caller may see it (404 for other families, 403 for another child).
func (s *Server) loadSession(ctx context.Context, sessionID string) (sessRow, plChild, error) {
	p, err := plCaller(ctx)
	if err != nil {
		return sessRow{}, plChild{}, err
	}
	var r sessRow
	var raw string
	err = s.Store.R.QueryRowContext(ctx, `SELECT id, child_id, status, COALESCE(world_id,''), json, started_at, paused_at, finished_at
		FROM sessions WHERE id = ?`, sessionID).Scan(&r.ID, &r.ChildID, &r.Status, &r.World, &raw, &r.Started, &r.Paused, &r.Finished)
	if errors.Is(err, sql.ErrNoRows) {
		return sessRow{}, plChild{}, httpapi.ErrNotFound
	}
	if err != nil {
		return sessRow{}, plChild{}, s.plInternalErr(ctx, "load session", err)
	}
	if err := json.Unmarshal([]byte(raw), &r.Data); err != nil {
		return sessRow{}, plChild{}, s.plInternalErr(ctx, "decode session", err)
	}
	c, err := s.plOwnedChild(ctx, p, r.ChildID)
	return r, c, err
}

// beatRow is one beats row.
type beatRow struct {
	ID, SessionID, QuestionID string
	Index                     int
	Shape                     string
	Created                   string
}

// answerValue converts an engine value (int, string, []int) to the contract's AnswerValue union.
func answerValue(v engine.Value) api.AnswerValue {
	var out api.AnswerValue
	b, _ := json.Marshal(engine.NormValue(v)) // ints, strings and int lists always marshal
	_ = out.UnmarshalJSON(b)
	return out
}

// engineValue converts the contract's AnswerValue to an engine value (int, string, []int, float64).
func engineValue(a api.AnswerValue) (engine.Value, error) {
	b, err := a.MarshalJSON()
	if err != nil {
		return nil, err
	}
	var v any
	if err := json.Unmarshal(b, &v); err != nil {
		return nil, err
	}
	return engine.NormValue(v), nil
}

// questionRow is a stored question (bank row).
type questionRow struct {
	ID      string
	Problem engine.Problem
}

// loadQuestion reads one bank question; the answer stays inside the returned struct and server code.
func loadQuestion(ctx context.Context, q interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}, id string) (questionRow, error) {
	var raw string
	if err := q.QueryRowContext(ctx, `SELECT json FROM questions WHERE id = ?`, id).Scan(&raw); err != nil {
		return questionRow{}, err
	}
	qr := questionRow{ID: id}
	if err := json.Unmarshal([]byte(raw), &qr.Problem); err != nil {
		return questionRow{}, err
	}
	qr.Problem.Normalize()
	return qr, nil
}

// localMath returns the statement in the request language (the French "(avec reste)" suffix is translated).
func localMath(p *engine.Problem, lang string) string {
	if lang == "en" {
		return strings.ReplaceAll(p.EnonceMath, "(avec reste)", "(with remainder)")
	}
	return p.EnonceMath
}

// newID returns a random UUID text.
func newID() string { return uuid.NewString() }
