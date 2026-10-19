package adminapi

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Perrin-Nj/numerix-api/internal/assetstore"
	"github.com/Perrin-Nj/numerix-api/internal/contentdb"
	"github.com/Perrin-Nj/numerix-api/internal/drafter"
	"github.com/Perrin-Nj/numerix-api/internal/engine"
	"github.com/Perrin-Nj/numerix-api/internal/mongosync"
	"github.com/Perrin-Nj/numerix-api/internal/seedload"
	"github.com/Perrin-Nj/numerix-api/internal/store"
)

// stubDrafter is a fake AI provider: it returns the JSON it was built with.
type stubDrafter struct {
	reply json.RawMessage
	calls int
}

func (s *stubDrafter) Name() string  { return "stub" }
func (s *stubDrafter) Model() string { return "stub-model" }
func (s *stubDrafter) Draft(context.Context, drafter.Request) (json.RawMessage, error) {
	s.calls++
	return s.reply, nil
}

// content test environment: a database with worlds, quests and misconception codes loaded from the real seed.
type env struct {
	h    http.Handler
	db   *store.Store
	repo *contentdb.Repo
}

func setupContent(t *testing.T, dr drafter.Provider) env {
	t.Helper()
	db, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "c.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	repo := contentdb.New(db, mongosync.NewOutbox(true))
	g, err := engine.LoadGraph()
	if err != nil {
		t.Fatal(err)
	}
	files, err := seedload.Read("../../seed", seedload.ReadOptions{SkipAssets: true, Only: []string{seedload.ExamplesFile}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := seedload.Apply(context.Background(), repo, assetstore.New(db), g, files, seedload.Options{Actor: "test", Only: []string{"worlds", "quests"}}); err != nil {
		t.Fatal(err)
	}
	h := New(Deps{Store: db, Log: slog.New(slog.NewTextHandler(io.Discard, nil)), Token: tok, Content: repo, Assets: assetstore.New(db), Drafter: dr, Now: time.Now})
	return env{h: h, db: db, repo: repo}
}

// goodQuestion is a valid numeric question body for the quest cuisine.fournee.
func goodQuestion() string {
	return `{"quest":"cuisine.fournee","skill":"ARI-OPE-A02c","type":"numeric","grade":2,"difficulty":2,
	"story":{"fr":"Léa sort deux plaques de biscuits du four.","en":"Lea takes two trays of cookies out of the oven."},
	"prompt":{"fr":"Il y a 8 biscuits sur la première plaque et 7 sur la deuxième. Combien de biscuits en tout ?","en":"There are 8 cookies on the first tray and 7 on the second. How many cookies in all?"},
	"math_text":"8 + 7 = ?","answer":{"kind":"numeric","value":15,"calc":"8+7"},
	"common_errors":[{"value":14,"misconception":"mis_off_by_one","feedback":{"fr":"Presque ! Compte encore.","en":"Close! Count again."}}],
	"hint":{"fr":"Pars de 8 et avance de 7 pas.","en":"Start at 8 and hop 7 steps."},
	"explain":{"fr":"8 + 7 = 8 + 2 + 5, donc 10 + 5.","en":"8 + 7 = 8 + 2 + 5, so 10 + 5."},
	"tags":["cookies","add-within-20"],"status":"published","author":"somebody else","version":9}`
}

// create posts goodQuestion and returns the created question id.
func create(t *testing.T, e env, body string, hdr ...string) string {
	t.Helper()
	code, m, _ := call(e.h, "POST", "/admin/v1/questions", body, tok, hdr...)
	if code != 201 {
		t.Fatalf("create: %d %v", code, m)
	}
	return m["id"].(string)
}

func TestQuestionLifecycle(t *testing.T) {
	e := setupContent(t, nil)
	if code, _, _ := call(e.h, "GET", "/admin/v1/questions", "", ""); code != 401 {
		t.Fatalf("no token must be 401, got %d", code)
	}
	// create: always a draft, authored by the caller, whatever the body claims
	code, m, _ := call(e.h, "POST", "/admin/v1/questions", goodQuestion(), tok, "X-Admin-Actor", "marie")
	if code != 201 || m["status"] != "draft" || m["id"] != "cuisine.fournee.001" || m["version"] != 1.0 || m["world"] != "cuisine" {
		t.Fatalf("create: %d %v", code, m)
	}
	q := m["question"].(map[string]any)
	if q["author"] != "marie" || q["source"] != "admin" || q["status"] != "draft" {
		t.Fatalf("server owns author, source and status: %v", q)
	}
	id := m["id"].(string)
	if id2 := create(t, e, goodQuestion()); id2 != "cuisine.fournee.002" {
		t.Fatalf("second id = %s", id2)
	}
	// read with versions
	code, m, _ = call(e.h, "GET", "/admin/v1/questions/"+id, "", tok)
	if code != 200 || len(m["versions"].([]any)) != 1 || len(m["issues"].([]any)) != 0 {
		t.Fatalf("get: %d %v", code, m)
	}
	// edit with If-Match: version 2; a stale version conflicts
	edited := strings.Replace(goodQuestion(), "Il y a 8 biscuits", "Voici 8 biscuits", 1)
	code, m, _ = call(e.h, "PUT", "/admin/v1/questions/"+id, edited, tok, "If-Match", `"1"`)
	if code != 200 || m["version"] != 2.0 {
		t.Fatalf("put: %d %v", code, m)
	}
	if code, m, _ := call(e.h, "PUT", "/admin/v1/questions/"+id, edited, tok, "If-Match", "1"); code != 409 || m["code"] != "version_conflict" {
		t.Fatalf("stale put: %d %v", code, m)
	}
	// an unchanged body does not create a version
	if _, m, _ := call(e.h, "PUT", "/admin/v1/questions/"+id, edited, tok); m["version"] != 2.0 {
		t.Fatalf("no-op put bumped the version: %v", m["version"])
	}
	// draft -> review -> published, then an edit sends it back to review
	if code, m, _ := call(e.h, "POST", "/admin/v1/questions/"+id+"/transition", `{"to":"review","comment":"ready"}`, tok); code != 200 || m["status"] != "review" {
		t.Fatalf("to review: %d %v", code, m)
	}
	if code, m, _ := call(e.h, "POST", "/admin/v1/questions/"+id+"/transition", `{"to":"published"}`, tok, "X-Admin-Actor", "sam"); code != 200 || m["status"] != "published" || m["reviewed_by"] != "sam" || m["published_at"] == "" {
		t.Fatalf("publish: %d %v", code, m)
	}
	if code, m, _ := call(e.h, "PUT", "/admin/v1/questions/"+id, strings.Replace(edited, "tags", "tags", 1), tok); code != 200 {
		t.Fatalf("edit of a published question: %d %v", code, m)
	}
	code, m, _ = call(e.h, "PUT", "/admin/v1/questions/"+id, strings.Replace(edited, `"difficulty":2`, `"difficulty":3`, 1), tok)
	if code != 200 || m["status"] != "review" {
		t.Fatalf("an edited published question returns to review: %d %v", code, m)
	}
	// moves that the status machine refuses
	if code, m, _ := call(e.h, "POST", "/admin/v1/questions/"+id+"/transition", `{"to":"nonsense"}`, tok); code != 409 || m["code"] != "transition_not_allowed" {
		t.Fatalf("bad move: %d %v", code, m)
	}
	// retire, then restore an old version as a fresh draft
	call(e.h, "POST", "/admin/v1/questions/"+id+"/transition", `{"to":"retired"}`, tok)
	if code, _, _ := call(e.h, "PUT", "/admin/v1/questions/"+id, edited, tok); code != 409 {
		t.Fatalf("a retired question cannot be edited: %d", code)
	}
	code, m, _ = call(e.h, "POST", "/admin/v1/questions/"+id+"/restore", `{"version":1}`, tok)
	if code != 200 || m["status"] != "draft" || m["question"].(map[string]any)["prompt"].(map[string]any)["fr"].(string)[:6] != "Il y a" {
		t.Fatalf("restore: %d %v", code, m)
	}
	code, m, _ = call(e.h, "GET", "/admin/v1/questions/"+id+"/versions", "", tok)
	if code != 200 || len(m["items"].([]any)) < 7 {
		t.Fatalf("history: %d %v", code, m)
	}
	if code, m, _ := call(e.h, "GET", "/admin/v1/questions/"+id+"/versions/1", "", tok); code != 200 || m["question"] == nil {
		t.Fatalf("version 1: %d %v", code, m)
	}
	if code, _, _ := call(e.h, "GET", "/admin/v1/questions/nope.nope.001", "", tok); code != 404 {
		t.Fatalf("unknown id: %d", code)
	}
	// every change is audited twice (question_audit and admin_audit) and queued for Mongo
	var qa, aa, ob int
	_ = e.db.R.QueryRow(`SELECT COUNT(*) FROM question_audit WHERE question_id = ?`, id).Scan(&qa)
	_ = e.db.R.QueryRow(`SELECT COUNT(*) FROM admin_audit WHERE target_type = 'question' AND target_id = ?`, id).Scan(&aa)
	_ = e.db.R.QueryRow(`SELECT COUNT(*) FROM outbox WHERE kind = ? AND key = ?`, mongosync.KindQuestionBank, id).Scan(&ob)
	if qa != aa || qa < 7 || ob < 7 {
		t.Fatalf("audit rows: question_audit=%d admin_audit=%d outbox=%d", qa, aa, ob)
	}
	if _, err := e.db.W.Exec(`UPDATE question_audit SET actor = 'x'`); err == nil {
		t.Fatal("question_audit must be append-only")
	}
	if _, err := e.db.W.Exec(`DELETE FROM question_versions`); err == nil {
		t.Fatal("question_versions must be append-only")
	}
}

func TestInvalidQuestionCannotEnterReviewOrPublish(t *testing.T) {
	e := setupContent(t, nil)
	bad := strings.Replace(goodQuestion(), `"calc":"8+7"`, `"calc":"8+9"`, 1)
	id := create(t, e, bad)
	code, m, _ := call(e.h, "GET", "/admin/v1/questions/"+id, "", tok)
	issues := m["issues"].([]any)
	if code != 200 || len(issues) == 0 || issues[0].(map[string]any)["code"] != "calc_mismatch" {
		t.Fatalf("issues on read: %v", issues)
	}
	for _, to := range []string{"review", "published"} {
		code, m, _ := call(e.h, "POST", "/admin/v1/questions/"+id+"/transition", `{"to":"`+to+`"}`, tok)
		if code != 422 || m["code"] != "validation_failed" || !strings.Contains(fmt.Sprint(m["errors"]), "calc_mismatch") {
			t.Fatalf("to %s: %d %v", to, code, m)
		}
	}
	// the validate endpoint returns the same report without saving
	code, m, _ = call(e.h, "POST", "/admin/v1/questions/validate", bad, tok)
	if code != 200 || m["ok"] != false {
		t.Fatalf("validate: %d %v", code, m)
	}
	code, m, _ = call(e.h, "POST", "/admin/v1/questions/validate", goodQuestion(), tok)
	if code != 200 || m["ok"] != true {
		t.Fatalf("validate good: %d %v", code, m)
	}
	// unknown fields and unknown types are refused with a readable error
	if code, _, _ := call(e.h, "POST", "/admin/v1/questions", `{"timer":5}`, tok); code != 422 {
		t.Fatalf("unknown field: %d", code)
	}
	if code, _, _ := call(e.h, "POST", "/admin/v1/questions", strings.Replace(goodQuestion(), `"type":"numeric"`, `"type":"poem"`, 1), tok); code != 422 {
		t.Fatalf("unknown type: %d", code)
	}
	if code, _, _ := call(e.h, "POST", "/admin/v1/questions", strings.Replace(goodQuestion(), `"quest":"cuisine.fournee"`, `"quest":"nope.nope"`, 1), tok); code != 422 {
		t.Fatalf("unknown quest: %d", code)
	}
	if code, _, _ := call(e.h, "POST", "/admin/v1/questions", strings.Replace(goodQuestion(), `"skill":"ARI-OPE-A02c"`, `"skill":"ARI-OPE-A02a"`, 1), tok); code != 422 {
		t.Fatalf("skill that differs from the quest: %d", code)
	}
	// validate-all reports the failing question
	code, m, _ = call(e.h, "POST", "/admin/v1/questions/validate-all", "", tok)
	if code != 200 || m["total"] != 1.0 || m["with_errors"] != 1.0 {
		t.Fatalf("validate-all: %d %v", code, m)
	}
}

func TestListFiltersAndPaging(t *testing.T) {
	e := setupContent(t, nil)
	for i := 0; i < 3; i++ {
		create(t, e, goodQuestion())
	}
	id := create(t, e, strings.Replace(goodQuestion(), "Léa", "Zéphyrine", 1))
	call(e.h, "POST", "/admin/v1/questions/"+id+"/transition", `{"to":"review"}`, tok)
	check := func(query string, want float64) {
		t.Helper()
		code, m, _ := call(e.h, "GET", "/admin/v1/questions?"+query, "", tok)
		if code != 200 || m["total"] != want {
			t.Fatalf("%s: %d total=%v want %v", query, code, m["total"], want)
		}
	}
	check("", 4)
	check("status=review", 1)
	check("status=draft&world=cuisine", 3)
	check("world=sport", 0)
	check("type=numeric&grade=2&difficulty=2", 4)
	check("quest=cuisine.fournee&skill=ARI-OPE-A02c", 4)
	check("q=zéphyrine", 1)
	check("tag=cookies", 4)
	check("ai=1", 0)
	check("author=admin", 4)
	code, m, _ := call(e.h, "GET", "/admin/v1/questions?page_size=2&page=2", "", tok)
	if code != 200 || len(m["items"].([]any)) != 2 || m["total"] != 4.0 || m["page"] != 2.0 {
		t.Fatalf("paging: %v", m)
	}
}

func TestExportImportRoundTrip(t *testing.T) {
	e := setupContent(t, nil)
	create(t, e, goodQuestion())
	id2 := create(t, e, goodQuestion())
	call(e.h, "POST", "/admin/v1/questions/"+id2+"/transition", `{"to":"published"}`, tok, "X-Admin-Actor", "sam")
	code, exported, _ := call(e.h, "GET", "/admin/v1/questions/export", "", tok)
	if code != 200 || len(exported["questions"].([]any)) != 2 {
		t.Fatalf("export: %d %v", code, exported)
	}
	body, _ := json.Marshal(exported)
	// export then import is a no-op
	code, rep, _ := call(e.h, "POST", "/admin/v1/questions/import?dry_run=1", string(body), tok)
	if code != 200 || rep["unchanged"] != 2.0 || rep["added"] != 0.0 || rep["changed"] != 0.0 {
		t.Fatalf("round trip dry run: %d %v", code, rep)
	}
	// a changed question and a new one, dry run first: nothing is written
	edited := strings.ReplaceAll(string(body), "Il y a 8 biscuits", "Voici 8 biscuits")
	code, rep, _ = call(e.h, "POST", "/admin/v1/questions/import?dry_run=1", edited, tok)
	if code != 200 || rep["changed"] != 2.0 {
		t.Fatalf("changed dry run: %d %v", code, rep)
	}
	var n int
	_ = e.db.R.QueryRow(`SELECT COUNT(*) FROM question_versions`).Scan(&n)
	if n != 3 { // 2 creates + 1 publish
		t.Fatalf("dry run wrote versions: %d", n)
	}
	// apply: changed questions come back as drafts for review (publish=1 would keep review/published when valid)
	code, rep, _ = call(e.h, "POST", "/admin/v1/questions/import", edited, tok)
	if code != 200 || rep["changed"] != 2.0 {
		t.Fatalf("apply: %d %v", code, rep)
	}
	_, m, _ := call(e.h, "GET", "/admin/v1/questions/"+id2, "", tok)
	if m["status"] != "draft" {
		t.Fatalf("an imported change returns to draft: %v", m["status"])
	}
	// invalid items are reported, valid ones still load
	mixed := `{"questions":[` + strings.Replace(goodQuestion(), `"calc":"8+7"`, `"calc":"1+1"`, 1) + `]}`
	mixed = strings.Replace(mixed, `"tags"`, `"id":"cuisine.fournee.050","tags"`, 1)
	code, rep, _ = call(e.h, "POST", "/admin/v1/questions/import", mixed, tok)
	if code != 200 || rep["added"] != 1.0 {
		t.Fatalf("a draft with problems is still imported as a draft: %d %v", code, rep)
	}
	if code, _, _ := call(e.h, "POST", "/admin/v1/questions/import", `"nope"`, tok); code != 422 {
		t.Fatalf("garbage import: %d", code)
	}
}

func TestAIDraftNotConfiguredAndRules(t *testing.T) {
	// without a provider: a clear 503 and nothing is created
	e := setupContent(t, nil)
	code, m, _ := call(e.h, "POST", "/admin/v1/questions/ai-draft", `{"quest":"cuisine.fournee","type":"numeric"}`, tok)
	if code != 503 || m["code"] != "not_configured" {
		t.Fatalf("not configured: %d %v", code, m)
	}
	if _, h, _ := call(e.h, "GET", "/admin/v1/health", "", tok); h["ai"] != false {
		t.Fatalf("health must report ai:false: %v", h)
	}
	var n int
	_ = e.db.R.QueryRow(`SELECT COUNT(*) FROM bank_questions`).Scan(&n)
	if n != 0 {
		t.Fatal("nothing may be created without a provider")
	}

	// with a stub: the model claims 99, the server recomputes calc 8+7 and stores 15, as a draft nobody has reviewed
	stub := &stubDrafter{reply: json.RawMessage(strings.Replace(goodQuestion(), `"value":15`, `"value":99`, 1))}
	e = setupContent(t, stub)
	if _, h, _ := call(e.h, "GET", "/admin/v1/health", "", tok); h["ai"] != true {
		t.Fatalf("health must report ai:true: %v", h)
	}
	code, m, _ = call(e.h, "POST", "/admin/v1/questions/ai-draft", `{"quest":"cuisine.fournee","type":"numeric","grade":2,"difficulty":3,"topic_hint":"cookies"}`, tok, "X-Admin-Actor", "marie")
	if code != 201 || stub.calls != 1 {
		t.Fatalf("draft: %d %v", code, m)
	}
	v := m["question"].(map[string]any)
	q := v["question"].(map[string]any)
	if v["status"] != "draft" || v["ai_drafted"] != true || q["author"] != "ai:stub-model" || q["source"] != "ai-draft" || q["difficulty"] != 3.0 {
		t.Fatalf("draft flags: %v", v)
	}
	if string(mustJSON(q["answer"].(map[string]any)["value"])) != "15" || len(m["calc_corrections"].([]any)) != 1 || m["unverified"] != false {
		t.Fatalf("the server must verify the number: %v", m)
	}
	id := v["id"].(string)
	// publishing an AI draft: not by its own author, and the SQL CHECK backs it up
	if code, m, _ := call(e.h, "POST", "/admin/v1/questions/"+id+"/transition", `{"to":"published"}`, tok, "X-Admin-Actor", "ai:stub-model"); code != 409 || m["code"] != "needs_reviewer" {
		t.Fatalf("self publish: %d %v", code, m)
	}
	if _, err := e.db.W.Exec(`UPDATE bank_questions SET status = 'published' WHERE id = ?`, id); err == nil {
		t.Fatal("the database must refuse a published AI draft without a reviewer")
	}
	code, m, _ = call(e.h, "POST", "/admin/v1/questions/"+id+"/transition", `{"to":"published"}`, tok, "X-Admin-Actor", "sam")
	if code != 200 || m["status"] != "published" || m["reviewed_by"] != "sam" || m["ai_drafted"] != true {
		t.Fatalf("reviewed publish: %d %v", code, m)
	}
	// a body that does not follow the format, and a draft without calc, are reported clearly
	stub.reply = json.RawMessage(`{"surprise":1}`)
	if code, m, _ := call(e.h, "POST", "/admin/v1/questions/ai-draft", `{"quest":"cuisine.fournee","type":"numeric"}`, tok); code != 422 || m["code"] != "draft_unreadable" {
		t.Fatalf("unreadable draft: %d %v", code, m)
	}
	stub.reply = json.RawMessage(strings.Replace(goodQuestion(), `,"calc":"8+7"`, ``, 1))
	code, m, _ = call(e.h, "POST", "/admin/v1/questions/ai-draft", `{"quest":"cuisine.fournee","type":"numeric"}`, tok)
	if code != 201 || m["unverified"] != true {
		t.Fatalf("a draft without calc is flagged unverified: %d %v", code, m)
	}
	// validation of the request
	for _, body := range []string{`{"quest":"nope","type":"numeric"}`, `{"quest":"cuisine.fournee","type":"poem"}`, `{"quest":"cuisine.fournee","type":"numeric","grade":6}`, `{"quest":"cuisine.fournee","type":"numeric","difficulty":9}`} {
		if code, _, _ := call(e.h, "POST", "/admin/v1/questions/ai-draft", body, tok); code != 422 {
			t.Fatalf("%s: %d", body, code)
		}
	}
}

func mustJSON(v any) []byte {
	b, _ := json.Marshal(v)
	return b
}

func TestAIDraftIsLimitedPerAdmin(t *testing.T) {
	var l draftLimiter
	now := time.Now()
	for i := 0; i < draftsPerHour; i++ {
		if ok, _ := l.allow("marie", now); !ok {
			t.Fatalf("draft %d should pass", i)
		}
	}
	if ok, wait := l.allow("marie", now); ok || wait < 1 {
		t.Fatalf("the 31st draft in an hour must wait, got ok=%v wait=%d", ok, wait)
	}
	if ok, _ := l.allow("sam", now); !ok {
		t.Fatal("the limit is per admin")
	}
	if ok, _ := l.allow("marie", now.Add(61*time.Minute)); !ok {
		t.Fatal("the window slides")
	}
}

func TestWorldsQuestsCoverageAndMongoDocs(t *testing.T) {
	e := setupContent(t, nil)
	code, m, _ := call(e.h, "GET", "/admin/v1/worlds", "", tok)
	if code != 200 || len(m["items"].([]any)) != 20 {
		t.Fatalf("worlds: %d %v", code, m)
	}
	code, m, _ = call(e.h, "GET", "/admin/v1/quests?world=cuisine", "", tok)
	if code != 200 || len(m["items"].([]any)) != 5 {
		t.Fatalf("quests: %d %v", code, m)
	}
	code, m, _ = call(e.h, "PUT", "/admin/v1/worlds/cuisine", `{"hook":{"fr":"Un festin à préparer","en":"A feast to cook"}}`, tok)
	if code != 200 || m["hook"].(map[string]any)["fr"] != "Un festin à préparer" || m["name"].(map[string]any)["en"] != "Cooking" {
		t.Fatalf("put world: %d %v", code, m)
	}
	if code, _, _ := call(e.h, "PUT", "/admin/v1/worlds/cuisine", `{"accent_color":"red"}`, tok); code != 422 {
		t.Fatalf("bad colour: %d", code)
	}
	if code, _, _ := call(e.h, "PUT", "/admin/v1/worlds/atlantis", `{}`, tok); code != 404 {
		t.Fatalf("unknown world: %d", code)
	}
	code, m, _ = call(e.h, "PUT", "/admin/v1/quests/cuisine.fournee", `{"target_count":14}`, tok)
	if code != 200 || m["target_count"] != 14.0 {
		t.Fatalf("put quest: %d %v", code, m)
	}
	id := create(t, e, goodQuestion())
	call(e.h, "POST", "/admin/v1/questions/"+id+"/transition", `{"to":"published"}`, tok)
	code, m, _ = call(e.h, "GET", "/admin/v1/content/coverage", "", tok)
	if code != 200 {
		t.Fatalf("coverage: %d", code)
	}
	var found bool
	for _, c := range m["quests"].([]any) {
		cm := c.(map[string]any)
		if cm["quest"] == "cuisine.fournee" {
			found = cm["target_count"] == 14.0 && cm["by_status"].(map[string]any)["published"] == 1.0 && cm["by_type"].(map[string]any)["numeric"] == 1.0
		}
	}
	if !found {
		t.Fatalf("coverage of the quest: %v", m["quests"])
	}
	// the Mongo documents are built from the current state
	ctx := context.Background()
	op, ok, err := e.repo.ResolveEvent(ctx, mongosync.Event{Kind: mongosync.KindQuestionBank, Key: id})
	if err != nil || !ok || op.Delete || op.Doc["status"] != "published" || op.Doc["world"] != "cuisine" || op.Doc["question"].(map[string]any)["id"] != id {
		t.Fatalf("question_bank doc: %v %v %v", op, ok, err)
	}
	var auditID string
	_ = e.db.R.QueryRow(`SELECT id FROM question_audit WHERE question_id = ? ORDER BY at DESC LIMIT 1`, id).Scan(&auditID)
	if op, _, err := e.repo.ResolveEvent(ctx, mongosync.Event{Kind: mongosync.KindQuestionAudit, Key: auditID}); err != nil || op.Doc["question_id"] != id {
		t.Fatalf("question_audit doc: %v %v", op, err)
	}
	if op, _, err := e.repo.ResolveEvent(ctx, mongosync.Event{Kind: mongosync.KindContentWorld, Key: "cuisine"}); err != nil || op.Doc["name"].(map[string]string)["en"] != "Cooking" {
		t.Fatalf("content_worlds doc: %v %v", op, err)
	}
	if op, _, err := e.repo.ResolveEvent(ctx, mongosync.Event{Kind: mongosync.KindContentQuest, Key: "cuisine.fournee"}); err != nil || op.Doc["target_count"] != 14 {
		t.Fatalf("content_quests doc: %v %v", op, err)
	}
	if op, _, err := e.repo.ResolveEvent(ctx, mongosync.Event{Kind: mongosync.KindQuestionBank, Key: "gone.gone.001"}); err != nil || !op.Delete {
		t.Fatalf("a missing question becomes a delete: %v %v", op, err)
	}
	// below 5 children the statistics document is deleted, and it never holds a child id
	if op, _, err := e.repo.ResolveEvent(ctx, mongosync.Event{Kind: mongosync.KindQuestionStats, Key: id}); err != nil || !op.Delete {
		t.Fatalf("stats under the floor: %v %v", op, err)
	}
	if _, ok, _ := e.repo.ResolveEvent(ctx, mongosync.Event{Kind: "personal_bests", Key: "x"}); ok {
		t.Fatal("a non content kind is not handled here")
	}
}

func TestStatsFloorAndShape(t *testing.T) {
	under := contentdb.StatsOp("q", 4, 20, 10, 5, nil, 1200, nil)
	if !under.Delete {
		t.Fatal("4 children is below the floor")
	}
	op := contentdb.StatsOp("q", 5, 20, 10, 5, []map[string]any{{"code": "mis_x", "count": 5}}, 1200, map[string]any{"world": "cuisine"})
	if op.Delete || op.Doc["correct_rate"] != 0.5 || op.Doc["typical_error_rate"] != 0.25 || op.Doc["median_response_ms"] != 1200 || op.Doc["world"] != "cuisine" {
		t.Fatalf("stats doc: %v", op.Doc)
	}
	for k := range op.Doc {
		if strings.Contains(k, "child") && k != "children" {
			t.Fatalf("stats must not carry a child field: %s", k)
		}
	}
}

func TestAssetAdminEndpoints(t *testing.T) {
	e := setupContent(t, nil)
	ctx := context.Background()
	yes := assetstore.Flag(true)
	as := assetstore.New(e.db)
	doc := assetstore.Doc{ID: "a_00000000000000000009", Kind: "photo", WorldID: "cuisine", GradeBand: "all", Mood: "joyful", DominantColor: "#aa8844", LQIP: "data:image/webp;base64,AAAA",
		Width: 960, Height: 540, AltFR: "", AltEN: "A tray of cookies", SourceName: "pexels", SourceURL: "https://example.test/p", Creator: "Jane Doe", Licence: "CC0",
		LicenceURL: "https://example.test/cc0", AttributionText: "CC0", PeopleFree: &yes, BrandFree: &yes, ReviewStatus: "pending",
		Tags: assetstore.TagList{"world": {"cuisine"}, "topic": {"baking"}, "object": {"cookie"}}}
	if err := e.db.WithWriteTx(ctx, func(tx *sql.Tx) error {
		_, _, err := as.UpsertDoc(ctx, tx, doc, map[int][]byte{480: []byte("RIFF....WEBPx")})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	id := doc.ID
	code, m, _ := call(e.h, "GET", "/admin/v1/assets?status=pending&facet=topic&value=baking", "", tok)
	if code != 200 || m["total"] != 1.0 {
		t.Fatalf("list: %d %v", code, m)
	}
	// approval needs alt text in both languages
	if code, m, _ := call(e.h, "PATCH", "/admin/v1/assets/"+id, `{"review_status":"approved"}`, tok); code != 422 {
		t.Fatalf("approve without alt_fr: %d %v", code, m)
	}
	code, m, _ = call(e.h, "PATCH", "/admin/v1/assets/"+id, `{"alt_fr":"Une plaque de biscuits","review_status":"approved","tags":{"topic":["baking","tray"],"object":["cookie"]}}`, tok, "X-Admin-Actor", "sam")
	if code != 200 || m["review_status"] != "approved" || m["reviewed_by"] != "sam" || len(m["tags"].(map[string]any)["topic"].([]any)) != 2 {
		t.Fatalf("approve: %d %v", code, m)
	}
	if code, _, _ := call(e.h, "PATCH", "/admin/v1/assets/"+id, `{"tags":{"colour":["red"]}}`, tok); code != 422 {
		t.Fatalf("unknown facet: %d", code)
	}
	if code, _, _ := call(e.h, "PATCH", "/admin/v1/assets/a_nope", `{}`, tok); code != 404 {
		t.Fatalf("unknown asset: %d", code)
	}
	code, m, _ = call(e.h, "GET", "/admin/v1/assets/search?world=cuisine&topic=baking&object=cookie&kind=photo", "", tok)
	items := m["items"].([]any)
	if code != 200 || len(items) != 1 || items[0].(map[string]any)["score"] != 12.0 { // object 5 + topic 4 + world 3
		t.Fatalf("search: %d %v", code, m)
	}
	code, m, _ = call(e.h, "GET", "/admin/v1/assets/resolve?world=cuisine&topic=baking&kind=photo&grade=2&child=c1&question=q1", "", tok)
	if code != 200 || m["asset"].(map[string]any)["id"] != id {
		t.Fatalf("resolve: %d %v", code, m)
	}
	code, m, _ = call(e.h, "GET", "/admin/v1/assets/resolve?world=sport&topic=astronomy", "", tok)
	if code != 200 || m["asset"] != nil || m["fallback"] != "gradient" {
		t.Fatalf("resolve with nothing: %d %v", code, m)
	}
	if code, m, _ := call(e.h, "POST", "/admin/v1/assets", `{}`, tok); code != 501 || m["code"] != "not_implemented" {
		t.Fatalf("upload: %d %v", code, m)
	}
	if code, m, _ := call(e.h, "GET", "/admin/v1/assets/"+id, "", tok); code != 200 || m["media"].(map[string]any)["id"] != id {
		t.Fatalf("get: %d %v", code, m)
	}
}
