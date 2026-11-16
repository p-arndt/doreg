package api_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/p-arndt/doreg/internal/meta"
)

// githubReadme is served for acme/tool. It mixes relative and absolute links.
const githubReadme = "# Tool\n\n" +
	"![logo](docs/logo.png)\n\n" +
	"[guide](./docs/guide.md \"Guide\")\n\n" +
	"[site](https://example.com/x)\n\n" +
	"<img src=\"img/a.png\" alt=\"a\">\n\n" +
	"<a href=\"/CONTRIBUTING.md\">contributing</a>\n\n" +
	"[anchor](#usage)\n\n" +
	"![abs](https://img.example.com/b.png)\n"

const githubLimit = 64 << 10

type githubHit struct {
	path   string
	header http.Header
}

// fakeGitHub stands in for the GitHub REST API and records every request.
type fakeGitHub struct {
	srv  *httptest.Server
	mu   sync.Mutex
	hits []githubHit
}

func newFakeGitHub(t *testing.T) *fakeGitHub {
	t.Helper()
	f := &fakeGitHub{}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.hits = append(f.hits, githubHit{path: r.URL.Path, header: r.Header.Clone()})
		f.mu.Unlock()
		switch r.URL.Path {
		case "/repos/acme/tool/readme":
			writeGitHubFile(w, "README.md", []byte(githubReadme))
		case "/repos/acme/exact/readme":
			writeGitHubFile(w, "README.md", bytes.Repeat([]byte("a"), githubLimit))
		case "/repos/acme/big/readme":
			writeGitHubFile(w, "README.md", bytes.Repeat([]byte("a"), githubLimit+1))
		case "/repos/acme/plain/readme":
			writeGitHubFile(w, "README", []byte("plain text"))
		case "/repos/acme/ascii/readme":
			writeGitHubFile(w, "README.adoc", []byte("= Title"))
		case "/repos/acme/forbidden/readme":
			w.WriteHeader(http.StatusForbidden)
		case "/repos/acme/broken/readme":
			w.WriteHeader(http.StatusInternalServerError)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(f.srv.Close)
	return f
}

// writeGitHubFile answers like GitHub's contents API: base64 broken into
// 60-character lines.
func writeGitHubFile(w http.ResponseWriter, name string, content []byte) {
	enc := base64.StdEncoding.EncodeToString(content)
	var b strings.Builder
	for len(enc) > 60 {
		b.WriteString(enc[:60] + "\n")
		enc = enc[60:]
	}
	b.WriteString(enc)
	json.NewEncoder(w).Encode(map[string]string{"name": name, "content": b.String(), "encoding": "base64"})
}

func (f *fakeGitHub) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.hits)
}

func (f *fakeGitHub) last() githubHit {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.hits[len(f.hits)-1]
}

// githubEnv returns the team environment with repository team/app and a fake
// GitHub wired into the API.
func githubEnv(t *testing.T) (*apitEnv, *fakeGitHub) {
	t.Helper()
	e := apitTeamEnv(t)
	err := e.db.Tx(context.Background(), func(tx *sql.Tx) error {
		_, err := meta.EnsureRepo(context.Background(), tx, "team/app")
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	gh := newFakeGitHub(t)
	e.api.GitHubAPI = gh.srv.URL
	return e, gh
}

func importReadme(c *apitClient, body any) (int, []byte) {
	return c.raw(http.MethodPost, "/api/repo/readme/github?name=team/app", body, nil)
}

func TestAPIGitHubReadmeImport(t *testing.T) {
	e, gh := githubEnv(t)
	bob := e.login("bob")

	status, data := importReadme(bob, map[string]string{"source": "https://github.com/acme/tool.git"})
	if status != http.StatusOK {
		t.Fatalf("status %d: %s", status, data)
	}
	var out struct {
		Readme string `json:"readme"`
		Source string `json:"source"`
	}
	apitDecode(t, data, &out)
	if out.Source != "https://github.com/acme/tool" {
		t.Errorf("source = %q", out.Source)
	}
	for _, want := range []string{
		"![logo](https://raw.githubusercontent.com/acme/tool/HEAD/docs/logo.png)",
		"[guide](https://github.com/acme/tool/blob/HEAD/docs/guide.md \"Guide\")",
		"<img src=\"https://raw.githubusercontent.com/acme/tool/HEAD/img/a.png\" alt=\"a\">",
		"<a href=\"https://github.com/acme/tool/blob/HEAD/CONTRIBUTING.md\">",
		"[site](https://example.com/x)",
		"[anchor](#usage)",
		"![abs](https://img.example.com/b.png)",
	} {
		if !strings.Contains(out.Readme, want) {
			t.Errorf("readme lacks %q:\n%s", want, out.Readme)
		}
	}

	hit := gh.last()
	if hit.path != "/repos/acme/tool/readme" {
		t.Errorf("upstream path = %q", hit.path)
	}
	if got := hit.header.Get("Accept"); got != "application/vnd.github+json" {
		t.Errorf("Accept = %q", got)
	}
	if got := hit.header.Get("X-GitHub-Api-Version"); got != "2022-11-28" {
		t.Errorf("X-GitHub-Api-Version = %q", got)
	}
	if got := hit.header.Get("User-Agent"); got != "doreg" {
		t.Errorf("User-Agent = %q", got)
	}
	if got, ok := hit.header["Authorization"]; ok {
		t.Errorf("Authorization sent without a token: %v", got)
	}

	e.api.GitHubToken = "test-token"
	bob.expect(http.StatusOK, http.MethodPost, "/api/repo/readme/github?name=team/app",
		map[string]string{"source": "github.com/acme/tool"})
	if got := gh.last().header.Get("Authorization"); got != "Bearer test-token" {
		t.Errorf("Authorization = %q, want the bearer token", got)
	}

	// The import does not save: the stored README stays empty.
	var repo struct {
		Readme string `json:"readme"`
	}
	apitDecode(t, bob.expect(http.StatusOK, http.MethodGet, "/api/repo?name=team/app", nil), &repo)
	if repo.Readme != "" {
		t.Errorf("import stored a README: %q", repo.Readme)
	}
}

func TestAPIGitHubReadmeEmptySourceWithoutLabel(t *testing.T) {
	e, gh := githubEnv(t)
	bob := e.login("bob")

	// No tag carries the source label, so an empty body is rejected.
	status, data := importReadme(bob, map[string]string{})
	if status != http.StatusBadRequest {
		t.Fatalf("status %d, want 400: %s", status, data)
	}
	if !strings.Contains(string(data), "org.opencontainers.image.source") {
		t.Errorf("error does not name the label: %s", data)
	}
	if gh.count() != 0 {
		t.Errorf("upstream was called %d times", gh.count())
	}
}

func TestAPIGitHubReadmePermissions(t *testing.T) {
	e, gh := githubEnv(t)
	body := map[string]string{"source": "https://github.com/acme/tool"}

	carol := e.login("carol")
	if status, _ := importReadme(carol, body); status != http.StatusForbidden {
		t.Errorf("read-only member: status %d, want 403", status)
	}
	status, _ := importReadme(e.guest(), body)
	if status != http.StatusUnauthorized {
		t.Errorf("guest: status %d, want 401", status)
	}
	if gh.count() != 0 {
		t.Errorf("upstream was called %d times for denied requests", gh.count())
	}
}

func TestAPIGitHubReadmeMissingRepo(t *testing.T) {
	e, gh := githubEnv(t)
	bob := e.login("bob")
	status, _ := bob.raw(http.MethodPost, "/api/repo/readme/github?name=team/missing", map[string]string{"source": "acme/tool"}, nil)
	if status != http.StatusNotFound {
		t.Errorf("status %d, want 404", status)
	}
	if gh.count() != 0 {
		t.Errorf("upstream was called %d times", gh.count())
	}
}

func TestAPIGitHubReadmeRejectsForeignSources(t *testing.T) {
	e, gh := githubEnv(t)
	bob := e.login("bob")
	for _, source := range []string{
		"https://gitlab.com/a/b",
		"http://github.com/a/b",
		"https://github.com@evil.com/a/b",
		"https://github.com/../x",
		"https://user@github.com/acme/tool",
		"https://github.com:8443/acme/tool",
		"",
	} {
		status, data := importReadme(bob, map[string]string{"source": source})
		if status != http.StatusBadRequest {
			t.Errorf("source %q: status %d, want 400: %s", source, status, data)
		}
	}
	if gh.count() != 0 {
		t.Errorf("upstream received %d requests for rejected sources", gh.count())
	}
}

func TestAPIGitHubReadmeUpstreamErrors(t *testing.T) {
	e, _ := githubEnv(t)
	bob := e.login("bob")
	for _, tc := range []struct {
		source string
		want   int
	}{
		{"https://github.com/acme/missing", http.StatusNotFound},
		{"https://github.com/acme/forbidden", http.StatusBadGateway},
		{"https://github.com/acme/broken", http.StatusBadGateway},
	} {
		status, data := importReadme(bob, map[string]string{"source": tc.source})
		if status != tc.want {
			t.Errorf("%s: status %d, want %d: %s", tc.source, status, tc.want, data)
		}
	}
}

func TestAPIGitHubReadmeSizeLimit(t *testing.T) {
	e, _ := githubEnv(t)
	bob := e.login("bob")

	status, data := importReadme(bob, map[string]string{"source": "https://github.com/acme/big"})
	if status != http.StatusBadRequest {
		t.Errorf("oversize README: status %d, want 400: %s", status, data)
	}
	status, data = importReadme(bob, map[string]string{"source": "https://github.com/acme/exact"})
	if status != http.StatusOK {
		t.Errorf("README at the limit: status %d, want 200: %s", status, data)
	}
}

func TestAPIGitHubReadmeFormats(t *testing.T) {
	e, _ := githubEnv(t)
	bob := e.login("bob")

	status, data := importReadme(bob, map[string]string{"source": "https://github.com/acme/ascii"})
	if status != http.StatusUnprocessableEntity || !strings.Contains(string(data), "README.adoc is not Markdown") {
		t.Errorf("AsciiDoc README: status %d, want 422 naming the file: %s", status, data)
	}
	status, data = importReadme(bob, map[string]string{"source": "https://github.com/acme/plain"})
	if status != http.StatusOK || !strings.Contains(string(data), "plain text") {
		t.Errorf("README without extension: status %d, want 200: %s", status, data)
	}
}
