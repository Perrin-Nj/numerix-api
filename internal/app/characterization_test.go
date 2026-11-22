package app

// T004: characterization tests. They document what the API does today (origin/main) for the things the
// three-worlds release changes, so every later change to them is a visible, deliberate diff in this file.

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"
)

// answerOnce starts a bank chapter and returns the session, the open beat and a wrong answer for it.
func charBeat(t *testing.T) (f feedFixture, sid, beatID string, wrong, right json.RawMessage) {
	t.Helper()
	f = newFeedFixture(t, true)
	seedBank(t, f, onlyClones("cuisine.fournee.901", 9))
	sid = f.startIn(t, "cuisine", "ARI-OPE-A02c")
	beat, _ := f.nextBeat(t, sid)
	beatID = beat["id"].(string)
	q := f.curated(t, beatID)
	return f, sid, beatID, json.RawMessage(q.CommonErrors[0].Value), json.RawMessage(q.CorrectAnswer("fr"))
}

func TestCharAnswerKeysToday(t *testing.T) {
	f, sid, beatID, wrong, _ := charBeat(t)
	key := uuid.NewString()
	if code, res := f.answer(t, sid, beatID, wrong, key); code != 200 || res["correct_answer"] == nil {
		t.Fatalf("first answer reveals everything today: %d %v", code, res)
	}
	if code, res := f.answer(t, sid, beatID, wrong, key); code != 200 || res["correct_answer"] == nil {
		t.Fatalf("same key replays: %d %v", code, res)
	}
	code, res := f.answer(t, sid, beatID, wrong, uuid.NewString())
	if code != 409 || res["code"] != "beat_answered" {
		t.Fatalf("another key is 409 beat_answered: %d %v", code, res)
	}
	if code, res, _ := f.gcall(t, "POST", "/v1/sessions/"+sid+"/beats/"+beatID+"/hint", f.childTok, "{}", nil); code != 409 || res["code"] != "beat_answered" {
		t.Fatalf("hint after the answer is 409: %d %v", code, res)
	}
}

func TestCharWorldsAndSettingsToday(t *testing.T) {
	f := newFeedFixture(t, true)
	f.sql(t, `UPDATE worlds SET status = 'draft' WHERE id = 'jeux'`)
	code, _, raw := f.gcall(t, "GET", "/v1/worlds", f.childTok, "", nil)
	if code != 200 || strings.Contains(raw, `"jeux"`) { // origin/main listed it; T018 hides draft worlds (see worlds_test.go)
		t.Fatalf("a draft world must not be listed: %d", code)
	}
	body := `{"daily_goal_chapters":1,"enabled_worlds":["jeux","nature"],"read_aloud":true,"sound":true}`
	if code, out, _ := f.gcall(t, "PUT", "/v1/children/"+f.childID+"/settings", f.parentTok, body, nil); code != 422 || out["code"] != "world_inactive" {
		t.Fatalf("origin/main accepted any world; now a draft world is refused: %d %v", code, out) // T018
	}
}
