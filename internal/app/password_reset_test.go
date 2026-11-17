package app

// Password reset tests (docs/specs/PASSWORD-RESET.md): whole HTTP stack on a temp SQLite database.
// Codes are issued with pwreset.Issue, the same function the admin endpoint uses.

import (
	"bytes"
	"context"
	"database/sql"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Perrin-Nj/numerix-api/internal/pwreset"
)

type syncBuf struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuf) Write(p []byte) (int, error) { s.mu.Lock(); defer s.mu.Unlock(); return s.b.Write(p) }
func (s *syncBuf) String() string              { s.mu.Lock(); defer s.mu.Unlock(); return s.b.String() }

type resetFx struct {
	h    http.Handler
	s    *Server
	logs *syncBuf
}

const oldPW, newPW = "correct-horse-battery", "brand-new-passphrase"

// newResetFx builds a server with one registered parent ana@example.com.
func newResetFx(t *testing.T) *resetFx {
	t.Helper()
	h, s := newTestServer(t)
	fx := &resetFx{h: h, s: s, logs: &syncBuf{}}
	s.Log = slog.New(slog.NewTextHandler(fx.logs, nil))
	code, body, _ := plCall(t, h, "POST", "/v1/auth/register", "", map[string]any{"email": "ana@example.com", "password": oldPW, "locale": "fr-CA"})
	plWant(t, code, body, 201, "register")
	return fx
}

// issue creates a code for ana, like the admin endpoint does.
func (fx *resetFx) issue(t *testing.T) string {
	t.Helper()
	var code string
	if err := fx.s.Store.WithWriteTx(context.Background(), func(tx *sql.Tx) (err error) {
		code, err = pwreset.Issue(tx, fx.parentID(t), time.Now())
		return
	}); err != nil {
		t.Fatal(err)
	}
	return code
}

func (fx *resetFx) reset(t *testing.T, email, code, pw string) (int, map[string]any) {
	t.Helper()
	c, body, _ := plCall(t, fx.h, "POST", "/v1/auth/password/reset", "", map[string]any{"email": email, "code": code, "password": pw})
	return c, body
}

func (fx *resetFx) login(t *testing.T, email, pw string) int {
	t.Helper()
	code, _, _ := plCall(t, fx.h, "POST", "/v1/auth/login", "", map[string]any{"email": email, "password": pw})
	return code
}

func (fx *resetFx) exec(t *testing.T, q string, args ...any) {
	t.Helper()
	if _, err := fx.s.Store.W.Exec(q, args...); err != nil {
		t.Fatal(err)
	}
}

func (fx *resetFx) count(t *testing.T, q string, args ...any) (n int) {
	t.Helper()
	if err := fx.s.Store.R.QueryRow(q, args...).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func (fx *resetFx) parentID(t *testing.T) (id string) {
	t.Helper()
	if err := fx.s.Store.R.QueryRow(`SELECT id FROM parents WHERE email='ana@example.com'`).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

func wrongCode(good string) string {
	if strings.HasPrefix(good, "0") {
		return "1" + good[1:]
	}
	return "0" + good[1:]
}

func TestResetFullFlow(t *testing.T) {
	fx := newResetFx(t)
	code, login, _ := plCall(t, fx.h, "POST", "/v1/auth/login", "", map[string]any{"email": "ana@example.com", "password": oldPW})
	plWant(t, code, login, 200, "login")
	refresh := plStr(login, "refresh_token")
	fx.exec(t, `INSERT INTO devices (id,parent_id,token_hash,label,user_agent_family,expires_at) VALUES ('d1',?,'dh','Chrome','Chrome','2999-01-01T00:00:00.000Z')`, fx.parentID(t))
	for i := 0; i < 5; i++ { // lock the email out
		fx.login(t, "ana@example.com", "wrong-wrong-wrong")
	}
	if c := fx.login(t, "ana@example.com", oldPW); c != 429 {
		t.Fatalf("expected the lockout, got %d", c)
	}
	rc := fx.issue(t)
	if len(rc) != 9 || rc[4] != '-' {
		t.Fatalf("code shape %q", rc)
	}
	if c, b := fx.reset(t, "Ana@Example.com", rc, newPW); c != 204 {
		t.Fatalf("reset: %d %v", c, b)
	}
	if c := fx.login(t, "ana@example.com", oldPW); c != 401 {
		t.Fatalf("old password must stop working: %d", c)
	}
	if c := fx.login(t, "ana@example.com", newPW); c != 200 {
		t.Fatalf("new password must work (lockout cleared): %d", c)
	}
	if c, _, _ := plCall(t, fx.h, "POST", "/v1/auth/refresh", "", map[string]any{"refresh_token": refresh}); c != 401 {
		t.Fatalf("old session must be signed out: %d", c)
	}
	if fx.count(t, `SELECT COUNT(*) FROM devices WHERE id = 'd1' AND revoked_at IS NULL`) != 0 {
		t.Fatal("remembered devices must be revoked")
	}
	if c, b := fx.reset(t, "ana@example.com", rc, "another-passphrase-1"); c != 400 || plStr(b, "code") != "reset_code_invalid" { // replay
		t.Fatalf("replay: %d %v", c, b)
	}
	var detail string
	if err := fx.s.Store.R.QueryRow(`SELECT detail FROM admin_audit WHERE action = 'parent.password_reset_used'`).Scan(&detail); err != nil {
		t.Fatalf("audit: %v", err)
	}
	plain := strings.ReplaceAll(rc, "-", "")
	for _, secret := range []string{rc, plain} {
		if strings.Contains(fx.logs.String(), secret) || strings.Contains(detail, secret) {
			t.Fatal("code must never be logged or audited")
		}
	}
	var hash, salt string
	_ = fx.s.Store.R.QueryRow(`SELECT code_hash, salt FROM password_resets`).Scan(&hash, &salt)
	if hash == plain || hash != pwreset.Hash(salt, plain) {
		t.Fatal("code must be stored salted and hashed")
	}
}

func TestResetAcceptsDashAndSpaces(t *testing.T) {
	for _, form := range []func(string) string{
		func(c string) string { return strings.ReplaceAll(c, "-", "") },
		func(c string) string { return " " + strings.ReplaceAll(c, "-", " ") + " " },
	} {
		fx := newResetFx(t)
		if c, b := fx.reset(t, "ana@example.com", form(fx.issue(t)), newPW); c != 204 {
			t.Fatalf("%d %v", c, b)
		}
	}
}

func TestResetEveryFailureIsOneGenericAnswer(t *testing.T) {
	fx := newResetFx(t)
	rc := fx.issue(t)
	fx.exec(t, `INSERT INTO parents (id,email,password_hash,locale,blocked_at) VALUES ('pb','blocked@example.com','h','fr-CA','2026-01-01T00:00:00.000Z')`)
	fx.exec(t, `INSERT INTO parents (id,email,password_hash,locale,deleted_at) VALUES ('pd','deleted@example.com','!','fr-CA','2026-01-01T00:00:00.000Z')`)
	for _, id := range []string{"pb", "pd"} {
		fx.exec(t, `INSERT INTO password_resets (id,parent_id,code_hash,salt,expires_at) VALUES (?,?,?,'s',?)`, "r"+id, id, pwreset.Hash("s", "12345678"), pwreset.Stamp(time.Now().Add(time.Hour)))
	}
	cases := []struct{ name, email, code string }{
		{"unknown email", "ghost@example.com", rc},
		{"wrong code", "ana@example.com", wrongCode(rc)},
		{"malformed code", "ana@example.com", "abcd-efgh"},
		{"short code", "ana@example.com", "1234"},
		{"empty", "ana@example.com", ""},
		{"blocked parent", "blocked@example.com", "1234-5678"},
		{"deleted parent", "deleted@example.com", "1234-5678"},
		{"no active code", "ana2@example.com", rc},
		{"injection", "x'--@example.com", "1234-5678"},
		{"huge email", strings.Repeat("a", 5000) + "@example.com", rc},
	}
	var first map[string]any
	for _, c := range cases {
		code, body := fx.reset(t, c.email, c.code, newPW)
		if code != 400 || plStr(body, "code") != "reset_code_invalid" {
			t.Fatalf("%s: %d %v", c.name, code, body)
		}
		if first == nil {
			first = body
		} else if plStr(body, "detail") != plStr(first, "detail") {
			t.Fatalf("%s: body differs from the others: %v vs %v", c.name, body, first)
		}
	}
	if c := fx.login(t, "ana@example.com", oldPW); c != 200 {
		t.Fatal("password must be unchanged")
	}
}

func TestResetTimingSimilarForKnownAndUnknownEmail(t *testing.T) {
	fx := newResetFx(t)
	rc := fx.issue(t)
	avg := func(email string) time.Duration {
		// only 3 attempts each: stays under the burn threshold and the IP limit
		t0 := time.Now()
		for i := 0; i < 3; i++ {
			fx.reset(t, email, wrongCode(rc), newPW)
		}
		return time.Since(t0) / 3
	}
	known, unknown := avg("ana@example.com"), avg("ghost@example.com")
	if d := known - unknown; d > 100*time.Millisecond || d < -100*time.Millisecond {
		t.Fatalf("timing classes differ: known %v unknown %v", known, unknown)
	}
}

func TestResetBruteForceBurnsTheCode(t *testing.T) {
	fx := newResetFx(t)
	rc := fx.issue(t)
	for i := 0; i < 5; i++ {
		if c, _ := fx.reset(t, "ana@example.com", wrongCode(rc), newPW); c != 400 {
			t.Fatalf("guess %d: %d", i, c)
		}
	}
	if c, b := fx.reset(t, "ana@example.com", rc, newPW); c != 400 || plStr(b, "code") != "reset_code_invalid" {
		t.Fatalf("the right code must be burned after 5 wrong ones: %d %v", c, b)
	}
	if fx.count(t, `SELECT COUNT(*) FROM password_resets WHERE burned_at IS NOT NULL`) != 1 {
		t.Fatal("code not burned")
	}
	// the admin issues a new one and the parent can go on (attempts restart)
	if c, _ := fx.reset(t, "ana@example.com", fx.issue(t), newPW); c != 204 {
		t.Fatalf("new code after burn: %d", c)
	}
}

func TestResetExpiredCodeRejected(t *testing.T) {
	fx := newResetFx(t)
	rc := fx.issue(t)
	fx.exec(t, `UPDATE password_resets SET expires_at = '2000-01-01T00:00:00.000Z'`)
	if c, b := fx.reset(t, "ana@example.com", rc, newPW); c != 400 || plStr(b, "code") != "reset_code_invalid" {
		t.Fatalf("%d %v", c, b)
	}
}

func TestResetReplacedCodeRejected(t *testing.T) {
	fx := newResetFx(t)
	first := fx.issue(t)
	second := fx.issue(t)
	if c, _ := fx.reset(t, "ana@example.com", first, newPW); c != 400 {
		t.Fatalf("replaced code must fail: %d", c)
	}
	if c, _ := fx.reset(t, "ana@example.com", second, newPW); c != 204 {
		t.Fatalf("latest code must work: %d", c)
	}
}

func TestResetPasswordPolicyKeepsCode(t *testing.T) {
	fx := newResetFx(t)
	rc := fx.issue(t)
	for _, pw := range []string{"", "short", strings.Repeat("a", 129), strings.Repeat("a", 5000)} {
		if c, b := fx.reset(t, "ana@example.com", rc, pw); c != 422 {
			t.Fatalf("%d chars: %d %v", len(pw), c, b)
		}
	}
	if c, b, _ := plCall(t, fx.h, "POST", "/v1/auth/password/reset", "", map[string]any{"email": "ana@example.com", "code": rc, "password": strings.Repeat("a", 300000)}); c != 413 {
		t.Fatalf("oversize body: %d %v", c, b)
	}
	if c, _ := fx.reset(t, "ana@example.com", rc, newPW); c != 204 {
		t.Fatal("a refused password must not burn the code")
	}
}

func TestResetSingleUseUnderConcurrency(t *testing.T) {
	fx := newResetFx(t)
	rc := fx.issue(t)
	var wg sync.WaitGroup
	codes := make([]int, 8)
	for i := range codes {
		wg.Add(1)
		go func() {
			defer wg.Done()
			codes[i], _ = fx.reset(t, "ana@example.com", rc, newPW+string(rune('a'+i)))
		}()
	}
	wg.Wait()
	ok := 0
	for _, c := range codes {
		if c == 204 {
			ok++
		} else if c != 400 {
			t.Fatalf("unexpected %v", codes)
		}
	}
	if ok != 1 {
		t.Fatalf("exactly one reset must win: %v", codes)
	}
}

func TestResetRefusedWhenParentBlockedAfterIssue(t *testing.T) {
	fx := newResetFx(t)
	rc := fx.issue(t)
	fx.exec(t, `UPDATE parents SET blocked_at = '2026-01-01T00:00:00.000Z' WHERE email = 'ana@example.com'`)
	if c, _ := fx.reset(t, "ana@example.com", rc, newPW); c != 400 {
		t.Fatalf("blocked: %d", c)
	}
	fx.exec(t, `UPDATE parents SET blocked_at = NULL WHERE email = 'ana@example.com'`)
	if c, _ := fx.reset(t, "ana@example.com", rc, newPW); c != 204 {
		t.Fatalf("code stays usable once unblocked: %d", c)
	}
}

func TestResetIPRateLimit(t *testing.T) {
	fx := newResetFx(t)
	var last int
	for i := 0; i < 11; i++ {
		last, _ = fx.reset(t, "ghost@example.com", "1234-5678", newPW)
	}
	if last != 429 {
		t.Fatalf("11th attempt: %d, want 429", last)
	}
}

func TestResetsCascadeWithAccountDeletion(t *testing.T) {
	fx := newResetFx(t)
	fx.issue(t)
	fx.exec(t, `DELETE FROM parents WHERE email = 'ana@example.com'`)
	if fx.count(t, `SELECT COUNT(*) FROM password_resets`) != 0 {
		t.Fatal("reset rows must go with the parent")
	}
}
