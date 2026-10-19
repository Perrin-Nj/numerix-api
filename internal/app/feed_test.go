package app

// Go for Spring people: this file is a @SpringBootTest + MockMvc suite. Every test builds the whole HTTP stack on a
// throw-away SQLite file (newTestServer, from server_test.go), inserts rows with plain SQL and calls real URLs.

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// feedFixture is one family: a parent, a consented child on the UTC time zone, and their tokens.
type feedFixture struct {
	h                                  http.Handler
	s                                  *Server
	parentID, childID                  string
	childTok, parentTok, otherChildTok string
}

// newFeedFixture creates the family. consent=false leaves the child without parental consent.
func newFeedFixture(t *testing.T, consent bool) feedFixture {
	t.Helper()
	h, s := newTestServer(t)
	f := feedFixture{h: h, s: s, parentID: "p-1", childID: "11111111-1111-4111-8111-111111111111"}
	exec := func(q string, args ...any) {
		if _, err := s.Store.W.ExecContext(context.Background(), q, args...); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	exec(`INSERT INTO parents (id, email, password_hash) VALUES ('p-1', 'a@b.c', 'x')`)
	exec(`INSERT INTO children (id, parent_id, display_name, timezone, favorite_world) VALUES ('11111111-1111-4111-8111-111111111111', 'p-1', 'Zoé Tremblay', 'UTC', 'echecs')`)
	exec(`INSERT INTO children (id, parent_id, display_name, timezone) VALUES ('22222222-2222-4222-8222-222222222222', 'p-1', 'Max', 'UTC')`)
	if consent {
		exec(`INSERT INTO consents (id, parent_id, child_id, notice_version) VALUES ('k-1', 'p-1', '11111111-1111-4111-8111-111111111111', 'v1')`)
	}
	var err error
	if f.childTok, _, err = s.Issuer.IssueChild("p-1", "11111111-1111-4111-8111-111111111111"); err != nil {
		t.Fatal(err)
	}
	if f.otherChildTok, _, err = s.Issuer.IssueChild("p-1", "22222222-2222-4222-8222-222222222222"); err != nil {
		t.Fatal(err)
	}
	if f.parentTok, _, err = s.Issuer.IssueParent("p-1"); err != nil {
		t.Fatal(err)
	}
	return f
}

// call sends one JSON request and decodes the JSON answer into a generic map.
func (f feedFixture) call(t *testing.T, method, path, token, body string) (int, map[string]any) {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	f.h.ServeHTTP(rec, req)
	var out map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	return rec.Code, out
}

// sql runs a write statement for test setup.
func (f feedFixture) sql(t *testing.T, q string, args ...any) {
	t.Helper()
	if _, err := f.s.Store.W.ExecContext(context.Background(), q, args...); err != nil {
		t.Fatalf("%s: %v", q, err)
	}
}

// session inserts a session of 11111111-1111-4111-8111-111111111111 with the given status, world and finish time.
func (f feedFixture) session(t *testing.T, id, status, world string, started, finished time.Time) {
	t.Helper()
	fin := any(nil)
	if !finished.IsZero() {
		fin = finished.UTC().Format(plTSLayout)
	}
	f.sql(t, `INSERT INTO sessions (id, child_id, status, world_id, started_at, finished_at) VALUES (?, '11111111-1111-4111-8111-111111111111', ?, ?, ?, ?)`,
		id, status, world, started.UTC().Format(plTSLayout), fin)
}

func TestPuzzleFor(t *testing.T) {
	for n := 0; n < 200; n++ {
		p := puzzleFor(n)
		if p.Correct != p.A*p.B || p.Typical != p.A+p.B {
			t.Fatalf("n=%d bad numbers %+v", n, p)
		}
		if len(p.Choices) < 2 || len(p.Choices) > 4 {
			t.Fatalf("n=%d %d choices", n, len(p.Choices))
		}
		hasCorrect := false
		for _, c := range p.Choices {
			hasCorrect = hasCorrect || c == p.Correct
		}
		if !hasCorrect {
			t.Fatalf("n=%d correct answer missing from %v", n, p.Choices)
		}
	}
}

func TestFeedItemDeterministicAndSafe(t *testing.T) {
	cases := []struct {
		n    int
		kind string
		cta  bool
	}{{0, "story_short", true}, {1, "fun_fact", false}, {2, "puzzle", false}, {3, "challenge", true}, {4, "story_short", true}, {5, "puzzle", false}}
	for _, c := range cases {
		t.Run(fmt.Sprintf("%d-%s", c.n, c.kind), func(t *testing.T) {
			a := feedItem(c.n, false, false, false, "Zoé", "nature", "Nature")
			b := feedItem(c.n, false, false, false, "Zoé", "nature", "Nature")
			ja, _ := json.Marshal(a)
			jb, _ := json.Marshal(b)
			if string(ja) != string(jb) {
				t.Fatal("same input gave two items")
			}
			if string(a.Kind) != c.kind || (a.Cta != nil) != c.cta || !bool(a.AiGenerated) {
				t.Fatalf("unexpected item %+v", a)
			}
			if strings.Contains(a.Text, "{") || a.Text == "" {
				t.Fatalf("slots not filled: %q", a.Text)
			}
			if strings.Contains(string(ja), "correct") {
				t.Fatalf("answer leaked: %s", ja)
			}
		})
	}
	if en := feedItem(0, true, false, false, "Zoé", "nature", "Nature"); !strings.Contains(en.Text, "Nature world") {
		t.Fatalf("english slots: %q", en.Text)
	}
}

func TestFeedPagingConsentAndAccess(t *testing.T) {
	f := newFeedFixture(t, true)
	code, page := f.call(t, "GET", "/v1/children/11111111-1111-4111-8111-111111111111/feed", f.childTok, "")
	if code != 200 || len(page["items"].([]any)) != 5 || page["next_cursor"] != "i5" {
		t.Fatalf("first page: %d %v", code, page)
	}
	_, page = f.call(t, "GET", "/v1/children/11111111-1111-4111-8111-111111111111/feed?cursor=i5&limit=2", f.childTok, "")
	items := page["items"].([]any)
	if len(items) != 2 || items[0].(map[string]any)["id"] != "feed-5" || page["next_cursor"] != "i7" {
		t.Fatalf("second page: %v", page)
	}
	if code, _ = f.call(t, "GET", "/v1/children/11111111-1111-4111-8111-111111111111/feed", f.otherChildTok, ""); code != 403 {
		t.Fatalf("sibling token: %d", code)
	}
	if code, _ = f.call(t, "GET", "/v1/children/11111111-1111-4111-8111-111111111111/feed", f.parentTok, ""); code != 403 {
		t.Fatalf("parent role is not allowed on the feed: %d", code)
	}
	nc := newFeedFixture(t, false)
	if code, body := nc.call(t, "GET", "/v1/children/11111111-1111-4111-8111-111111111111/feed", nc.childTok, ""); code != 403 || body["code"] != "consent_missing" {
		t.Fatalf("no consent: %d %v", code, body)
	}
}

func TestReactionAndAnswer(t *testing.T) {
	f := newFeedFixture(t, true)
	if code, body := f.call(t, "PUT", "/v1/children/11111111-1111-4111-8111-111111111111/feed/feed-1/reaction", f.childTok, `{"liked":true,"saved":false}`); code != 200 || body["liked"] != true {
		t.Fatalf("react: %d %v", code, body)
	}
	_, page := f.call(t, "GET", "/v1/children/11111111-1111-4111-8111-111111111111/feed?limit=3", f.childTok, "")
	if it := page["items"].([]any)[1].(map[string]any); it["liked"] != true || it["saved"] != false {
		t.Fatalf("like not visible in the feed: %v", it)
	}
	f.call(t, "PUT", "/v1/children/11111111-1111-4111-8111-111111111111/feed/feed-1/reaction", f.childTok, `{"liked":false,"saved":false}`)
	_, page = f.call(t, "GET", "/v1/children/11111111-1111-4111-8111-111111111111/feed?limit=3", f.childTok, "")
	if page["items"].([]any)[1].(map[string]any)["liked"] != false {
		t.Fatal("unlike not visible")
	}
	if code, _ := f.call(t, "PUT", "/v1/children/11111111-1111-4111-8111-111111111111/feed/other/reaction", f.childTok, `{"liked":true,"saved":false}`); code != 404 {
		t.Fatalf("foreign item id: %d", code)
	}

	p := puzzleFor(2) // feed-2 is a puzzle
	cases := []struct {
		name, body, outcome string
		path                string
		code                int
	}{
		{"right", fmt.Sprintf(`{"answer":%d}`, p.Correct), "correct", "feed-2", 200},
		{"typical mistake", fmt.Sprintf(`{"answer":%d}`, p.Typical), "erreur_typique", "feed-2", 200},
		{"string answer", fmt.Sprintf(`{"answer":"%d"}`, p.Correct), "correct", "feed-2", 200},
		{"random", `{"answer":1}`, "erreur_aleatoire", "feed-2", 200},
		{"not a puzzle", `{"answer":1}`, "", "feed-0", 404},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			code, body := f.call(t, "POST", "/v1/children/11111111-1111-4111-8111-111111111111/feed/"+c.path+"/answer", f.childTok, c.body)
			if code != c.code || (c.code == 200 && (body["outcome"] != c.outcome || body["message"] == "")) {
				t.Fatalf("%d %v", code, body)
			}
		})
	}
}

func TestEngagementStreakAndGoal(t *testing.T) {
	f := newFeedFixture(t, true)
	today := time.Now().UTC().Truncate(24 * time.Hour)
	f.session(t, "s1", "completed", "nature", today.AddDate(0, 0, -2), today.AddDate(0, 0, -2).Add(time.Minute))
	f.session(t, "s2", "completed", "nature", today.AddDate(0, 0, -1), today.AddDate(0, 0, -1).Add(time.Minute))
	// nothing today yet: the streak survives until the day ends
	_, e := f.call(t, "GET", "/v1/children/11111111-1111-4111-8111-111111111111/engagement", f.childTok, "")
	streak := e["streak"].(map[string]any)
	goal := e["daily_goal"].(map[string]any)
	if streak["current_days"] != 2.0 || streak["longest_days"] != 2.0 || goal["done_chapters"] != 0.0 || goal["target_chapters"] != 1.0 {
		t.Fatalf("before today: %v", e)
	}
	f.session(t, "s3", "completed", "espace", today, today.Add(time.Second))
	_, e = f.call(t, "GET", "/v1/children/11111111-1111-4111-8111-111111111111/engagement", f.parentTok, "") // parent may read it too
	streak, goal = e["streak"].(map[string]any), e["daily_goal"].(map[string]any)
	if streak["current_days"] != 3.0 || goal["done_chapters"] != 1.0 || streak["last_active_date"] != today.Format("2006-01-02") {
		t.Fatalf("after today: %v", e)
	}
	// a gap resets the current streak but not the longest
	f.sql(t, `DELETE FROM sessions WHERE id IN ('s2','s3')`)
	_, e = f.call(t, "GET", "/v1/children/11111111-1111-4111-8111-111111111111/engagement", f.childTok, "")
	if s := e["streak"].(map[string]any); s["current_days"] != 0.0 || s["longest_days"] != 1.0 {
		t.Fatalf("gap: %v", e)
	}
}

func TestChallengeFlowAndCollection(t *testing.T) {
	f := newFeedFixture(t, true)
	code, list := f.call(t, "GET", "/v1/children/11111111-1111-4111-8111-111111111111/challenges", f.childTok, "")
	if items := list["items"].([]any); code != 200 || len(items) != 7 || items[0].(map[string]any)["status"] != "available" {
		t.Fatalf("catalogue: %d %v", code, list)
	}
	if code, _ = f.call(t, "POST", "/v1/children/11111111-1111-4111-8111-111111111111/challenges/nope/accept", f.childTok, ""); code != 404 {
		t.Fatalf("unknown challenge: %d", code)
	}
	_, first := f.call(t, "POST", "/v1/children/11111111-1111-4111-8111-111111111111/challenges/feed_fan/accept", f.childTok, "")
	_, again := f.call(t, "POST", "/v1/children/11111111-1111-4111-8111-111111111111/challenges/feed_fan/accept", f.childTok, "")
	if first["status"] != "active" || first["accepted_at"] == nil || first["accepted_at"] != again["accepted_at"] {
		t.Fatalf("accept not idempotent: %v / %v", first, again)
	}
	var last map[string]any
	for i := 0; i < 5; i++ { // five likes complete feed_fan on the fifth
		_, last = f.call(t, "PUT", fmt.Sprintf("/v1/children/11111111-1111-4111-8111-111111111111/feed/feed-%d/reaction", i), f.childTok, `{"liked":true,"saved":false}`)
		if (i < 4) != (last["rewards"] == nil) {
			t.Fatalf("like %d rewards: %v", i, last)
		}
	}
	reward := last["rewards"].([]any)[0].(map[string]any)
	if reward["type"] != "card_earned" || reward["card"].(map[string]any)["world"] != "cuisine" {
		t.Fatalf("reward: %v", reward)
	}
	_, last = f.call(t, "PUT", "/v1/children/11111111-1111-4111-8111-111111111111/feed/feed-7/reaction", f.childTok, `{"liked":true,"saved":false}`)
	if last["rewards"] != nil {
		t.Fatal("card awarded twice")
	}
	_, list = f.call(t, "GET", "/v1/children/11111111-1111-4111-8111-111111111111/challenges", f.parentTok, "")
	got := list["items"].([]any)
	if got[len(got)-1].(map[string]any)["id"] != "feed_fan" || got[len(got)-1].(map[string]any)["status"] != "done" || got[len(got)-1].(map[string]any)["card"] == nil {
		t.Fatalf("done challenge last with its card: %v", got)
	}
	_, col := f.call(t, "GET", "/v1/children/11111111-1111-4111-8111-111111111111/collection", f.childTok, "")
	if n := len(col["items"].([]any)); n != 1 {
		t.Fatalf("collection: %v", col)
	}
	if _, none := f.call(t, "GET", "/v1/children/11111111-1111-4111-8111-111111111111/collection?world=sport", f.childTok, ""); len(none["items"].([]any)) != 0 {
		t.Fatalf("world filter: %v", none)
	}
}

func TestChallengeMetricsFromGameData(t *testing.T) {
	f := newFeedFixture(t, true)
	ev := feedEvents{sessions: []feedSession{
		{World: "nature", Status: "completed", Started: time.Unix(100, 0), Finished: time.Unix(200, 0)},
		{World: "espace", Status: "active", Started: time.Unix(300, 0)},
		{World: "nature", Status: "completed", Started: time.Unix(400, 0), Finished: time.Unix(90000, 0)},
	}, answers: []feedAnswer{{"correct", time.Unix(150, 0)}, {"correct", time.Unix(160, 0)}, {"erreur_typique", time.Unix(170, 0)}, {"correct", time.Unix(180, 0)}}}
	cases := []struct {
		metric string
		since  time.Time
		want   int
	}{
		{"chapters_done", time.Unix(0, 0), 2}, {"chapters_done", time.Unix(250, 0), 1},
		{"worlds_played", time.Unix(0, 0), 2}, {"worlds_played", time.Unix(350, 0), 1},
		{"days_played", time.Unix(0, 0), 2}, {"correct_streak", time.Unix(0, 0), 2},
		{"correct_streak", time.Unix(165, 0), 1}, {"likes", time.Unix(0, 0), 0},
	}
	for _, c := range cases {
		t.Run(fmt.Sprintf("%s-%d", c.metric, c.since.Unix()), func(t *testing.T) {
			if got := feedProgress(c.metric, ev, c.since, feedLoc(plChild{Timezone: "UTC"})); got != c.want {
				t.Fatalf("got %d want %d", got, c.want)
			}
		})
	}
	_ = f
}

func TestGiftHomeAndQuest(t *testing.T) {
	f := newFeedFixture(t, true)
	if code, _ := f.call(t, "POST", "/v1/children/11111111-1111-4111-8111-111111111111/gift/claim", f.childTok, ""); code != 404 {
		t.Fatalf("feature off must be 404, got %d", code)
	}
	_, home := f.call(t, "GET", "/v1/children/11111111-1111-4111-8111-111111111111/home", f.childTok, "")
	if home["gift_available"] != false || home["active_challenges"] != 7.0 || len(home["worlds"].([]any)) != 20 {
		t.Fatalf("home with the gift off: %v", home)
	}
	if sg := home["suggested"].(map[string]any); sg["world"] != "echecs" || sg["skill_id"] != "ARI-OPE-A06a" {
		t.Fatalf("suggested: %v", sg)
	}
	f.s.Config.FeatureWelcomeGift = true
	_, home = f.call(t, "GET", "/v1/children/11111111-1111-4111-8111-111111111111/home", f.childTok, "")
	if home["gift_available"] != true {
		t.Fatalf("gift should be available: %v", home)
	}
	c1, card := f.call(t, "POST", "/v1/children/11111111-1111-4111-8111-111111111111/gift/claim", f.childTok, "")
	c2, again := f.call(t, "POST", "/v1/children/11111111-1111-4111-8111-111111111111/gift/claim", f.childTok, "")
	if c1 != 201 || c2 != 200 || card["id"] != again["id"] || card["world"] != "nature" {
		t.Fatalf("gift claims: %d %d %v %v", c1, c2, card, again)
	}
	_, home = f.call(t, "GET", "/v1/children/11111111-1111-4111-8111-111111111111/home", f.childTok, "")
	if home["gift_available"] != false || len(home["recent_cards"].([]any)) != 1 {
		t.Fatalf("after claim: %v", home)
	}
	_, q := f.call(t, "GET", "/v1/children/11111111-1111-4111-8111-111111111111/quests/daily", f.childTok, "")
	if q["available"] != true || q["completed"] != false || q["skill_ids"].([]any)[0] != "ARI-OPE-A06a" {
		t.Fatalf("quest: %v", q)
	}
	f.sql(t, `INSERT INTO sessions (id, child_id, status, json, started_at, finished_at) VALUES ('q1', '11111111-1111-4111-8111-111111111111', 'completed', '{"kind":"daily_quest"}', ?, ?)`,
		time.Now().UTC().Truncate(24*time.Hour).Format(plTSLayout), time.Now().UTC().Truncate(24*time.Hour).Format(plTSLayout))
	if _, q = f.call(t, "GET", "/v1/children/11111111-1111-4111-8111-111111111111/quests/daily", f.childTok, ""); q["completed"] != true {
		t.Fatalf("quest not completed: %v", q)
	}
	if code, _ := f.call(t, "GET", "/v1/children/22222222-2222-4222-8222-222222222222/home", f.childTok, ""); code != 403 {
		t.Fatalf("home of a sibling: %d", code)
	}
}
