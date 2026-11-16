package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"strings"
	"syscall"
	"time"
)

const (
	defaultPollInterval  = 5 * time.Second
	defaultLoginLifetime = 15 * time.Minute
	loginUsage           = "usage: doreg login [--docker-host HOST] [--no-browser] [--print] REGISTRY"
)

var (
	errDockerNotFound = errors.New("docker is not on PATH")
	errLoginCancelled = errors.New("login cancelled")
	errLoginTimeout   = errors.New("login timed out before it was approved; run doreg login again")
)

// deviceCode is the answer to POST /api/cli/device.
type deviceCode struct {
	DeviceCode      string `json:"deviceCode"`
	UserCode        string `json:"userCode"`
	VerificationURL string `json:"verificationUrl"`
	Interval        int    `json:"interval"`
	ExpiresIn       int    `json:"expiresIn"`
}

// cliToken is the answer to a successful POST /api/cli/token.
type cliToken struct {
	Username  string `json:"username"`
	Token     string `json:"token"`
	ExpiresAt int64  `json:"expiresAt"` // unix seconds
}

// loginOptions are the parsed command line settings of doreg login.
type loginOptions struct {
	Base       string
	DockerHost string
	ClientName string
	NoBrowser  bool
	Print      bool
}

// loginDeps are the side effects of a login, replaceable in tests.
type loginDeps struct {
	Client      *http.Client
	Sleep       func(time.Duration)
	OpenBrowser func(string) error
	DockerLogin func(host, username, token string) error
	Stdout      io.Writer
	Stderr      io.Writer
}

func login(args []string) error {
	opts, err := planLogin(args)
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return runLoginFlow(ctx, opts, loginDeps{
		Client: &http.Client{Timeout: 30 * time.Second},
		Sleep: func(d time.Duration) {
			select {
			case <-ctx.Done():
			case <-time.After(d):
			}
		},
		OpenBrowser: openInBrowser,
		DockerLogin: dockerLogin,
		Stdout:      os.Stdout,
		Stderr:      os.Stderr,
	})
}

// planLogin parses the flags and the registry argument. Flags may follow the
// registry, which the standard flag package does not support on its own.
func planLogin(args []string) (loginOptions, error) {
	fs := flag.NewFlagSet("login", flag.ExitOnError)
	dockerHost := fs.String("docker-host", "", "registry host passed to docker login (default: the host of REGISTRY)")
	noBrowser := fs.Bool("no-browser", false, "only print the approval URL")
	printOnly := fs.Bool("print", false, "print the username and token instead of running docker login")

	var positional []string
	rest := args
	for {
		fs.Parse(rest)
		if fs.NArg() == 0 {
			break
		}
		positional = append(positional, fs.Arg(0))
		rest = fs.Args()[1:]
	}
	if len(positional) != 1 {
		return loginOptions{}, errors.New(loginUsage)
	}
	base, host, err := normalizeRegistry(positional[0])
	if err != nil {
		return loginOptions{}, err
	}
	if *dockerHost != "" {
		host = *dockerHost
	}
	return loginOptions{
		Base:       base,
		DockerHost: host,
		ClientName: clientName(),
		NoBrowser:  *noBrowser,
		Print:      *printOnly,
	}, nil
}

// normalizeRegistry turns a host or URL into the API base URL and the default
// docker host. Hosts without a scheme use https, except local addresses.
func normalizeRegistry(raw string) (base, dockerHost string, err error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", "", errors.New("missing registry host or URL")
	}
	if !strings.Contains(raw, "://") {
		// Parsing with a leading "//" puts the host into u.Host, which
		// "localhost:8080" would otherwise read as a scheme.
		u, err := url.Parse("//" + raw)
		if err != nil {
			return "", "", fmt.Errorf("invalid registry %q: %w", raw, err)
		}
		scheme := "https"
		if isLocalHost(u.Hostname()) {
			scheme = "http"
		}
		raw = scheme + "://" + raw
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", "", fmt.Errorf("invalid registry %q: %w", raw, err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", "", fmt.Errorf("registry %q must use http or https", raw)
	}
	if u.Host == "" {
		return "", "", fmt.Errorf("registry %q has no host", raw)
	}
	path := strings.TrimRight(u.Path, "/")
	return u.Scheme + "://" + u.Host + path, u.Host, nil
}

func isLocalHost(host string) bool {
	return host == "localhost" || host == "127.0.0.1" || host == "::1"
}

func clientName() string {
	host, err := os.Hostname()
	if err != nil || host == "" {
		host = "unknown"
	}
	return fmt.Sprintf("%s (%s)", host, runtime.GOOS)
}

// runLoginFlow requests a device code, waits for approval in the browser and
// then either prints the token or hands it to docker login.
func runLoginFlow(ctx context.Context, opts loginOptions, deps loginDeps) error {
	dc, err := requestDeviceCode(ctx, deps.Client, opts.Base, opts.ClientName)
	if err != nil {
		return err
	}
	approveURL := opts.Base + "/" + strings.TrimPrefix(dc.VerificationURL, "/")
	fmt.Fprintf(deps.Stderr, "Confirm code %s in your browser: %s\n", dc.UserCode, approveURL)
	if !opts.NoBrowser {
		if err := deps.OpenBrowser(approveURL); err != nil {
			fmt.Fprintf(deps.Stderr, "Could not open a browser (%v). Open the link above by hand.\n", err)
		}
	}

	tok, err := pollToken(ctx, deps.Client, opts.Base, dc, deps.Sleep)
	if err != nil {
		return err
	}
	expiry := formatExpiry(tok.ExpiresAt)

	if opts.Print {
		fmt.Fprintf(deps.Stdout, "username: %s\ntoken: %s\n", tok.Username, tok.Token)
		if expiry != "" {
			fmt.Fprintf(deps.Stderr, "The token expires on %s.\n", expiry)
		}
		return nil
	}

	if err := deps.DockerLogin(opts.DockerHost, tok.Username, tok.Token); err != nil {
		if errors.Is(err, errDockerNotFound) {
			fmt.Fprintf(deps.Stderr, "Equivalent command, with the token on stdin: docker login %s -u %s --password-stdin\n",
				opts.DockerHost, tok.Username)
			fmt.Fprintf(deps.Stderr, "The token was issued but is not shown. Run `doreg login --print %s` on a machine with docker to get one.\n",
				opts.Base)
		}
		return fmt.Errorf("docker login: %w", err)
	}

	msg := fmt.Sprintf("Logged in to %s as %s.", opts.DockerHost, tok.Username)
	if expiry != "" {
		msg += fmt.Sprintf(" The token expires on %s.", expiry)
	}
	fmt.Fprintln(deps.Stdout, msg)
	return nil
}

func requestDeviceCode(ctx context.Context, client *http.Client, base, name string) (*deviceCode, error) {
	var dc deviceCode
	status, msg, err := postJSON(ctx, client, base+"/api/cli/device", map[string]string{"client": name}, &dc)
	if err != nil {
		return nil, fmt.Errorf("start login at %s: %w", base, err)
	}
	switch {
	case status == http.StatusTooManyRequests:
		return nil, fmt.Errorf("too many pending logins at %s, try again later (%s)", base, msg)
	case status != http.StatusOK:
		return nil, fmt.Errorf("start login at %s: HTTP %d %s", base, status, msg)
	}
	if dc.DeviceCode == "" || dc.VerificationURL == "" || dc.UserCode == "" {
		return nil, fmt.Errorf("start login at %s: incomplete response", base)
	}
	return &dc, nil
}

// pollToken polls until the login is approved, denied, expired or the device
// code lifetime runs out. Waiting time is counted from the sleeps, so tests
// with a no-op sleep see the same timeout behaviour.
func pollToken(ctx context.Context, client *http.Client, base string, dc *deviceCode, sleep func(time.Duration)) (*cliToken, error) {
	interval := defaultPollInterval
	if dc.Interval > 0 {
		interval = time.Duration(dc.Interval) * time.Second
	}
	lifetime := defaultLoginLifetime
	if dc.ExpiresIn > 0 {
		lifetime = time.Duration(dc.ExpiresIn) * time.Second
	}

	var waited time.Duration
	for {
		sleep(interval)
		if ctx.Err() != nil {
			return nil, errLoginCancelled
		}
		waited += interval

		var tok cliToken
		status, code, err := postJSON(ctx, client, base+"/api/cli/token", map[string]string{"deviceCode": dc.DeviceCode}, &tok)
		if err != nil {
			if ctx.Err() != nil {
				return nil, errLoginCancelled
			}
			return nil, fmt.Errorf("poll login at %s: %w", base, err)
		}
		if status == http.StatusOK {
			if tok.Token == "" || tok.Username == "" {
				return nil, fmt.Errorf("poll login at %s: incomplete response", base)
			}
			return &tok, nil
		}
		switch code {
		case "authorization_pending":
		case "access_denied":
			return nil, errors.New("login was denied in the browser")
		case "expired_token":
			return nil, errors.New("the login code expired before it was approved; run doreg login again")
		default:
			return nil, fmt.Errorf("poll login at %s: HTTP %d %s", base, status, code)
		}
		if waited >= lifetime {
			return nil, errLoginTimeout
		}
	}
}

// postJSON posts in as JSON. On 200 it decodes the body into out; otherwise it
// returns the status and the "error" field of the body.
func postJSON(ctx context.Context, client *http.Client, endpoint string, in, out any) (int, string, error) {
	body, err := json.Marshal(in)
	if err != nil {
		return 0, "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return 0, "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return 0, "", err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return resp.StatusCode, "", err
	}
	if resp.StatusCode == http.StatusOK {
		if err := json.Unmarshal(data, out); err != nil {
			return resp.StatusCode, "", fmt.Errorf("decode response: %w", err)
		}
		return resp.StatusCode, "", nil
	}
	var apiErr struct {
		Error string `json:"error"`
	}
	_ = json.Unmarshal(data, &apiErr)
	return resp.StatusCode, apiErr.Error, nil
}

func formatExpiry(unix int64) string {
	if unix <= 0 {
		return ""
	}
	return time.Unix(unix, 0).Format("2006-01-02")
}

// dockerLogin runs `docker login` with the token on stdin. docker's own output
// goes straight to the terminal.
func dockerLogin(host, username, token string) error {
	if _, err := exec.LookPath("docker"); err != nil {
		return errDockerNotFound
	}
	cmd := exec.Command("docker", "login", host, "-u", username, "--password-stdin")
	cmd.Stdin = strings.NewReader(token)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// openInBrowser starts the platform's URL opener without waiting for it.
func openInBrowser(target string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", target)
	case "linux":
		cmd = exec.Command("xdg-open", target)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", target)
	default:
		return fmt.Errorf("no browser launcher for %s", runtime.GOOS)
	}
	return cmd.Start()
}
