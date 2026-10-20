package app

// Go for Spring people: integration tests of the game module on a real HTTP stack and a throw-away SQLite file
// (Spring: @SpringBootTest + MockMvc). They reuse newFeedFixture (a family with a consented child).

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"

	"github.com/Perrin-Nj/numerix-api/internal/engine"
	"github.com/Perrin-Nj/numerix-api/internal/narrator"
)

// gcall sends a request with optional extra headers and returns status, decoded JSON and raw body.
func (f feedFixture) gcall(t *testing.T, method, path, token, body string, hdr map[string]string) (int, map[string]any, string) {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	if method == "POST" {
		req.Header.Set("Idempotency-Key", uuid.NewString()) // every POST of the contract requires one
	}
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	f.h.ServeHTTP(rec, req)
	var out map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	return rec.Code, out, rec.Body.String()
}

// startChapter opens a chapter for the fixture child and returns its id.
func (f feedFixture) startChapter(t *testing.T, skill string) string {
	t.Helper()
	body := `{"world":"nature","kind":"chapter"`
	if skill != "" {
		f.unlock(t, skill) // an explicit skill must be unlocked (QA-BE-004), so master its prerequisites first
		body += `,"skill_id":"` + skill + `"`
	}
	code, out, raw := f.gcall(t, "POST", "/v1/children/"+f.childID+"/sessions", f.childTok, body+"}", map[string]string{"Idempotency-Key": uuid.NewString()})
	if code != 201 {
		t.Fatalf("start session: %d %s", code, raw)
	}
	return out["id"].(string)
}

// problemOf reads the stored question behind a beat (what only the server knows).
func (f feedFixture) problemOf(t *testing.T, beatID string) engine.Problem {
	t.Helper()
	var raw string
	if err := f.s.Store.R.QueryRowContext(context.Background(), `SELECT q.json FROM beats b JOIN questions q ON q.id = b.question_id WHERE b.id = ?`, beatID).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	var p engine.Problem
	if err := json.Unmarshal([]byte(raw), &p); err != nil {
		t.Fatal(err)
	}
	p.Normalize()
	return p
}

// answer posts an answer with a fresh idempotency key.
func (f feedFixture) answer(t *testing.T, sessionID, beatID string, v any, key string) (int, map[string]any) {
	t.Helper()
	b, _ := json.Marshal(map[string]any{"answer": v})
	code, out, _ := f.gcall(t, "POST", "/v1/sessions/"+sessionID+"/beats/"+beatID+"/answer", f.childTok, string(b), map[string]string{"Idempotency-Key": key})
	return code, out
}

// TestGameChapterFlow plays a whole chapter: right, typical error, right.
func TestGameChapterFlow(t *testing.T) {
	f := newFeedFixture(t, true)
	sid := f.startChapter(t, "ARI-OPE-A06a")

	wantOutcomes := []string{"correct", "erreur_typique", "correct"}
	var shapes []string
	for i, want := range wantOutcomes {
		code, beat, raw := f.gcall(t, "POST", "/v1/sessions/"+sid+"/beats", f.childTok, "{}", nil)
		if code != 201 {
			t.Fatalf("beat %d: %d %s", i, code, raw)
		}
		if strings.Contains(raw, "correct_answer") || strings.Contains(raw, "reponse") {
			t.Fatalf("beat must not carry the answer: %s", raw)
		}
		q := beat["question"].(map[string]any)
		if len(q["choices"].([]any)) != 4 || q["math_text"] == "" {
			t.Fatalf("question = %v", q)
		}
		beatID := beat["id"].(string)
		p := f.problemOf(t, beatID)
		shapes = append(shapes, p.Shape)
		if i > 0 && shapes[i] == shapes[i-1] {
			t.Fatalf("beats %d and %d share shape %q", i-1, i, p.Shape)
		}
		// asking for the beat again returns the same open beat (200), not a new one
		if code, again, _ := f.gcall(t, "POST", "/v1/sessions/"+sid+"/beats", f.childTok, "{}", nil); code != 200 || again["id"] != beatID {
			t.Fatalf("open beat must be returned again: %d %v", code, again["id"])
		}
		// the denouement is not available before the answer
		if code, _, _ := f.gcall(t, "GET", "/v1/sessions/"+sid+"/beats/"+beatID+"/narration?phase=denouement", f.childTok, "", nil); code != 409 {
			t.Fatalf("denouement before answer: %d", code)
		}
		// the scene streams as server-sent events and ends with done
		code, _, sse := f.gcall(t, "GET", "/v1/sessions/"+sid+"/beats/"+beatID+"/narration?phase=scene", f.childTok, "", nil)
		if code != 200 || !strings.Contains(sse, "event: scene") || !strings.Contains(sse, "event: done") || !strings.Contains(sse, "id: 1") {
			t.Fatalf("scene stream: %d %q", code, sse)
		}
		given := p.Reponse
		if want == "erreur_typique" {
			given = p.Distracteurs[0].Valeur
		}
		key := uuid.NewString()
		code, res := f.answer(t, sid, beatID, given, key)
		if code != 200 || res["outcome"] != want {
			t.Fatalf("answer %d: %d %v", i, code, res)
		}
		if _, ok := res["correct_answer"]; !ok {
			t.Fatal("correct_answer expected after grading")
		}
		if mv, ok := res["method_visual"].(map[string]any); !ok || mv["kind"] == "" {
			t.Fatalf("method_visual missing: %v", res["method_visual"])
		}
		// a retry with the same key replays; another key conflicts
		if code, again := f.answer(t, sid, beatID, given, key); code != 200 || again["outcome"] != want {
			t.Fatalf("replay: %d %v", code, again)
		}
		if code, _ := f.answer(t, sid, beatID, given, uuid.NewString()); code != 409 {
			t.Fatalf("second answer with another key: %d", code)
		}
		// the denouement now streams
		if code, _, sse := f.gcall(t, "GET", "/v1/sessions/"+sid+"/beats/"+beatID+"/narration?phase=denouement", f.childTok, "", nil); code != 200 || !strings.Contains(sse, "event: denouement") {
			t.Fatalf("denouement: %d %q", code, sse)
		}
		if i == 2 {
			sess := res["session"].(map[string]any)
			if sess["status"] != "completed" {
				t.Fatalf("session status = %v", sess["status"])
			}
			score := sess["score"].(map[string]any)
			if score["correct"].(float64) != 2 || score["total"].(float64) != 3 || score["card_awarded"] != false {
				t.Fatalf("score = %v", score)
			}
			if r := fmt.Sprint(res["rewards"]); !strings.Contains(r, "chapter_complete") || strings.Contains(r, "card_earned") {
				t.Fatalf("rewards = %v", r)
			}
		}
	}
	// the chapter is complete: no more beats
	if code, _, _ := f.gcall(t, "POST", "/v1/sessions/"+sid+"/beats", f.childTok, "{}", nil); code != 409 {
		t.Fatalf("beat after completion: %d", code)
	}
	var served int
	_ = f.s.Store.R.QueryRow(`SELECT COUNT(*) FROM child_questions WHERE child_id = ?`, f.childID).Scan(&served)
	if served != 3 {
		t.Fatalf("3 questions must be recorded as served, got %d", served)
	}
	// summary, progress, reviews, growth and the weekly report answer
	for _, p := range []string{"/v1/sessions/" + sid + "/summary", "/v1/children/" + f.childID + "/progress", "/v1/children/" + f.childID + "/reviews", "/v1/children/" + f.childID + "/growth", "/v1/sessions/" + sid, "/v1/children/" + f.childID + "/sessions"} {
		if code, _, raw := f.gcall(t, "GET", p, f.parentTok, "", nil); code != 200 {
			t.Fatalf("GET %s: %d %s", p, code, raw)
		}
	}
	code, sum, _ := f.gcall(t, "GET", "/v1/sessions/"+sid+"/summary", f.parentTok, "", nil)
	if code != 200 || len(sum["stuck_points"].([]any)) != 1 || !strings.Contains(sum["story_recap"].(string), "2 sur 3") {
		t.Fatalf("summary = %v", sum)
	}
	code, rep, raw := f.gcall(t, "GET", "/v1/reports/weekly?child_id="+f.childID+"&week_start=2020-01-06", f.parentTok, "", nil)
	if code != 200 || rep["sessions"].(float64) != 0 {
		t.Fatalf("weekly report: %d %s", code, raw)
	}
	if code, _, _ := f.gcall(t, "GET", "/v1/reports/weekly?child_id="+f.childID+"&week_start=2020-01-07", f.parentTok, "", nil); code != 422 {
		t.Fatalf("a Tuesday is not a week start: %d", code)
	}
}

// TestGamePerfectChapterAwardsCard: three right answers reach the card threshold and write the collection.
func TestGamePerfectChapterAwardsCard(t *testing.T) {
	f := newFeedFixture(t, true)
	sid := f.startChapter(t, "ARI-OPE-A03a")
	var last map[string]any
	for i := 0; i < 3; i++ {
		_, beat, _ := f.gcall(t, "POST", "/v1/sessions/"+sid+"/beats", f.childTok, "{}", nil)
		p := f.problemOf(t, beat["id"].(string))
		_, last = f.answer(t, sid, beat["id"].(string), p.Reponse, uuid.NewString())
	}
	if r := fmt.Sprint(last["rewards"]); !strings.Contains(r, "card_earned") {
		t.Fatalf("a 100%% chapter must earn a card: %v", r)
	}
	var n int
	_ = f.s.Store.R.QueryRow(`SELECT COUNT(*) FROM collection WHERE child_id = ?`, f.childID).Scan(&n)
	if n != 1 {
		t.Fatalf("collection rows = %d", n)
	}
	if code, c, _ := f.gcall(t, "GET", "/v1/children/"+f.childID+"/collection", f.childTok, "", nil); code != 200 || len(c["items"].([]any)) != 1 {
		t.Fatalf("collection endpoint: %d %v", code, c)
	}
}

// TestGameCatalog covers worlds, skills and the journey.
func TestGameCatalog(t *testing.T) {
	f := newFeedFixture(t, true)
	_, w, _ := f.gcall(t, "GET", "/v1/worlds", f.childTok, "", nil)
	if len(w["items"].([]any)) != 20 {
		t.Fatalf("worlds = %v", w)
	}
	_, sk, _ := f.gcall(t, "GET", "/v1/skills?implemented=true", f.parentTok, "", nil)
	if len(sk["items"].([]any)) != 8 {
		t.Fatalf("implemented skills = %d", len(sk["items"].([]any)))
	}
	_, all, _ := f.gcall(t, "GET", "/v1/skills", f.parentTok, "", nil)
	if len(all["items"].([]any)) != 75 {
		t.Fatalf("skills = %d", len(all["items"].([]any)))
	}
	code, j, raw := f.gcall(t, "GET", "/v1/children/"+f.childID+"/journey?world=nature", f.childTok, "", nil)
	if code != 200 {
		t.Fatalf("journey: %d %s", code, raw)
	}
	nodes := j["nodes"].([]any)
	first := nodes[0].(map[string]any)
	if first["skill_id"] != "ARI-OPE-A02a" || first["state"] != "available" || j["current_skill_id"] != "ARI-OPE-A02a" {
		t.Fatalf("first node = %v current=%v", first, j["current_skill_id"])
	}
	if code, _, _ := f.gcall(t, "GET", "/v1/children/"+f.childID+"/journey?world=atlantis", f.childTok, "", nil); code == 200 {
		t.Fatal("unknown world must fail")
	}
}

// TestGameAccessRules: consent, other child, paused session, planner choice.
func TestGameAccessRules(t *testing.T) {
	f := newFeedFixture(t, false)
	if code, _, _ := f.gcall(t, "POST", "/v1/children/"+f.childID+"/sessions", f.childTok, `{"world":"nature","kind":"chapter"}`, map[string]string{"Idempotency-Key": uuid.NewString()}); code != 403 {
		t.Fatalf("no consent must be 403, got %d", code)
	}
	f.sql(t, `INSERT INTO consents (id, parent_id, child_id, notice_version) VALUES ('k-9', 'p-1', ?, 'v1')`, f.childID)
	sid := f.startChapter(t, "") // planner picks the first new skill
	var skill string
	_ = f.s.Store.R.QueryRow(`SELECT json_extract(json,'$.skill_id') FROM sessions WHERE id = ?`, sid).Scan(&skill)
	if skill != "ARI-OPE-A02a" {
		t.Fatalf("planner choice = %q", skill)
	}
	if code, _, _ := f.gcall(t, "GET", "/v1/sessions/"+sid, f.otherChildTok, "", nil); code != 403 {
		t.Fatalf("another child must get 403, got %d", code)
	}
	if code, _, _ := f.gcall(t, "GET", "/v1/sessions/"+sid, f.parentTok, "", nil); code != 200 {
		t.Fatalf("parent may read: %d", code)
	}
	if code, o, _ := f.gcall(t, "POST", "/v1/sessions/"+sid+"/pause", f.childTok, "{}", nil); code != 200 || o["status"] != "paused" {
		t.Fatalf("pause: %d %v", code, o)
	}
	if code, _, _ := f.gcall(t, "POST", "/v1/sessions/"+sid+"/beats", f.childTok, "{}", nil); code != 409 {
		t.Fatalf("beat on paused session: %d", code)
	}
	if code, o, _ := f.gcall(t, "POST", "/v1/sessions/"+sid+"/resume", f.childTok, "{}", nil); code != 200 || o["status"] != "active" {
		t.Fatalf("resume: %d %v", code, o)
	}
	if code, o, _ := f.gcall(t, "POST", "/v1/sessions/"+sid+"/finish", f.childTok, "{}", nil); code != 200 || o["status"] != "completed" {
		t.Fatalf("finish: %d %v", code, o)
	}
	if code, _, _ := f.gcall(t, "POST", "/v1/children/"+f.childID+"/sessions", f.childTok, `{"world":"nature","kind":"chapter","skill_id":"ARI-SEN-A01a"}`, map[string]string{"Idempotency-Key": uuid.NewString()}); code != 422 {
		t.Fatalf("skill without generator must be 422, got %d", code)
	}
}

// TestPickQuestionNeverServesTwice runs the bank selection from many goroutines: every call gets a distinct question.
func TestPickQuestionNeverServesTwice(t *testing.T) {
	f := newFeedFixture(t, true)
	const n = 40
	ids := make([]string, n)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			err := f.s.Store.WithWriteTx(context.Background(), func(tx *sql.Tx) error {
				id, _, e := f.s.pickQuestion(context.Background(), tx, f.childID, "echecs", "ARI-OPE-A06a", 4, "")
				ids[i] = id
				return e
			})
			if err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	seen := map[string]bool{}
	for _, id := range ids {
		if id == "" || seen[id] {
			t.Fatalf("question %q served twice or empty", id)
		}
		seen[id] = true
	}
	// a second child is served from the bank: no new question rows are created while unserved ones exist
	var before, after int
	_ = f.s.Store.R.QueryRow(`SELECT COUNT(*) FROM questions`).Scan(&before)
	err := f.s.Store.WithWriteTx(context.Background(), func(tx *sql.Tx) error {
		_, _, e := f.s.pickQuestion(context.Background(), tx, "22222222-2222-4222-8222-222222222222", "echecs", "ARI-OPE-A06a", 4, "")
		return e
	})
	if err != nil {
		t.Fatal(err)
	}
	_ = f.s.Store.R.QueryRow(`SELECT COUNT(*) FROM questions`).Scan(&after)
	if after != before {
		t.Fatalf("bank must be reused: %d -> %d questions", before, after)
	}
}

// spyProvider records the payloads it receives and returns a fixed text.
type spyProvider struct {
	mu       sync.Mutex
	payloads []string
	text     string
}

func (p *spyProvider) Name() string { return "spy" }
func (p *spyProvider) Generate(_ context.Context, r narrator.Request) (string, error) {
	b, _ := json.Marshal(r.Payload)
	p.mu.Lock()
	p.payloads = append(p.payloads, string(b))
	p.mu.Unlock()
	return p.text, nil
}

// TestNarratorNeverSeesAnswer: the provider payloads hold no answer, and a leaking text becomes a fallback event.
func TestNarratorNeverSeesAnswer(t *testing.T) {
	f := newFeedFixture(t, true)
	spy := &spyProvider{}
	f.s.game().narr = narrator.New(spy, narrationCache{f.s}, nil)
	// Numbers already written in the statement are allowed by design (AnswerTokens skips them),
	// so draw fresh beats until the answer is hidden and can really leak.
	var sid, beatID, ans string
	var p engine.Problem
	for try := 0; try < 30 && ans == ""; try++ {
		sid = f.startChapter(t, "ARI-OPE-A06a")
		_, beat, _ := f.gcall(t, "POST", "/v1/sessions/"+sid+"/beats", f.childTok, "{}", nil)
		beatID = beat["id"].(string)
		p = f.problemOf(t, beatID)
		if toks := p.AnswerTokens(); len(toks) > 0 {
			ans = toks[0]
		}
	}
	if ans == "" {
		t.Fatal("no beat with a hidden answer in 30 tries")
	}

	spy.text = "Mira ouvre la porte avec le nombre " + ans + " !" // leaks the answer
	_, _, sse := f.gcall(t, "GET", "/v1/sessions/"+sid+"/beats/"+beatID+"/narration?phase=scene", f.childTok, "", nil)
	if !strings.Contains(sse, "event: fallback") || strings.Contains(sse, "nombre "+ans+" !") {
		t.Fatalf("leaking text must become a fallback event: %q", sse)
	}
	spy.text = "Mira ouvre la porte doucement."
	_, _, sse = f.gcall(t, "GET", "/v1/sessions/"+sid+"/beats/"+beatID+"/narration?phase=scene", f.childTok, "", nil)
	if !strings.Contains(sse, "event: scene") || !strings.Contains(sse, "event: done") {
		t.Fatalf("clean text must stream: %q", sse)
	}
	// second request for the same text is served from the narrations table
	var cached int
	_ = f.s.Store.R.QueryRow(`SELECT COUNT(*) FROM narrations`).Scan(&cached)
	if cached != 1 {
		t.Fatalf("narrations cached = %d", cached)
	}
	f.answer(t, sid, beatID, p.Reponse, uuid.NewString())
	f.gcall(t, "GET", "/v1/sessions/"+sid+"/beats/"+beatID+"/narration?phase=denouement", f.childTok, "", nil)
	for _, payload := range spy.payloads {
		if strings.Contains(payload, `"resultat"`) {
			t.Fatalf("payload carries the result key: %s", payload)
		}
		if strings.Contains(payload, "reponse") {
			t.Fatalf("payload carries the answer: %s", payload)
		}
	}
	if len(spy.payloads) < 2 {
		t.Fatal("provider was never called")
	}
}
