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
