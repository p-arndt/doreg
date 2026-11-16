package registry_test

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/p-arndt/doreg/internal/auth"
	"github.com/p-arndt/doreg/internal/registry"
)

func TestBasicAuthRateLimited(t *testing.T) {
	e := newEnv(t)
	authn := auth.New(e.db)
	authn.Limiter.UserFailures = 3
	h := &registry.Handler{DB: e.db, Store: e.store, Auth: authn, Log: slog.New(slog.DiscardHandler)}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)

	get := func(ip, password string) *http.Response {
		t.Helper()
		req, _ := http.NewRequest(http.MethodGet, srv.URL+"/v2/", nil)
		req.SetBasicAuth("admin", password)
		req.Header.Set("X-Forwarded-For", ip)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { resp.Body.Close() })
		return resp
	}

	for range 3 {
		expect(t, get("192.0.2.1", "wrong"), http.StatusUnauthorized)
	}
	resp := get("192.0.2.1", testPassword)
	expect(t, resp, http.StatusTooManyRequests)
	if resp.Header.Get("Retry-After") == "" {
		t.Error("429 without Retry-After")
	}
	var body struct {
		Errors []struct{ Code string } `json:"errors"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil || len(body.Errors) == 0 || body.Errors[0].Code != "TOOMANYREQUESTS" {
		t.Errorf("error body = %+v (%v), want code TOOMANYREQUESTS", body, err)
	}

	expect(t, get("192.0.2.2", testPassword), http.StatusOK)
}
