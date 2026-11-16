package importer

import "testing"

func TestParseChallenge(t *testing.T) {
	scheme, p := parseChallenge(`Bearer realm="https://auth.example.com/token",service="registry.example.com",scope="repository:a/b:pull,push"`)
	if scheme != "bearer" || p["realm"] != "https://auth.example.com/token" || p["service"] != "registry.example.com" ||
		p["scope"] != "repository:a/b:pull,push" {
		t.Errorf("bearer: %q %v", scheme, p)
	}
	scheme, p = parseChallenge(`Basic realm="doreg"`)
	if scheme != "basic" || p["realm"] != "doreg" {
		t.Errorf("basic: %q %v", scheme, p)
	}
	scheme, p = parseChallenge(`Bearer realm=https://x/token, service=reg`)
	if scheme != "bearer" || p["realm"] != "https://x/token" || p["service"] != "reg" {
		t.Errorf("unquoted: %q %v", scheme, p)
	}
}
