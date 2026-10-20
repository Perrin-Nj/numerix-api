package app

// Regression tests for QA-BE-001 (size caps), QA-BE-002 (auth before validation), QA-BE-003 (idempotent replay vs rate limit).
//
// Go for Spring people: t.Run makes a named sub-test (JUnit @ParameterizedTest row);
// httptest.NewRecorder is MockMvc: it records the response without opening a socket.

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/Perrin-Nj/numerix-api/internal/engine"
	"github.com/Perrin-Nj/numerix-api/internal/httpapi"
)

var qaPathParam = regexp.MustCompile(`\{[^}]+\}`) // {childId} -> placeholder

// qaRaw sends a raw body (not JSON-encoded by the helper) and returns the recorder.
func qaRaw(h http.Handler, method, path, bearer, body string, hdr map[string]string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// QA-BE-002: with no token, a protected operation answers 401 even when the body or a parameter is invalid.
func TestQAAuthBeforeValidationEveryOperation(t *testing.T) {
	h, _ := newTestServer(t)
	for id, m := range httpapi.Ops {
		t.Run(id, func(t *testing.T) {
			path := "/v1" + qaPathParam.ReplaceAllString(m.Path, "x") // invalid uuid on purpose: parameter errors must not beat auth
			rec := qaRaw(h, m.Method, path, "", `{"bad`, nil)         // broken JSON, no token, no Idempotency-Key
			if m.Public {
				if rec.Code >= 500 && rec.Code != http.StatusNotImplemented { // public ops may answer 401 themselves (bad device cookie)
					t.Fatalf("public op: status %d", rec.Code)
				}
				return
			}
			if rec.Code != http.StatusUnauthorized {
				t.Fatalf("status %d, want 401; body %s", rec.Code, rec.Body.String())
			}
		})
	}
}

// QA-BE-002: a token with the wrong role gets 403 before validation too.
func TestQAWrongRoleBeforeValidation(t *testing.T) {
	f := newFeedFixture(t, true)
	rec := qaRaw(f.h, "POST", "/v1/children", f.childTok, `{"bad`, nil) // CreateChild is parent-only
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status %d, want 403", rec.Code)
	}
}

// QA-BE-001: oversized bodies and fields are refused, argon2 never sees them.
func TestQASizeCaps(t *testing.T) {
	h, _ := newTestServer(t)
	ok := func(email, pw string) string {
		return `{"email":"` + email + `","password":"` + pw + `","locale":"fr-CA"}`
	}
	t.Run("2MB body is 413", func(t *testing.T) {
		rec := qaRaw(h, "POST", "/v1/auth/register", "", ok("big@b.co", strings.Repeat("x", 2_000_000)), nil)
		if rec.Code != http.StatusRequestEntityTooLarge {
			t.Fatalf("status %d, want 413", rec.Code)
		}
	})
	t.Run("129 char password is 422", func(t *testing.T) {
		rec := qaRaw(h, "POST", "/v1/auth/register", "", ok("p129@b.co", strings.Repeat("x", 129)), nil)
		if rec.Code != http.StatusUnprocessableEntity {
			t.Fatalf("status %d, want 422", rec.Code)
		}
	})
	t.Run("128 char password is accepted", func(t *testing.T) {
		rec := qaRaw(h, "POST", "/v1/auth/register", "", ok("max128@b.co", strings.Repeat("x", 128)), nil)
		if rec.Code != http.StatusCreated {
			t.Fatalf("status %d, want 201", rec.Code)
		}
	})
	t.Run("login long password is 422", func(t *testing.T) {
		rec := qaRaw(h, "POST", "/v1/auth/login", "", `{"email":"a@b.co","password":"`+strings.Repeat("x", 5000)+`"}`, nil)
		if rec.Code != http.StatusUnprocessableEntity {
			t.Fatalf("status %d, want 422", rec.Code)
		}
	})
	t.Run("long email is 422", func(t *testing.T) {
		rec := qaRaw(h, "POST", "/v1/auth/login", "", `{"email":"`+strings.Repeat("a", 300)+`@b.co","password":"whatever-long-enough"}`, nil)
		if rec.Code != http.StatusUnprocessableEntity {
			t.Fatalf("status %d, want 422", rec.Code)
		}
	})
	t.Run("delete account long password is 422", func(t *testing.T) {
		f := newFeedFixture(t, true)
		rec := qaRaw(f.h, "DELETE", "/v1/account", f.parentTok, `{"password":"`+strings.Repeat("x", 5000)+`"}`, nil)
		if rec.Code != http.StatusUnprocessableEntity {
			t.Fatalf("status %d, want 422", rec.Code)
		}
	})
	t.Run("unauthenticated 2MB body is 401 not 413", func(t *testing.T) {
		rec := qaRaw(h, "POST", "/v1/children", "", strings.Repeat("x", 2_000_000), nil)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("status %d, want 401", rec.Code)
		}
	})
}

// QA-BE-003: a replay with the same Idempotency-Key returns the stored result, not 429.
func TestQAIdempotentReplayBypassesRateLimit(t *testing.T) {
	f := newFeedFixture(t, true)
	start := func(key string) (int, map[string]any) {
		code, out, _ := f.gcall(t, "POST", "/v1/children/"+f.childID+"/sessions", f.childTok, `{"world":"nature","kind":"chapter"}`, map[string]string{"Idempotency-Key": key})
		return code, out
	}
	key := uuid.NewString()
	code, first := start(key)
	if code != 201 {
		t.Fatalf("first start: %d", code)
	}
	saw429 := false
	for i := 0; i < 12; i++ { // StartSession allows 10 per minute per child
		if c, _ := start(uuid.NewString()); c == 429 {
			saw429 = true
		}
	}
	if !saw429 {
		t.Fatal("limiter never tripped, test is not testing anything")
	}
	code, again := start(key)
	if code != 201 || again["id"] != first["id"] {
		t.Fatalf("replay: %d id %v, want 201 id %v", code, again["id"], first["id"])
	}
	if c, _ := start(uuid.NewString()); c != 429 { // a fresh key is still limited
		t.Fatalf("fresh key after the limit: %d, want 429", c)
	}
}

// QA-BE-003 for answers: the replay of an answered beat survives an exhausted per-session limit.
func TestQAAnswerReplayBypassesRateLimit(t *testing.T) {
	f := newFeedFixture(t, true)
	sid := f.startChapter(t, "")
	code, beat, raw := f.gcall(t, "POST", "/v1/sessions/"+sid+"/beats", f.childTok, "{}", nil)
	if code != 201 {
		t.Fatalf("next beat: %d %s", code, raw)
	}
	beatID := beat["id"].(string)
	key := uuid.NewString()
	c1, first := f.answer(t, sid, beatID, 1, key)
	if c1 != 200 {
		t.Fatalf("first answer: %d %v", c1, first)
	}
	saw429 := false
	for i := 0; i < 35; i++ { // AnswerBeat allows 30 per minute per session
		if c, _ := f.answer(t, sid, beatID, 1, uuid.NewString()); c == 429 {
			saw429 = true
		}
	}
	if !saw429 {
		t.Fatal("limiter never tripped")
	}
	c2, again := f.answer(t, sid, beatID, 1, key)
	if c2 != 200 || again["outcome"] != first["outcome"] {
		t.Fatalf("replay: %d %v, want 200 %v", c2, again, first)
	}
}

// unlock marks the direct prerequisites of a skill as mastered for the fixture child (test setup only).
func (f feedFixture) unlock(t *testing.T, skill string) {
	t.Helper()
	ctx := context.Background()
	sk, err := f.s.game().graph.Get(skill) // the skill record, including its prerequisite ids
	if err != nil {
		t.Fatal(err)
	}
	prof, err := f.s.loadProfile(ctx, f.childID)
	if err != nil {
		t.Fatal(err)
	}
	now := f.s.game().now()
	for _, p := range sk.Prerequisites {
		// P=1 and a huge half-life: mastery stays above the threshold; Peak keeps it out of "needs review"
		prof.States[p] = &engine.SkillState{P: 1, HalfLife: 1e6, Last: now.UTC().Format(time.RFC3339Nano), Peak: 1}
	}
	if err := f.s.Store.WithWriteTx(ctx, func(tx *sql.Tx) error { return saveProfile(ctx, tx, prof, now) }); err != nil {
		t.Fatal(err)
	}
}

// QA-BE-004: an explicit skill_id locked by prerequisites is refused with 422 (like the planner, which never picks it),
// and the same skill starts once its prerequisites are mastered.
func TestQAExplicitLockedSkillIsRefused(t *testing.T) {
	f := newFeedFixture(t, true)
	_, j, _ := f.gcall(t, "GET", "/v1/children/"+f.childID+"/journey?world=nature", f.childTok, "", nil)
	var locked []string
	for _, n := range j["nodes"].([]any) {
		if m := n.(map[string]any); m["state"] == "locked" && m["locked_reason"] == "prerequisites" {
			locked = append(locked, m["skill_id"].(string))
		}
	}
	if len(locked) == 0 {
		t.Fatal("no prerequisite-locked node in nature, test is not testing anything")
	}
	start := func(skill string) (int, string) {
		code, _, raw := f.gcall(t, "POST", "/v1/children/"+f.childID+"/sessions", f.childTok,
			`{"world":"nature","kind":"chapter","skill_id":"`+skill+`"}`, map[string]string{"Idempotency-Key": uuid.NewString()})
		return code, raw
	}
	for _, sk := range locked {
		if code, raw := start(sk); code != 422 || !strings.Contains(raw, "locked") {
			t.Fatalf("%s: status %d body %s, want 422 locked", sk, code, raw)
		}
	}
	f.unlock(t, locked[0])
	if code, raw := start(locked[0]); code != 201 {
		t.Fatalf("after unlock: %d %s", code, raw)
	}
}

// QA-BE-005: a full register + login ceremony (software authenticator) from a dev web app on a 13xxx port works with
// the default dev config; before the fix only http://localhost:3000 was accepted and registration failed.
func TestQAPasskeyCeremonyOnDevPort(t *testing.T) {
	h, _ := newTestServer(t)
	tok, _ := dvRegister(t, h, "qa5@example.com")
	const origin = "http://localhost:13150"
	k := &fakeKey{}
	session, ch, uid := optionsOf(t, dvCall(t, h, "POST", "/v1/auth/passkeys/register/begin", tok, "", nil))
	rec := dvCall(t, h, "POST", "/v1/auth/passkeys/register/finish", tok, "", map[string]any{
		"session_id": session, "nickname": "Virtual", "credential": k.register(ch, origin, uid)})
	if rec.Code != 201 {
		t.Fatalf("register on dev port: %d %s", rec.Code, rec.Body)
	}
	session, ch, _ = optionsOf(t, dvCall(t, h, "POST", "/v1/auth/passkeys/login/begin", "", "", nil))
	rec = dvCall(t, h, "POST", "/v1/auth/passkeys/login/finish", "", "", map[string]any{"session_id": session, "credential": k.assert(ch, origin, 1)})
	if rec.Code != 200 {
		t.Fatalf("login on dev port: %d %s", rec.Code, rec.Body)
	}
}

// R3-B1: the family comes from the skill id; a typical error adds the misconception id, a right answer never does.
func TestR3SceneKey(t *testing.T) {
	cases := []struct{ skill, outcome, code, want string }{
		{"ARI-SEN-B02", "correct", "", "fractions"},
		{"ARI-SEN-A01a", "correct", "", "number"},
		{"ARI-OPE-A03a", "correct", "", "add_sub"},
		{"ARI-OPE-A06a", "correct", "", "mult_div"},
		{"ARI-OPE-A06a", "erreur_typique", "MIS-X", "mult_div:MIS-X"},
		{"ARI-OPE-A06a", "erreur_aleatoire", "", "mult_div"},
		{"GEO-A01", "correct", "", "geometry"},
		{"MES-A03", "correct", "", "measure"},
		{"STA-02", "correct", "", "data"},
	}
	for _, c := range cases {
		if got := sceneKeyFor(c.skill, c.outcome, c.code); got != c.want {
			t.Errorf("%s/%s: got %q, want %q", c.skill, c.outcome, got, c.want)
		}
	}
}

// R3-B1 and B2 over HTTP: the beat and the answer carry scene_key; variant_history is stored, capped, returned on reload.
func TestR3BeatSceneKeyAndVariantHistory(t *testing.T) {
	f := newFeedFixture(t, true)
	sid := f.startChapter(t, "ARI-OPE-A06a")
	code, beat, raw := f.gcall(t, "POST", "/v1/sessions/"+sid+"/beats", f.childTok, "{}", nil)
	if code != 201 || beat["scene_key"] != "mult_div" {
		t.Fatalf("beat: %d scene_key %v (%s)", code, beat["scene_key"], raw)
	}
	beatID := beat["id"].(string)
	// 31 ids is refused before anything is written
	long := `["` + strings.Repeat(`a","`, 30) + `a"]`
	if c, _, _ := f.gcall(t, "POST", "/v1/sessions/"+sid+"/beats/"+beatID+"/answer", f.childTok, `{"answer":1,"variant_history":`+long+`}`, nil); c != 422 {
		t.Fatalf("31 ids: %d, want 422", c)
	}
	c, out, raw := f.gcall(t, "POST", "/v1/sessions/"+sid+"/beats/"+beatID+"/answer", f.childTok, `{"answer":999999,"variant_history":["w1","r2"]}`, nil)
	if c != 200 || out["scene_key"] != "mult_div" {
		t.Fatalf("answer: %d scene_key %v (%s)", c, out["scene_key"], raw)
	}
	// reload: the session read back still has the history
	c, sess, _ := f.gcall(t, "GET", "/v1/sessions/"+sid, f.childTok, "", nil)
	got, _ := sess["variant_history"].([]any)
	if c != 200 || len(got) != 2 || got[0] != "w1" || got[1] != "r2" {
		t.Fatalf("reload: %d variant_history %v", c, sess["variant_history"])
	}
}

// R3-B3: parent locks music; a child can change its own volume and voice but not the lock or the computer voice.
func TestR3MusicVoiceSettings(t *testing.T) {
	f := newFeedFixture(t, true)
	path := "/v1/children/" + f.childID + "/settings"
	_, st, _ := f.gcall(t, "GET", path, f.childTok, "", nil)
	if m := st["music"].(map[string]any); m["enabled"] != true || m["volume"] != float64(30) || m["parent_locked"] != false {
		t.Fatalf("default music %v", m)
	}
	if v := st["voice"].(map[string]any); v["human_enabled"] != true || v["computer_voice"] != false {
		t.Fatalf("default voice %v", v)
	}
	// parent: lock music off and opt in to the computer voice
	st["music"] = map[string]any{"enabled": true, "volume": 50, "parent_locked": true}
	st["voice"] = map[string]any{"human_enabled": true, "computer_voice": true}
	b, _ := json.Marshal(st)
	if c, out, raw := f.gcall(t, "PUT", path, f.parentTok, string(b), nil); c != 200 || out["music"].(map[string]any)["enabled"] != false {
		t.Fatalf("parent put: %d %s", c, raw)
	}
	// child: tries to unlock, turn music on and drop the daily goal
	child := `{"daily_goal_chapters":5,"enabled_worlds":[],"read_aloud":false,"sound":false,"music":{"enabled":true,"volume":80,"parent_locked":false},"voice":{"human_enabled":false,"computer_voice":false}}`
	c, out, raw := f.gcall(t, "PUT", path, f.childTok, child, nil)
	if c != 200 {
		t.Fatalf("child put: %d %s", c, raw)
	}
	m, v := out["music"].(map[string]any), out["voice"].(map[string]any)
	if m["parent_locked"] != true || m["enabled"] != false || m["volume"] != float64(80) {
		t.Fatalf("lock must hold, volume may change: %v", m)
	}
	if v["human_enabled"] != false || v["computer_voice"] != true {
		t.Fatalf("child may switch human voice only: %v", v)
	}
	if out["daily_goal_chapters"] != float64(1) {
		t.Fatalf("child changed the daily goal: %v", out["daily_goal_chapters"])
	}
	if c, _, _ := f.gcall(t, "PUT", path, f.parentTok, strings.Replace(string(b), `"volume":50`, `"volume":101`, 1), nil); c != 422 {
		t.Fatalf("volume 101: %d, want 422", c)
	}
}
