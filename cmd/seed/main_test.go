package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/Perrin-Nj/numerix-api/internal/store"
)

// seedDir is the repository seed directory seen from cmd/seed.
const seedDir = "../../seed"

// runSeed calls run with a fake environment and returns the exit code and both outputs.
func runSeed(t *testing.T, env map[string]string, args ...string) (int, string, string) {
	t.Helper()
	var out, errOut bytes.Buffer
	code := run(args, &out, &errOut, func(k string) string { return env[k] })
	return code, out.String(), errOut.String()
}

func count(t *testing.T, db *store.Store, query string) int {
	t.Helper()
	var n int
	if err := db.R.QueryRow(query).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestValidateOnlyNeverTouchesTheDatabase(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "never.db")
	code, out, errOut := runSeed(t, nil, "--db", dbPath, "--seed-dir", seedDir, "--validate-only")
	if code != 0 || !strings.Contains(out, "seed files valid: 20 worlds, 89 quests") {
		t.Fatalf("code=%d out=%q err=%q", code, out, errOut)
	}
	if _, err := os.Stat(dbPath); err == nil {
		t.Fatal("--validate-only created the database")
	}
}

func TestDryRunThenApplyThenNothingToDo(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "seed.db")
	base := []string{"--db", dbPath, "--seed-dir", seedDir, "--only", "assets,worlds,quests,questions", "--actor", "tester"}

	code, out, errOut := runSeed(t, nil, append(base, "--dry-run")...)
	if code != 0 || !strings.Contains(out, "dry run (nothing written)") || !has(out, "quests added=89") {
		t.Fatalf("dry run: code=%d", code)
	}
	if _, err := os.Stat(dbPath); err == nil {
		t.Fatal("a dry run on a missing database created it")
	}

	code, out, errOut = runSeed(t, nil, base...)
	if code != 0 || !has(out, "quests added=89") {
		t.Fatalf("first apply: code=%d out=%q err=%q", code, out, errOut)
	}
	// second dry run: nothing left to change
	code, out, _ = runSeed(t, nil, append(base, "--dry-run")...)
	if code != 0 || !has(out, "quests added=0 changed=0 unchanged=89") {
		t.Fatalf("second dry run: code=%d out=%q", code, out)
	}
	// second real run takes a backup of the existing database and changes nothing
	code, out, errOut = runSeed(t, nil, base...)
	if code != 0 || !strings.Contains(out, "backup written to") || !has(out, "added=0 changed=0") {
		t.Fatalf("second apply: code=%d out=%q err=%q", code, out, errOut)
	}
	backups, _ := filepath.Glob(filepath.Join(dir, "backups", "pre-seed-*.db"))
	if len(backups) != 1 {
		t.Fatalf("backup files: %v", backups)
	}
	b, err := store.Open(context.Background(), backups[0]) // the backup opens as a normal database
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	if n := count(t, b, `SELECT COUNT(*) FROM world_quests`); n != 89 {
		t.Fatalf("backup holds %d quests", n)
	}
}

func TestRollbackUndoesARun(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "seed.db")
	base := []string{"--db", dbPath, "--seed-dir", seedDir, "--only", "assets,worlds,quests,questions"}
	code, out, errOut := runSeed(t, nil, base...)
	if code != 0 {
		t.Fatalf("apply: %q %q", out, errOut)
	}
	id := regexp.MustCompile(`seed run ([0-9a-f-]{36}):`).FindStringSubmatch(out)
	if id == nil {
		t.Fatalf("no run id in %q", out)
	}
	code, out, errOut = runSeed(t, nil, "--db", dbPath, "--seed-dir", seedDir, "--rollback", id[1])
	if code != 0 || !strings.Contains(out, "rolled back run") {
		t.Fatalf("rollback: code=%d out=%q err=%q", code, out, errOut)
	}
	db, err := store.Open(context.Background(), dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if n := count(t, db, `SELECT COUNT(*) FROM bank_questions WHERE status <> 'retired'`); n != 0 {
		t.Fatalf("%d questions still active after the rollback", n)
	}
	if code, _, _ := runSeed(t, nil, "--db", dbPath, "--seed-dir", seedDir, "--rollback", "no-such-run"); code == 0 {
		t.Fatal("an unknown run id must fail")
	}
}

func TestRefusals(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "x.db")
	if code, _, errOut := runSeed(t, map[string]string{"APP_ENV": "production"}, "--db", dbPath, "--seed-dir", seedDir, "--no-backup"); code == 0 || !strings.Contains(errOut, "--no-backup is refused") {
		t.Fatalf("production no-backup: %d %q", code, errOut)
	}
	if code, _, _ := runSeed(t, nil, "--db", dbPath, "--seed-dir", "no/such/dir"); code == 0 {
		t.Fatal("a missing seed directory must fail")
	}
	if code, _, _ := runSeed(t, nil, "--db", dbPath, "--rollback", "x", "--dry-run"); code == 0 {
		t.Fatal("rollback with dry-run must fail")
	}
	if code, _, _ := runSeed(t, nil, "--bogus-flag"); code != 2 {
		t.Fatalf("unknown flag must be a usage error, got %d", code)
	}
}

// has reports whether the output matches a pattern where runs of spaces are flexible.
func has(out, pattern string) bool {
	return regexp.MustCompile(strings.ReplaceAll(pattern, " ", `\s+`)).MatchString(out)
}
