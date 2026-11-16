package api_test

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/p-arndt/doreg/internal/meta"
)

func apitAuditActions(t *testing.T, e *apitEnv, action string) []meta.AuditEntry {
	t.Helper()
	entries, err := e.db.Audit(context.Background(), meta.AuditQuery{Action: action, Limit: 1000})
	if err != nil {
		t.Fatal(err)
	}
	return entries
}

func TestAPILoginRateLimited(t *testing.T) {
	e := apitNewEnv(t)
	anon := &apitClient{t: t, base: e.srv.URL, http: &http.Client{}}
	bad := map[string]string{"username": "bob", "password": "wrong"}
	limit := e.authn.Limiter.UserFailures
	for range limit {
		anon.expect(http.StatusUnauthorized, http.MethodPost, "/api/login", bad)
	}
	if n := len(apitAuditActions(t, e, "login_failed")); n != limit {
		t.Fatalf("%d login_failed entries, want %d", n, limit)
	}

	req, _ := http.NewRequest(http.MethodPost, e.srv.URL+"/api/login", strings.NewReader(`{"username":"bob","password":"wrong"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", e.srv.URL)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("status %d, want 429", resp.StatusCode)
	}
	if resp.Header.Get("Retry-After") == "" {
		t.Error("429 without Retry-After")
	}

	// The correct password is refused too while blocked, so guessing gains nothing.
	anon.expect(http.StatusTooManyRequests, http.MethodPost, "/api/login", map[string]string{"username": "bob", "password": apitPassword})
	if n := len(apitAuditActions(t, e, "login_failed")); n != limit {
		t.Errorf("%d login_failed entries after blocking, want %d", n, limit)
	}

	// Other accounts from the same client are only limited per IP.
	e.login("carol")
}

func TestAPILoginSuccessResetsFailures(t *testing.T) {
	e := apitNewEnv(t)
	anon := &apitClient{t: t, base: e.srv.URL, http: &http.Client{}}
	bad := map[string]string{"username": "bob", "password": "wrong"}
	for range e.authn.Limiter.UserFailures - 1 {
		anon.expect(http.StatusUnauthorized, http.MethodPost, "/api/login", bad)
	}
	e.login("bob")
	anon.expect(http.StatusUnauthorized, http.MethodPost, "/api/login", bad)
}

func TestAPILoginFailedTruncatesUsername(t *testing.T) {
	e := apitNewEnv(t)
	anon := &apitClient{t: t, base: e.srv.URL, http: &http.Client{}}
	long := strings.Repeat("ü", 10000)
	anon.expect(http.StatusUnauthorized, http.MethodPost, "/api/login", map[string]string{"username": long, "password": "x"})
	entries := apitAuditActions(t, e, "login_failed")
	if len(entries) != 1 {
		t.Fatalf("%d login_failed entries, want 1", len(entries))
	}
	if got := entries[0].Actor; got != strings.Repeat("ü", 128) {
		t.Errorf("audit actor has %d runes, want 128", len([]rune(got)))
	}
}
