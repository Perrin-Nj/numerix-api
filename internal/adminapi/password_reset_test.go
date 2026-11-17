package adminapi

import (
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/Perrin-Nj/numerix-api/internal/pwreset"
)

var codeRe = regexp.MustCompile(`^\d{4}-\d{4}$`)

func TestAdminPasswordReset(t *testing.T) {
	h, db := setupDB(t)
	code, body, _ := call(h, "POST", "/admin/v1/parents/p1/password-reset", "", tok, "X-Admin-Actor", "marie")
	if code != 201 {
		t.Fatalf("status %d %v", code, body)
	}
	pc, _ := body["code"].(string)
	parent, _ := body["parent"].(map[string]any)
	if !codeRe.MatchString(pc) || body["expires_in_seconds"] != 3600.0 || parent["id"] != "p1" || parent["email"] != "a@x.ca" {
		t.Fatalf("body %v", body)
	}
	plain := strings.ReplaceAll(pc, "-", "")
	var exp, created, salt, hash string
	if err := db.R.QueryRow(`SELECT expires_at, created_at, salt, code_hash FROM password_resets WHERE parent_id = 'p1' AND used_at IS NULL AND burned_at IS NULL`).Scan(&exp, &created, &salt, &hash); err != nil {
		t.Fatalf("stored: %v", err)
	}
	ce, _ := time.Parse(time.RFC3339Nano, exp)
	cc, _ := time.Parse(time.RFC3339Nano, created)
	if ce.Sub(cc) != time.Hour || hash == plain || hash != pwreset.Hash(salt, plain) {
		t.Fatalf("ttl %v, hash must be salted: %s", ce.Sub(cc), hash)
	}
	var actorName, detail, target string
	if err := db.R.QueryRow(`SELECT actor, detail, target_id FROM admin_audit WHERE action = 'parent.password_reset'`).Scan(&actorName, &detail, &target); err != nil {
		t.Fatalf("audit: %v", err)
	}
	if actorName != "marie" || target != "p1" || strings.Contains(detail, plain) || strings.Contains(detail, pc) {
		t.Fatalf("audit %q %q %q", actorName, detail, target)
	}

	// a second code replaces the first
	_, second, _ := call(h, "POST", "/admin/v1/parents/p1/password-reset", "", tok)
	var n int
	_ = db.R.QueryRow(`SELECT COUNT(*) FROM password_resets WHERE parent_id = 'p1' AND used_at IS NULL AND burned_at IS NULL`).Scan(&n)
	if n != 1 || second["code"] == pc {
		t.Fatalf("one active code expected, got %d", n)
	}
}

func TestAdminPasswordResetRefusals(t *testing.T) {
	h, db := setupDB(t)
	if _, err := db.W.Exec(`INSERT INTO parents (id,email,password_hash,locale,deleted_at) VALUES ('pd','d@x.ca','!','fr-CA','2026-01-01T00:00:00.000Z')`); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"nope", "pd"} {
		if code, body, _ := call(h, "POST", "/admin/v1/parents/"+id+"/password-reset", "", tok); code != 404 {
			t.Fatalf("%s: %d %v", id, code, body)
		}
	}
	// no credential, or a parent / child style token: refused like every admin route
	for _, bearer := range []string{"", "eyJhbGciOiJIUzI1NiJ9.eyJyb2xlIjoicGFyZW50In0.sig", "eyJhbGciOiJIUzI1NiJ9.eyJyb2xlIjoiY2hpbGQifQ.sig"} {
		if code, _, _ := call(h, "POST", "/admin/v1/parents/p1/password-reset", "", bearer); code != 401 {
			t.Fatalf("bearer %q: %d", bearer, code)
		}
	}
	var n int
	_ = db.R.QueryRow(`SELECT COUNT(*) FROM password_resets`).Scan(&n)
	if n != 0 {
		t.Fatal("refused calls must create nothing")
	}
}
