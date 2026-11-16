package auth

import (
	"fmt"
	"testing"
	"time"
)

type fakeClock struct{ t time.Time }

func (c *fakeClock) now() time.Time          { return c.t }
func (c *fakeClock) advance(d time.Duration) { c.t = c.t.Add(d) }

func testLimiter() (*Limiter, *fakeClock) {
	c := &fakeClock{t: time.Unix(1_700_000_000, 0)}
	l := NewLimiter()
	l.UserFailures, l.IPFailures, l.Window = 3, 5, time.Minute
	l.Now = c.now
	return l, c
}

func mustAllow(t *testing.T, l *Limiter, ip, user string, want bool) time.Duration {
	t.Helper()
	ok, wait := l.Allowed(ip, user)
	if ok != want {
		t.Fatalf("Allowed(%q, %q) = %v, want %v", ip, user, ok, want)
	}
	if ok && wait != 0 || !ok && wait <= 0 {
		t.Fatalf("Allowed(%q, %q) wait = %v with ok = %v", ip, user, wait, ok)
	}
	return wait
}

func TestLimiterBlocksAfterUserFailures(t *testing.T) {
	l, c := testLimiter()
	for range 3 {
		mustAllow(t, l, "1.1.1.1", "alice", true)
		l.Failed("1.1.1.1", "alice")
	}
	c.advance(10 * time.Second)
	if wait := mustAllow(t, l, "1.1.1.1", "alice", false); wait != 50*time.Second {
		t.Errorf("wait = %v, want 50s (rest of the window)", wait)
	}
	mustAllow(t, l, "2.2.2.2", "alice", true)
	mustAllow(t, l, "1.1.1.1", "bob", true)
}

func TestLimiterBlocksIPAcrossUsernames(t *testing.T) {
	l, _ := testLimiter()
	for i := range 5 {
		l.Failed("1.1.1.1", fmt.Sprintf("user%d", i))
	}
	mustAllow(t, l, "1.1.1.1", "fresh", false)
	mustAllow(t, l, "2.2.2.2", "fresh", true)
}

func TestLimiterSuccessResetsUserKey(t *testing.T) {
	l, _ := testLimiter()
	for range 2 {
		l.Failed("1.1.1.1", "alice")
	}
	l.Succeeded("1.1.1.1", "alice")
	for range 2 {
		l.Failed("1.1.1.1", "alice")
	}
	mustAllow(t, l, "1.1.1.1", "alice", true)
	l.Failed("1.1.1.1", "alice")
	mustAllow(t, l, "1.1.1.1", "alice", false)
}

func TestLimiterWindowExpires(t *testing.T) {
	l, c := testLimiter()
	for range 5 {
		l.Failed("1.1.1.1", "alice")
	}
	mustAllow(t, l, "1.1.1.1", "alice", false)
	c.advance(time.Minute)
	mustAllow(t, l, "1.1.1.1", "alice", true)
	l.Failed("1.1.1.1", "alice")
	mustAllow(t, l, "1.1.1.1", "alice", true)
}

func TestLimiterMemoryCap(t *testing.T) {
	l, c := testLimiter()
	l.MaxTracked = 100
	for i := range 1000 {
		l.Failed(fmt.Sprintf("10.0.%d.%d", i/256, i%256), "x")
		if n := l.Tracked(); n > l.MaxTracked {
			t.Fatalf("tracking %d keys, cap %d", n, l.MaxTracked)
		}
	}
	c.advance(time.Minute)
	l.Failed("1.1.1.1", "alice")
	if n := l.Tracked(); n != 2 {
		t.Errorf("after expiry tracking %d keys, want 2", n)
	}
}

func TestRetryAfter(t *testing.T) {
	for d, want := range map[time.Duration]string{
		0:                       "1",
		time.Millisecond:        "1",
		time.Second:             "1",
		1500 * time.Millisecond: "2",
		5 * time.Minute:         "300",
	} {
		if got := RetryAfter(d); got != want {
			t.Errorf("RetryAfter(%v) = %q, want %q", d, got, want)
		}
	}
}
