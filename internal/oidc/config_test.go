package oidc

import (
	"reflect"
	"testing"
)

func TestConfigFromEnv(t *testing.T) {
	t.Setenv("OIDC_ISSUER", "")
	if c, err := ConfigFromEnv(); c != nil || err != nil {
		t.Fatalf("unset issuer: %v %v", c, err)
	}

	t.Setenv("OIDC_ISSUER", "http://localhost:9500/realms/dev")
	t.Setenv("OIDC_CLIENT_ID", "doreg")
	// `just` loads .env, which would otherwise supply these values.
	for _, k := range []string{"OIDC_CLIENT_SECRET", "OIDC_REDIRECT_URI", "OIDC_PROVIDER_NAME", "OIDC_SCOPES",
		"OIDC_USERNAME_CLAIM", "OIDC_GROUPS_CLAIM", "OIDC_ADMIN_ROLE", "OIDC_ADMIN_EMAILS"} {
		t.Setenv(k, "")
	}
	if _, err := ConfigFromEnv(); err == nil {
		t.Fatal("missing secret and redirect URI accepted")
	}

	t.Setenv("OIDC_CLIENT_SECRET", "s")
	t.Setenv("OIDC_REDIRECT_URI", "http://localhost:5000/api/auth/oidc/callback")
	t.Setenv("OIDC_SCOPES", "openid, profile email,roles")
	t.Setenv("OIDC_ADMIN_ROLE", "registry-admin")
	c, err := ConfigFromEnv()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(c.Scopes, []string{"openid", "profile", "email", "roles"}) {
		t.Errorf("scopes = %v", c.Scopes)
	}
	if c.ProviderName != "Single sign-on" || c.AdminRole != "registry-admin" || c.UsernameClaim != "preferred_username" {
		t.Errorf("config = %+v", c)
	}
}

func TestStringList(t *testing.T) {
	if got := stringList([]any{"a", 1, "b"}); !reflect.DeepEqual(got, []string{"a", "b"}) {
		t.Errorf("got %v", got)
	}
	if got := stringList("solo"); !reflect.DeepEqual(got, []string{"solo"}) {
		t.Errorf("got %v", got)
	}
}

func TestAdminRule(t *testing.T) {
	none := Config{}
	if configured, _ := none.adminRule(Identity{Groups: []string{"x"}}); configured {
		t.Error("empty config reported as configured")
	}
	c := Config{AdminRole: "registry-admin", AdminEmails: []string{"boss@example.com"}}
	cases := []struct {
		name string
		id   Identity
		want bool
	}{
		{"role", Identity{Groups: []string{"dev", "registry-admin"}}, true},
		{"verified email", Identity{Email: "BOSS@example.com", EmailVerified: true}, true},
		{"unverified email", Identity{Email: "boss@example.com"}, false},
		{"other", Identity{Email: "x@example.com", EmailVerified: true, Groups: []string{"dev"}}, false},
	}
	for _, tc := range cases {
		if configured, admin := c.adminRule(tc.id); !configured || admin != tc.want {
			t.Errorf("%s: admin = %v, want %v", tc.name, admin, tc.want)
		}
	}
}

func TestConfigAdminEmails(t *testing.T) {
	t.Setenv("OIDC_ISSUER", "http://issuer")
	t.Setenv("OIDC_CLIENT_ID", "doreg")
	t.Setenv("OIDC_CLIENT_SECRET", "s")
	t.Setenv("OIDC_REDIRECT_URI", "http://localhost:5000/api/auth/oidc/callback")
	t.Setenv("OIDC_ADMIN_EMAILS", "A@example.com, b@example.com")
	c, err := ConfigFromEnv()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(c.AdminEmails, []string{"a@example.com", "b@example.com"}) {
		t.Errorf("admin emails = %v", c.AdminEmails)
	}
}
