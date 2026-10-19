package httpapi

import (
	"context"
	"net/http"
	"slices"
	"sync"
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
	case "GetAsset":
		return rule{byIP, 1200, time.Minute} // a world page loads many images; they are cached for a year after the first load
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
			if err := authorize(meta, p, authed); err != nil {
				return nil, err
			}
			// QA-BE-003: a replay of an already-served Idempotency-Key returns the stored answer and takes no rate-limit token.
			rk := replayKey(opID, r, p)
			if rk != "" {
				if resp, hit := replays.get(rk); hit {
					return resp, nil
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
			resp, err := next(ctx, w, r, req)
			if err == nil && rk != "" {
				replays.put(rk, resp)
			}
			return resp, err
		}
	}
}

// authorize is the 401/403 check, shared by the guard and by the decode-error hooks (QA-BE-002).
func authorize(meta OpMeta, p auth.Principal, authed bool) error {
	if meta.Public {
		return nil
	}
	if !authed {
		return ErrUnauthorized
	}
	if len(meta.Roles) > 0 && !slices.Contains(meta.Roles, string(p.Role)) {
		return ErrForbidden
	}
	return nil
}

// opsByPattern finds an operation from the mux pattern Go records on the request ("POST /v1/children/{childId}/sessions").
var opsByPattern = func() map[string]OpMeta {
	m := make(map[string]OpMeta, len(Ops))
	for _, o := range Ops {
		m[o.Method+" /v1"+o.Path] = o
	}
	return m
}()

// authFirst runs when the generated code fails to decode the body or a parameter, which happens BEFORE the guard.
// If the caller is not allowed to call the operation it writes 401/403 and returns true (auth always beats validation).
func authFirst(w http.ResponseWriter, r *http.Request) bool {
	meta, ok := opsByPattern[r.Pattern]
	if !ok {
		return false
	}
	p, authed := auth.FromContext(r.Context())
	if err := authorize(meta, p, authed); err != nil {
		WriteError(w, r, err)
		return true
	}
	return false
}

// replayOps are the operations whose handlers store their result per Idempotency-Key (contract section 4).
var replayOps = map[string]bool{"StartSession": true, "NextBeat": true, "AnswerBeat": true, "CheckoutSubscription": true}

// replays remembers recent successful answers so a retry never hits the rate limiter.
// shortcut: per process and lost on restart; after a restart a replay is limited like any request (the handlers still store the durable result). Use Redis with several instances.
var replays = &replayCache{m: map[string]replayEntry{}}

type replayEntry struct {
	resp    any
	expires time.Time
}

type replayCache struct {
	mu sync.Mutex
	m  map[string]replayEntry
}

const (
	replayTTL = 10 * time.Minute
	replayMax = 5000 // bound memory
)

// replayKey is "" when the request is not an idempotent replay candidate. It includes the caller and the full path, so one caller can never read another's result.
func replayKey(opID string, r *http.Request, p auth.Principal) string {
	k := r.Header.Get("Idempotency-Key")
	if !replayOps[opID] || k == "" || len(k) > 64 {
		return ""
	}
	return opID + "|" + p.ParentID + "|" + p.ChildID + "|" + r.URL.Path + "|" + k
}

func (c *replayCache) get(k string) (any, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.m[k]
	if !ok || time.Now().After(e.expires) {
		delete(c.m, k)
		return nil, false
	}
	return e.resp, true
}

func (c *replayCache) put(k string, resp any) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.m) >= replayMax { // drop expired entries, then everything if still full (replays then fall back to the stored result)
		now := time.Now()
		for key, e := range c.m {
			if now.After(e.expires) {
				delete(c.m, key)
			}
		}
		if len(c.m) >= replayMax {
			clear(c.m)
		}
	}
	c.m[k] = replayEntry{resp: resp, expires: time.Now().Add(replayTTL)}
}
