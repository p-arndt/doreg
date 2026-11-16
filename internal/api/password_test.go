package api_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strconv"
	"testing"

	"github.com/p-arndt/doreg/internal/meta"
)

func TestChangePasswordRejectsCLISecretAsCurrent(t *testing.T) {
	e := apitNewEnv(t)
	bob := e.login("bob")
	var created struct {
		Secret string `json:"secret"`
	}
	apitDecode(t, bob.expect(http.StatusOK, http.MethodPost, "/api/me/cli-secret", nil), &created)
	if created.Secret == "" {
		t.Fatal("no CLI secret returned")
	}

	bob.expect(http.StatusBadRequest, http.MethodPut, "/api/me/password",
		map[string]string{"current": created.Secret, "new": "new-password"})

	if _, ok := e.authn.CheckPassword(t.Context(), "bob", apitPassword); !ok {
		t.Error("password changed although the CLI secret was given as current password")
	}
}

func TestChangePasswordKeepsOnlyCurrentSession(t *testing.T) {
	e := apitNewEnv(t)
	first := e.login("bob")
	second := e.login("bob")

	first.expect(http.StatusNoContent, http.MethodPut, "/api/me/password",
		map[string]string{"current": apitPassword, "new": "new-password"})

	first.expect(http.StatusOK, http.MethodGet, "/api/me", nil)
	second.expect(http.StatusUnauthorized, http.MethodGet, "/api/me", nil)
}

func TestAdminSetPasswordEndsUserSessions(t *testing.T) {
	e := apitNewEnv(t)
	bob := e.login("bob")
	u, err := e.db.UserByName(t.Context(), "bob")
	if err != nil {
		t.Fatal(err)
	}

	e.admin.expect(http.StatusNoContent, http.MethodPut, "/api/users/"+strconv.FormatInt(u.ID, 10),
		map[string]string{"role": meta.RoleUser, "password": "new-password"})

	bob.expect(http.StatusUnauthorized, http.MethodGet, "/api/me", nil)
	e.admin.expect(http.StatusOK, http.MethodGet, "/api/me", nil)
}

func TestAdminSetOwnPasswordKeepsCurrentSession(t *testing.T) {
	e := apitNewEnv(t)
	other := e.login("admin")
	u, err := e.db.UserByName(t.Context(), "admin")
	if err != nil {
		t.Fatal(err)
	}

	e.admin.expect(http.StatusNoContent, http.MethodPut, "/api/users/"+strconv.FormatInt(u.ID, 10),
		map[string]string{"role": meta.RoleAdmin, "password": "new-password"})

	e.admin.expect(http.StatusOK, http.MethodGet, "/api/me", nil)
	other.expect(http.StatusUnauthorized, http.MethodGet, "/api/me", nil)
}

func TestAdminRoleChangeKeepsSessions(t *testing.T) {
	e := apitNewEnv(t)
	bob := e.login("bob")
	u, err := e.db.UserByName(t.Context(), "bob")
	if err != nil {
		t.Fatal(err)
	}

	e.admin.expect(http.StatusNoContent, http.MethodPut, "/api/users/"+strconv.FormatInt(u.ID, 10),
		map[string]string{"role": meta.RoleAdmin})

	bob.expect(http.StatusOK, http.MethodGet, "/api/me", nil)
}

func TestSSOAccountCannotSetPassword(t *testing.T) {
	e := newOIDCEnv(t, "")
	e.idp.setUser("sub-carol", "carol", "carol@example.com")
	if resp := e.login(t, "/"); resp.StatusCode != http.StatusFound {
		t.Fatalf("login: status %d, want 302", resp.StatusCode)
	}
	resp, body := e.do(t, e.client, http.MethodPost, "/api/me/cli-secret", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("POST cli-secret: status %d: %s", resp.StatusCode, body)
	}
	var created struct {
		Secret string `json:"secret"`
	}
	if err := json.Unmarshal(body, &created); err != nil {
		t.Fatal(err)
	}

	for _, current := range []string{"", created.Secret} {
		req, _ := json.Marshal(map[string]string{"current": current, "new": "new-password"})
		resp, body = e.do(t, e.client, http.MethodPut, "/api/me/password", bytes.NewReader(req))
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("PUT password: status %d, want 400: %s", resp.StatusCode, body)
		}
	}

	u, err := e.db.UserByName(t.Context(), "carol")
	if err != nil {
		t.Fatal(err)
	}
	if u.PasswordHash != "" {
		t.Error("SSO account got a local password")
	}
}
