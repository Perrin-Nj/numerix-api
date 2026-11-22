package app

// T017: the second try. First answer counts for mastery, the second is stored apart and counts half for the card.

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"

	"github.com/Perrin-Nj/numerix-api/internal/appconfig"
	"github.com/Perrin-Nj/numerix-api/internal/content"
	"github.com/Perrin-Nj/numerix-api/internal/seedload"
)

func (f feedFixture) answerAttempt(t *testing.T, sid, beatID string, v any, key string, attempt int) (int, map[string]any, string) {
	t.Helper()
	body := map[string]any{"answer": v}
	if attempt > 0 {
		body["attempt"] = attempt
	}
	b, _ := json.Marshal(body)
	return f.gcall(t, "POST", "/v1/sessions/"+sid+"/beats/"+beatID+"/answer", f.childTok, string(b), map[string]string{"Idempotency-Key": key})
}

func (f feedFixture) retryBeat(t *testing.T, quest, world, skill string) (sid, beatID string, q content.Question) {
	t.Helper()
	seedBank(t, f, onlyClones(quest, 9))
	sid = f.startIn(t, world, skill)
	beat, _ := f.nextBeat(t, sid)
	beatID = beat["id"].(string)
	return sid, beatID, f.curated(t, beatID)
}

var revealKeys = []string{"correct_answer", "explain", "feedback", "method_visual", "misconception", "scene_key"}

func TestRetryWithheldThenRevealed(t *testing.T) {
	f := newFeedFixture(t, true)
	sid, beatID, q := f.retryBeat(t, "cuisine.fournee.901", "cuisine", "ARI-OPE-A02c")
	wrong := json.RawMessage(q.CommonErrors[0].Value)
	key := uuid.NewString()
	code, res, rawRes := f.answerAttempt(t, sid, beatID, wrong, key, 1)
	if code != 200 || res["outcome"] != "erreur_typique" || res["retry_available"] != true || res["attempt"] != 1.0 {
		t.Fatalf("withheld first answer: %d %s", code, rawRes)
	}
	for _, k := range revealKeys {
		if _, has := res[k]; has {
			t.Fatalf("%s must be withheld: %s", k, rawRes)
		}
	}
	if rw, ok := res["rewards"].([]any); !ok || len(rw) != 0 {
		t.Fatalf("rewards must be empty: %v", res["rewards"])
	}
	// same key replays the withheld form, another key is 409
	if _, again, _ := f.answerAttempt(t, sid, beatID, wrong, key, 1); again["retry_available"] != true || again["correct_answer"] != nil {
		t.Fatalf("replay must stay withheld: %v", again)
	}
	if code, e, _ := f.answerAttempt(t, sid, beatID, wrong, uuid.NewString(), 1); code != 409 || e["code"] != "beat_answered" {
		t.Fatalf("another key: %d %v", code, e)
	}
	// the hint of the window: kind retry_hint, never the answer
	code, h, rawH := f.gcall(t, "POST", "/v1/sessions/"+sid+"/beats/"+beatID+"/hint", f.childTok, "{}", nil)
	if code != 200 || h["kind"] != "retry_hint" || h["hint"] == "" {
		t.Fatalf("retry hint: %d %s", code, rawH)
	}
	if q.Leaks(h["hint"].(string), "fr") {
		t.Fatalf("the retry hint gives the answer away: %v", h["hint"])
	}
	// second try, right
	key2 := uuid.NewString()
	code, r2, raw2 := f.answerAttempt(t, sid, beatID, json.RawMessage(q.Answer.Value), key2, 2)
	if code != 200 || r2["outcome"] != "correct" || r2["attempt"] != 2.0 || r2["correct_answer"] == nil || r2["explain"] == nil {
		t.Fatalf("second try: %d %s", code, raw2)
	}
	if _, again, _ := f.answerAttempt(t, sid, beatID, json.RawMessage(q.Answer.Value), key2, 2); again["outcome"] != "correct" || again["explain"] != r2["explain"] {
		t.Fatalf("second try replay: %v", again)
	}
	if code, e, _ := f.answerAttempt(t, sid, beatID, wrong, uuid.NewString(), 2); code != 409 || e["code"] != "beat_answered" {
		t.Fatalf("third try: %d %v", code, e)
	}

}

func TestRetryMasteryAndChaptersUseTheFirstAttemptOnly(t *testing.T) {
	f := newFeedFixture(t, true)
	sid, beatID, q := f.retryBeat(t, "cuisine.fournee.901", "cuisine", "ARI-OPE-A02c")
	if code, _, raw := f.answerAttempt(t, sid, beatID, json.RawMessage(q.CommonErrors[0].Value), uuid.NewString(), 1); code != 200 {
		t.Fatal(raw)
	}
	profile := func() string {
		var p string
		_ = f.s.Store.R.QueryRow(`SELECT bkt FROM child_profiles WHERE child_id = ?`, f.childID).Scan(&p)
		return p
	}
	before := profile()
	if before == "" {
		t.Fatal("the first answer must have saved the profile")
	}
	if code, _, raw := f.answerAttempt(t, sid, beatID, json.RawMessage(q.Answer.Value), uuid.NewString(), 2); code != 200 {
		t.Fatal(raw)
	}
	if after := profile(); after != before {
		t.Fatalf("the second try changed mastery:\n%s\n%s", before, after)
	}
	var answers, retries int
	_ = f.s.Store.R.QueryRow(`SELECT (SELECT COUNT(*) FROM answers WHERE session_id = ?), (SELECT COUNT(*) FROM answer_retries WHERE session_id = ?)`, sid, sid).Scan(&answers, &retries)
	if answers != 1 || retries != 1 {
		t.Fatalf("answers=%d retries=%d, want 1 and 1", answers, retries)
	}
}

func TestRetryNotAllowedCases(t *testing.T) {
	f := newFeedFixture(t, true)
	sid, beatID, q := f.retryBeat(t, "cuisine.fournee.901", "cuisine", "ARI-OPE-A02c")
	// attempt 2 with no first answer
	if code, e, _ := f.answerAttempt(t, sid, beatID, json.RawMessage(q.Answer.Value), uuid.NewString(), 2); code != 409 || e["code"] != "retry_not_allowed" {
		t.Fatalf("attempt 2 before the first: %d %v", code, e)
	}
	// a right first answer leaves nothing to retry
	if code, r, _ := f.answerAttempt(t, sid, beatID, json.RawMessage(q.Answer.Value), uuid.NewString(), 1); code != 200 || r["retry_available"] != nil || r["correct_answer"] == nil {
		t.Fatalf("right first answer reveals at once: %d %v", code, r)
	}
	if code, e, _ := f.answerAttempt(t, sid, beatID, json.RawMessage(q.Answer.Value), uuid.NewString(), 2); code != 409 || e["code"] != "retry_not_allowed" {
		t.Fatalf("attempt 2 after a right answer: %d %v", code, e)
	}
	if code, _, _ := f.answerAttempt(t, sid, beatID, 1, uuid.NewString(), 3); code != 422 {
		t.Fatalf("attempt 3: %d", code)
	}
	// a legacy client wrong answer reveals everything and offers no retry
	beat, _ := f.nextBeat(t, sid)
	b2 := beat["id"].(string)
	q2 := f.curated(t, b2)
	if code, r, _ := f.answerAttempt(t, sid, b2, json.RawMessage(q2.CommonErrors[0].Value), uuid.NewString(), 0); code != 200 || r["correct_answer"] == nil || r["retry_available"] != nil || r["attempt"] != nil {
		t.Fatalf("legacy wrong answer: %d %v", code, r)
	}
	if code, e, _ := f.answerAttempt(t, sid, b2, json.RawMessage(q2.Answer.Value), uuid.NewString(), 2); code != 409 || e["code"] != "retry_not_allowed" {
		t.Fatalf("attempt 2 after a legacy answer: %d %v", code, e)
	}
}

func TestRetryNotOfferedOnTrueFalseOrWhenSwitchedOff(t *testing.T) {
	f := newFeedFixture(t, true)
	sid, beatID, _ := f.retryBeat(t, "cuisine.fournee.907", "cuisine", "ARI-OPE-A02c") // truefalse sample
	_, r, raw := f.answerAttempt(t, sid, beatID, "perhaps", uuid.NewString(), 1)
	if r["retry_available"] != nil || r["correct_answer"] == nil {
		t.Fatalf("no retry on true/false: %s", raw)
	}
	g := newFeedFixture(t, true)
	g.setPlatform(t, func(p *appconfig.Platform) { p.RetryEnabled = false })
	sid, beatID, q := g.retryBeat(t, "cuisine.fournee.901", "cuisine", "ARI-OPE-A02c")
	_, r, raw = g.answerAttempt(t, sid, beatID, json.RawMessage(q.CommonErrors[0].Value), uuid.NewString(), 1)
	if r["retry_available"] != nil || r["correct_answer"] == nil || r["attempt"] != 1.0 {
		t.Fatalf("retry_enabled=false must reveal at once: %s", raw)
	}
}

// Half credit: 4 right plus a wrong last answer is 80 percent; with a threshold of 90 only a right second try (4.5 of 5) earns the card.
func TestRetryHalfCreditReachesTheCard(t *testing.T) {
	for _, retry := range []bool{false, true} {
		t.Run(fmt.Sprintf("retry=%v", retry), func(t *testing.T) {
			f := newFeedFixture(t, true)
			f.setPlatform(t, func(p *appconfig.Platform) { p.CardThresholdPct = 90 })
			seedBank(t, f, onlyClones("cuisine.fournee.901", 9))
			sid := f.startIn(t, "cuisine", "ARI-OPE-A02c")
			var last string
			var lastQ content.Question
			for i := 0; i < 5; i++ {
				beat, _ := f.nextBeat(t, sid)
				last = beat["id"].(string)
				lastQ = f.curated(t, last)
				if i < 4 {
					if code, _, raw := f.answerAttempt(t, sid, last, json.RawMessage(lastQ.Answer.Value), uuid.NewString(), 1); code != 200 {
						t.Fatal(raw)
					}
				}
			}
			code, r, raw := f.answerAttempt(t, sid, last, json.RawMessage(lastQ.CommonErrors[0].Value), uuid.NewString(), 1)
			if code != 200 || r["retry_available"] != true {
				t.Fatalf("last wrong answer: %s", raw)
			}
			ct := 0
			_ = f.s.Store.R.QueryRow(`SELECT COUNT(*) FROM collection WHERE child_id = ?`, f.childID).Scan(&ct)
			if ct != 0 {
				t.Fatalf("80 percent is under 90: no card yet, got %d", ct)
			}
			if !retry {
				return
			}
			_, r2, raw2 := f.answerAttempt(t, sid, last, json.RawMessage(lastQ.Answer.Value), uuid.NewString(), 2)
			got := false
			for _, x := range r2["rewards"].([]any) {
				got = got || x.(map[string]any)["type"] == "card_earned"
			}
			if !got {
				t.Fatalf("a right second try must reach 90 percent (4.5 of 5): %s", raw2)
			}
			_ = f.s.Store.R.QueryRow(`SELECT COUNT(*) FROM collection WHERE child_id = ?`, f.childID).Scan(&ct)
			if ct != 1 {
				t.Fatalf("one card in the collection, got %d", ct)
			}
		})
	}
}

func TestRetryConcurrentSecondTries(t *testing.T) {
	f := newFeedFixture(t, true)
	sid, beatID, q := f.retryBeat(t, "cuisine.fournee.901", "cuisine", "ARI-OPE-A02c")
	f.answerAttempt(t, sid, beatID, json.RawMessage(q.CommonErrors[0].Value), uuid.NewString(), 1)
	var wg sync.WaitGroup
	codes := make([]int, 8)
	for i := range codes {
		wg.Add(1)
		go func() {
			defer wg.Done()
			codes[i], _, _ = f.answerAttempt(t, sid, beatID, json.RawMessage(q.Answer.Value), uuid.NewString(), 2)
		}()
	}
	wg.Wait()
	ok := 0
	for _, c := range codes {
		if c == http.StatusOK {
			ok++
		} else if c != http.StatusConflict {
			t.Fatalf("unexpected status %d in %v", c, codes)
		}
	}
	if ok != 1 {
		t.Fatalf("exactly one second try may win, got %d in %v", ok, codes)
	}
}

// Scan every seed question: the text served as a second-try hint never contains the answer.
func TestRetryHintNeverLeaksAcrossTheSeed(t *testing.T) {
	files, err := seedload.Read(testSeedDir, seedload.ReadOptions{SkipAssets: true})
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, q := range files.Questions() {
		for _, lang := range []string{"fr", "en"} {
			given := json.RawMessage(`999999`)
			if len(q.CommonErrors) > 0 {
				given = q.CommonErrors[0].Value
			}
			if h := retryHintText(q, given, lang); h == "" || q.Leaks(h, lang) {
				t.Errorf("%s (%s): retry hint %q is empty or leaks the answer", q.ID, lang, h)
			}
			n++
		}
	}
	if n < 100 || strings.TrimSpace(retryNudge("numeric", "fr")) == "" {
		t.Fatalf("scanned only %d hints", n)
	}
}
