package importer

import (
	"context"
	"fmt"
	"io"
	"path"
	"sort"
	"sync"

	"github.com/p-arndt/doreg/internal/digest"
	"github.com/p-arndt/doreg/internal/oci"
)

// RepoPlan is one repository to import with the tags found in the source.
type RepoPlan struct {
	Name string   `json:"name"`
	Tags []string `json:"tags"`
}

// Progress counts what an import has done so far.
type Progress struct {
	Repositories int   `json:"repositories"`
	Tags         int   `json:"tags"`
	TagsCopied   int   `json:"tagsCopied"`
	TagsSkipped  int   `json:"tagsSkipped"`
	TagsFailed   int   `json:"tagsFailed"`
	Blobs        int   `json:"blobs"`
	Bytes        int64 `json:"bytes"`
	// Errors holds the first maxErrors failures as "repo:tag: message".
	Errors []string `json:"errors"`
}

const maxErrors = 100

// Tag outcomes reported to OnTag.
const (
	Copied  = "copied"
	Skipped = "skipped"
	Failed  = "failed"
)

// Importer copies tags from Src to Dst. Manifests are copied byte for byte, so
// digests stay the same and references pinned to a digest keep working.
type Importer struct {
	Src, Dst    *Client
	Concurrency int
	// OnTag, if set, is called after each tag (from several goroutines).
	OnTag func(repo, tag, outcome string, err error)

	mu       sync.Mutex
	progress Progress
	blobMu   sync.Map // digest -> *sync.Mutex
}

// ValidatePatterns reports a malformed repository pattern.
func ValidatePatterns(patterns []string) error {
	for _, p := range patterns {
		if _, err := path.Match(p, ""); err != nil {
			return fmt.Errorf("invalid repository pattern %q", p)
		}
	}
	return nil
}

func hasMeta(p string) bool {
	for _, c := range p {
		if c == '*' || c == '?' || c == '[' || c == '\\' {
			return true
		}
	}
	return false
}

// Plan lists the repositories matching patterns (path.Match syntax; none means
// all) and their tags. Registries without a catalog (Docker Hub) still work
// when every pattern is a plain repository name.
func (im *Importer) Plan(ctx context.Context, patterns []string) ([]RepoPlan, error) {
	if err := ValidatePatterns(patterns); err != nil {
		return nil, err
	}
	var repos []string
	literal := len(patterns) > 0
	for _, p := range patterns {
		literal = literal && !hasMeta(p)
	}
	if literal {
		repos = patterns
	} else {
		all, err := im.Src.Catalog(ctx)
		if err != nil {
			return nil, fmt.Errorf("listing repositories: %w", err)
		}
		for _, r := range all {
			if matchAny(patterns, r) {
				repos = append(repos, r)
			}
		}
	}
	sort.Strings(repos)
	plan := []RepoPlan{}
	seen := map[string]bool{}
	for _, r := range repos {
		if seen[r] {
			continue
		}
		seen[r] = true
		tags, err := im.Src.Tags(ctx, r)
		if err != nil {
			return nil, fmt.Errorf("listing tags of %s: %w", r, err)
		}
		if len(tags) > 0 {
			sort.Strings(tags)
			plan = append(plan, RepoPlan{Name: r, Tags: tags})
		}
	}
	return plan, nil
}

func matchAny(patterns []string, name string) bool {
	if len(patterns) == 0 {
		return true
	}
	for _, p := range patterns {
		if ok, _ := path.Match(p, name); ok {
			return true
		}
	}
	return false
}

// Progress returns a snapshot of the counters.
func (im *Importer) Progress() Progress {
	im.mu.Lock()
	defer im.mu.Unlock()
	p := im.progress
	p.Errors = append([]string{}, im.progress.Errors...)
	return p
}

func (im *Importer) update(fn func(p *Progress)) {
	im.mu.Lock()
	fn(&im.progress)
	im.mu.Unlock()
}

// Run copies every tag of the plan. A failing tag is counted and reported but
// does not stop the others; Run returns an error only when ctx ends.
func (im *Importer) Run(ctx context.Context, plan []RepoPlan) (Progress, error) {
	type job struct{ repo, tag string }
	jobs := make(chan job)
	im.update(func(p *Progress) {
		p.Repositories = len(plan)
		for _, r := range plan {
			p.Tags += len(r.Tags)
		}
	})
	n := max(im.Concurrency, 1)
	var wg sync.WaitGroup
	for range n {
		wg.Go(func() {
			for j := range jobs {
				outcome, err := im.copyTag(ctx, j.repo, j.tag)
				if ctx.Err() != nil {
					continue
				}
				im.update(func(p *Progress) {
					switch outcome {
					case Copied:
						p.TagsCopied++
					case Skipped:
						p.TagsSkipped++
					default:
						p.TagsFailed++
						if len(p.Errors) < maxErrors {
							p.Errors = append(p.Errors, j.repo+":"+j.tag+": "+err.Error())
						}
					}
				})
				if im.OnTag != nil {
					im.OnTag(j.repo, j.tag, outcome, err)
				}
			}
		})
	}
feed:
	for _, r := range plan {
		for _, t := range r.Tags {
			select {
			case jobs <- job{r.Name, t}:
			case <-ctx.Done():
				break feed
			}
		}
	}
	close(jobs)
	wg.Wait()
	return im.Progress(), ctx.Err()
}

func (im *Importer) copyTag(ctx context.Context, repo, tag string) (string, error) {
	body, mediaType, err := im.Src.Manifest(ctx, repo, tag)
	if err != nil {
		return Failed, err
	}
	d := digest.FromBytes("sha256", body)
	if have, err := im.Dst.ManifestDigest(ctx, repo, tag); err != nil {
		return Failed, err
	} else if have == d.String() {
		return Skipped, nil
	}
	if err := im.copyReferenced(ctx, repo, body, mediaType); err != nil {
		return Failed, err
	}
	if err := im.Dst.PutManifest(ctx, repo, tag, body, mediaType); err != nil {
		return Failed, err
	}
	return Copied, nil
}

// copyReferenced copies what a manifest points to: the child manifests of an
// index, or the config and layers of an image.
func (im *Importer) copyReferenced(ctx context.Context, repo string, body []byte, mediaType string) error {
	m, err := oci.Parse(body, mediaType)
	if err != nil {
		return err
	}
	if oci.IsIndex(m.MediaType) {
		for _, c := range m.Manifests {
			if err := im.copyChild(ctx, repo, c.Digest); err != nil {
				return err
			}
		}
		return nil
	}
	blobs := []oci.Descriptor{*m.Config}
	for _, l := range m.Layers {
		if len(l.URLs) == 0 { // foreign layers are fetched from their URLs, not stored
			blobs = append(blobs, l)
		}
	}
	for _, b := range blobs {
		if err := im.copyBlob(ctx, repo, b.Digest); err != nil {
			return err
		}
	}
	return nil
}

func (im *Importer) copyChild(ctx context.Context, repo, dgst string) error {
	if have, err := im.Dst.ManifestDigest(ctx, repo, dgst); err != nil {
		return err
	} else if have != "" {
		return nil
	}
	body, mediaType, err := im.Src.Manifest(ctx, repo, dgst)
	if err != nil {
		return err
	}
	if got := digest.FromBytes(digest.Digest(dgst).Algorithm(), body); got.String() != dgst {
		return fmt.Errorf("manifest %s: source returned content with digest %s", dgst, got)
	}
	if err := im.copyReferenced(ctx, repo, body, mediaType); err != nil {
		return err
	}
	return im.Dst.PutManifest(ctx, repo, dgst, body, mediaType)
}

// copyBlob copies one blob unless the repository already has it. Workers share
// base layers, so a per-digest lock lets the first copy finish and the others
// mount it instead of downloading it again.
func (im *Importer) copyBlob(ctx context.Context, repo, dgst string) error {
	lock, _ := im.blobMu.LoadOrStore(dgst, &sync.Mutex{})
	lock.(*sync.Mutex).Lock()
	defer lock.(*sync.Mutex).Unlock()
	if ok, err := im.Dst.HasBlob(ctx, repo, dgst); err != nil || ok {
		return err
	}
	mounted, err := im.Dst.PushBlob(ctx, repo, dgst, func() (io.ReadCloser, int64, error) {
		body, size, err := im.Src.Blob(ctx, repo, dgst)
		if err != nil {
			return nil, 0, err
		}
		return &countingReader{ReadCloser: body, im: im}, size, nil
	})
	if err != nil {
		return fmt.Errorf("blob %s: %w", dgst, err)
	}
	if !mounted {
		im.update(func(p *Progress) { p.Blobs++ })
	}
	return nil
}

// countingReader adds transferred bytes to the progress as they flow, so a
// large layer does not look stalled.
type countingReader struct {
	io.ReadCloser
	im *Importer
}

func (r *countingReader) Read(b []byte) (int, error) {
	n, err := r.ReadCloser.Read(b)
	if n > 0 {
		r.im.update(func(p *Progress) { p.Bytes += int64(n) })
	}
	return n, err
}
