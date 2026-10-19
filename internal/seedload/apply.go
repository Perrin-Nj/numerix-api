package seedload

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/google/uuid"

	"github.com/Perrin-Nj/numerix-api/internal/assetstore"
	"github.com/Perrin-Nj/numerix-api/internal/content"
	"github.com/Perrin-Nj/numerix-api/internal/contentdb"
	"github.com/Perrin-Nj/numerix-api/internal/engine"
)

// Counts is the result for one kind of object.
type Counts struct {
	Added     int `json:"added"`
	Changed   int `json:"changed"`
	Unchanged int `json:"unchanged"`
	Missing   int `json:"missing_in_seed,omitempty"` // questions in the database that the seed no longer lists
	Retired   int `json:"retired,omitempty"`
}

// Report is what a run did (or would do, for a dry run).
type Report struct {
	RunID          string   `json:"run_id"`
	DryRun         bool     `json:"dry_run"`
	BackupPath     string   `json:"backup_path,omitempty"`
	FilesHash      string   `json:"files_hash"`
	Misconceptions Counts   `json:"misconceptions"`
	Vocab          Counts   `json:"vocab"`
	Assets         Counts   `json:"assets"`
	Worlds         Counts   `json:"worlds"`
	Quests         Counts   `json:"quests"`
	Questions      Counts   `json:"questions"`
	Changes        []string `json:"changes,omitempty"` // first 20 per kind
	Warnings       []string `json:"warnings,omitempty"`
}

// Totals returns added, changed, retired and unchanged over every kind (the seed_runs columns).
func (r Report) Totals() (added, changed, retired, unchanged int) {
	for _, c := range []Counts{r.Misconceptions, r.Vocab, r.Assets, r.Worlds, r.Quests, r.Questions} {
		added, changed, retired, unchanged = added+c.Added, changed+c.Changed, retired+c.Retired, unchanged+c.Unchanged
	}
	return
}

// Options controls Apply.
type Options struct {
	DryRun      bool
	Actor       string                                    // who runs the seed (audit)
	AllowRetire bool                                      // retire questions that left the seed files
	Only        []string                                  // kinds to apply: worlds, quests, questions, assets (empty = all)
	World       string                                    // restrict questions to one world
	Strict      bool                                      // post checks also demand target_count-2 published per quest
	GitRev      string                                    // stored in seed_runs
	Backup      func(ctx context.Context) (string, error) // makes the pre-seed backup; nil = none
}

// ValidationError is returned when the seed has blocking issues; nothing was written.
type ValidationError struct{ Issues []content.Issue }

func (e *ValidationError) Error() string {
	return fmt.Sprintf("seed validation failed: %d errors", len(content.Errors(e.Issues)))
}

// errDryRun rolls back the single transaction of a dry run.
var errDryRun = errors.New("dry run")

// batchSize is how many questions go into one write transaction.
const batchSize = 200

// maxListed limits the change list per kind in a report.
const maxListed = 20

func (o Options) wants(kind string) bool { return len(o.Only) == 0 || slices.Contains(o.Only, kind) }

// run carries the state of one Apply call.
type run struct {
	repo   *contentdb.Repo
	assets *assetstore.Store
	g      *engine.Graph
	f      *Files
	opt    Options
	rep    *Report
	ids    []string // question ids touched, for cache invalidation
}

func (r *run) note(kind, id, what string, counts *Counts) {
	if n := countListed(r.rep.Changes, kind); n < maxListed {
		r.rep.Changes = append(r.rep.Changes, kind+" "+id+" "+what)
	}
	_ = counts
}

func countListed(list []string, kind string) int {
	n := 0
	for _, l := range list {
		if strings.HasPrefix(l, kind+" ") {
			n++
		}
	}
	return n
}

func (r *run) count(c *Counts, ch contentdb.Change, kind, id string) {
	switch ch {
	case contentdb.Added:
		c.Added++
		r.note(kind, id, "added", c)
	case contentdb.Changed:
		c.Changed++
		r.note(kind, id, "changed", c)
	default:
		c.Unchanged++
	}
}

// Apply validates the seed, then writes only what changed. A dry run does everything in one transaction and rolls it back,
// so its counts are exactly what a real run would produce.
func Apply(ctx context.Context, repo *contentdb.Repo, assets *assetstore.Store, g *engine.Graph, f *Files, opt Options) (Report, error) {
	rep := Report{RunID: uuid.NewString(), DryRun: opt.DryRun, FilesHash: f.Hash}
	if opt.Actor == "" {
		opt.Actor = "seed"
	}
	if issues := f.Validate(g); content.HasErrors(issues) {
		return rep, &ValidationError{Issues: issues}
	}
	r := &run{repo: repo, assets: assets, g: g, f: f, opt: opt, rep: &rep}
	actor := "seed:" + rep.RunID

	if opt.DryRun {
		err := repo.DB.WithWriteTx(ctx, func(tx *sql.Tx) error {
			if err := r.allKinds(ctx, tx, actor, false); err != nil {
				return err
			}
			return errDryRun
		})
		if err != nil && !errors.Is(err, errDryRun) {
			return rep, err
		}
		return rep, nil
	}

	if opt.Backup != nil {
		path, err := opt.Backup(ctx)
		if err != nil {
			return rep, fmt.Errorf("backup before seed failed, nothing was changed: %w", err)
		}
		rep.BackupPath = path
	}
	run0 := contentdb.SeedRun{ID: rep.RunID, Actor: opt.Actor, GitRev: opt.GitRev, FilesHash: f.Hash, BackupPath: rep.BackupPath}
	if err := repo.DB.WithWriteTx(ctx, func(tx *sql.Tx) error { return repo.StartRun(ctx, tx, run0) }); err != nil {
		return rep, err
	}
	err := r.applyReal(ctx, actor)
	run0.Added, run0.Changed, run0.Retired, run0.Unchanged = rep.Totals()
	run0.Status = "ok"
	if err != nil {
		run0.Status = "failed"
	}
	if ferr := repo.DB.WithWriteTx(ctx, func(tx *sql.Tx) error { return repo.FinishRun(ctx, tx, run0) }); ferr != nil {
		err = errors.Join(err, ferr)
	}
	repo.AfterCommit(r.ids...)
	if err != nil {
		return rep, fmt.Errorf("seed run %s failed (backup: %q): %w", rep.RunID, rep.BackupPath, err)
	}
	if perr := r.postChecks(ctx); perr != nil {
		return rep, fmt.Errorf("seed run %s applied but post checks failed (backup: %q): %w", rep.RunID, rep.BackupPath, perr)
	}
	return rep, nil
}

// applyReal runs each kind in its own transaction (questions in batches).
func (r *run) applyReal(ctx context.Context, actor string) error {
	return r.allKinds(ctx, nil, actor, true)
}

// allKinds applies every wanted kind. With tx != nil everything shares that transaction (dry run); with
// tx == nil and perKind true each kind and each batch opens its own transaction.
func (r *run) allKinds(ctx context.Context, tx *sql.Tx, actor string, perKind bool) error {
	do := func(fn func(tx *sql.Tx) error) error {
		if tx != nil {
			return fn(tx)
		}
		return r.repo.DB.WithWriteTx(ctx, fn)
	}
	if err := do(func(tx *sql.Tx) error { return r.misconceptions(ctx, tx) }); err != nil {
		return fmt.Errorf("misconceptions: %w", err)
	}
	if r.opt.wants("assets") {
		if err := do(func(tx *sql.Tx) error { return r.assetKind(ctx, tx) }); err != nil {
			return fmt.Errorf("assets: %w", err)
		}
	}
	if r.opt.wants("worlds") {
		if err := do(func(tx *sql.Tx) error { return r.worlds(ctx, tx, actor) }); err != nil {
			return fmt.Errorf("worlds: %w", err)
		}
	}
	if r.opt.wants("quests") {
		if err := do(func(tx *sql.Tx) error { return r.quests(ctx, tx, actor) }); err != nil {
			return fmt.Errorf("quests: %w", err)
		}
	}
	if r.opt.wants("questions") {
		qs := r.selected()
		for start := 0; start < len(qs); start += batchSize {
			batch := qs[start:min(len(qs), start+batchSize)]
			if err := do(func(tx *sql.Tx) error { return r.questions(ctx, tx, batch, actor) }); err != nil {
				return fmt.Errorf("questions: %w", err)
			}
		}
		if err := do(func(tx *sql.Tx) error { return r.missing(ctx, tx, actor) }); err != nil {
			return fmt.Errorf("missing questions: %w", err)
		}
	}
	return nil
}

// selected returns the questions to apply (all, or one world).
func (r *run) selected() []*content.Question {
	var out []*content.Question
	for _, q := range r.f.Questions() {
		if r.opt.World == "" || strings.HasPrefix(q.ID, r.opt.World+".") {
			out = append(out, q)
		}
	}
	return out
}

func (r *run) misconceptions(ctx context.Context, tx *sql.Tx) error {
	list := append(CurriculumMisconceptions(r.g), r.f.Misconception...)
	existing, err := r.repo.MisconceptionCodes(ctx, tx)
	if err != nil {
		return err
	}
	n, err := r.repo.UpsertMisconceptions(ctx, tx, list)
	if err != nil {
		return err
	}
	added := 0
	for _, m := range list {
		if !existing[m.Code] {
			added++
		}
	}
	r.rep.Misconceptions = Counts{Added: added, Changed: max(0, n-added), Unchanged: len(list) - n}
	return nil
}

func (r *run) assetKind(ctx context.Context, tx *sql.Tx) error {
	vocabChanged, err := r.assets.ReplaceVocab(ctx, tx, r.f.Assets.Vocab)
	if err != nil {
		return err
	}
	r.rep.Vocab = Counts{Changed: vocabChanged, Unchanged: len(r.f.Assets.Vocab) - vocabChanged}
	for _, d := range r.f.Assets.Assets {
		files, err := r.f.Assets.ReadFiles(d)
		if err != nil {
			return err
		}
		created, changed, err := r.assets.UpsertDoc(ctx, tx, d, files)
		if err != nil {
			return err
		}
		switch {
		case created:
			r.count(&r.rep.Assets, contentdb.Added, "asset", d.ID)
		case changed:
			r.count(&r.rep.Assets, contentdb.Changed, "asset", d.ID)
		default:
			r.count(&r.rep.Assets, contentdb.Unchanged, "asset", d.ID)
		}
	}
	return nil
}

func (r *run) worlds(ctx context.Context, tx *sql.Tx, actor string) error {
	for _, w := range r.f.Worlds {
		ch, err := r.repo.UpsertWorld(ctx, tx, w, actor)
		if err != nil {
			return err
		}
		r.count(&r.rep.Worlds, ch, "world", w.ID)
	}
	return nil
}

func (r *run) quests(ctx context.Context, tx *sql.Tx, actor string) error {
	for _, q := range r.f.Quests {
		ch, err := r.repo.UpsertQuest(ctx, tx, q, actor)
		if err != nil {
			return err
		}
		r.count(&r.rep.Quests, ch, "quest", q.ID)
	}
	return nil
}

func (r *run) questions(ctx context.Context, tx *sql.Tx, batch []*content.Question, actor string) error {
	for _, q := range batch {
		res, err := r.repo.Save(ctx, tx, contentdb.SaveInput{Q: *q, Actor: actor, Action: "seed", KeepStatus: true, Detail: q.Source})
		if err != nil {
			return fmt.Errorf("%s: %w", q.ID, err)
		}
		ch := contentdb.Unchanged
		switch {
		case res.Created:
			ch = contentdb.Added
		case res.Changed:
			ch = contentdb.Changed
		}
		if ch != contentdb.Unchanged {
			r.ids = append(r.ids, q.ID)
		}
		r.count(&r.rep.Questions, ch, "question", q.ID)
	}
	return nil
}

// missing reports (and with AllowRetire retires) seeded questions that left the seed files.
func (r *run) missing(ctx context.Context, tx *sql.Tx, actor string) error {
	if r.opt.World != "" {
		return nil // a partial run cannot tell what is missing
	}
	inDB, err := r.repo.SeedSourced(ctx, tx)
	if err != nil {
		return err
	}
	seeded := map[string]bool{}
	for _, q := range r.f.Questions() {
		seeded[q.ID] = true
	}
	var gone []string
	for id := range inDB {
		if !seeded[id] {
			gone = append(gone, id)
		}
	}
	slices.Sort(gone)
	r.rep.Questions.Missing = len(gone)
	if !r.opt.AllowRetire {
		if len(gone) > 0 {
			r.rep.Warnings = append(r.rep.Warnings, fmt.Sprintf("%d seeded questions are no longer in the seed files; they stay as they are (use --allow-retire to retire them)", len(gone)))
		}
		return nil
	}
	for _, id := range gone {
		if _, err := r.repo.Transition(ctx, tx, id, content.StatusRetired, actor, "removed from the seed files", nil); err != nil {
			return fmt.Errorf("retire %s: %w", id, err)
		}
		r.rep.Questions.Retired++
		r.ids = append(r.ids, id)
	}
	return nil
}

// postChecks verifies the database after a real run.
func (r *run) postChecks(ctx context.Context) error {
	var res string
	if err := r.repo.DB.R.QueryRowContext(ctx, `PRAGMA integrity_check`).Scan(&res); err != nil || res != "ok" {
		return fmt.Errorf("integrity_check: %q %v", res, err)
	}
	rows, err := r.repo.DB.R.QueryContext(ctx, `PRAGMA foreign_key_check`)
	if err != nil {
		return fmt.Errorf("foreign_key_check: %w", err)
	}
	broken := rows.Next()
	_ = rows.Close()
	if broken {
		return errors.New("foreign_key_check found a broken reference")
	}
	if r.opt.wants("worlds") {
		var n int
		if err := r.repo.DB.R.QueryRowContext(ctx, `SELECT COUNT(*) FROM worlds WHERE status = 'published'`).Scan(&n); err != nil {
			return err
		}
		if n < len(r.f.Worlds) {
			return fmt.Errorf("expected at least %d published worlds, found %d", len(r.f.Worlds), n)
		}
	}
	if r.opt.Strict {
		cov, err := r.repo.Coverage(ctx, r.repo.DB.R)
		if err != nil {
			return err
		}
		var short []string
		for _, c := range cov {
			if c.ByStatus[content.StatusPublished] < c.Target-2 {
				short = append(short, c.Quest)
			}
		}
		if len(short) > 0 {
			return fmt.Errorf("%d quests have fewer than target-2 published questions, for example %s", len(short), short[0])
		}
	}
	return nil
}

// Rollback undoes the question changes of a seed run (see contentdb.RollbackRun) and marks the run.
func Rollback(ctx context.Context, repo *contentdb.Repo, runID, actor string) (int, error) {
	var n int
	var ids []string
	err := repo.DB.WithWriteTx(ctx, func(tx *sql.Tx) error {
		var status string
		if e := tx.QueryRowContext(ctx, `SELECT status FROM seed_runs WHERE id = ?`, runID).Scan(&status); errors.Is(e, sql.ErrNoRows) {
			return fmt.Errorf("seed run %q does not exist", runID)
		} else if e != nil {
			return e
		}
		rows, e := tx.QueryContext(ctx, `SELECT DISTINCT question_id FROM question_versions WHERE actor = ?`, "seed:"+runID)
		if e != nil {
			return e
		}
		for rows.Next() {
			var id string
			if e := rows.Scan(&id); e != nil {
				_ = rows.Close()
				return e
			}
			ids = append(ids, id)
		}
		if e := rows.Close(); e != nil {
			return e
		}
		var e2 error
		if n, e2 = repo.RollbackRun(ctx, tx, runID, actor); e2 != nil {
			return e2
		}
		_, e2 = tx.ExecContext(ctx, `UPDATE seed_runs SET status = 'rolled_back' WHERE id = ?`, runID)
		return e2
	})
	if err == nil {
		repo.AfterCommit(ids...)
	}
	return n, err
}
