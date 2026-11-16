package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"testing"

	"github.com/p-arndt/doreg/internal/auth"
	"github.com/p-arndt/doreg/internal/meta"
)

var clitUserCode = regexp.MustCompile(`^[BCDFGHJKLMNPQRSTVWXZ]{4}-[BCDFGHJKLMNPQRSTVWXZ]{4}$`)

// clitCall sends a request without an Origin header, as the CLI does.
func clitCall(t *testing.T, base, method, path string, body any) (int, map[string]any) {
	t.Helper()
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, base+path, rd)
	if err != nil {
		t.Fatal(err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	var out map[string]any
	if len(data) > 0 {
		if err := json.Unmarshal(data, &out); err != nil {
			t.Fatalf("decode %s: %v", data, err)
		}
	}
	return resp.StatusCode, out
}

// clitStart opens a device login and returns the device code and user code.
func clitStart(t *testing.T, base, client string) (string, string) {
	t.Helper()
	status, out := clitCall(t, base, http.MethodPost, "/api/cli/device", map[string]string{"client": client})
	if status != http.StatusOK {
		t.Fatalf("POST /api/cli/device: status %d, want 200: %v", status, out)
	}
	deviceCode, _ := out["deviceCode"].(string)
	userCode, _ := out["userCode"].(string)
	if deviceCode == "" || !clitUserCode.MatchString(userCode) {
		t.Fatalf("device response = %v", out)
	}
	return deviceCode, userCode
}

// clitPoll polls the token endpoint and returns the status and error code or body.
func clitPoll(t *testing.T, base, deviceCode string) (int, map[string]any) {
	t.Helper()
	return clitCall(t, base, http.MethodPost, "/api/cli/token", map[string]string{"deviceCode": deviceCode})
}

func clitExpectError(t *testing.T, status int, out map[string]any, want string) {
	t.Helper()
	if status != http.StatusBadRequest || out["error"] != want {
		t.Fatalf("poll = status %d %v, want 400 %s", status, out, want)
	}
}

func TestCLILoginFlow(t *testing.T) {
	e := apitNewEnv(t)
	ctx := context.Background()
	deviceCode, userCode := clitStart(t, e.srv.URL, "  macbook (darwin)  ")

	bob := e.login("bob")
	var status struct {
		UserCode string `json:"userCode"`
		Client   string `json:"client"`
		Status   string `json:"status"`
	}
	apitDecode(t, bob.expect(http.StatusOK, http.MethodGet, "/api/cli/device/"+userCode, nil), &status)
	if status.Client != "macbook (darwin)" || status.Status != meta.DevicePending || status.UserCode != userCode {
		t.Fatalf("device status = %+v", status)
	}

	code, out := clitPoll(t, e.srv.URL, deviceCode)
	clitExpectError(t, code, out, "authorization_pending")

	bob.expect(http.StatusNoContent, http.MethodPost, "/api/cli/device/"+userCode+"/approve", nil)

	code, out = clitPoll(t, e.srv.URL, deviceCode)
	if code != http.StatusOK {
		t.Fatalf("poll after approval = status %d %v, want 200", code, out)
	}
	token, _ := out["token"].(string)
	if out["username"] != "bob" || !strings.HasPrefix(token, auth.TokenPrefix) {
		t.Fatalf("token response = %v", out)
	}
	if exp, _ := out["expiresAt"].(float64); int64(exp) < meta.Now()+80*24*3600 {
		t.Errorf("expiresAt = %v, want about 90 days out", exp)
	}

	code, out = clitPoll(t, e.srv.URL, deviceCode)
	clitExpectError(t, code, out, "expired_token")

	if _, ok := e.authn.Basic(ctx, "bob", token); !ok {
		t.Fatal("issued token does not authenticate as bob")
	}
	if _, ok := e.authn.Basic(ctx, "carol", token); ok {
		t.Fatal("issued token authenticates as another user")
	}

	var tokens []meta.CLIToken
	apitDecode(t, bob.expect(http.StatusOK, http.MethodGet, "/api/me/tokens", nil), &tokens)
	if len(tokens) != 1 || tokens[0].Name != "macbook (darwin)" {
		t.Fatalf("tokens = %+v, want one named after the client", tokens)
	}

	carol := e.login("carol")
	carol.expect(http.StatusNotFound, http.MethodDelete, fmt.Sprintf("/api/me/tokens/%d", tokens[0].ID), nil)

	bob.expect(http.StatusNoContent, http.MethodDelete, fmt.Sprintf("/api/me/tokens/%d", tokens[0].ID), nil)
	if _, ok := e.authn.Basic(ctx, "bob", token); ok {
		t.Fatal("revoked token still authenticates")
	}

	var audit []struct {
		Action string `json:"action"`
	}
	apitDecode(t, e.admin.expect(http.StatusOK, http.MethodGet, "/api/audit", nil), &audit)
	seen := map[string]bool{}
	for _, en := range audit {
		seen[en.Action] = true
	}
	for _, want := range []string{"cli_login_approve", "cli_token_revoke"} {
		if !seen[want] {
			t.Errorf("audit has no %s entry", want)
		}
	}
}

func TestCLILoginDeny(t *testing.T) {
	e := apitNewEnv(t)
	deviceCode, userCode := clitStart(t, e.srv.URL, "ci runner")

	bob := e.login("bob")
	bob.expect(http.StatusNoContent, http.MethodPost, "/api/cli/device/"+userCode+"/deny", nil)

	code, out := clitPoll(t, e.srv.URL, deviceCode)
	clitExpectError(t, code, out, "access_denied")

	code, out = clitPoll(t, e.srv.URL, deviceCode)
	clitExpectError(t, code, out, "expired_token")

	// The poll consumed the decided request, so the code is gone.
	bob.expect(http.StatusNotFound, http.MethodPost, "/api/cli/device/"+userCode+"/approve", nil)
}

func TestCLILoginExpired(t *testing.T) {
	e := apitNewEnv(t)
	deviceCode, userCode := clitStart(t, e.srv.URL, "laptop")
	bob := e.login("bob")

	start := meta.Now()
	orig := meta.Now
	meta.Now = func() int64 { return start + 10*60 + 1 }
	t.Cleanup(func() { meta.Now = orig })

	bob.expect(http.StatusBadRequest, http.MethodPost, "/api/cli/device/"+userCode+"/approve", nil)
	bob.expect(http.StatusBadRequest, http.MethodPost, "/api/cli/device/"+userCode+"/deny", nil)

	code, out := clitPoll(t, e.srv.URL, deviceCode)
	clitExpectError(t, code, out, "expired_token")

	code, out = clitPoll(t, e.srv.URL, "unknown-device-code")
	clitExpectError(t, code, out, "expired_token")
}

func TestCLIUserCodeNormalisation(t *testing.T) {
	e := apitNewEnv(t)
	deviceCode, userCode := clitStart(t, e.srv.URL, "laptop")
	bob := e.login("bob")

	typed := strings.ToLower(strings.ReplaceAll(userCode, "-", ""))
	var status struct {
		UserCode string `json:"userCode"`
	}
	apitDecode(t, bob.expect(http.StatusOK, http.MethodGet, "/api/cli/device/"+typed, nil), &status)
	if status.UserCode != userCode {
		t.Fatalf("normalised lookup returned %q, want %q", status.UserCode, userCode)
	}

	bob.expect(http.StatusNoContent, http.MethodPost, "/api/cli/device/"+typed+"/approve", nil)
	code, out := clitPoll(t, e.srv.URL, deviceCode)
	if code != http.StatusOK || out["username"] != "bob" {
		t.Fatalf("poll after typed approval = status %d %v", code, out)
	}
}

func TestCLIApproveRequiresSignIn(t *testing.T) {
	e := apitNewEnv(t)
	deviceCode, userCode := clitStart(t, e.srv.URL, "laptop")
	anon := &apitClient{t: t, base: e.srv.URL, http: &http.Client{}}

	anon.expect(http.StatusUnauthorized, http.MethodGet, "/api/cli/device/"+userCode, nil)
	anon.expect(http.StatusUnauthorized, http.MethodPost, "/api/cli/device/"+userCode+"/approve", nil)
	anon.expect(http.StatusUnauthorized, http.MethodPost, "/api/cli/device/"+userCode+"/deny", nil)
	anon.expect(http.StatusUnauthorized, http.MethodGet, "/api/me/tokens", nil)

	code, out := clitPoll(t, e.srv.URL, deviceCode)
	clitExpectError(t, code, out, "authorization_pending")
}

func TestCLIDeviceLimit(t *testing.T) {
	e := apitNewEnv(t)
	var lastStatus int
	for i := 0; i < 201; i++ {
		lastStatus, _ = clitCall(t, e.srv.URL, http.MethodPost, "/api/cli/device", map[string]string{"client": "x"})
	}
	if lastStatus != http.StatusTooManyRequests {
		t.Fatalf("201st pending login status = %d, want 429", lastStatus)
	}
}
