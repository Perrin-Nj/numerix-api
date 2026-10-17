package httpapi

import (
	"hash/fnv"
	"math"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

const limiterShards = 32

// Limiter is an in-memory token-bucket rate limiter keyed by string (Spring: Bucket4j / Resilience4j RateLimiter).
// Each key owns a bucket holding up to `limit` tokens that refills evenly over `per`. One request takes one token.
// shortcut: state lives in this process only, so with several instances each enforces its own limit; use Redis then.
type Limiter struct {
	now    func() time.Time // injectable clock for tests
	shards [limiterShards]limiterShard
}

type limiterShard struct {
	mu sync.Mutex
	m  map[string]*bucket
}

type bucket struct {
	tokens float64
	last   time.Time
}

// NewLimiter builds an empty limiter.
func NewLimiter() *Limiter {
	l := &Limiter{now: time.Now}
	for i := range l.shards {
		l.shards[i].m = map[string]*bucket{}
	}
	return l
}

// Allow takes one token from the bucket of key. When none is left it returns false and how long to wait.
func (l *Limiter) Allow(key string, limit int, per time.Duration) (bool, time.Duration) {
	h := fnv.New32a()
	_, _ = h.Write([]byte(key))
	s := &l.shards[h.Sum32()%limiterShards]
	now := l.now()
	rate := float64(limit) / per.Seconds() // tokens per second

	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.m) > 10000 { // bound memory: a bucket idle for a full period is full again, so forgetting it changes nothing
		for k, b := range s.m {
			if now.Sub(b.last) > per {
				delete(s.m, k)
			}
		}
	}
	b, ok := s.m[key]
	if !ok {
		b = &bucket{tokens: float64(limit), last: now}
		s.m[key] = b
	}
	b.tokens = math.Min(float64(limit), b.tokens+now.Sub(b.last).Seconds()*rate) // refill
	b.last = now
	if b.tokens >= 1 {
		b.tokens--
		return true, 0
	}
	wait := time.Duration((1 - b.tokens) / rate * float64(time.Second))
	return false, wait
}

// RetrySeconds rounds a wait up to whole seconds (at least 1) for Retry-After.
func RetrySeconds(d time.Duration) int { return max(1, int(math.Ceil(d.Seconds()))) }

// ClientIP returns the caller's IP. Behind a trusted proxy (TRUST_PROXY=true) it reads the first
// X-Forwarded-For entry; otherwise the socket address, which a client cannot forge.
func ClientIP(r *http.Request, trustProxy bool) string {
	if trustProxy {
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			return strings.TrimSpace(strings.Split(xff, ",")[0])
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// IPLimit applies a per-IP ceiling to every request (contract section 16: 600 per minute at the edge).
func IPLimit(l *Limiter, limit int, trustProxy bool) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if ok, wait := l.Allow("ip:"+ClientIP(r, trustProxy), limit, time.Minute); !ok {
				WriteError(w, r, RateLimited(RetrySeconds(wait)))
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
