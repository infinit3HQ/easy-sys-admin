package auth

import (
	"sync"
	"time"
)

// failureLimiter locks a key (an email address, or a client address) after
// too many failed sign-ins within a window.
type failureLimiter struct {
	mu     sync.Mutex
	max    int
	window time.Duration
	hits   map[string][]time.Time
}

func newFailureLimiter(max int, window time.Duration) *failureLimiter {
	return &failureLimiter{max: max, window: window, hits: map[string][]time.Time{}}
}

func (l *failureLimiter) prune(key string, now time.Time) []time.Time {
	ts := l.hits[key]
	kept := ts[:0]
	for _, t := range ts {
		if now.Sub(t) < l.window {
			kept = append(kept, t)
		}
	}
	if len(kept) == 0 {
		delete(l.hits, key)
		return nil
	}
	l.hits[key] = kept
	return kept
}

// Blocked reports whether key has used up its attempts.
func (l *failureLimiter) Blocked(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.prune(key, time.Now())) >= l.max
}

// Fail records a failed attempt.
func (l *failureLimiter) Fail(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	l.hits[key] = append(l.prune(key, now), now)
	if len(l.hits) > 100_000 {
		// Bound memory under a flood of distinct keys.
		for k := range l.hits {
			l.prune(k, now)
		}
	}
}

// Reset clears a key after a successful sign-in.
func (l *failureLimiter) Reset(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.hits, key)
}
