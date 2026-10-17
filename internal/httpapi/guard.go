package httpapi

import (
	"context"
	"net/http"
	"slices"
	"time"

	"github.com/Perrin-Nj/numerix-api/api"
	"github.com/Perrin-Nj/numerix-api/internal/auth"
)

// OpMeta is what the contract says about one operation (generated into opmeta_gen.go).
type OpMeta struct {
	Method string
	Path   string
	Public bool     // `security: []` in the contract: no token needed
	Roles  []string // `x-roles`: which roles may call it (empty on public ops)
}

// scope says whose bucket a rate limit uses.
type scope int

const (
	byIP        scope = iota // the caller's IP address
	byPrincipal              // parent id or child id from the token
	bySession                // the {sessionId} path value
)

// rule is one row of the rate-limit table in BACKEND_INTEGRATION.md section 16.
type rule struct {
	scope scope
	limit int
	per   time.Duration
}

// ruleFor (opID is the Go operation name, e.g. LoginParent) returns the limit for an operation: explicit rows first, then "other reads" / "other writes".
func ruleFor(opID string, method string) rule {
	switch opID {
	case "RegisterParent":
		return rule{byIP, 5, time.Hour}
	case "LoginParent":
		return rule{byIP, 10, time.Minute} // the 5-failures-per-email lockout is done by the handler (auth.AttemptLimiter)
	case "RefreshToken":
		return rule{byIP, 30, time.Minute}
	case "CreateChildToken":
		return rule{byPrincipal, 10, time.Minute} // the 5-wrong-PINs lockout is done by the handler
	case "StartSession":
		return rule{byPrincipal, 10, time.Minute}
	case "NextBeat", "AnswerBeat", "StreamNarration":
		return rule{bySession, 30, time.Minute}
	case "GetFeed", "ReactToFeedItem", "AnswerFeedPuzzle":
		return rule{byPrincipal, 60, time.Minute}
	case "SubscribePush", "CheckoutSubscription":
		return rule{byPrincipal, 10, time.Hour}
	}
	if method == http.MethodGet {
		return rule{byPrincipal, 300, time.Minute} // other reads
	}
	return rule{byPrincipal, 60, time.Minute} // other writes
}

// Guard returns the strict-handler middleware that, for every operation, enforces in this order:
// public -> IP rate limit; protected -> 401 without a valid token, 403 for a wrong role, then rate limit per principal.
// (Spring: Spring Security's filter chain + @PreAuthorize + a rate-limit interceptor, driven by the contract.)
func Guard(l *Limiter, trustProxy bool) api.StrictMiddlewareFunc {
	return func(next api.StrictHandlerFunc, opID string) api.StrictHandlerFunc {
		meta, known := Ops[opID]
		return func(ctx context.Context, w http.ResponseWriter, r *http.Request, req any) (any, error) {
			if !known { // an operation missing from the table means the generator was not re-run: fail closed
				return nil, ErrForbidden
			}
			ctx = WithExchange(ctx, w, r) // lets handlers read and set cookies (device sessions)
			p, authed := auth.FromContext(ctx)
			if !meta.Public {
				if !authed {
					return nil, ErrUnauthorized
				}
				if len(meta.Roles) > 0 && !slices.Contains(meta.Roles, string(p.Role)) {
					return nil, ErrForbidden
				}
			}
			if opID != "Health" && opID != "Ready" { // probes must never be throttled
				ru := ruleFor(opID, meta.Method)
				key := opID + ":"
				switch {
				case ru.scope == bySession:
					key += r.PathValue("sessionId")
				case ru.scope == byPrincipal && authed && p.Role == auth.RoleChild:
					key += "c:" + p.ChildID
				case ru.scope == byPrincipal && authed:
					key += "p:" + p.ParentID
				default:
					key += "ip:" + ClientIP(r, trustProxy)
				}
				if ok, wait := l.Allow(key, ru.limit, ru.per); !ok {
					return nil, RateLimited(RetrySeconds(wait))
				}
			}
			return next(ctx, w, r, req)
		}
	}
}
