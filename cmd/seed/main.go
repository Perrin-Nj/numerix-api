// Command seed loads the content seed files (worlds, quests, misconception codes, questions, assets) into the
// database. It is idempotent: it compares a checksum of every object with the stored one and writes only what
// changed. See docs/DB_PROD_DEPLOY.md for the production runbook and seed/README.md for the file formats.
//
//	seed --db data/numerix.db --dry-run             show what would change, write nothing
//	seed --db data/numerix.db --validate-only        validate the files only, never open the database
//	seed --db data/numerix.db                        back up, migrate, apply the changes
//	seed --db data/numerix.db --rollback <run_id>    undo the question changes of one run
//
// Go for Spring people: this is a second main package (a second executable) in the same module, like a Spring
// Boot CommandLineRunner packaged on its own. run() returns the exit code so tests can call it directly.
package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/Perrin-Nj/numerix-api/internal/assetstore"
	"github.com/Perrin-Nj/numerix-api/internal/content"
	"github.com/Perrin-Nj/numerix-api/internal/contentdb"
	"github.com/Perrin-Nj/numerix-api/internal/engine"
	"github.com/Perrin-Nj/numerix-api/internal/mongosync"
	"github.com/Perrin-Nj/numerix-api/internal/seedload"
	"github.com/Perrin-Nj/numerix-api/internal/store"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr, os.Getenv))
}

// options are the parsed command line flags.
type options struct {
	db, seedDir, backupDir, rollback, actor, only, world, exclude string
	dryRun, validateOnly, noBackup, allowRetire, strict, asJSON   bool
}

// run is the whole program; it returns the process exit code (0 ok, 1 failure, 2 bad usage).
func run(args []string, stdout, stderr io.Writer, getenv func(string) string) int {
	var o options
	fs := flag.NewFlagSet("seed", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.StringVar(&o.db, "db", envOr(getenv, "DB_PATH", "./data/numerix.db"), "SQLite database file")
	fs.StringVar(&o.seedDir, "seed-dir", "seed", "directory with worlds.json, quests.json, questions/ and assets/")
	fs.StringVar(&o.backupDir, "backup-dir", "", "where the pre-seed backup goes (default: backups next to the database)")
	fs.StringVar(&o.rollback, "rollback", "", "undo the question changes of this seed run id")
	fs.StringVar(&o.actor, "actor", envOr(getenv, "USER", envOr(getenv, "USERNAME", "seed")), "who runs it (audit)")
	fs.StringVar(&o.only, "only", "", "comma separated kinds to apply: worlds,quests,questions,assets")
	fs.StringVar(&o.world, "world", "", "restrict questions to one world id")
	fs.StringVar(&o.exclude, "exclude", "", "comma separated question file stems to skip, for example _examples")
	fs.BoolVar(&o.dryRun, "dry-run", false, "compute the changes on a copy of the database and write nothing")
	fs.BoolVar(&o.validateOnly, "validate-only", false, "validate the seed files and stop (no database access)")
	fs.BoolVar(&o.noBackup, "no-backup", false, "skip the backup (refused when APP_ENV=production)")
	fs.BoolVar(&o.allowRetire, "allow-retire", false, "retire questions that are no longer in the seed files")
	fs.BoolVar(&o.strict, "strict", false, "post checks also demand target_count-2 published questions per quest")
	fs.BoolVar(&o.asJSON, "json", false, "print the report as JSON")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	ctx, stop := signalContext()
	defer stop()
	if err := execute(ctx, o, stdout, stderr, getenv); err != nil {
		fmt.Fprintln(stderr, "seed: "+err.Error())
		return 1
	}
	return 0
}

func envOr(getenv func(string) string, key, def string) string {
	if v := strings.TrimSpace(getenv(key)); v != "" {
		return v
	}
	return def
}

func splitList(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// execute does the work for one parsed command line.
func execute(ctx context.Context, o options, stdout, stderr io.Writer, getenv func(string) string) error {
	prod := getenv("APP_ENV") == "production"
	if o.noBackup && prod {
		return fmt.Errorf("--no-backup is refused when APP_ENV=production")
	}
	if o.rollback != "" && (o.dryRun || o.validateOnly) {
		return fmt.Errorf("--rollback cannot be combined with --dry-run or --validate-only")
	}
	g, err := engine.LoadGraph()
	if err != nil {
		return fmt.Errorf("load curriculum: %w", err)
	}
	files, err := seedload.Read(o.seedDir, seedload.ReadOptions{Exclude: splitList(o.exclude)})
	if err != nil {
		return fmt.Errorf("read seed files: %w", err)
	}
	issues := files.Validate(g)
	printIssues(stderr, issues)
	if content.HasErrors(issues) {
		return fmt.Errorf("the seed files have %d errors; nothing was changed", len(content.Errors(issues)))
	}
	if o.validateOnly {
		fmt.Fprintf(stdout, "seed files valid: %d worlds, %d quests, %d questions, %d assets (%d warnings)\n",
			len(files.Worlds), len(files.Quests), len(files.Questions()), len(files.Assets.Assets), len(issues))
		return nil
	}
	if o.rollback != "" {
		return rollback(ctx, o, stdout, stderr)
	}
	if o.dryRun {
		return dryRun(ctx, o, files, g, stdout, stderr)
	}

	backupPath := ""
	if !o.noBackup {
		if backupPath, err = backupDB(ctx, o.db, o.backupDir, stdout, stderr); err != nil {
			return fmt.Errorf("backup failed, nothing was changed: %w", err)
		}
	} else {
		fmt.Fprintln(stderr, "seed: WARNING --no-backup: no safety copy was made")
	}
	db, err := store.Open(ctx, o.db) // opens the database and applies pending migrations
	if err != nil {
		return err
	}
	defer db.Close()
	repo, assets := contentdb.New(db, mongosync.NewOutbox(strings.TrimSpace(getenv("MONGODB_URI")) != "")), assetstore.New(db)
	rep, err := seedload.Apply(ctx, repo, assets, g, files, seedload.Options{
		Actor: o.actor, AllowRetire: o.allowRetire, Only: splitList(o.only), World: o.world, Strict: o.strict, GitRev: gitRev(o.seedDir),
		Backup: func(context.Context) (string, error) { return backupPath, nil },
	})
	printReport(stdout, rep, o.asJSON)
	if err != nil {
		return err
	}
	fmt.Fprintf(stdout, "to undo this run: seed --db %s --rollback %s\n", o.db, rep.RunID)
	return nil
}

// dryRun copies the database to a temporary file, applies there (so pending migrations are also tried) and
// reports. The real database is only read.
func dryRun(ctx context.Context, o options, files *seedload.Files, g *engine.Graph, stdout, stderr io.Writer) error {
	tmpDir, err := os.MkdirTemp("", "seed-dry-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmpDir)
	tmp := filepath.Join(tmpDir, "copy.db")
	if _, statErr := os.Stat(o.db); statErr == nil {
		if err := vacuumInto(ctx, o.db, tmp); err != nil {
			return fmt.Errorf("copy the database for the dry run: %w", err)
		}
	} else {
		fmt.Fprintf(stderr, "seed: %s does not exist; the dry run uses an empty database\n", o.db)
	}
	db, err := store.Open(ctx, tmp)
	if err != nil {
		return err
	}
	defer db.Close()
	rep, err := seedload.Apply(ctx, contentdb.New(db, nil), assetstore.New(db), g, files, seedload.Options{
		DryRun: true, Actor: o.actor, AllowRetire: o.allowRetire, Only: splitList(o.only), World: o.world,
	})
	printReport(stdout, rep, o.asJSON)
	return err
}

// rollback undoes one run after taking a backup.
func rollback(ctx context.Context, o options, stdout, stderr io.Writer) error {
	if !o.noBackup {
		if _, err := backupDB(ctx, o.db, o.backupDir, stdout, stderr); err != nil {
			return fmt.Errorf("backup failed, nothing was changed: %w", err)
		}
	}
	db, err := store.Open(ctx, o.db)
	if err != nil {
		return err
	}
	defer db.Close()
	n, err := seedload.Rollback(ctx, contentdb.New(db, nil), o.rollback, "rollback:"+o.actor)
	if err != nil {
		return err
	}
	fmt.Fprintf(stdout, "rolled back run %s: %d questions restored or retired\n", o.rollback, n)
	return nil
}

// backupDB makes a consistent copy of the live database with VACUUM INTO (safe while the API runs) and checks
// Litestream when it is installed. It returns the backup path ("" when the database does not exist yet).
func backupDB(ctx context.Context, dbPath, dir string, stdout, stderr io.Writer) (string, error) {
	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		fmt.Fprintln(stdout, "seed: no database file yet, nothing to back up")
		return "", nil
	}
	if dir == "" {
		dir = filepath.Join(filepath.Dir(dbPath), "backups")
	}
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return "", err
	}
	dest := filepath.Join(dir, "pre-seed-"+time.Now().UTC().Format("20060102T150405Z")+".db")
	if err := vacuumInto(ctx, dbPath, dest); err != nil {
		return "", err
	}
	fmt.Fprintf(stdout, "seed: backup written to %s\n", dest)
	litestreamCheck(ctx, dbPath, stdout, stderr)
	return dest, nil
}

// vacuumInto copies src into a new file dst with "VACUUM INTO" over a read-only connection.
func vacuumInto(ctx context.Context, src, dst string) error {
	conn, err := sql.Open("sqlite", "file:"+filepath.ToSlash(src)+"?mode=ro&_pragma=busy_timeout(10000)")
	if err != nil {
		return err
	}
	defer conn.Close()
	if _, err := conn.ExecContext(ctx, "VACUUM INTO ?", dst); err != nil {
		return fmt.Errorf("VACUUM INTO %s: %w", dst, err)
	}
	return nil
}

// litestreamCheck prints the newest Litestream snapshot when the tool is installed. A missing tool or a failing
// check is a warning, never an error (development machines do not run Litestream).
func litestreamCheck(ctx context.Context, dbPath string, stdout, stderr io.Writer) {
	bin, err := exec.LookPath("litestream")
	if err != nil {
		fmt.Fprintln(stderr, "seed: litestream not installed here; the file backup above is the safety copy")
		return
	}
	cctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	out, err := exec.CommandContext(cctx, bin, "snapshots", dbPath).CombinedOutput()
	if err != nil {
		fmt.Fprintf(stderr, "seed: WARNING litestream snapshots failed: %v\n", err)
		return
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	fmt.Fprintf(stdout, "seed: litestream replica: %s\n", lines[len(lines)-1])
}

// gitRev returns the short git revision of the seed directory's repository, or "unknown".
func gitRev(dir string) string {
	out, err := exec.Command("git", "-C", dir, "rev-parse", "--short", "HEAD").Output()
	if err != nil {
		return "unknown"
	}
	return strings.TrimSpace(string(out))
}

// printIssues prints warnings and errors to stderr.
func printIssues(w io.Writer, is []content.Issue) {
	for _, i := range is {
		fmt.Fprintln(w, i.String())
	}
}

// printReport prints the run report.
func printReport(w io.Writer, rep seedload.Report, asJSON bool) {
	if asJSON {
		b, _ := json.MarshalIndent(rep, "", "  ") // a plain struct always marshals
		fmt.Fprintln(w, string(b))
		return
	}
	mode := "applied"
	if rep.DryRun {
		mode = "dry run (nothing written)"
	}
	fmt.Fprintf(w, "seed run %s: %s, files %s\n", rep.RunID, mode, short(rep.FilesHash))
	row := func(name string, c seedload.Counts) {
		fmt.Fprintf(w, "  %-15s added=%d changed=%d unchanged=%d", name, c.Added, c.Changed, c.Unchanged)
		if c.Missing > 0 || c.Retired > 0 {
			fmt.Fprintf(w, " missing_in_seed=%d retired=%d", c.Missing, c.Retired)
		}
		fmt.Fprintln(w)
	}
	row("misconceptions", rep.Misconceptions)
	row("vocab", rep.Vocab)
	row("assets", rep.Assets)
	row("worlds", rep.Worlds)
	row("quests", rep.Quests)
	row("questions", rep.Questions)
	for _, c := range rep.Changes {
		fmt.Fprintln(w, "    "+c)
	}
	for _, x := range rep.Warnings {
		fmt.Fprintln(w, "  warning: "+x)
	}
	if rep.BackupPath != "" {
		fmt.Fprintf(w, "  backup: %s\n", rep.BackupPath)
	}
}

func short(h string) string {
	if len(h) > 12 {
		return h[:12]
	}
	return h
}

// signalContext is cancelled by Ctrl+C or SIGTERM so a long run stops between transactions.
func signalContext() (context.Context, context.CancelFunc) {
	return signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
}
