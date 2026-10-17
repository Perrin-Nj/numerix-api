package auth

import (
	"context"
	"errors"
	"net/http"
	"strings"
)

// Role is who is calling. Values match `x-roles` in the OpenAPI contract.
type Role string

const (
	RoleParent Role = "parent"
	RoleChild  Role = "child"
)

// Principal is the authenticated caller, put in the request context by Middleware
// (Spring: the Authentication object inside SecurityContextHolder).
type Principal struct {
	Role     Role
	ParentID string // always set
	ChildID  string // only for role child
}

// ErrForbidden is returned by Require when the role is not allowed.
var ErrForbidden = errors.New("auth: role not allowed")

// ctxKey is a private type so no other package can collide with our context key.
type ctxKey struct{}

// WithPrincipal returns a copy of ctx that carries p.
func WithPrincipal(ctx context.Context, p Principal) context.Context {
	return context.WithValue(ctx, ctxKey{}, p)
}

// FromContext returns the Principal, and false for an anonymous request.
func FromContext(ctx context.Context) (Principal, bool) {
	p, ok := ctx.Value(ctxKey{}).(Principal)
	return p, ok
}

// Require returns ErrForbidden unless the principal has one of the roles
// (Spring: @PreAuthorize("hasAnyRole(...)")).
func Require(p Principal, roles ...Role) error {
	for _, r := range roles {
		if p.Role == r {
			return nil
		}
	}
	return ErrForbidden
}

// OwnsChild tells whether the principal may act on childID: a child only on itself.
// Parent ownership of a child is a database question, answered by the caller.
func (p Principal) OwnsChild(childID string) bool {
	return p.Role != RoleChild || p.ChildID == childID
}

// Middleware reads "Authorization: Bearer <jwt>" and, when the token is valid, stores the Principal
// in the context. It never rejects: anonymous requests pass through and the per-operation check
// (httpapi) answers 401 where the contract demands a token. This keeps public routes simple.
func Middleware(iss *Issuer) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if h := r.Header.Get("Authorization"); strings.HasPrefix(h, "Bearer ") {
				if p, err := iss.Parse(strings.TrimPrefix(h, "Bearer ")); err == nil {
					r = r.WithContext(WithPrincipal(r.Context(), p))
				}
			}
			next.ServeHTTP(w, r)
		})
	}
}
