package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

const (
	testDeviceCode = "dev-123"
	testApproveURL = "/cli/approve?code=BCDF-GHJK"
)

type tokenStep struct {
	status int
	body   string
}

// fakeCLIServer mimics the device and token endpoints. Token polls walk
// through tokenSteps and repeat the last one.
type fakeCLIServer struct {
	*httptest.Server
	mu           sync.Mutex
	deviceStatus int
	expiresIn    int
	clientNames  []string
	tokenSteps   []tokenStep
	tokenPolls   int
}

func newFakeCLIServer(t *testing.T, deviceStatus int, steps ...tokenStep) *fakeCLIServer {
	t.Helper()
	f := &fakeCLIServer{deviceStatus: deviceStatus, expiresIn: 60, tokenSteps: steps}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/cli/device", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Client string `json:"client"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		f.mu.Lock()
		defer f.mu.Unlock()
		f.clientNames = append(f.clientNames, req.Client)
		w.Header().Set("Content-Type", "application/json")
		if f.deviceStatus != http.StatusOK {
			w.WriteHeader(f.deviceStatus)
			io.WriteString(w, `{"error":"too many pending logins"}`)
			return
		}
		json.NewEncoder(w).Encode(map[string]any{
			"deviceCode":      testDeviceCode,
			"userCode":        "BCDF-GHJK",
			"verificationUrl": testApproveURL,
			"interval":        1,
			"expiresIn":       f.expiresIn,
		})
	})
	mux.HandleFunc("POST /api/cli/token", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			DeviceCode string `json:"deviceCode"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		f.mu.Lock()
		defer f.mu.Unlock()
		step := f.tokenSteps[min(f.tokenPolls, len(f.tokenSteps)-1)]
		f.tokenPolls++
		if req.DeviceCode != testDeviceCode {
			step = tokenStep{http.StatusBadRequest, `{"error":"invalid_grant"}`}
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(step.status)
		io.WriteString(w, step.body)
	})
	f.Server = httptest.NewServer(mux)
	t.Cleanup(f.Close)
	return f
}

func (f *fakeCLIServer) polls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.tokenPolls
}

// testEnv records the side effects of a login.
type testEnv struct {
	stdout bytes.Buffer
	stderr bytes.Buffer
	sleeps []time.Duration
	opened []string

	dockerCalls int
	dockerHost  string
	dockerUser  string
	dockerToken string
	dockerErr   error
}

func (e *testEnv) deps(client *http.Client) loginDeps {
	return loginDeps{
		Client: client,
		Sleep:  func(d time.Duration) { e.sleeps = append(e.sleeps, d) },
		OpenBrowser: func(u string) error {
			e.opened = append(e.opened, u)
			return nil
		},
		DockerLogin: func(host, username, token string) error {
			e.dockerCalls++
			e.dockerHost, e.dockerUser, e.dockerToken = host, username, token
			return e.dockerErr
		},
		Stdout: &e.stdout,
		Stderr: &e.stderr,
	}
}

func TestNormalizeRegistry(t *testing.T) {
	tests := []struct {
		in         string
		wantBase   string
		wantDocker string
	}{
		{"reg.example.com", "https://reg.example.com", "reg.example.com"},
		{" reg.example.com ", "https://reg.example.com", "reg.example.com"},
		{"reg.example.com/", "https://reg.example.com", "reg.example.com"},
		{"localhost:8080", "http://localhost:8080", "localhost:8080"},
		{"127.0.0.1:5000", "http://127.0.0.1:5000", "127.0.0.1:5000"},
		{"[::1]:8080", "http://[::1]:8080", "[::1]:8080"},
		{"https://reg.example.com", "https://reg.example.com", "reg.example.com"},
		{"https://reg.example.com/", "https://reg.example.com", "reg.example.com"},
		{"https://reg.example.com/base/", "https://reg.example.com/base", "reg.example.com"},
		{"http://localhost:8080/", "http://localhost:8080", "localhost:8080"},
		{"https://localhost:8443", "https://localhost:8443", "localhost:8443"},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			base, host, err := normalizeRegistry(tt.in)
			if err != nil {
				t.Fatalf("normalizeRegistry(%q): %v", tt.in, err)
			}
			if base != tt.wantBase || host != tt.wantDocker {
				t.Errorf("normalizeRegistry(%q) = %q, %q; want %q, %q", tt.in, base, host, tt.wantBase, tt.wantDocker)
			}
		})
	}
}

func TestNormalizeRegistryRejects(t *testing.T) {
	for _, in := range []string{"", "   ", "ftp://reg.example.com", "https://"} {
		if _, _, err := normalizeRegistry(in); err == nil {
			t.Errorf("normalizeRegistry(%q) succeeded, want error", in)
		}
	}
}

func TestPlanLogin(t *testing.T) {
	t.Run("default docker host is the registry host", func(t *testing.T) {
		opts, err := planLogin([]string{"localhost:5173"})
		if err != nil {
			t.Fatal(err)
		}
		if opts.Base != "http://localhost:5173" || opts.DockerHost != "localhost:5173" {
			t.Errorf("got base %q, docker host %q", opts.Base, opts.DockerHost)
		}
	})
	t.Run("docker host override", func(t *testing.T) {
		opts, err := planLogin([]string{"--docker-host", "localhost:5050", "localhost:5173"})
		if err != nil {
			t.Fatal(err)
		}
		if opts.Base != "http://localhost:5173" || opts.DockerHost != "localhost:5050" {
			t.Errorf("got base %q, docker host %q", opts.Base, opts.DockerHost)
		}
	})
	t.Run("flags may follow the registry", func(t *testing.T) {
		opts, err := planLogin([]string{"reg.example.com", "--print", "--no-browser", "--docker-host=reg2.example.com"})
		if err != nil {
			t.Fatal(err)
		}
		if !opts.Print || !opts.NoBrowser || opts.DockerHost != "reg2.example.com" || opts.Base != "https://reg.example.com" {
			t.Errorf("unexpected options %+v", opts)
		}
	})
	t.Run("registry argument is required", func(t *testing.T) {
		if _, err := planLogin(nil); err == nil || !strings.Contains(err.Error(), "usage") {
			t.Errorf("planLogin(nil) error = %v, want usage error", err)
		}
	})
	t.Run("only one registry argument", func(t *testing.T) {
		if _, err := planLogin([]string{"a.example.com", "b.example.com"}); err == nil {
			t.Error("planLogin with two registries succeeded, want error")
		}
	})
}

func TestRunLoginFlowSuccess(t *testing.T) {
	srv := newFakeCLIServer(t, http.StatusOK,
		tokenStep{http.StatusBadRequest, `{"error":"authorization_pending"}`},
		tokenStep{http.StatusBadRequest, `{"error":"authorization_pending"}`},
		tokenStep{http.StatusOK, `{"username":"alice","token":"tok-secret","expiresAt":1802599200}`},
	)
	env := &testEnv{}
	opts := loginOptions{Base: srv.URL, DockerHost: "reg.example.com", ClientName: "box (linux)"}
	if err := runLoginFlow(context.Background(), opts, env.deps(srv.Client())); err != nil {
		t.Fatal(err)
	}

	if env.dockerCalls != 1 {
		t.Fatalf("docker login called %d times, want 1", env.dockerCalls)
	}
	if env.dockerHost != "reg.example.com" || env.dockerUser != "alice" || env.dockerToken != "tok-secret" {
		t.Errorf("docker login got host %q user %q token %q", env.dockerHost, env.dockerUser, env.dockerToken)
	}
	if got := srv.polls(); got != 3 {
		t.Errorf("token polled %d times, want 3", got)
	}
	if len(env.sleeps) != 3 {
		t.Fatalf("slept %d times, want 3", len(env.sleeps))
	}
	for _, d := range env.sleeps {
		if d != time.Second {
			t.Errorf("slept %v, want the server interval of 1s", d)
		}
	}
	wantURL := srv.URL + testApproveURL
	if !strings.Contains(env.stderr.String(), "Confirm code BCDF-GHJK in your browser: "+wantURL) {
		t.Errorf("stderr lacks the approval prompt:\n%s", env.stderr.String())
	}
	if len(env.opened) != 1 || env.opened[0] != wantURL {
		t.Errorf("opened %v, want [%s]", env.opened, wantURL)
	}
	wantMsg := "Logged in to reg.example.com as alice. The token expires on 2027-02-14."
	if !strings.Contains(env.stdout.String(), wantMsg) {
		t.Errorf("stdout = %q, want it to contain %q", env.stdout.String(), wantMsg)
	}
	if strings.Contains(env.stdout.String()+env.stderr.String(), "tok-secret") {
		t.Error("token printed without --print")
	}
	if len(srv.clientNames) != 1 || srv.clientNames[0] != "box (linux)" {
		t.Errorf("device request client names = %v", srv.clientNames)
	}
}

func TestRunLoginFlowNoBrowserDoesNotOpen(t *testing.T) {
	srv := newFakeCLIServer(t, http.StatusOK, tokenStep{http.StatusOK, `{"username":"alice","token":"t"}`})
	env := &testEnv{}
	opts := loginOptions{Base: srv.URL, DockerHost: "reg.example.com", NoBrowser: true}
	if err := runLoginFlow(context.Background(), opts, env.deps(srv.Client())); err != nil {
		t.Fatal(err)
	}
	if len(env.opened) != 0 {
		t.Errorf("browser opened despite --no-browser: %v", env.opened)
	}
}

func TestRunLoginFlowBrowserFailureIsNotFatal(t *testing.T) {
	srv := newFakeCLIServer(t, http.StatusOK, tokenStep{http.StatusOK, `{"username":"alice","token":"t"}`})
	env := &testEnv{}
	deps := env.deps(srv.Client())
	deps.OpenBrowser = func(string) error { return errors.New("no opener") }
	opts := loginOptions{Base: srv.URL, DockerHost: "reg.example.com"}
	if err := runLoginFlow(context.Background(), opts, deps); err != nil {
		t.Fatalf("browser failure aborted the login: %v", err)
	}
	if env.dockerCalls != 1 {
		t.Errorf("docker login called %d times, want 1", env.dockerCalls)
	}
}

func TestRunLoginFlowPrint(t *testing.T) {
	srv := newFakeCLIServer(t, http.StatusOK, tokenStep{http.StatusOK, `{"username":"ci","token":"tok-print","expiresAt":1802599200}`})
	env := &testEnv{}
	opts := loginOptions{Base: srv.URL, DockerHost: "reg.example.com", Print: true}
	if err := runLoginFlow(context.Background(), opts, env.deps(srv.Client())); err != nil {
		t.Fatal(err)
	}
	if env.dockerCalls != 0 {
		t.Errorf("docker login ran with --print")
	}
	if want := "username: ci\ntoken: tok-print\n"; env.stdout.String() != want {
		t.Errorf("stdout = %q, want %q", env.stdout.String(), want)
	}
}

func TestRunLoginFlowDenied(t *testing.T) {
	srv := newFakeCLIServer(t, http.StatusOK,
		tokenStep{http.StatusBadRequest, `{"error":"authorization_pending"}`},
		tokenStep{http.StatusBadRequest, `{"error":"access_denied"}`},
	)
	env := &testEnv{}
	err := runLoginFlow(context.Background(), loginOptions{Base: srv.URL, DockerHost: "reg.example.com"}, env.deps(srv.Client()))
	if err == nil || !strings.Contains(err.Error(), "denied") {
		t.Fatalf("error = %v, want one mentioning denied", err)
	}
	if env.dockerCalls != 0 {
		t.Error("docker login ran after denial")
	}
}

func TestRunLoginFlowExpired(t *testing.T) {
	srv := newFakeCLIServer(t, http.StatusOK, tokenStep{http.StatusBadRequest, `{"error":"expired_token"}`})
	env := &testEnv{}
	err := runLoginFlow(context.Background(), loginOptions{Base: srv.URL, DockerHost: "reg.example.com"}, env.deps(srv.Client()))
	if err == nil || !strings.Contains(err.Error(), "expired") {
		t.Fatalf("error = %v, want one mentioning expired", err)
	}
	if env.dockerCalls != 0 {
		t.Error("docker login ran after expiry")
	}
}

func TestRunLoginFlowTimesOut(t *testing.T) {
	srv := newFakeCLIServer(t, http.StatusOK, tokenStep{http.StatusBadRequest, `{"error":"authorization_pending"}`})
	srv.expiresIn = 2
	env := &testEnv{}
	err := runLoginFlow(context.Background(), loginOptions{Base: srv.URL, DockerHost: "reg.example.com"}, env.deps(srv.Client()))
	if !errors.Is(err, errLoginTimeout) {
		t.Fatalf("error = %v, want errLoginTimeout", err)
	}
	if got := srv.polls(); got != 2 {
		t.Errorf("polled %d times within a 2s lifetime at 1s interval, want 2", got)
	}
}

func TestRunLoginFlowTooManyPending(t *testing.T) {
	srv := newFakeCLIServer(t, http.StatusTooManyRequests)
	env := &testEnv{}
	err := runLoginFlow(context.Background(), loginOptions{Base: srv.URL, DockerHost: "reg.example.com"}, env.deps(srv.Client()))
	if err == nil || !strings.Contains(err.Error(), "too many") {
		t.Fatalf("error = %v, want one mentioning too many pending logins", err)
	}
	if srv.polls() != 0 {
		t.Error("token polled after device code was refused")
	}
}

func TestRunLoginFlowCancelled(t *testing.T) {
	srv := newFakeCLIServer(t, http.StatusOK, tokenStep{http.StatusBadRequest, `{"error":"authorization_pending"}`})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	env := &testEnv{}
	deps := env.deps(srv.Client())
	deps.Sleep = func(time.Duration) { cancel() }
	err := runLoginFlow(ctx, loginOptions{Base: srv.URL, DockerHost: "reg.example.com"}, deps)
	if !errors.Is(err, errLoginCancelled) {
		t.Fatalf("error = %v, want errLoginCancelled", err)
	}
	if env.dockerCalls != 0 {
		t.Error("docker login ran after cancel")
	}
}

func TestRunLoginFlowDockerNotFound(t *testing.T) {
	srv := newFakeCLIServer(t, http.StatusOK, tokenStep{http.StatusOK, `{"username":"alice","token":"tok-hidden"}`})
	env := &testEnv{dockerErr: errDockerNotFound}
	err := runLoginFlow(context.Background(), loginOptions{Base: srv.URL, DockerHost: "reg.example.com"}, env.deps(srv.Client()))
	if !errors.Is(err, errDockerNotFound) {
		t.Fatalf("error = %v, want errDockerNotFound", err)
	}
	out := env.stderr.String()
	if want := "docker login reg.example.com -u alice --password-stdin"; !strings.Contains(out, want) {
		t.Errorf("stderr lacks the equivalent command %q:\n%s", want, out)
	}
	if strings.Contains(out+env.stdout.String(), "tok-hidden") {
		t.Error("token printed when docker is missing")
	}
}

func TestRunLoginFlowDockerFailure(t *testing.T) {
	srv := newFakeCLIServer(t, http.StatusOK, tokenStep{http.StatusOK, `{"username":"alice","token":"t"}`})
	env := &testEnv{dockerErr: fmt.Errorf("exit status 1")}
	err := runLoginFlow(context.Background(), loginOptions{Base: srv.URL, DockerHost: "reg.example.com"}, env.deps(srv.Client()))
	if err == nil || !strings.Contains(err.Error(), "docker login") {
		t.Fatalf("error = %v, want docker login failure", err)
	}
	if strings.Contains(env.stdout.String(), "Logged in") {
		t.Error("reported success after docker failed")
	}
}

func TestDockerHostOverrideReachesDocker(t *testing.T) {
	srv := newFakeCLIServer(t, http.StatusOK, tokenStep{http.StatusOK, `{"username":"alice","token":"t"}`})
	opts, err := planLogin([]string{"--docker-host", "localhost:5050", srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	env := &testEnv{}
	if err := runLoginFlow(context.Background(), opts, env.deps(srv.Client())); err != nil {
		t.Fatal(err)
	}
	if env.dockerHost != "localhost:5050" {
		t.Errorf("docker login host = %q, want localhost:5050", env.dockerHost)
	}
}
