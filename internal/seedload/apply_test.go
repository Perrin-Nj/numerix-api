package seedload

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/Perrin-Nj/numerix-api/internal/assetstore"
	"github.com/Perrin-Nj/numerix-api/internal/content"
	"github.com/Perrin-Nj/numerix-api/internal/contentdb"
	"github.com/Perrin-Nj/numerix-api/internal/engine"
	"github.com/Perrin-Nj/numerix-api/internal/mongosync"
	"github.com/Perrin-Nj/numerix-api/internal/store"
)

// fixture opens a fresh database with the Mongo outbox enabled and loads the real seed directory.
type fixture struct {
	ctx    context.Context
	db     *store.Store
	repo   *contentdb.Repo
	assets *assetstore.Store
	g      *engine.Graph
	f      *Files
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	ctx := context.Background()
	db, err := store.Open(ctx, filepath.Join(t.TempDir(), "seed.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	g, err := engine.LoadGraph()
	if err != nil {
		t.Fatal(err)
	}
	f, err := Read(seedDir, ReadOptions{SkipAssets: true})
	if err != nil {
		t.Fatal(err)
	}
	return &fixture{ctx: ctx, db: db, repo: contentdb.New(db, mongosync.NewOutbox(true)), assets: assetstore.New(db), g: g, f: f}
}

func (x *fixture) count(t *testing.T, query string, args ...any) int {
	t.Helper()
	var n int
	if err := x.db.R.QueryRowContext(x.ctx, query, args...).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func (x *fixture) apply(t *testing.T, opt Options) Report {
	t.Helper()
	rep, err := Apply(x.ctx, x.repo, x.assets, x.g, x.f, opt)
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	return rep
}

func TestApplyIsIdempotentAndDryRunWritesNothing(t *testing.T) {
	x := newFixture(t)
	nQuestions := len(x.f.Questions())

	dry := x.apply(t, Options{DryRun: true, Actor: "test"})
	if dry.Questions.Added != nQuestions || dry.Quests.Added != 89 || dry.Worlds.Added != 0 {
		t.Fatalf("dry run counts: %+v", dry)
	}
	if n := x.count(t, `SELECT COUNT(*) FROM bank_questions`); n != 0 {
		t.Fatalf("dry run wrote %d questions", n)
	}
	if n := x.count(t, `SELECT COUNT(*) FROM seed_runs`); n != 0 {
		t.Fatalf("dry run wrote a seed_runs row")
	}
	if n := x.count(t, `SELECT COUNT(*) FROM outbox`); n != 0 {
		t.Fatalf("dry run left %d outbox rows", n)
	}

	first := x.apply(t, Options{Actor: "test", Strict: false})
	if first.Questions.Added != nQuestions || first.Quests.Added != 89 {
		t.Fatalf("first run: %+v", first)
	}
	if first.Worlds.Changed != 20 { // the migration inserted 20 rows with an empty hash; the first run refreshes them
		t.Fatalf("worlds changed = %d, want 20", first.Worlds.Changed)
	}
	second := x.apply(t, Options{Actor: "test"})
	for name, c := range map[string]Counts{"worlds": second.Worlds, "quests": second.Quests, "questions": second.Questions, "assets": second.Assets} {
		if c.Added != 0 || c.Changed != 0 {
			t.Errorf("second run %s changed something: %+v", name, c)
		}
	}
	if second.Questions.Unchanged != nQuestions {
		t.Errorf("unchanged = %d, want %d", second.Questions.Unchanged, nQuestions)
	}
	if n := x.count(t, `SELECT COUNT(*) FROM seed_runs WHERE status = 'ok'`); n != 2 {
		t.Errorf("seed_runs ok = %d, want 2", n)
	}
	if n := x.count(t, `SELECT COUNT(*) FROM worlds`); n != 20 {
		t.Errorf("worlds = %d", n)
	}
	// every question has a version snapshot, an audit row and an outbox event
	if a, b := x.count(t, `SELECT COUNT(*) FROM question_versions`), x.count(t, `SELECT COUNT(*) FROM question_audit`); a != nQuestions || b != nQuestions {
		t.Errorf("versions %d, audit %d, want %d", a, b, nQuestions)
	}
	if n := x.count(t, `SELECT COUNT(DISTINCT key) FROM outbox WHERE kind = ?`, mongosync.KindQuestionBank); n != nQuestions {
		t.Errorf("question_bank outbox keys = %d, want %d", n, nQuestions)
	}
	if n := x.count(t, `SELECT COUNT(DISTINCT key) FROM outbox WHERE kind = ?`, mongosync.KindContentQuest); n != 89 {
		t.Errorf("content_quests outbox keys = %d", n)
	}
}

func TestMisconceptionCountComesFromTheCurriculum(t *testing.T) {
	x := newFixture(t)
	x.apply(t, Options{Actor: "test"})
	want := len(CurriculumMisconceptions(x.g)) + len(x.f.Misconception)
	if got := x.count(t, `SELECT COUNT(*) FROM misconceptions`); got != want || want < 90 {
		t.Fatalf("misconceptions in the table = %d, from files = %d", got, want)
	}
	if n := x.count(t, `SELECT COUNT(*) FROM misconceptions WHERE source = 'curriculum'`); n != len(CurriculumMisconceptions(x.g)) {
		t.Errorf("curriculum codes = %d", n)
	}
}

func TestEditChangesExactlyOneRowAndRollbackRestoresIt(t *testing.T) {
	x := newFixture(t)
	x.apply(t, Options{Actor: "test"})
	target := x.f.Questions()[0]
	original, _ := target.ContentHash()
	target.Hint.FR = "Un autre indice pour tester."
	rep := x.apply(t, Options{Actor: "test"})
	if rep.Questions.Changed != 1 || rep.Questions.Added != 0 {
		t.Fatalf("edit run: %+v", rep.Questions)
	}
	if n := x.count(t, `SELECT COUNT(*) FROM question_versions WHERE question_id = ?`, target.ID); n != 2 {
		t.Fatalf("versions of the edited question = %d, want 2", n)
	}
	n, err := Rollback(x.ctx, x.repo, rep.RunID, "tester")
	if err != nil || n != 1 {
		t.Fatalf("rollback n=%d err=%v", n, err)
	}
	row, err := x.repo.Get(x.ctx, x.db.R, target.ID)
	if err != nil {
		t.Fatal(err)
	}
	if row.Hash != original || row.Q.Version != 3 {
		t.Fatalf("after rollback hash=%s version=%d, want %s v3", row.Hash, row.Q.Version, original)
	}
	if got := x.count(t, `SELECT COUNT(*) FROM seed_runs WHERE id = ? AND status = 'rolled_back'`, rep.RunID); got != 1 {
		t.Error("run not marked rolled_back")
	}
	if _, err := Rollback(x.ctx, x.repo, "no-such-run", "tester"); err == nil {
		t.Error("rollback of an unknown run must fail")
	}
}

func TestRollbackOfFirstRunRetiresWhatItAdded(t *testing.T) {
	x := newFixture(t)
	rep := x.apply(t, Options{Actor: "test"})
	n, err := Rollback(x.ctx, x.repo, rep.RunID, "tester")
	if err != nil || n != len(x.f.Questions()) {
		t.Fatalf("rollback n=%d err=%v", n, err)
	}
	if got := x.count(t, `SELECT COUNT(*) FROM bank_questions WHERE status <> 'retired'`); got != 0 {
		t.Fatalf("%d questions still active", got)
	}
}

func TestValidationErrorBlocksEverything(t *testing.T) {
	x := newFixture(t)
	x.f.Questions()[0].Answer.Calc = "1+1" // wrong calc
	_, err := Apply(x.ctx, x.repo, x.assets, x.g, x.f, Options{Actor: "test"})
	var ve *ValidationError
	if !errors.As(err, &ve) || !content.HasErrors(ve.Issues) {
		t.Fatalf("want a ValidationError, got %v", err)
	}
	if n := x.count(t, `SELECT COUNT(*) FROM bank_questions`); n != 0 {
		t.Fatal("nothing may be written when validation fails")
	}
}

func TestBackupHookFailureStopsTheRun(t *testing.T) {
	x := newFixture(t)
	_, err := Apply(x.ctx, x.repo, x.assets, x.g, x.f, Options{Actor: "test", Backup: func(context.Context) (string, error) { return "", errors.New("disk full") }})
	if err == nil {
		t.Fatal("a failing backup must stop the run")
	}
	if n := x.count(t, `SELECT COUNT(*) FROM world_quests`); n != 0 {
		t.Fatal("nothing may be written when the backup fails")
	}
}

func TestMissingQuestionsAreRetiredOnlyWithTheFlag(t *testing.T) {
	x := newFixture(t)
	x.apply(t, Options{Actor: "test"})
	gone := x.f.QuestionFiles[0].Questions[0].ID
	x.f.QuestionFiles[0].Questions = x.f.QuestionFiles[0].Questions[1:]
	rep := x.apply(t, Options{Actor: "test"})
	if rep.Questions.Missing != 1 || rep.Questions.Retired != 0 || len(rep.Warnings) == 0 {
		t.Fatalf("without the flag: %+v %v", rep.Questions, rep.Warnings)
	}
	rep = x.apply(t, Options{Actor: "test", AllowRetire: true})
	if rep.Questions.Retired != 1 {
		t.Fatalf("with the flag: %+v", rep.Questions)
	}
	if got := x.count(t, `SELECT COUNT(*) FROM bank_questions WHERE id = ? AND status = 'retired'`, gone); got != 1 {
		t.Fatal("question not retired")
	}
}

func TestSeedWorldsMatchTheMigration(t *testing.T) {
	x := newFixture(t) // migrations only, no seed run yet
	rows, err := x.repo.Worlds(x.ctx, x.db.R)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != len(x.f.Worlds) {
		t.Fatalf("migration has %d worlds, seed has %d", len(rows), len(x.f.Worlds))
	}
	for i, w := range x.f.Worlds {
		r := rows[i]
		if r.ID != w.ID || r.Ord != w.Ord || r.Name != w.Name || r.Tagline != w.Tagline || r.Hook != w.Hook || r.AccentColor != w.AccentColor ||
			r.GradeMin != w.GradeMin || r.GradeMax != w.GradeMax || r.Strand != w.Strand {
			t.Errorf("world %s differs between the migration and seed/worlds.json: %+v vs %+v", w.ID, r.World, w)
		}
	}
}

func TestSeedShape(t *testing.T) {
	f, err := Read(seedDir, ReadOptions{SkipAssets: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(f.Worlds) != 20 || len(f.Quests) != 89 {
		t.Fatalf("worlds=%d quests=%d, want 20 and 89", len(f.Worlds), len(f.Quests))
	}
	perWorld := map[string]int{}
	for _, q := range f.Quests {
		perWorld[q.World]++
	}
	for _, w := range []string{"nature", "espace", "cuisine", "sport", "construction", "medecine", "science", "echecs"} {
		if perWorld[w] != 5 {
			t.Errorf("legacy world %s has %d quests, want 5", w, perWorld[w])
		}
	}
	if perWorld["marche"] != 5 {
		t.Errorf("marche has %d quests, want 5", perWorld["marche"])
	}
	types := map[string]int{}
	for _, q := range f.Questions() {
		types[q.Type]++
	}
	for _, typ := range content.AllTypes {
		if types[typ] < 3 {
			t.Errorf("type %s has %d sample questions, want at least 3", typ, types[typ])
		}
	}
	if c := Contrast("#000000"); c < 20 || Contrast("#FFFFFF") > 1.1 {
		t.Errorf("contrast helper broken: %f", c)
	}
}

func TestExcludeSkipsTheExamples(t *testing.T) {
	f, err := Read(seedDir, ReadOptions{Exclude: []string{ExamplesFile}, SkipAssets: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(f.Questions()) != 0 {
		t.Fatalf("examples not excluded: %d", len(f.Questions()))
	}
}

// TestRealAssetFilesLoadAndApply loads whatever art tools/assets produced so far (skipped when there is none)
// and checks it passes the same normalisation the production seed uses, files included.
func TestRealAssetFilesLoadAndApply(t *testing.T) {
	probe, err := Read(seedDir, ReadOptions{})
	if err != nil {
		t.Fatalf("the asset files do not load: %v", err)
	}
	if len(probe.Assets.Assets) == 0 {
		t.Skip("no seed/assets yet")
	}
	x := newFixture(t)
	x.f = probe
	x.f.QuestionFiles = nil // questions are covered by the other tests
	rep := x.apply(t, Options{Actor: "test", Only: []string{"assets"}})
	if rep.Assets.Added != len(probe.Assets.Assets) {
		t.Fatalf("assets added %d of %d", rep.Assets.Added, len(probe.Assets.Assets))
	}
	again := x.apply(t, Options{Actor: "test", Only: []string{"assets"}})
	if again.Assets.Added != 0 || again.Assets.Changed != 0 {
		t.Fatalf("second asset run changed something: %+v", again.Assets)
	}
	if n := x.count(t, `SELECT COUNT(*) FROM asset_variants`); n == 0 {
		t.Fatal("no variant bytes stored")
	}
}
