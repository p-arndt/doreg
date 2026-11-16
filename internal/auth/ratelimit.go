package auth

import (
	"crypto/sha256"
	"strconv"
	"sync"
	"time"
)

// Default limits for failed credential checks. The per-client limit stops
// password guessing against one account; the per-IP limit stops a client from
// rotating usernames. A username is never locked on its own, so nobody can lock
// a real user out from elsewhere.
const (
	DefaultUserFailures = 10
	DefaultIPFailures   = 50
	DefaultFailWindow   = 5 * time.Minute
	DefaultMaxTracked   = 10000
)

// Limiter counts failed credential checks per client IP and per client IP
// plus username in fixed windows. It is safe for concurrent use.
type Limiter struct {
	UserFailures int           // failures per IP and username in one window
	IPFailures   int           // failures per IP in one window
	Window       time.Duration // window length; a block lasts until it ends
	MaxTracked   int           // bound on tracked keys, so spoofed or rotating keys cannot grow memory
	Now          func() time.Time

	mu      sync.Mutex
	entries map[[32]byte]*window
}

type window struct {
	failures int
	ends     time.Time
}

// NewLimiter returns a Limiter with the default limits.
func NewLimiter() *Limiter {
	return &Limiter{
		UserFailures: DefaultUserFailures,
		IPFailures:   DefaultIPFailures,
		Window:       DefaultFailWindow,
		MaxTracked:   DefaultMaxTracked,
		Now:          time.Now,
		entries:      map[[32]byte]*window{},
	}
}

// Keys are hashed so an attacker-chosen username (up to a full header or
// request body) costs a fixed 32 bytes. The prefix byte keeps the IP-only key
// from colliding with an IP+username key.
func ipKey(ip string) [32]byte { return sha256.Sum256([]byte("i\x00" + ip)) }

func userKey(ip, username string) [32]byte {
	return sha256.Sum256([]byte("u\x00" + ip + "\x00" + username))
}

// Allowed reports whether a credential check from ip for username may run, and
// if not, how long the client has to wait.
func (l *Limiter) Allowed(ip, username string) (bool, time.Duration) {
	now := l.Now()
	l.mu.Lock()
	defer l.mu.Unlock()
	var wait time.Duration
	for _, k := range []struct {
		key   [32]byte
		limit int
	}{{ipKey(ip), l.IPFailures}, {userKey(ip, username), l.UserFailures}} {
		w, ok := l.entries[k.key]
		if !ok || !now.Before(w.ends) || w.failures < k.limit {
			continue
		}
		wait = max(wait, w.ends.Sub(now))
	}
	return wait == 0, wait
}

// Failed records a failed credential check.
func (l *Limiter) Failed(ip, username string) {
	now := l.Now()
	l.mu.Lock()
	defer l.mu.Unlock()
	l.bump(ipKey(ip), now)
	l.bump(userKey(ip, username), now)
}

// Succeeded clears the failures of ip and username. The per-IP count stays, so
// a client cannot interleave a known login to keep guessing at other accounts.
func (l *Limiter) Succeeded(ip, username string) {
	k := userKey(ip, username)
	l.mu.Lock()
	delete(l.entries, k)
	l.mu.Unlock()
}

func (l *Limiter) bump(k [32]byte, now time.Time) {
	if w, ok := l.entries[k]; ok && now.Before(w.ends) {
		w.failures++
		return
	}
	if _, ok := l.entries[k]; !ok && len(l.entries) >= l.MaxTracked {
		l.evict(now)
	}
	l.entries[k] = &window{failures: 1, ends: now.Add(l.Window)}
}

// evict drops expired windows and, if the table is still full, arbitrary live
// ones: forgetting a few counters is better than unbounded memory.
func (l *Limiter) evict(now time.Time) {
	for k, w := range l.entries {
		if !now.Before(w.ends) {
			delete(l.entries, k)
		}
	}
	for k := range l.entries {
		if len(l.entries) < l.MaxTracked {
			break
		}
		delete(l.entries, k)
	}
}

// Tracked returns the number of keys held, for tests and metrics.
func (l *Limiter) Tracked() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.entries)
}

// RetryAfter formats d as a Retry-After header value: whole seconds, rounded
// up, at least 1.
func RetryAfter(d time.Duration) string {
	return strconv.Itoa(max(1, int((d+time.Second-1)/time.Second)))
}
