package auth

import (
	"net"
	"net/http"
	"strings"
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

// loginLimits throttle password sign-in. A guesser is stopped quickly per
// (email, client) pair; the per-email lock needs many more failures, so
// someone else cannot lock an owner out with a handful of tries. The per
// client and global budgets cap guessing across many emails.
type loginLimits struct {
	pair, email, client, global *failureLimiter
}

func newLoginLimits() *loginLimits {
	const window = 15 * time.Minute
	return &loginLimits{
		pair:   newFailureLimiter(5, window),
		email:  newFailureLimiter(100, window),
		client: newFailureLimiter(30, window),
		global: newFailureLimiter(1000, window),
	}
}

// blocked returns the error code to show, or "" when the attempt may go ahead.
func (l *loginLimits) blocked(email, ip string) string {
	switch {
	case l.pair.Blocked(email + "\x00" + ip), l.email.Blocked(email):
		return "locked"
	case l.client.Blocked(ip), l.global.Blocked(""):
		return "rate_limited"
	}
	return ""
}

func (l *loginLimits) fail(email, ip string) {
	l.pair.Fail(email + "\x00" + ip)
	l.email.Fail(email)
	l.client.Fail(ip)
	l.global.Fail("")
}

// succeed clears the counters only the account owner can clear: knowing the
// password proves it. The client and global budgets are left alone, so
// signing in to your own account does not buy more guesses at others.
func (l *loginLimits) succeed(email, ip string) {
	l.pair.Reset(email + "\x00" + ip)
	l.email.Reset(email)
}

// clientIP returns the address used for rate limits. In production Traefik
// appends the address it accepted the connection from to X-Forwarded-For,
// and the web app's proxy passes the header on unchanged, so the rightmost
// entry is the one a client cannot forge. Without the header (direct
// access, or development) it is the TCP peer. If the control plane is ever
// reachable without Traefik in front, clients can choose this value, and
// only the per-email and global limits still hold.
func clientIP(r *http.Request) string {
	if xff := r.Header.Values("X-Forwarded-For"); len(xff) > 0 {
		last := xff[len(xff)-1]
		if i := strings.LastIndexByte(last, ','); i >= 0 {
			last = last[i+1:]
		}
		if ip := strings.TrimSpace(last); ip != "" {
			return ip
		}
	}
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		return host
	}
	return r.RemoteAddr
}
