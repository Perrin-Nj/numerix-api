package app

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/Perrin-Nj/numerix-api/api"
	"github.com/Perrin-Nj/numerix-api/internal/httpapi"
	"github.com/Perrin-Nj/numerix-api/internal/narrator"
)

// sseResponse writes a prepared event stream. The generated response type cannot set Cache-Control,
// so this small type implements api.StreamNarrationResponseObject itself (Spring: a ResponseEntity with headers).
type sseResponse struct{ body []byte }

// VisitStreamNarrationResponse sends headers, then the whole stream, flushed once.
func (r sseResponse) VisitStreamNarrationResponse(w http.ResponseWriter) error {
	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-cache")
	h.Set("X-Accel-Buffering", "no") // tell proxies not to buffer the stream
	w.WriteHeader(http.StatusOK)
	if _, err := w.Write(r.body); err != nil {
		return err
	}
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}
	return nil
}

// sseBlocks renders narration as contract events: id, event name, JSON data. delta chunks of about four
// words, then `done`; or one `fallback` event with the full text when the story was simplified.
// Blocks with id <= after are skipped (reconnect with Last-Event-ID).
func sseBlocks(phase string, res narrator.Result, after int) []byte {
	var b bytes.Buffer
	n := 0
	emit := func(event string, data any) {
		n++
		if n <= after {
			return
		}
		j, _ := json.Marshal(data)
		fmt.Fprintf(&b, "id: %d\nevent: %s\ndata: %s\n\n", n, event, j)
	}
	if res.Status == narrator.StatusFallback {
		emit("fallback", map[string]string{"text": res.Text})
		return b.Bytes()
	}
	for _, chunk := range narrator.Words(res.Text) {
		emit(phase, map[string]string{"delta": chunk})
	}
	emit("done", map[string]string{})
	return b.Bytes()
}

// StreamNarration answers GET /sessions/{sessionId}/beats/{beatId}/narration?phase=scene|denouement.
// The text comes from the narrator (LLM when configured, canned otherwise) and always passes the leak check.
func (s *Server) StreamNarration(ctx context.Context, req api.StreamNarrationRequestObject) (api.StreamNarrationResponseObject, error) {
	r, c, err := s.loadSession(ctx, plUID(req.SessionId))
	if err != nil {
		return nil, err
	}
	phase := string(req.Params.Phase)
	if phase != narrator.PhaseScene && phase != narrator.PhaseDenouement {
		return nil, httpapi.Validation(plField("phase", "must be scene or denouement"))
	}
	lang := gameLang(req.Params.AcceptLanguage, c.Locale)
	var qid, answerJSON string
	var idx int
	if err := s.Store.R.QueryRowContext(ctx, `SELECT b.question_id, b.idx, COALESCE((SELECT a.json FROM answers a WHERE a.beat_id = b.id LIMIT 1),'')
		FROM beats b WHERE b.id = ? AND b.session_id = ?`, plUID(req.BeatId), r.ID).Scan(&qid, &idx, &answerJSON); errors.Is(err, sql.ErrNoRows) {
		return nil, httpapi.ErrNotFound
	} else if err != nil {
		return nil, s.plInternalErr(ctx, "load beat", err)
	}
	q, err := loadQuestion(ctx, s.Store.R, qid)
	if err != nil {
		return nil, s.plInternalErr(ctx, "load question", err)
	}
	nreq := narrator.Request{World: r.World, Lang: lang, Phase: phase, Seed: r.Data.Seed, Index: idx, MathText: localMath(&q.Problem, lang)}
	if phase == narrator.PhaseScene {
		nreq.Payload = q.Problem.ForLLMScene()
	} else {
		if answerJSON == "" {
			return nil, plConflict("beat_not_answered", "Answer the question first.")
		}
		var st storedAnswer
		if err := json.Unmarshal([]byte(answerJSON), &st); err != nil {
			return nil, s.plInternalErr(ctx, "decode answer", err)
		}
		res := q.Problem.Evaluate(st.Given)
		nreq.Outcome = string(res.Type)
		nreq.Payload = q.Problem.ForLLMDenouement(res, true) // phase 1: the narrator never sees the answer
	}
	nreq.CacheKey = strings.Join([]string{qid, r.World, lang, phase, nreq.Outcome, narrator.PromptVersion}, "|")
	res := s.game().narr.Narrate(ctx, nreq, q.Problem.AnswerTokens())

	after := 0
	if req.Params.LastEventID != nil {
		after, _ = strconv.Atoi(*req.Params.LastEventID) // a bad value restarts from the beginning
	}
	return sseResponse{body: sseBlocks(phase, res, after)}, nil
}

// compile-time check: sseResponse satisfies the generated interface.
var _ api.StreamNarrationResponseObject = sseResponse{}
