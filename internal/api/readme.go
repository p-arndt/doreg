package api

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"unicode"

	"github.com/microcosm-cc/bluemonday"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/renderer/html"
	"github.com/yuin/goldmark/util"

	"github.com/p-arndt/doreg/internal/auth"
	"github.com/p-arndt/doreg/internal/meta"
	"github.com/p-arndt/doreg/internal/oci"
)

// maxReadme bounds a stored README.
const maxReadme = 64 << 10

// maxDescription bounds the description shown in repository lists.
const maxDescription = 300

const (
	labelDescription = "org.opencontainers.image.description"
	labelSource      = "org.opencontainers.image.source"
)

// markdown keeps raw HTML because READMEs written for GitHub rely on it
// (centered headers, sized logos); readmePolicy then strips everything that
// could run script, since anonymous visitors read these pages.
var markdown = goldmark.New(
	goldmark.WithExtensions(extension.GFM),
	goldmark.WithParserOptions(parser.WithAutoHeadingID()),
	goldmark.WithRendererOptions(html.WithUnsafe()),
)

// readmePolicy is roughly what GitHub lets through in READMEs: user-generated
// content markup, alignment, and heading ids so in-page links resolve.
var readmePolicy = func() *bluemonday.Policy {
	p := bluemonday.UGCPolicy()
	p.AllowAttrs("align").Matching(regexp.MustCompile(`(?i)^(left|center|right)$`)).
		OnElements("div", "p", "h1", "h2", "h3", "h4", "h5", "h6", "img", "td", "th")
	p.AllowAttrs("id").Matching(regexp.MustCompile(`^[\p{L}\p{M}\p{N}_-]+$`)).
		OnElements("h1", "h2", "h3", "h4", "h5", "h6")
	p.AllowElements("details", "summary", "picture")
	p.AllowAttrs("open").OnElements("details")
	return p
}()

// renderMarkdown turns README Markdown into HTML that is safe to show to
// anonymous visitors.
func renderMarkdown(src string) string {
	if strings.TrimSpace(src) == "" {
		return ""
	}
	var buf bytes.Buffer
	ctx := parser.NewContext(parser.WithIDs(&githubIDs{seen: map[string]int{}}))
	if err := markdown.Convert([]byte(src), &buf, parser.WithContext(ctx)); err != nil {
		return ""
	}
	return readmePolicy.Sanitize(buf.String())
}

// githubIDs generates heading ids the way GitHub does, so links such as
// [Quick start](#-quick-start) written against GitHub keep working.
type githubIDs struct {
	seen map[string]int
}

func (g *githubIDs) Generate(value []byte, _ ast.NodeKind) []byte {
	var b strings.Builder
	for _, r := range strings.ToLower(string(value)) {
		switch {
		case r == ' ':
			b.WriteByte('-')
		case r == '-' || r == '_' || unicode.IsLetter(r) || unicode.IsNumber(r) || unicode.IsMark(r):
			b.WriteRune(r)
		}
	}
	id := b.String()
	if id == "" {
		id = "section"
	}
	n := g.seen[id]
	g.seen[id] = n + 1
	if n > 0 {
		id += "-" + strconv.Itoa(n)
	}
	return []byte(id)
}

func (g *githubIDs) Put(value []byte) {
	g.seen[util.BytesToReadOnlyString(value)]++
}

// repoLabels returns the description and source URL of the repository's most
// recently updated tag, from manifest annotations or image config labels.
// Labels are decoration, so every failure yields empty strings.
func (a *API) repoLabels(ctx context.Context, repoID int64) (description, source string) {
	var digest, mediaType string
	var content []byte
	err := a.DB.R.QueryRowContext(ctx, `
		SELECT m.digest, m.media_type, m.content FROM tags t JOIN manifests m ON m.id = t.manifest_id
		WHERE t.repo_id = ? ORDER BY t.updated_at DESC, t.name LIMIT 1`, repoID).
		Scan(&digest, &mediaType, &content)
	if err != nil {
		return "", ""
	}
	parsed, err := oci.Parse(content, mediaType)
	if err != nil {
		return "", ""
	}
	description, source = a.labelsOf(parsed)
	if oci.IsIndex(parsed.MediaType) && (description == "" || source == "") {
		// Fall back to the first platform image; attestations carry no labels
		// worth showing.
		for _, c := range parsed.Manifests {
			if oci.IsAttestation(c) {
				continue
			}
			child := a.repoManifest(ctx, repoID, c.Digest)
			if child == nil {
				continue
			}
			d, s := a.labelsOf(child)
			if description == "" {
				description = d
			}
			if source == "" {
				source = s
			}
			break
		}
	}
	description = truncateRunes(strings.TrimSpace(description), maxDescription)
	source = strings.TrimSpace(source)
	if !strings.HasPrefix(source, "https://") && !strings.HasPrefix(source, "http://") {
		source = ""
	}
	return description, source
}

// labelsOf reads the description and source of one manifest: its annotations
// first, then, for image manifests, the labels of its config.
func (a *API) labelsOf(m *oci.Manifest) (description, source string) {
	description = m.Annotations[labelDescription]
	source = m.Annotations[labelSource]
	if description != "" && source != "" {
		return description, source
	}
	if m.Config == nil || m.Config.MediaType == oci.MediaTypeOCIEmpty || oci.IsIndex(m.MediaType) {
		return description, source
	}
	cfg := a.imageConfig(m.Config.Digest)
	if cfg == nil {
		return description, source
	}
	if description == "" {
		description = cfg.Config.Labels[labelDescription]
	}
	if source == "" {
		source = cfg.Config.Labels[labelSource]
	}
	return description, source
}

// repoManifest loads and parses a manifest of the repository, or returns nil.
func (a *API) repoManifest(ctx context.Context, repoID int64, digest string) *oci.Manifest {
	var mediaType string
	var content []byte
	err := a.DB.R.QueryRowContext(ctx,
		`SELECT media_type, content FROM manifests WHERE repo_id = ? AND digest = ?`, repoID, digest).
		Scan(&mediaType, &content)
	if err != nil {
		return nil
	}
	parsed, err := oci.Parse(content, mediaType)
	if err != nil {
		return nil
	}
	return parsed
}

// readmeBody decodes a README request body and enforces the size limit.
// It returns the README with LF line endings.
func readmeBody(w http.ResponseWriter, r *http.Request) (string, bool) {
	var body struct {
		Readme string `json:"readme"`
	}
	if !decode(w, r, &body) {
		return "", false
	}
	readme := strings.ReplaceAll(body.Readme, "\r\n", "\n")
	if len(readme) > maxReadme {
		writeErr(w, http.StatusBadRequest, "readme is larger than 64 KiB")
		return "", false
	}
	return readme, true
}

func (a *API) saveReadme(w http.ResponseWriter, r *http.Request) {
	name := r.URL.Query().Get("name")
	if !a.allow(w, r, meta.ProjectOf(name), auth.Push) {
		return
	}
	readme, ok := readmeBody(w, r)
	if !ok {
		return
	}
	if err := a.DB.SetRepoReadme(r.Context(), name, readme); err != nil {
		a.fail(w, r, err)
		return
	}
	a.record(r, "repository_readme", meta.ProjectOf(name), name, fmt.Sprintf("%d bytes", len(readme)))
	writeJSON(w, http.StatusOK, map[string]string{"readme": readme, "readmeHtml": renderMarkdown(readme)})
}

// previewReadme renders a draft README for the editor. It needs a session but
// no project permission, since nothing is stored.
func (a *API) previewReadme(w http.ResponseWriter, r *http.Request) {
	readme, ok := readmeBody(w, r)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"html": renderMarkdown(readme)})
}
