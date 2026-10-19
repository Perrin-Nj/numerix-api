package app

// Go for Spring people: integration tests of the curated question bank in the game (picker, beat view, grading per
// question type, narration, hints, assets, worlds). They seed the real sample questions of seed/questions/_examples.json
// through the same loader production uses, then talk to the server over HTTP like the web app does.

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/Perrin-Nj/numerix-api/internal/assetstore"
	"github.com/Perrin-Nj/numerix-api/internal/content"
	"github.com/Perrin-Nj/numerix-api/internal/seedload"
)

const testSeedDir = "../../seed"

// seedBank loads worlds, quests and the sample questions into the fixture database. mutate may reshape the files first.
func seedBank(t *testing.T, f feedFixture, mutate func(*seedload.Files)) {
	t.Helper()
	files, err := seedload.Read(testSeedDir, seedload.ReadOptions{SkipAssets: true})
	if err != nil {
		t.Fatal(err)
	}
	if mutate != nil {
		mutate(files)
	}
	if _, err := seedload.Apply(context.Background(), f.s.Content, f.s.Assets, f.s.game().graph, files, seedload.Options{Actor: "test"}); err != nil {
		t.Fatalf("seed: %v", err)
	}
}

// onlyClones keeps one sample question and makes n published copies of it (ids quest.801, quest.802, ...), so its
// quest has enough questions to be playable (8) even for skills without a generator.
func onlyClones(id string, n int) func(*seedload.Files) {
	return func(files *seedload.Files) {
		var src *content.Question
		for _, q := range files.Questions() {
			if q.ID == id {
				src = q
			}
		}
		var list []*content.Question
		for i := 1; i <= n; i++ {
			c := *src
			c.ID = fmt.Sprintf("%s.%d", src.Quest, 800+i)
			c.Status = content.StatusPublished
			list = append(list, &c)
		}
		files.QuestionFiles = []seedload.QuestionFile{{Name: seedload.ExamplesFile, World: seedload.ExamplesFile, Questions: list}}
	}
}

// startIn opens a chapter in a world for a skill.
func (f feedFixture) startIn(t *testing.T, world, skill string) string {
	t.Helper()
	body := fmt.Sprintf(`{"world":%q,"kind":"chapter","skill_id":%q}`, world, skill)
	code, out, raw := f.gcall(t, "POST", "/v1/children/"+f.childID+"/sessions", f.childTok, body, nil)
	if code != 201 {
		t.Fatalf("start %s/%s: %d %s", world, skill, code, raw)
	}
	return out["id"].(string)
}

// nextBeat asks for the next beat and returns its JSON map and raw text.
func (f feedFixture) nextBeat(t *testing.T, sid string) (map[string]any, string) {
	t.Helper()
	code, beat, raw := f.gcall(t, "POST", "/v1/sessions/"+sid+"/beats", f.childTok, "{}", nil)
	if code != 201 && code != 200 {
		t.Fatalf("next beat: %d %s", code, raw)
	}
	return beat, raw
}

// curated reads the bank question behind a beat.
func (f feedFixture) curated(t *testing.T, beatID string) content.Question {
	t.Helper()
	p := f.problemOf(t, beatID)
	var q content.Question
	if err := json.Unmarshal(p.Curated, &q); err != nil {
		t.Fatalf("beat %s is not curated: %v", beatID, err)
	}
	return q
}

func raw(v any) json.RawMessage {
	b, _ := json.Marshal(v)
	return b
}

func TestCuratedBankIsServedFirstWithRotationAndNoRepeat(t *testing.T) {
	f := newFeedFixture(t, true)
	seedBank(t, f, nil)
	// cuisine.fournee has 12 published sample questions on a skill that also has a generator
	var types []string
	seen := map[string]bool{}
	var sid string
	for i := 0; i < 12; i++ {
		if i%3 == 0 {
			sid = f.startIn(t, "cuisine", "ARI-OPE-A02c")
		}
		beat, rawBeat := f.nextBeat(t, sid)
		beatID := beat["id"].(string)
		q := f.curated(t, beatID)
		if seen[q.ID] {
			t.Fatalf("bank question %s served twice", q.ID)
		}
		seen[q.ID] = true
		types = append(types, q.Type)
		if i < 9 && i%3 > 0 && types[i] == types[i-1] { // later beats may have only one type left
			t.Errorf("beats %d and %d share the type %q although other types were available", i-1, i, q.Type)
		}
		scene := beat["scene"].(map[string]any)
		if scene["status"] != "ready" || scene["text"] == "" || scene["text"] == nil {
			t.Fatalf("a curated beat has its scene at once: %v", scene)
		}
		qv := beat["question"].(map[string]any)
		if qv["kind"] != q.Type || qv["prompt"] == "" || qv["story"] == "" {
			t.Fatalf("question view: %v", qv)
		}
		for _, banned := range []string{"correct_answer", "explain", "calc", "common_errors", "misconception", "\"answer\""} {
			if strings.Contains(rawBeat, banned) {
				t.Fatalf("the beat leaks %q: %s", banned, rawBeat)
			}
		}
		f.answer(t, sid, beatID, 0, uuid.NewString())
	}
	if len(seen) != 12 {
		t.Fatalf("served %d distinct bank questions, want 12", len(seen))
	}
	// the bank is exhausted for this child: the generator takes over, as a generated question
	sid = f.startIn(t, "cuisine", "ARI-OPE-A02c")
	beat, _ := f.nextBeat(t, sid)
	p := f.problemOf(t, beat["id"].(string))
	if len(p.Curated) != 0 || p.Generateur == "curated" {
		t.Fatalf("after the bank, a generated question is expected, got %+v", p.Generateur)
	}
}

func TestBankOnlySkillReServesOldestWhenExhausted(t *testing.T) {
	f := newFeedFixture(t, true)
	seedBank(t, f, onlyClones("jeux.cartes.901", 8)) // ARI-SEN-A08 has no generator
	served := map[string]bool{}
	for chapter := 0; chapter < 3; chapter++ {
		sid := f.startIn(t, "jeux", "ARI-SEN-A08")
		for i := 0; i < 3; i++ {
			beat, _ := f.nextBeat(t, sid)
			q := f.curated(t, beat["id"].(string))
			if chapter < 2 || i < 2 {
				if served[q.ID] {
					t.Fatalf("%s served twice before the bank of 8 was used", q.ID)
				}
			}
			served[q.ID] = true
			f.answer(t, sid, beat["id"].(string), 0, uuid.NewString())
		}
	}
	// 9 beats on a bank of 8: the ninth re-serves the oldest instead of failing
	if len(served) != 8 {
		t.Fatalf("distinct questions = %d, want 8", len(served))
	}
}

func TestEveryTypeIsGradedWithTheThreeOutcomes(t *testing.T) {
	cases := []struct {
		name, quest, world, skill string
		correct, typical, random  func(q content.Question) any
		wantCode                  string
	}{
		{"numeric", "cuisine.fournee.901", "cuisine", "ARI-OPE-A02c",
			func(q content.Question) any { return raw(json.RawMessage(q.Answer.Value)) },
			func(q content.Question) any { return json.RawMessage(q.CommonErrors[0].Value) },
			func(q content.Question) any { return 987654 }, "mis_off_by_one"},
		{"choice", "cuisine.fournee.904", "cuisine", "ARI-OPE-A02c",
			func(q content.Question) any { return json.RawMessage(q.CorrectAnswer("fr")) },
			func(q content.Question) any { return json.RawMessage(content.ChoiceValue(q.Choices[0], "fr")) },
			func(q content.Question) any { return "no such choice" }, "mis_off_by_one"},
		{"truefalse", "cuisine.fournee.907", "cuisine", "ARI-OPE-A02c",
			func(q content.Question) any { return false },
			func(q content.Question) any { return true },
			func(q content.Question) any { return "perhaps" }, "mis_off_by_one"},
		{"order", "jeux.cartes.901", "jeux", "ARI-SEN-A08",
			func(q content.Question) any { return q.Answer.Sequence },
			func(q content.Question) any { return []string{"a", "b", "c"} },
			func(q content.Question) any { return 5 }, "mis_order_reversed"},
		{"match", "construction.formes.901", "construction", "GEO-C02",
			func(q content.Question) any { return q.Answer.Pairs },
			func(q content.Question) any { return [][2]string{{"l1", "r1"}, {"l2", "r2"}, {"l3", "r3"}} },
			func(q content.Question) any { return 5 }, "mis_shape_name"},
		{"estimate", "espace.distances.901", "espace", "ARI-OPE-A01a",
			func(q content.Question) any { return json.RawMessage(q.CorrectAnswer("fr")) },
			func(q content.Question) any { return json.RawMessage(q.CommonErrors[0].Value) },
			func(q content.Question) any { return 7 }, "mis_estimate_rounding"},
		{"visual", "nature.observer.901", "nature", "STA-03a",
			func(q content.Question) any { return json.RawMessage(q.Answer.Value) },
			func(q content.Question) any { return json.RawMessage(q.CommonErrors[0].Value) },
			func(q content.Question) any { return 77 }, "mis_read_wrong_bar"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newFeedFixture(t, true)
			seedBank(t, f, onlyClones(tc.quest, 9))
			sid := f.startIn(t, tc.world, tc.skill)
			// each call needs the question of its own beat, so build the answers from the served question
			ask := func(pick func(q content.Question) any, outcome, code string) {
				beat, _ := f.nextBeat(t, sid)
				q := f.curated(t, beat["id"].(string))
				a := pick(q)
				status, res := f.answer(t, sid, beat["id"].(string), a, uuid.NewString())
				if status != 200 || res["outcome"] != outcome {
					t.Fatalf("%s: answer %v -> %d %v", q.Type, a, status, res)
				}
				if res["explain"] == nil || res["explain"] == "" {
					t.Fatalf("explain must come with the verdict: %v", res)
				}
				if res["misconception"] != nilIfEmpty(code) {
					t.Fatalf("misconception = %v, want %q", res["misconception"], code)
				}
				if res["correct_answer"] == nil {
					t.Fatal("correct_answer expected")
				}
			}
			ask(tc.correct, "correct", "")
			ask(tc.typical, "erreur_typique", tc.wantCode)
			ask(tc.random, "erreur_aleatoire", "")
		})
	}
}

func nilIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func TestCuratedNarrationHintAndReplay(t *testing.T) {
	f := newFeedFixture(t, true)
	seedBank(t, f, onlyClones("cuisine.fournee.901", 9))
	sid := f.startIn(t, "cuisine", "ARI-OPE-A02c")
	beat, _ := f.nextBeat(t, sid)
	beatID := beat["id"].(string)
	q := f.curated(t, beatID)
	if beat["question"].(map[string]any)["hint_available"] != true {
		t.Fatal("hint_available expected")
	}
	// hint: guides, never states the answer
	code, h, rawHint := f.gcall(t, "POST", "/v1/sessions/"+sid+"/beats/"+beatID+"/hint", f.childTok, "{}", nil)
	if code != 200 || h["hint"] != q.Hint.FR {
		t.Fatalf("hint: %d %s", code, rawHint)
	}
	if _, h, _ := f.gcall(t, "POST", "/v1/sessions/"+sid+"/beats/"+beatID+"/hint", f.childTok, "{}", map[string]string{"Accept-Language": "en-CA"}); h["hint"] != q.Hint.EN {
		t.Fatalf("english hint: %v", h)
	}
	// scene: the question's own story, replayed by the stream, no model involved
	code, _, sse := f.gcall(t, "GET", "/v1/sessions/"+sid+"/beats/"+beatID+"/narration?phase=scene", f.childTok, "", nil)
	if code != 200 || !strings.Contains(sse, "event: scene") || !strings.Contains(sse, strings.Fields(q.Story.FR)[0]) {
		t.Fatalf("scene stream: %d %q", code, sse)
	}
	if code, _, _ := f.gcall(t, "GET", "/v1/sessions/"+sid+"/beats/"+beatID+"/narration?phase=denouement", f.childTok, "", nil); code != 409 {
		t.Fatalf("denouement before the answer: %d", code)
	}
	key := uuid.NewString()
	code, res := f.answer(t, sid, beatID, json.RawMessage(q.CommonErrors[0].Value), key)
	if code != 200 || res["outcome"] != "erreur_typique" || res["feedback"] != q.CommonErrors[0].Feedback.FR {
		t.Fatalf("typical answer: %d %v", code, res)
	}
	// an identical retry replays the same verdict, explanation and feedback
	code, again := f.answer(t, sid, beatID, json.RawMessage(q.CommonErrors[0].Value), key)
	if code != 200 || again["explain"] != res["explain"] || again["misconception"] != res["misconception"] || again["feedback"] != res["feedback"] {
		t.Fatalf("replay differs: %v vs %v", again, res)
	}
	// the denouement shows the message written for that typical error
	code, _, sse = f.gcall(t, "GET", "/v1/sessions/"+sid+"/beats/"+beatID+"/narration?phase=denouement", f.childTok, "", nil)
	if code != 200 || !strings.Contains(sse, strings.Fields(q.CommonErrors[0].Feedback.FR)[0]) {
		t.Fatalf("denouement: %d %q", code, sse)
	}
	// no hint once answered
	if code, _, _ := f.gcall(t, "POST", "/v1/sessions/"+sid+"/beats/"+beatID+"/hint", f.childTok, "{}", nil); code != 409 {
		t.Fatalf("hint after the answer: %d", code)
	}
}

func TestHintIsNotFoundForGeneratedQuestions(t *testing.T) {
	f := newFeedFixture(t, true)
	sid := f.startChapter(t, "ARI-OPE-A06a") // no bank content: generator
	beat, _ := f.nextBeat(t, sid)
	if q := beat["question"].(map[string]any); q["hint_available"] != nil || q["kind"] != nil {
		t.Fatalf("a generated question keeps its old shape: %v", q)
	}
	if code, _, _ := f.gcall(t, "POST", "/v1/sessions/"+sid+"/beats/"+beat["id"].(string)+"/hint", f.childTok, "{}", nil); code != 404 {
		t.Fatalf("hint for a generated question: %d", code)
	}
}

// TestBeatNeverLeaksForAnyQuestionType renders a beat for every sample question and scans the JSON keys and texts.
func TestBeatNeverLeaksForAnyQuestionType(t *testing.T) {
	f := newFeedFixture(t, true)
	seedBank(t, f, nil)
	files, err := seedload.Read(testSeedDir, seedload.ReadOptions{SkipAssets: true})
	if err != nil {
		t.Fatal(err)
	}
	f.session(t, "sess-leak", "active", "cuisine", time.Now().UTC(), time.Now().UTC())
	forbiddenKeys := []string{"answer", "explain", "calc", "calc_choice", "common_errors", "misconception", "correct", "sequence", "pairs", "exact_for_check", "key", "random"}
	for _, q := range files.Questions() {
		b, err := f.s.curatedBeatView(context.Background(), beatRow{ID: "beat-" + q.ID, SessionID: "sess-leak", Created: plTS(time.Now().UTC())}, q, "fr")
		if err != nil {
			t.Fatalf("%s: %v", q.ID, err)
		}
		bj, _ := json.Marshal(b)
		var generic any
		_ = json.Unmarshal(bj, &generic)
		walkKeys(generic, func(k string) {
			for _, bad := range forbiddenKeys {
				if k == bad {
					t.Errorf("%s: the beat has the key %q", q.ID, k)
				}
			}
		})
		if ex := q.Explain.FR; ex != "" && strings.Contains(string(bj), ex) {
			t.Errorf("%s: the explanation is inside the beat", q.ID)
		}
		if q.Type == content.TypeOrder {
			cards := b.Question.Items.Cards
			ids := make([]string, len(*cards))
			for i, c := range *cards {
				ids[i] = c.Id
			}
			if strings.Join(ids, ",") == strings.Join(q.Answer.Sequence, ",") {
				t.Errorf("%s: the cards are shown already in the right order", q.ID)
			}
		}
	}
}

func walkKeys(v any, fn func(string)) {
	switch x := v.(type) {
	case map[string]any:
		for k, e := range x {
			fn(k)
			walkKeys(e, fn)
		}
	case []any:
		for _, e := range x {
			walkKeys(e, fn)
		}
	}
}

func TestWorldsJourneyAndUnknownWorld(t *testing.T) {
	f := newFeedFixture(t, true)
	seedBank(t, f, nil)
	code, w, raw := f.gcall(t, "GET", "/v1/worlds", f.childTok, "", map[string]string{"Accept-Language": "en-CA"})
	if code != 200 {
		t.Fatalf("worlds: %d %s", code, raw)
	}
	items := w["items"].([]any)
	if len(items) != 20 {
		t.Fatalf("worlds = %d", len(items))
	}
	ids := map[string]map[string]any{}
	for _, it := range items {
		m := it.(map[string]any)
		ids[m["id"].(string)] = m
	}
	for _, id := range []string{"ocean", "dinosaures", "animaux", "musique", "transport", "pirates", "marche", "meteo", "robots", "arts", "hiver", "jeux"} {
		m, ok := ids[id]
		if !ok || m["name"] == "" || m["accent_color"] == "" || m["grade_min"] == nil || m["scenes"] == nil {
			t.Fatalf("new world %s: %v", id, m)
		}
	}
	if ids["cuisine"]["name"] != "Cooking" || ids["cuisine"]["hook"] != "A big meal to cook" || ids["cuisine"]["skill_count"].(float64) < 5 {
		t.Fatalf("english legacy strings must be kept: %v", ids["cuisine"])
	}
	// journey: quests with names, goals, in quest order; only playable ones
	code, j, raw := f.gcall(t, "GET", "/v1/children/"+f.childID+"/journey?world=cuisine", f.childTok, "", nil)
	if code != 200 {
		t.Fatalf("journey: %d %s", code, raw)
	}
	nodes := j["nodes"].([]any)
	first := nodes[0].(map[string]any)
	quest := first["quest"].(map[string]any)
	if quest["id"] != "cuisine.fournee" || first["label"] != "La fournée de biscuits" || quest["goal"] == "" {
		t.Fatalf("first node = %v", first)
	}
	// a world without playable content: the journey is empty, a chapter cannot start, an unknown world is a 422
	code, j, _ = f.gcall(t, "GET", "/v1/children/"+f.childID+"/journey?world=pirates", f.childTok, "", nil)
	if code != 200 || len(j["nodes"].([]any)) != 0 {
		t.Fatalf("empty world journey: %d %v", code, j)
	}
	if code, _, _ := f.gcall(t, "POST", "/v1/children/"+f.childID+"/sessions", f.childTok, `{"world":"pirates","kind":"chapter"}`, nil); code != 422 {
		t.Fatalf("a world with no chapter must be 422, got %d", code)
	}
	if code, _, _ := f.gcall(t, "GET", "/v1/children/"+f.childID+"/journey?world=atlantis", f.childTok, "", nil); code == 200 {
		t.Fatal("unknown world must fail")
	}
	// skills: implemented now means generator OR a bank of 8; cuisine.fournee has 12 questions on a generator skill
	_, sk, _ := f.gcall(t, "GET", "/v1/skills?world=ocean", f.parentTok, "", nil)
	if len(sk["items"].([]any)) == 0 {
		t.Fatal("the skills of a new world come from its quests")
	}
}

func TestSkillBecomesPlayableAtEightPublishedQuestions(t *testing.T) {
	f := newFeedFixture(t, true)
	seedBank(t, f, onlyClones("jeux.cartes.901", 7))
	if code, _, _ := f.gcall(t, "POST", "/v1/children/"+f.childID+"/sessions", f.childTok, `{"world":"jeux","kind":"chapter","skill_id":"ARI-SEN-A08"}`, nil); code != 422 {
		t.Fatalf("7 questions are not enough, got %d", code)
	}
	// the 8th is published through the repository: the graph notices (cache generation) without a restart
	files, _ := seedload.Read(testSeedDir, seedload.ReadOptions{SkipAssets: true})
	onlyClones("jeux.cartes.901", 8)(files)
	if _, err := seedload.Apply(context.Background(), f.s.Content, f.s.Assets, f.s.game().graph, files, seedload.Options{Actor: "test"}); err != nil {
		t.Fatal(err)
	}
	f.startIn(t, "jeux", "ARI-SEN-A08")
	_, sk, _ := f.gcall(t, "GET", "/v1/skills?implemented=true", f.parentTok, "", nil)
	found := false
	for _, it := range sk["items"].([]any) {
		found = found || it.(map[string]any)["id"] == "ARI-SEN-A08"
	}
	if !found {
		t.Fatal("ARI-SEN-A08 must be listed as implemented")
	}
}

func TestCuratedQuestionCacheAndInvalidation(t *testing.T) {
	f := newFeedFixture(t, true)
	seedBank(t, f, onlyClones("cuisine.fournee.901", 9))
	sid := f.startIn(t, "cuisine", "ARI-OPE-A02c")
	beat, _ := f.nextBeat(t, sid)
	h0, _ := f.s.Content.Cache.Stats()
	for i := 0; i < 3; i++ { // each GET of the session renders the open beat from the cache
		if code, _, _ := f.gcall(t, "GET", "/v1/sessions/"+sid, f.childTok, "", nil); code != 200 {
			t.Fatal("get session")
		}
	}
	h1, _ := f.s.Content.Cache.Stats()
	if h1 <= h0 {
		t.Fatalf("the served question must come from the LRU (hits %d -> %d)", h0, h1)
	}
	qid := f.curated(t, beat["id"].(string)).ID
	f.s.Content.AfterCommit(qid)
	if _, ok := f.s.Content.Cache.Get(qid, 1); ok {
		t.Fatal("an edit must drop the cached question")
	}
}

func TestAssetsEndpointsAndMedia(t *testing.T) {
	f := newFeedFixture(t, true)
	seedBank(t, f, onlyClones("cuisine.fournee.901", 9)) // 901 has a scene criteria {world: cuisine, topic: baking, kind: photo}
	ctx := context.Background()
	yes := assetstore.Flag(true)
	webp := func(n int) []byte { return append([]byte("RIFF\x00\x00\x00\x00WEBPVP8 "), make([]byte, n)...) }
	doc := func(id, status string, required bool) assetstore.Doc {
		return assetstore.Doc{ID: id, Kind: "photo", WorldID: "cuisine", GradeBand: "all", Mood: "joyful", DominantColor: "#aa8844", LQIP: "data:image/webp;base64,AAAA",
			Width: 960, Height: 540, AltFR: "Une plaque de biscuits", AltEN: "A tray of cookies", SourceName: "pexels", SourceURL: "https://example.test/p", Creator: "Jane Doe",
			Licence: "PEXELS", LicenceURL: "https://www.pexels.com/license/", AttributionText: "Photo by Jane Doe on Pexels", AttributionRequired: assetstore.Flag(required),
			PeopleFree: &yes, BrandFree: &yes, ReviewStatus: status, CreatorURL: "https://example.test/jane",
			Tags: assetstore.TagList{"world": {"cuisine"}, "topic": {"baking"}, "object": {"cookie"}}}
	}
	good, pending := "a_00000000000000000001", "a_00000000000000000002"
	err := f.s.Store.WithWriteTx(ctx, func(tx *sql.Tx) error {
		if _, _, err := f.s.Assets.UpsertDoc(ctx, tx, doc(good, "approved", true), map[int][]byte{480: webp(10), 960: webp(20)}); err != nil {
			return err
		}
		_, _, err := f.s.Assets.UpsertDoc(ctx, tx, doc(pending, "pending", true), map[int][]byte{480: webp(5)})
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	get := func(path string, hdr map[string]string) *httptest.ResponseRecorder {
		req := httptest.NewRequest("GET", path, nil)
		for k, v := range hdr {
			req.Header.Set(k, v)
		}
		rec := httptest.NewRecorder()
		f.h.ServeHTTP(rec, req)
		return rec
	}
	rec := get("/v1/assets/"+good+"-960.webp", nil) // public: no token
	if rec.Code != 200 || rec.Header().Get("Content-Type") != "image/webp" || rec.Header().Get("Cache-Control") != "public, max-age=31536000, immutable" ||
		rec.Header().Get("ETag") != `"`+good+`-960"` || rec.Body.Len() != len(webp(20)) {
		t.Fatalf("asset: %d %v len=%d", rec.Code, rec.Header(), rec.Body.Len())
	}
	if rec := get("/v1/assets/"+good+"-960.webp", map[string]string{"If-None-Match": `"` + good + `-960"`}); rec.Code != 304 || rec.Body.Len() != 0 || rec.Header().Get("ETag") == "" {
		t.Fatalf("conditional request: %d len=%d", rec.Code, rec.Body.Len())
	}
	if rec := get("/v1/assets/"+good+".webp", nil); rec.Code != 200 || rec.Header().Get("ETag") != `"`+good+`-960"` {
		t.Fatalf("widest variant: %d %v", rec.Code, rec.Header())
	}
	for _, path := range []string{"/v1/assets/" + good + "-777.webp", "/v1/assets/" + pending + "-480.webp", "/v1/assets/a_ffffffffffffffffffff.webp", "/v1/assets/not-an-id.webp", "/v1/assets/" + good + ".png"} {
		if rec := get(path, nil); rec.Code != 404 {
			t.Errorf("%s: want 404, got %d", path, rec.Code)
		}
	}
	// credits list only approved assets that need attribution
	rec = get("/v1/credits", nil)
	var credits struct{ Items []map[string]any }
	_ = json.Unmarshal(rec.Body.Bytes(), &credits)
	if rec.Code != 200 || len(credits.Items) != 1 || credits.Items[0]["id"] != good || credits.Items[0]["text"] != "Photo by Jane Doe on Pexels" {
		t.Fatalf("credits: %d %s", rec.Code, rec.Body.String())
	}
	// the world hero shows up in /worlds once set; the question media is resolved from criteria, same pick every time
	if err := f.s.Store.WithWriteTx(ctx, func(tx *sql.Tx) error {
		w, _ := f.s.Content.Worlds(ctx, f.s.Store.R)
		for _, x := range w {
			if x.ID == "cuisine" {
				x.HeroAsset = good
				_, err := f.s.Content.UpsertWorld(ctx, tx, x.World, "test")
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	f.s.Content.AfterCommit()
	_, w, _ := f.gcall(t, "GET", "/v1/worlds", f.childTok, "", nil)
	var cuisine map[string]any
	for _, it := range w["items"].([]any) {
		if it.(map[string]any)["id"] == "cuisine" {
			cuisine = it.(map[string]any)
		}
	}
	hero, _ := cuisine["hero"].(map[string]any)
	if hero == nil || hero["id"] != good || cuisine["image_url"] != "/assets/"+good+"-960.webp" {
		t.Fatalf("world hero: %v", cuisine)
	}
	sid := f.startIn(t, "cuisine", "ARI-OPE-A02c")
	beat, _ := f.nextBeat(t, sid)
	media := beat["question"].(map[string]any)["media"].([]any)
	if len(media) != 1 || media[0].(map[string]any)["image"].(map[string]any)["id"] != good {
		t.Fatalf("question media: %v", media)
	}
	if ill := beat["scene"].(map[string]any)["illustration_url"]; ill != "/assets/"+good+"-960.webp" {
		t.Fatalf("scene illustration: %v", ill)
	}
}
