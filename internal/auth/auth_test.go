package auth

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/Perrin-Nj/numerix-api/internal/store"
)

func TestHashVerify(t *testing.T) {
	h, err := Hash("1234")
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name, secret, hash string
		want               bool
		wantErr            bool
	}{
		{"right", "1234", h, true, false},
		{"wrong", "1235", h, false, false},
		{"malformed", "1234", "not-a-hash", false, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Verify(tc.secret, tc.hash)
			if got != tc.want || (err != nil) != tc.wantErr {
				t.Fatalf("got (%v, %v)", got, err)
			}
		})
	}
	h2, _ := Hash("1234")
	if h == h2 {
		t.Fatal("salt must make hashes differ")
	}
}

func TestJWT(t *testing.T) {
	iss := NewIssuer("0123456789abcdef0123456789abcdef", 15*time.Minute, 8*time.Hour)
	tok, exp, err := iss.IssueParent("p1")
	if err != nil || exp != 900 {
		t.Fatalf("parent token: %v exp=%d", err, exp)
	}
	if p, err := iss.Parse(tok); err != nil || p != (Principal{Role: RoleParent, ParentID: "p1"}) {
		t.Fatalf("parse parent: %+v %v", p, err)
	}
	ctok, exp, _ := iss.IssueChild("p1", "c1")
	if p, err := iss.Parse(ctok); err != nil || p.ChildID != "c1" || p.Role != RoleChild || exp != 28800 {
		t.Fatalf("parse child: %+v %v exp=%d", p, err, exp)
	}
	other := NewIssuer("another-secret-another-secret-123456", time.Minute, time.Minute)
	if _, err := other.Parse(tok); !errors.Is(err, ErrInvalidToken) {
		t.Fatal("token signed with another key must fail")
	}
	late := NewIssuer("0123456789abcdef0123456789abcdef", time.Minute, time.Minute)
	late.now = func() time.Time { return time.Now().Add(time.Hour) }
	if _, err := late.Parse(tok); !errors.Is(err, ErrInvalidToken) {
		t.Fatal("expired token must fail")
	}
	if _, err := iss.Parse("garbage"); err == nil {
		t.Fatal("garbage must fail")
	}
}

func TestRequireAndMiddleware(t *testing.T) {
	if Require(Principal{Role: RoleChild}, RoleParent) == nil || Require(Principal{Role: RoleParent}, RoleParent, RoleChild) != nil {
		t.Fatal("Require wrong")
	}
	if (Principal{Role: RoleChild, ChildID: "a"}).OwnsChild("b") {
		t.Fatal("child must not own another child")
	}
	iss := NewIssuer("0123456789abcdef0123456789abcdef", time.Minute, time.Minute)
	tok, _, _ := iss.IssueParent("p1")
	var got Principal
	var ok bool
	h := Middleware(iss)(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) { got, ok = FromContext(r.Context()) }))
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	h.ServeHTTP(httptest.NewRecorder(), req)
	if !ok || got.ParentID != "p1" {
		t.Fatalf("principal missing: %+v", got)
	}
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/", nil))
	if ok {
		t.Fatal("anonymous request must have no principal")
	}
}

func TestAttemptLimiter(t *testing.T) {
	l := NewAttemptLimiter(5, 15*time.Minute)
	now := time.Now()
	l.now = func() time.Time { return now }
	for i := 0; i < 4; i++ {
		l.Fail("c1")
	}
	if locked, _ := l.Locked("c1"); locked {
		t.Fatal("4 failures must not lock")
	}
	l.Fail("c1")
	locked, retry := l.Locked("c1")
	if !locked || retry != 15*time.Minute {
		t.Fatalf("5th failure must lock for 15 min, got %v %v", locked, retry)
	}
	if locked, _ := l.Locked("c2"); locked {
		t.Fatal("other key must be free")
	}
	now = now.Add(15 * time.Minute) // window passes
	if locked, _ := l.Locked("c1"); locked {
		t.Fatal("lock must expire")
	}
	l.Fail("c1")
	l.Reset("c1")
	for i := 0; i < 4; i++ {
		l.Fail("c1")
	}
	if locked, _ := l.Locked("c1"); locked {
		t.Fatal("Reset must clear the count")
	}
}

func TestRefreshRotation(t *testing.T) {
	ctx := context.Background()
	s, err := store.Open(ctx, filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if _, err := s.W.ExecContext(ctx, `INSERT INTO parents (id, email, password_hash) VALUES ('p1','a@x.test','x')`); err != nil {
		t.Fatal(err)
	}
	rt := NewRefreshTokens(s, time.Hour)
	t1, err := rt.Issue(ctx, "p1")
	if err != nil {
		t.Fatal(err)
	}
	pid, t2, err := rt.Rotate(ctx, t1)
	if err != nil || pid != "p1" || t2 == "" || t2 == t1 {
		t.Fatalf("rotate: %v %v", pid, err)
	}
	// Reusing t1 is refused and revokes the family, so t2 dies too.
	if _, _, err := rt.Rotate(ctx, t1); !errors.Is(err, ErrRefreshInvalid) {
		t.Fatalf("reuse must fail, got %v", err)
	}
	if _, _, err := rt.Rotate(ctx, t2); !errors.Is(err, ErrRefreshInvalid) {
		t.Fatalf("family must be revoked, got %v", err)
	}
	if _, _, err := rt.Rotate(ctx, "unknown"); !errors.Is(err, ErrRefreshInvalid) {
		t.Fatal("unknown token must fail")
	}
	// Logout revokes a live family; calling it twice or with junk is fine.
	t3, _ := rt.Issue(ctx, "p1")
	if err := rt.Revoke(ctx, t3); err != nil {
		t.Fatal(err)
	}
	if err := rt.Revoke(ctx, "junk"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := rt.Rotate(ctx, t3); !errors.Is(err, ErrRefreshInvalid) {
		t.Fatal("revoked token must fail")
	}
	// Expiry.
	rt.now = func() time.Time { return time.Now().Add(2 * time.Hour) }
	t4, _ := NewRefreshTokens(s, time.Hour).Issue(ctx, "p1")
	if _, _, err := rt.Rotate(ctx, t4); !errors.Is(err, ErrRefreshInvalid) {
		t.Fatal("expired token must fail")
	}
}
