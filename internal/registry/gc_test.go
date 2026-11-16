package registry_test

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/p-arndt/doreg/internal/digest"
	"github.com/p-arndt/doreg/internal/gc"
	"github.com/p-arndt/doreg/internal/meta"
	"github.com/p-arndt/doreg/internal/oci"
)

// advance shifts the metadata clock forward for the rest of the test.
func advance(t *testing.T, d time.Duration) {
	t.Helper()
	prev := meta.Now
	meta.Now = func() int64 { return prev() + int64(d.Seconds()) }
	t.Cleanup(func() { meta.Now = prev })
}

func (e *env) collector(s gc.Settings) *gc.Collector {
	e.t.Helper()
	raw, _ := json.Marshal(s)
	// Bypass validation so tests can use a zero grace period.
	if err := e.db.SetSetting(context.Background(), "gc", string(raw)); err != nil {
		e.t.Fatal(err)
	}
	return gc.New(e.db, e.store, slog.New(slog.DiscardHandler))
}

func (e *env) gc(c *gc.Collector, opts gc.Options) *gc.Report {
	e.t.Helper()
	run, err := c.Run(context.Background(), opts)
	if err != nil {
		e.t.Fatal(err)
	}
	return run.Report
}

func manifestBlobs(t *testing.T, e *env, repo, ref string) []oci.Descriptor {
	t.Helper()
	m, err := e.db.GetManifest(context.Background(), repo, ref, true)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := oci.Parse(m.Content, m.MediaType)
	if err != nil {
		t.Fatal(err)
	}
	return append([]oci.Descriptor{*parsed.Config}, parsed.Layers...)
}

func TestGCRemovesUntaggedAndFreesBlobs(t *testing.T) {
	e := newEnv(t)
	old := e.pushImage("app", "latest", "v1")
	cur := e.pushImage("app", "latest", "v2") // retags, old becomes untagged
	oldBlobs := manifestBlobs(t, e, "app", old)

	c := e.collector(gc.DefaultSettings)
	rep := e.gc(c, gc.Options{})
	if len(rep.DeletedManifests) != 0 || rep.DeletedBlobs != 0 || rep.KeptRecent != 1 {
		t.Fatalf("grace period must protect fresh content: %+v", rep)
	}

	advance(t, 2*time.Hour)
	dry := e.gc(c, gc.Options{DryRun: true})
	if len(dry.DeletedManifests) != 1 || dry.DeletedBlobs != 2 {
		t.Fatalf("dry run report: %+v", dry)
	}
	if e.count("manifests") != 2 || !e.store.Exists(digest.Digest(oldBlobs[1].Digest)) {
		t.Fatal("dry run changed state")
	}

	rep = e.gc(c, gc.Options{})
	if len(rep.DeletedManifests) != 1 || rep.DeletedManifests[0].Digest != old || rep.DeletedBlobs != 2 || rep.FreedBytes == 0 {
		t.Fatalf("report: %+v", rep)
	}
	for _, b := range oldBlobs {
		if e.store.Exists(digest.Digest(b.Digest)) {
			t.Fatalf("blob %s still on disk", b.Digest)
		}
	}
	expect(t, e.do("GET", "/v2/app/manifests/latest", nil, nil), http.StatusOK)
	for _, b := range manifestBlobs(t, e, "app", cur) {
		expect(t, e.do("GET", "/v2/app/blobs/"+b.Digest, nil, nil), http.StatusOK)
	}
	expect(t, e.do("GET", "/v2/app/manifests/"+old, nil, nil), http.StatusNotFound)
}

func TestGCKeepsSharedBlobsAcrossRepos(t *testing.T) {
	e := newEnv(t)
	e.pushImage("a", "v1", "same")
	d := e.pushImage("b", "v1", "same")
	expect(t, e.do("DELETE", "/v2/a/manifests/v1", nil, nil), http.StatusAccepted)
	advance(t, 2*time.Hour)
	rep := e.gc(e.collector(gc.DefaultSettings), gc.Options{})
	if rep.DeletedBlobs != 0 || rep.UnlinkedBlobs != 2 {
		t.Fatalf("report: %+v", rep)
	}
	for _, b := range manifestBlobs(t, e, "b", d) {
		expect(t, e.do("GET", "/v2/b/blobs/"+b.Digest, nil, nil), http.StatusOK)
		expect(t, e.do("GET", "/v2/a/blobs/"+b.Digest, nil, nil), http.StatusNotFound)
	}
}

func TestGCKeepsIndexChildrenAndReferrers(t *testing.T) {
	e := newEnv(t)
	amd := e.pushImage("app", "", "amd64")
	arm := e.pushImage("app", "", "arm64")
	idx := e.putManifest("app", "v1", oci.Manifest{SchemaVersion: 2, MediaType: oci.MediaTypeOCIIndex, Manifests: []oci.Descriptor{
		{MediaType: oci.MediaTypeOCIManifest, Digest: amd, Size: 1, Platform: &oci.Platform{OS: "linux", Architecture: "amd64"}},
		{MediaType: oci.MediaTypeOCIManifest, Digest: arm, Size: 1, Platform: &oci.Platform{OS: "linux", Architecture: "arm64"}},
	}})
	empty := e.pushBlob("app", []byte("{}"))
	empty.MediaType = oci.MediaTypeOCIEmpty
	sig := e.putManifest("app", "", oci.Manifest{SchemaVersion: 2, MediaType: oci.MediaTypeOCIManifest,
		ArtifactType: "application/vnd.example.sig", Config: &empty, Layers: []oci.Descriptor{empty},
		Subject: &oci.Descriptor{MediaType: oci.MediaTypeOCIIndex, Digest: idx, Size: 1}})
	stray := e.pushImage("app", "", "stray")

	advance(t, 2*time.Hour)
	rep := e.gc(e.collector(gc.DefaultSettings), gc.Options{})
	if len(rep.DeletedManifests) != 1 || rep.DeletedManifests[0].Digest != stray {
		t.Fatalf("report: %+v", rep)
	}
	for _, d := range []string{amd, arm, idx, sig} {
		expect(t, e.do("HEAD", "/v2/app/manifests/"+d, nil, nil), http.StatusOK)
	}
}

func TestGCKeepsRecentlyPulledUntagged(t *testing.T) {
	e := newEnv(t)
	pinned := e.pushImage("app", "latest", "v1")
	e.pushImage("app", "latest", "v2")
	advance(t, 2*time.Hour)
	// A digest-pinned deployment still pulls the old manifest.
	expect(t, e.do("GET", "/v2/app/manifests/"+pinned, nil, nil), http.StatusOK)

	c := e.collector(gc.DefaultSettings)
	if rep := e.gc(c, gc.Options{}); len(rep.DeletedManifests) != 0 {
		t.Fatalf("pulled manifest deleted: %+v", rep)
	}
	advance(t, 8*24*time.Hour)
	if rep := e.gc(c, gc.Options{}); len(rep.DeletedManifests) != 1 {
		t.Fatalf("stale pinned manifest kept: %+v", rep)
	}
}

func TestGCRemovesOrphanFilesAndStaleUploads(t *testing.T) {
	e := newEnv(t)
	orphan := []byte("crashed before the row was written")
	od := digest.FromBytes("sha256", orphan)
	if err := e.store.PutBytes(od, orphan); err != nil {
		t.Fatal(err)
	}
	expect(t, e.do("POST", "/v2/app/blobs/uploads/", nil, nil), http.StatusAccepted)

	s := gc.DefaultSettings
	s.GraceMinutes = 0
	advance(t, 48*time.Hour)
	rep := e.gc(e.collector(s), gc.Options{})
	if rep.OrphanFiles != 1 || rep.StaleUploads != 1 {
		t.Fatalf("report: %+v", rep)
	}
	if e.store.Exists(od) || e.count("uploads") != 0 {
		t.Fatal("orphan or upload left behind")
	}
}

func TestRetentionPolicies(t *testing.T) {
	e := newEnv(t)
	for i := 1; i <= 5; i++ {
		e.pushImage("team/api", fmt.Sprintf("1.0.%d", i), fmt.Sprint(i))
		advance(t, 24*time.Hour)
	}
	e.pushImage("team/api", "latest", "5")
	e.pushImage("other", "1.0.0", "x")

	if _, err := gc.SavePolicy(t.Context(), e.db, gc.Policy{
		Name: "keep 2", RepoPattern: "team/*", KeepPattern: "^latest$", KeepLast: 2, Enabled: true,
	}); err != nil {
		t.Fatal(err)
	}
	advance(t, 2*time.Hour)
	c := e.collector(gc.DefaultSettings)
	rep := e.gc(c, gc.Options{ApplyRetention: true})
	var deleted []string
	for _, d := range rep.DeletedTags {
		deleted = append(deleted, d.Repo+":"+d.Tag)
	}
	if fmt.Sprint(deleted) != "[team/api:1.0.3 team/api:1.0.2 team/api:1.0.1]" {
		t.Fatalf("deleted tags %v", deleted)
	}
	if len(rep.DeletedManifests) != 3 {
		t.Fatalf("manifests: %+v", rep.DeletedManifests)
	}
	tags, _ := e.db.ListTags(t.Context(), "team/api", "", -1)
	if fmt.Sprint(tags) != "[1.0.4 1.0.5 latest]" {
		t.Fatalf("remaining tags %v", tags)
	}
}

func TestPolicyValidation(t *testing.T) {
	bad := []gc.Policy{
		{Name: "x", RepoPattern: "*"}, // would delete everything
		{Name: "x", RepoPattern: "*", KeepLast: 1, TagPattern: "("},
		{Name: "", RepoPattern: "*", KeepLast: 1},
	}
	for _, p := range bad {
		if p.Validate() == nil {
			t.Errorf("policy %+v accepted", p)
		}
	}
}

// TestConcurrentPushDuringGC runs GC in a tight loop with no grace period while
// pushes that share a base layer keep arriving. A push may fail (its blob was
// collected before its manifest arrived; the grace period exists to prevent
// that), but every push that succeeded must stay fully pullable and every blob
// row must have its file.
func TestConcurrentPushDuringGC(t *testing.T) {
	if testing.Short() {
		t.Skip("stress test")
	}
	e := newEnv(t)
	s := gc.DefaultSettings
	s.GraceMinutes = 0
	c := e.collector(s)

	stop := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
				if _, err := c.Run(context.Background(), gc.Options{}); err != nil {
					t.Error(err)
					return
				}
			}
		}
	}()

	base := []byte("shared base layer")
	var ok atomic.Int32
	var pushers sync.WaitGroup
	for w := range 4 {
		pushers.Add(1)
		go func() {
			defer pushers.Done()
			for i := range 25 {
				repo := fmt.Sprintf("app%d", w%2)
				tag := fmt.Sprintf("t%d-%d", w, i)
				if pushTolerant(e, repo, tag, base, fmt.Sprintf("%d-%d", w, i)) {
					ok.Add(1)
				}
			}
		}()
	}
	pushers.Wait()
	close(stop)
	wg.Wait()

	if ok.Load() == 0 {
		t.Fatal("no push succeeded")
	}
	assertIntegrity(t, e)
}

// pushTolerant pushes an image and reports success; failures are expected
// races, not test errors.
func pushTolerant(e *env, repo, tag string, base []byte, unique string) bool {
	put := func(data []byte) (oci.Descriptor, bool) {
		d := digest.FromBytes("sha256", data)
		resp := e.do("POST", "/v2/"+repo+"/blobs/uploads/?digest="+d.String(), data, nil)
		return oci.Descriptor{MediaType: "application/octet-stream", Digest: d.String(), Size: int64(len(data))}, resp.StatusCode == http.StatusCreated
	}
	cfg, ok1 := put([]byte(`{"os":"linux","architecture":"amd64","u":"` + unique + `"}`))
	layer, ok2 := put(base)
	if !ok1 || !ok2 {
		return false
	}
	body, _ := json.Marshal(oci.Manifest{SchemaVersion: 2, MediaType: oci.MediaTypeOCIManifest, Config: &cfg, Layers: []oci.Descriptor{layer}})
	resp := e.do("PUT", "/v2/"+repo+"/manifests/"+tag, body, map[string]string{"Content-Type": oci.MediaTypeOCIManifest})
	return resp.StatusCode == http.StatusCreated
}

func assertIntegrity(t *testing.T, e *env) {
	t.Helper()
	rows, err := e.db.R.Query(`SELECT digest FROM blobs`)
	if err != nil {
		t.Fatal(err)
	}
	var blobs []string
	for rows.Next() {
		var d string
		rows.Scan(&d)
		blobs = append(blobs, d)
	}
	rows.Close()
	for _, d := range blobs {
		if !e.store.Exists(digest.Digest(d)) {
			t.Errorf("blob row %s has no file", d)
		}
	}
	rows, err = e.db.R.Query(`
		SELECT r.name, t.name, m.digest FROM tags t
		JOIN repositories r ON r.id = t.repo_id JOIN manifests m ON m.id = t.manifest_id`)
	if err != nil {
		t.Fatal(err)
	}
	type tagRef struct{ repo, tag, digest string }
	var tags []tagRef
	for rows.Next() {
		var tr tagRef
		rows.Scan(&tr.repo, &tr.tag, &tr.digest)
		tags = append(tags, tr)
	}
	rows.Close()
	for _, tr := range tags {
		for _, b := range manifestBlobs(t, e, tr.repo, tr.digest) {
			if resp := e.do("HEAD", "/v2/"+tr.repo+"/blobs/"+b.Digest, nil, nil); resp.StatusCode != http.StatusOK {
				t.Errorf("%s:%s references missing blob %s", tr.repo, tr.tag, b.Digest)
			}
		}
	}
}
