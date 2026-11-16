package api_test

import (
	"net/http"
	"net/http/cookiejar"
	"testing"

	"github.com/p-arndt/doreg/internal/meta"
	"github.com/p-arndt/doreg/internal/oidc"
)

// loginAs signs in a fresh browser as the given identity and returns its role.
func (e *oidcEnv) loginAs(t *testing.T, sub, username, email string, verified bool, groups ...string) string {
	t.Helper()
	jar, _ := cookiejar.New(nil)
	e.client = noRedirectClient(jar)
	e.idp.setUser(sub, username, email, groups...)
	if !verified {
		e.idp.mu.Lock()
		e.idp.claims["email_verified"] = false
		e.idp.mu.Unlock()
	}
	if resp := e.login(t, "/"); resp.StatusCode != http.StatusFound || cookieNamed(resp, "doreg_session") == nil {
		t.Fatalf("login of %s failed: %d", username, resp.StatusCode)
	}
	_, role, _ := e.me(t, e.client)
	return role
}

func TestOIDCFirstUserBecomesAdminWithoutConfig(t *testing.T) {
	e := newOIDCEnv(t, "")
	if role := e.loginAs(t, "s1", "first", "first@example.com", true); role != meta.RoleAdmin {
		t.Fatalf("first SSO user role = %q, want admin", role)
	}
	if role := e.loginAs(t, "s2", "second", "second@example.com", true); role != meta.RoleUser {
		t.Fatalf("second SSO user role = %q, want user", role)
	}
	// Without a rule, logging in again keeps the role an admin assigned.
	if role := e.loginAs(t, "s1", "first", "first@example.com", true); role != meta.RoleAdmin {
		t.Fatalf("first user lost admin on re-login: %q", role)
	}
	entries, err := e.db.Audit(t.Context(), meta.AuditQuery{Action: "user_create"})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, en := range entries {
		if en.Resource == "first" && en.Detail != "" {
			found = true
		}
	}
	if !found {
		t.Error("no audit entry for the first admin")
	}
}

func TestOIDCFirstUserRuleOffWhenAdminExists(t *testing.T) {
	e := newOIDCEnv(t, "")
	if err := e.db.UpsertUser(t.Context(), "root", "", meta.RoleAdmin); err != nil {
		t.Fatal(err)
	}
	if role := e.loginAs(t, "s1", "first", "first@example.com", true); role != meta.RoleUser {
		t.Fatalf("SSO user became %q although an admin exists", role)
	}
}

func TestOIDCAdminEmails(t *testing.T) {
	e := newOIDCEnvCfg(t, func(c *oidc.Config) { c.AdminEmails = []string{"boss@example.com"} })
	// A configured rule disables the first-user rule.
	if role := e.loginAs(t, "s0", "someone", "someone@example.com", true); role != meta.RoleUser {
		t.Fatalf("first user with email rule = %q, want user", role)
	}
	if role := e.loginAs(t, "s1", "boss", "Boss@Example.com", true); role != meta.RoleAdmin {
		t.Fatalf("listed verified email = %q, want admin", role)
	}
	// An unverified address never grants admin, and the rule demotes on re-login.
	if role := e.loginAs(t, "s1", "boss", "boss@example.com", false); role != meta.RoleUser {
		t.Fatalf("unverified listed email = %q, want user", role)
	}
}
