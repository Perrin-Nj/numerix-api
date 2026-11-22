package app

// T030 (API part): ownership and bypass checks for the new behaviour.

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"
)

func TestAnotherChildCannotAnswerOrHintMyBeat(t *testing.T) {
	f := newFeedFixture(t, true)
	f.sql(t, `INSERT INTO consents (id, parent_id, child_id, notice_version) VALUES ('k-2', 'p-1', '22222222-2222-4222-8222-222222222222', 'v1')`)
	sid, beatID, q := f.retryBeat(t, "cuisine.fournee.901", "cuisine", "ARI-OPE-A02c")
	wrong, _ := json.Marshal(map[string]any{"answer": json.RawMessage(q.CommonErrors[0].Value), "attempt": 1})
	hdr := map[string]string{"Idempotency-Key": uuid.NewString()}
	if code, _, _ := f.gcall(t, "POST", "/v1/sessions/"+sid+"/beats/"+beatID+"/answer", f.otherChildTok, string(wrong), hdr); code != 403 && code != 404 {
		t.Fatalf("a sibling answering my beat: %d", code)
	}
	if code, _, _ := f.answerAttempt(t, sid, beatID, json.RawMessage(q.CommonErrors[0].Value), uuid.NewString(), 1); code != 200 {
		t.Fatal("the owner answers")
	}
	for _, path := range []string{"/hint", "/answer"} {
		body := `{}`
		if path == "/answer" {
			b, _ := json.Marshal(map[string]any{"answer": json.RawMessage(q.Answer.Value), "attempt": 2})
			body = string(b)
		}
		if code, _, _ := f.gcall(t, "POST", "/v1/sessions/"+sid+"/beats/"+beatID+path, f.otherChildTok, body, map[string]string{"Idempotency-Key": uuid.NewString()}); code != 403 && code != 404 {
			t.Fatalf("a sibling using the second try window (%s): %d", path, code)
		}
	}
	// the retry still belongs to the owner and still works
	if code, _, raw := f.answerAttempt(t, sid, beatID, json.RawMessage(q.Answer.Value), uuid.NewString(), 2); code != 200 {
		t.Fatalf("the owner's second try: %d %s", code, raw)
	}
}

func TestSecondTryNeedsAnActiveOrCompletedOwnSession(t *testing.T) {
	f := newFeedFixture(t, true)
	sid, beatID, q := f.retryBeat(t, "cuisine.fournee.901", "cuisine", "ARI-OPE-A02c")
	f.answerAttempt(t, sid, beatID, json.RawMessage(q.CommonErrors[0].Value), uuid.NewString(), 1)
	f.sql(t, `UPDATE sessions SET status = 'paused' WHERE id = ?`, sid)
	if code, out, _ := f.answerAttempt(t, sid, beatID, json.RawMessage(q.Answer.Value), uuid.NewString(), 2); code != 409 || out["code"] != "session_paused" {
		t.Fatalf("paused session: %d %v", code, out)
	}
	f.sql(t, `UPDATE sessions SET status = 'abandoned' WHERE id = ?`, sid)
	if code, out, _ := f.answerAttempt(t, sid, beatID, json.RawMessage(q.Answer.Value), uuid.NewString(), 2); code != 409 || out["code"] != "session_completed" {
		t.Fatalf("abandoned session: %d %v", code, out)
	}
}

// A world turned to draft after a chapter started must not let a child start another one in it, with or without a skill.
func TestInactiveWorldBypassAttempts(t *testing.T) {
	f := newFeedFixture(t, true)
	f.sql(t, `UPDATE worlds SET status = 'draft' WHERE id = 'jeux'`)
	for _, body := range []string{
		`{"world":"jeux","kind":"chapter"}`,
		`{"world":"jeux","kind":"chapter","skill_id":"ARI-SEN-A08"}`,
		`{"world":"jeux","kind":"daily"}`,
	} {
		if code, out, _ := f.gcall(t, "POST", "/v1/children/"+f.childID+"/sessions", f.childTok, body, nil); code != 422 {
			t.Fatalf("%s: %d %v", body, code, out)
		}
	}
	// the parent cannot switch a draft world on through the settings either
	b := `{"daily_goal_chapters":1,"enabled_worlds":["jeux"],"read_aloud":true,"sound":true}`
	if code, out, _ := f.gcall(t, "PUT", "/v1/children/"+f.childID+"/settings", f.parentTok, b, nil); code != 422 || out["code"] != "world_inactive" {
		t.Fatalf("settings: %d %v", code, out)
	}
	// a child token may not write settings at all (worlds stay as the parent set them)
	if code, _, _ := f.gcall(t, "PUT", "/v1/children/"+f.childID+"/settings", f.childTok, `{"daily_goal_chapters":1,"enabled_worlds":["cuisine"],"read_aloud":true,"sound":true}`, nil); code == 200 {
		got := f.s.enabledWorlds(t.Context(), f.childID)
		for _, w := range got {
			if w == "jeux" {
				t.Fatal("child edit enabled a draft world")
			}
		}
	}
}
