package app

// Retry QA fixes: the denouement of a withheld answer is refused while the second try is open (H1), a reload resumes
// the open second try and the last question holds the chapter open until the window closes (H2).

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/Perrin-Nj/numerix-api/internal/appconfig"
	"github.com/Perrin-Nj/numerix-api/internal/content"
)

func (f feedFixture) session2(t *testing.T, sid string) map[string]any {
	t.Helper()
	code, out, raw := f.gcall(t, "GET", "/v1/sessions/"+sid, f.childTok, "", nil)
	if code != 200 {
		t.Fatalf("get session: %d %s", code, raw)
	}
	return out
}

func TestDenouementStreamRefusedDuringTheRetryWindow(t *testing.T) {
	f := newFeedFixture(t, true)
	sid, beatID, q := f.retryBeat(t, "cuisine.fournee.901", "cuisine", "ARI-OPE-A02c")
	f.answerAttempt(t, sid, beatID, json.RawMessage(q.CommonErrors[0].Value), uuid.NewString(), 1)
	path := "/v1/sessions/" + sid + "/beats/" + beatID + "/narration?phase=denouement"
	if code, out, raw := f.gcall(t, "GET", path, f.childTok, "", nil); code != 409 || out["code"] != "beat_not_answered" || strings.Contains(raw, "event: denouement") {
		t.Fatalf("denouement while the second try is open: %d %s", code, raw)
	}
	if code, _, _ := f.gcall(t, "GET", "/v1/sessions/"+sid+"/beats/"+beatID+"/narration?phase=scene", f.childTok, "", nil); code != 200 {
		t.Fatalf("the scene stream stays available: %d", code)
	}
	f.answerAttempt(t, sid, beatID, json.RawMessage(q.Answer.Value), uuid.NewString(), 2)
	if code, _, raw := f.gcall(t, "GET", path, f.childTok, "", nil); code != 200 || !strings.Contains(raw, "event: denouement") {
		t.Fatalf("denouement after the second try: %d %s", code, raw)
	}
}

func TestReloadMidRetryResumesTheSameBeat(t *testing.T) {
	f := newFeedFixture(t, true)
	sid, beatID, q := f.retryBeat(t, "cuisine.fournee.901", "cuisine", "ARI-OPE-A02c")
	f.answerAttempt(t, sid, beatID, json.RawMessage(q.CommonErrors[0].Value), uuid.NewString(), 1)
	cb, _ := f.session2(t, sid)["current_beat"].(map[string]any)
	if cb == nil || cb["id"] != beatID || cb["retry_available"] != true {
		t.Fatalf("a reload must resume the open second try: %v", cb)
	}
	for _, banned := range []string{"correct_answer", "explain"} {
		if _, has := cb[banned]; has {
			t.Fatalf("the resumed beat leaks %s", banned)
		}
	}
	f.answerAttempt(t, sid, beatID, json.RawMessage(q.Answer.Value), uuid.NewString(), 2)
	if cb, _ := f.session2(t, sid)["current_beat"].(map[string]any); cb != nil {
		t.Fatalf("after the second try there is no open beat: %v", cb)
	}
	// a client that moves on instead of retrying closes the window: the next beat is a new one and attempt 2 is over
	sid2, b2, q2 := f.retryBeat2(t, sid)
	f.answerAttempt(t, sid2, b2, json.RawMessage(q2.CommonErrors[0].Value), uuid.NewString(), 1)
	next, _ := f.nextBeat(t, sid2)
	if next["id"] == b2 {
		t.Fatal("asking for the next beat must move on")
	}
	if code, out, _ := f.answerAttempt(t, sid2, b2, json.RawMessage(q2.Answer.Value), uuid.NewString(), 2); code != 409 {
		t.Fatalf("a skipped second try is closed: %d %v", code, out)
	}
}

// retryBeat2 opens the next beat of the same session.
func (f feedFixture) retryBeat2(t *testing.T, sid string) (string, string, content.Question) {
	t.Helper()
	beat, _ := f.nextBeat(t, sid)
	id := beat["id"].(string)
	return sid, id, f.curated(t, id)
}

// playToLast answers four questions right and returns the open last beat.
func (f feedFixture) playToLast(t *testing.T, sid string) (string, content.Question) {
	t.Helper()
	var last string
	var lq content.Question
	for i := 0; i < 5; i++ {
		beat, _ := f.nextBeat(t, sid)
		last = beat["id"].(string)
		lq = f.curated(t, last)
		if i < 4 {
			if code, _, raw := f.answerAttempt(t, sid, last, json.RawMessage(lq.Answer.Value), uuid.NewString(), 1); code != 200 {
				t.Fatal(raw)
			}
		}
	}
	return last, lq
}

func cardsOf(t *testing.T, f feedFixture) int {
	t.Helper()
	n := 0
	_ = f.s.Store.R.QueryRow(`SELECT COUNT(*) FROM collection WHERE child_id = ?`, f.childID).Scan(&n)
	return n
}

// On the last question the chapter stays open while the second try is open; the card is decided once, when the
// window closes by the second answer.
func TestLastQuestionRetryHoldsTheChapterThenAwardsTheCardOnce(t *testing.T) {
	f := newFeedFixture(t, true)
	f.setPlatform(t, func(p *appconfig.Platform) { p.CardThresholdPct = 90 })
	seedBank(t, f, onlyClones("cuisine.fournee.901", 9))
	sid := f.startIn(t, "cuisine", "ARI-OPE-A02c")
	last, lq := f.playToLast(t, sid)
	code, r, raw := f.answerAttempt(t, sid, last, json.RawMessage(lq.CommonErrors[0].Value), uuid.NewString(), 1)
	if code != 200 || r["retry_available"] != true {
		t.Fatalf("withheld last answer: %s", raw)
	}
	sess := r["session"].(map[string]any)
	if sess["status"] != "active" || cardsOf(t, f) != 0 {
		t.Fatalf("the chapter must stay open while the second try is open: status=%v cards=%d", sess["status"], cardsOf(t, f))
	}
	cur := f.session2(t, sid)
	if cb, _ := cur["current_beat"].(map[string]any); cb == nil || cb["id"] != last || cb["retry_available"] != true || cur["status"] != "active" {
		t.Fatalf("reload on the last question resumes the second try: %v", cur)
	}
	_, r2, raw2 := f.answerAttempt(t, sid, last, json.RawMessage(lq.Answer.Value), uuid.NewString(), 2)
	types := []string{}
	for _, x := range r2["rewards"].([]any) {
		types = append(types, x.(map[string]any)["type"].(string))
	}
	if r2["session"].(map[string]any)["status"] != "completed" || !strings.Contains(strings.Join(types, ","), "chapter_complete") || !strings.Contains(strings.Join(types, ","), "card_earned") {
		t.Fatalf("second try closes the chapter with its rewards: %s", raw2)
	}
	if cardsOf(t, f) != 1 {
		t.Fatalf("one card, got %d", cardsOf(t, f))
	}
	// nothing is awarded twice by asking again
	f.gcall(t, "POST", "/v1/sessions/"+sid+"/beats", f.childTok, "{}", nil)
	if cardsOf(t, f) != 1 {
		t.Fatalf("a later call must not add a card: %d", cardsOf(t, f))
	}
}

// A child who does not retry the last question: asking for the next beat closes the window and completes the chapter
// with the first-try score; the card is awarded once when the threshold is met.
func TestLastQuestionSkippedRetryCompletesOnNextBeat(t *testing.T) {
	f := newFeedFixture(t, true)
	f.setPlatform(t, func(p *appconfig.Platform) { p.CardThresholdPct = 70 })
	seedBank(t, f, onlyClones("cuisine.fournee.901", 9))
	sid := f.startIn(t, "cuisine", "ARI-OPE-A02c")
	last, lq := f.playToLast(t, sid)
	f.answerAttempt(t, sid, last, json.RawMessage(lq.CommonErrors[0].Value), uuid.NewString(), 1) // 4 of 5 right: 80 percent
	code, out, _ := f.gcall(t, "POST", "/v1/sessions/"+sid+"/beats", f.childTok, "{}", nil)
	if code != 409 || out["code"] != "session_completed" {
		t.Fatalf("the next beat after the last question: %d %v", code, out)
	}
	cur := f.session2(t, sid)
	if cur["status"] != "completed" || cur["current_beat"] != nil || cardsOf(t, f) != 1 {
		t.Fatalf("completed with one card: status=%v cards=%d", cur["status"], cardsOf(t, f))
	}
	if code, _, _ := f.answerAttempt(t, sid, last, json.RawMessage(lq.Answer.Value), uuid.NewString(), 2); code != 409 {
		t.Fatalf("the window is closed: %d", code)
	}
	if cardsOf(t, f) != 1 {
		t.Fatal("still one card")
	}
}

// With retry switched off in the config after a withheld last answer, the chapter can still be closed.
func TestRetryFlagOffMidWindowStillClosesTheChapter(t *testing.T) {
	f := newFeedFixture(t, true)
	seedBank(t, f, onlyClones("cuisine.fournee.901", 9))
	sid := f.startIn(t, "cuisine", "ARI-OPE-A02c")
	last, lq := f.playToLast(t, sid)
	f.answerAttempt(t, sid, last, json.RawMessage(lq.CommonErrors[0].Value), uuid.NewString(), 1)
	f.setPlatform(t, func(p *appconfig.Platform) { p.RetryEnabled = false })
	if code, _, _ := f.answerAttempt(t, sid, last, json.RawMessage(lq.Answer.Value), uuid.NewString(), 2); code != 409 {
		t.Fatalf("attempt 2 needs the flag: %d", code)
	}
	if code, out, _ := f.gcall(t, "POST", "/v1/sessions/"+sid+"/beats", f.childTok, "{}", nil); code != 409 || out["code"] != "session_completed" {
		t.Fatalf("next beat closes it: %d %v", code, out)
	}
	if f.session2(t, sid)["status"] != "completed" {
		t.Fatal("chapter must be completed")
	}
}
