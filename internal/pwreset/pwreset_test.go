package pwreset

import (
	"context"
	"database/sql"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Perrin-Nj/numerix-api/internal/store"
)

func TestHashNeedsTheServerKey(t *testing.T) {
	k1, k2 := DeriveKey("secret-one"), DeriveKey("secret-two")
	h := Hash(k1, "salt", "12345678")
	if !Match(k1, "salt", h, "12345678") || Match(k1, "salt", h, "12345679") {
		t.Fatal("match with the right key must work and only for the right code")
	}
	// someone holding only a copy of the database (hash and salt) cannot verify a guess without the key
	if Match(k2, "salt", h, "12345678") || Match(nil, "salt", h, "12345678") || Hash(nil, "salt", "12345678") == h {
		t.Fatal("a guess must not verify without the server key")
	}
	if string(DeriveKey("secret-one")) == "secret-one" || strings.EqualFold(string(k1), "secret-one") {
		t.Fatal("the raw secret must not be the key")
	}
}

// Migration 0013 applies on a database that already holds data (as created by origin/main) and keeps it.
func TestMigrationAppliesOnPopulatedDatabase(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "old.db")
	db, err := store.Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`INSERT INTO parents (id,email,password_hash,locale) VALUES ('p1','a@x.ca','h','fr-CA')`,
		`INSERT INTO children (id,parent_id,display_name,grade) VALUES ('c1','p1','Zoe',3)`,
		`DROP TABLE password_resets`, // back to the origin/main schema
		`DELETE FROM schema_migrations WHERE version LIKE '0013%'`,
	} {
		if _, err := db.W.Exec(q); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	_ = db.Close()
	db, err = store.Open(ctx, path) // applies 0013 on the populated copy
	if err != nil {
		t.Fatalf("migration on populated db: %v", err)
	}
	defer db.Close()
	var parents, children int
	_ = db.R.QueryRow(`SELECT COUNT(*) FROM parents`).Scan(&parents)
	_ = db.R.QueryRow(`SELECT COUNT(*) FROM children`).Scan(&children)
	if parents != 1 || children != 1 {
		t.Fatalf("data lost: %d %d", parents, children)
	}
	if err := db.WithWriteTx(ctx, func(tx *sql.Tx) error { _, e := Issue(tx, DeriveKey("s"), "p1", time.Now()); return e }); err != nil {
		t.Fatalf("issue on migrated db: %v", err)
	}
}
