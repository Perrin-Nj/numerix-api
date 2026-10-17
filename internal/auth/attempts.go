package auth

import (
	"hash/fnv"
	"sync"
	"time"
)

// attemptShards splits the map into independent locks so 200 kids do not queue on one mutex.
const attemptShards = 16

// AttemptLimiter counts failures per key inside a sliding window and locks the key once the
// maximum is reached. Used for child PINs (key = child id, 5 per 15 min) and login by email.
// In memory, single node: a restart clears it. shortcut: not shared across instances, move to Redis/DB when scaling out.
type AttemptLimiter struct {
	max    int
	window time.Duration
	now    func() time.Time
	shards [attemptShards]attemptShard
}

type attemptShard struct {
	mu sync.Mutex
	m  map[string][]time.Time // failure timestamps, oldest first, at most max entries
}

// NewAttemptLimiter builds a limiter allowing max-1 failures; the max-th failure locks the key.
func NewAttemptLimiter(max int, window time.Duration) *AttemptLimiter {
	l := &AttemptLimiter{max: max, window: window, now: time.Now}
	for i := range l.shards {
		l.shards[i].m = map[string][]time.Time{}
	}
	return l
}

func (l *AttemptLimiter) shard(key string) *attemptShard {
	h := fnv.New32a()
	_, _ = h.Write([]byte(key))
	return &l.shards[h.Sum32()%attemptShards]
}

// prune drops failures older than the window. Caller holds the lock.
func (l *AttemptLimiter) prune(s *attemptShard, key string, now time.Time) []time.Time {
	ts := s.m[key]
	i := 0
	for i < len(ts) && now.Sub(ts[i]) >= l.window {
		i++
	}
	ts = ts[i:]
	if len(ts) == 0 {
		delete(s.m, key) // keeps the map from growing forever
		return nil
	}
	s.m[key] = ts
	return ts
}

// Locked reports whether key is locked and, if so, how long until it unlocks (for retry_after_seconds).
func (l *AttemptLimiter) Locked(key string) (bool, time.Duration) {
	s := l.shard(key)
	s.mu.Lock()
	defer s.mu.Unlock()
	now := l.now()
	ts := l.prune(s, key, now)
	if len(ts) >= l.max {
		return true, ts[0].Add(l.window).Sub(now)
	}
	return false, 0
}

// Fail records one wrong attempt.
func (l *AttemptLimiter) Fail(key string) {
	s := l.shard(key)
	s.mu.Lock()
	defer s.mu.Unlock()
	now := l.now()
	ts := append(l.prune(s, key, now), now)
	if len(ts) > l.max {
		ts = ts[len(ts)-l.max:]
	}
	s.m[key] = ts
}

// Reset forgets the failures of key (call after a correct PIN).
func (l *AttemptLimiter) Reset(key string) {
	s := l.shard(key)
	s.mu.Lock()
	delete(s.m, key)
	s.mu.Unlock()
}
