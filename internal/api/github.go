package api

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"path"
	"regexp"
	"strings"
	"time"

	"github.com/p-arndt/doreg/internal/auth"
	"github.com/p-arndt/doreg/internal/meta"
)

// githubTimeout bounds one README fetch, so a slow GitHub cannot hold a
// request open indefinitely.
const githubTimeout = 15 * time.Second

var (
	githubNamePattern = regexp.MustCompile(`^[A-Za-z0-9_.-]+$`)
	urlSchemePattern  = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9+.-]*:`)

	// markdownLinkPattern matches [text](target "title") and the image form
	// ![alt](target). Group 1 is the bracketed text, group 2 the target,
	// group 3 the optional title with the closing whitespace.
	markdownLinkPattern = regexp.MustCompile(`(!?\[[^\]]*\])\(([^)\s]+)((?:\s+(?:"[^"]*"|'[^']*'))?\s*)\)`)

	// htmlAttrPattern matches src and href attributes with a double-quoted value.
	htmlAttrPattern = regexp.MustCompile(`(?i)\b(src|href)(\s*=\s*)"([^"]*)"`)
)

// importGitHubReadme fetches the README of a GitHub repository and rewrites
// its relative links for use outside GitHub. Nothing is stored; the UI puts
// the result into the editor and saving is a separate request.
func (a *API) importGitHubReadme(w http.ResponseWriter, r *http.Request) {
	name := r.URL.Query().Get("name")
	if !a.allow(w, r, meta.ProjectOf(name), auth.Push) {
		return
	}
	var body struct {
		Source string `json:"source"`
	}
	if !decode(w, r, &body) {
		return
	}
	repo, err := a.DB.RepoByName(r.Context(), name)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	source := strings.TrimSpace(body.Source)
	if source == "" {
		_, source = a.repoLabels(r.Context(), repo.ID)
	}
	if source == "" {
		writeErr(w, http.StatusBadRequest, "no GitHub repository given and the image has no org.opencontainers.image.source label")
		return
	}
	owner, ghRepo, ok := parseGitHubSource(source)
	if !ok {
		writeErr(w, http.StatusBadRequest, "only GitHub repositories are supported")
		return
	}
	readme, ok := a.fetchGitHubReadme(w, r, owner, ghRepo)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{
		"readme": rewriteRelativeLinks(readme, owner, ghRepo),
		"source": "https://github.com/" + owner + "/" + ghRepo,
	})
}

// parseGitHubSource accepts https://github.com/owner/repo (with an optional
// .git suffix or further path) and the shorthands github.com/owner/repo and
// owner/repo. Other hosts, schemes, user info and ports are rejected, and the
// result is only ever used as a path under the configured GitHub API, so a
// user-supplied source cannot steer the request to another host.
func parseGitHubSource(s string) (owner, repo string, ok bool) {
	s = strings.TrimSpace(s)
	if strings.Contains(s, "://") {
		rest, found := strings.CutPrefix(s, "https://github.com/")
		if !found {
			return "", "", false
		}
		s = rest
	} else {
		s = strings.TrimPrefix(s, "github.com/")
	}
	if i := strings.IndexAny(s, "?#"); i >= 0 {
		s = s[:i]
	}
	parts := strings.Split(s, "/")
	if len(parts) < 2 {
		return "", "", false
	}
	// GitHub owners never contain a dot, so a dotted first segment is another
	// host such as gitlab.com/owner/repo.
	owner = parts[0]
	repo = strings.TrimSuffix(parts[1], ".git")
	if !githubNamePattern.MatchString(owner) || strings.Contains(owner, ".") {
		return "", "", false
	}
	if !githubNamePattern.MatchString(repo) || repo == "." || repo == ".." {
		return "", "", false
	}
	return owner, repo, true
}

// fetchGitHubReadme downloads the README through the configured GitHub API.
// On failure it writes the error response and reports false.
func (a *API) fetchGitHubReadme(w http.ResponseWriter, r *http.Request, owner, repo string) (string, bool) {
	client := &http.Client{Timeout: githubTimeout}
	url := strings.TrimSuffix(a.GitHubAPI, "/") + "/repos/" + owner + "/" + repo + "/readme"
	req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, url, nil)
	if err != nil {
		a.fail(w, r, err)
		return "", false
	}
	// The JSON form names the file, which tells Markdown from AsciiDoc or reST.
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	req.Header.Set("User-Agent", "doreg")
	if a.GitHubToken != "" {
		req.Header.Set("Authorization", "Bearer "+a.GitHubToken)
	}
	resp, err := client.Do(req)
	if err != nil {
		a.Log.Warn("github readme request failed", "repo", owner+"/"+repo, "err", err)
		writeErr(w, http.StatusBadGateway, "GitHub request failed")
		return "", false
	}
	defer resp.Body.Close()

	switch {
	case resp.StatusCode == http.StatusNotFound:
		writeErr(w, http.StatusNotFound, fmt.Sprintf("no README found in %s/%s", owner, repo))
		return "", false
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
		writeErr(w, http.StatusBadGateway, "GitHub refused the request (rate limit or private repository; set DOREG_GITHUB_TOKEN)")
		return "", false
	case resp.StatusCode < 200 || resp.StatusCode > 299:
		a.Log.Warn("github readme request failed", "repo", owner+"/"+repo, "status", resp.StatusCode)
		writeErr(w, http.StatusBadGateway, "GitHub request failed")
		return "", false
	}

	// Base64 with GitHub's line breaks plus the JSON envelope stays well below
	// twice the decoded size.
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 2*maxReadme+4096))
	if err != nil {
		a.Log.Warn("github readme read failed", "repo", owner+"/"+repo, "err", err)
		writeErr(w, http.StatusBadGateway, "GitHub request failed")
		return "", false
	}
	var file struct {
		Name     string `json:"name"`
		Content  string `json:"content"`
		Encoding string `json:"encoding"`
	}
	if err := json.Unmarshal(raw, &file); err != nil {
		if len(raw) > 2*maxReadme {
			writeErr(w, http.StatusBadRequest, "the README is larger than 64 KiB")
			return "", false
		}
		a.Log.Warn("github readme response invalid", "repo", owner+"/"+repo, "err", err)
		writeErr(w, http.StatusBadGateway, "GitHub request failed")
		return "", false
	}
	if !markdownFile(file.Name) {
		writeErr(w, http.StatusUnprocessableEntity,
			fmt.Sprintf("%s is not Markdown; only Markdown READMEs can be imported", file.Name))
		return "", false
	}
	data, err := base64.StdEncoding.DecodeString(strings.ReplaceAll(file.Content, "\n", ""))
	if err != nil || file.Encoding != "base64" {
		writeErr(w, http.StatusBadGateway, "GitHub request failed")
		return "", false
	}
	if len(data) > maxReadme {
		writeErr(w, http.StatusBadRequest, "the README is larger than 64 KiB")
		return "", false
	}
	return string(data), true
}

// markdownFile reports whether a README file name means Markdown. A README
// without an extension is plain text, which reads fine as Markdown.
func markdownFile(name string) bool {
	switch strings.ToLower(path.Ext(name)) {
	case ".md", ".markdown", ".mdown", ".mkdn", "":
		return true
	}
	return false
}

// rewriteRelativeLinks points relative Markdown and HTML links and images at
// the GitHub repository, so they keep working once the README is served from
// doreg. Images go to raw.githubusercontent.com, other links to the blob
// view. Absolute URLs, fragments and protocol-relative URLs are left alone.
func rewriteRelativeLinks(md, owner, repo string) string {
	md = markdownLinkPattern.ReplaceAllStringFunc(md, func(m string) string {
		sub := markdownLinkPattern.FindStringSubmatch(m)
		if !relativeTarget(sub[2]) {
			return m
		}
		image := strings.HasPrefix(sub[1], "!")
		return sub[1] + "(" + githubURL(sub[2], image, owner, repo) + sub[3] + ")"
	})
	return htmlAttrPattern.ReplaceAllStringFunc(md, func(m string) string {
		sub := htmlAttrPattern.FindStringSubmatch(m)
		if !relativeTarget(sub[3]) {
			return m
		}
		image := strings.EqualFold(sub[1], "src")
		return sub[1] + sub[2] + `"` + githubURL(sub[3], image, owner, repo) + `"`
	})
}

// relativeTarget reports whether a link target refers to a file in the
// repository rather than to an absolute or in-page URL.
func relativeTarget(target string) bool {
	if target == "" || strings.HasPrefix(target, "#") || strings.HasPrefix(target, "//") {
		return false
	}
	return !urlSchemePattern.MatchString(target)
}

// githubURL maps a repository-relative path to its GitHub URL on the default
// branch. A leading slash counts as the repository root, as on GitHub.
func githubURL(target string, image bool, owner, repo string) string {
	p := strings.TrimLeft(target, "/")
	for strings.HasPrefix(p, "./") {
		p = strings.TrimLeft(p[2:], "/")
	}
	if image {
		u := "https://raw.githubusercontent.com/" + owner + "/" + repo + "/HEAD/" + p
		// raw.githubusercontent.com serves SVG as text/plain, which browsers
		// refuse in <img>; sanitize=true makes it answer image/svg+xml.
		if strings.HasSuffix(strings.ToLower(p), ".svg") {
			u += "?sanitize=true"
		}
		return u
	}
	return "https://github.com/" + owner + "/" + repo + "/blob/HEAD/" + p
}
