package storage

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"testing"
	"time"

	"github.com/p-arndt/doreg/internal/digest"
)

// TestS3Backend runs against a live S3-compatible endpoint. It is skipped unless
// S3_TEST_ENDPOINT is set. Optional: S3_TEST_BUCKET (default doreg-test),
// S3_TEST_ACCESS_KEY_ID and S3_TEST_SECRET_ACCESS_KEY (default minioadmin).
// Every run writes under its own key prefix and removes its blob again.
func TestS3Backend(t *testing.T) {
	endpoint := os.Getenv("S3_TEST_ENDPOINT")
	if endpoint == "" {
		t.Skip("S3_TEST_ENDPOINT not set")
	}
	env := func(key, def string) string {
		if v := os.Getenv(key); v != "" {
			return v
		}
		return def
	}
	t.Setenv("S3_ENDPOINT", endpoint)
	t.Setenv("S3_BUCKET", env("S3_TEST_BUCKET", "doreg-test"))
	t.Setenv("S3_ACCESS_KEY_ID", env("S3_TEST_ACCESS_KEY_ID", "minioadmin"))
	t.Setenv("S3_SECRET_ACCESS_KEY", env("S3_TEST_SECRET_ACCESS_KEY", "minioadmin"))
	t.Setenv("S3_FORCE_PATH_STYLE", "true")
	t.Setenv("S3_PREFIX", fmt.Sprintf("test-%d/", time.Now().UnixNano()))

	store, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	content := bytes.Repeat([]byte("doreg s3 blob "), 1000)
	want := digest.FromBytes("sha256", content)
	missing := digest.FromBytes("sha256", []byte("never stored"))

	// Upload in two chunks through the local upload path, then commit to S3.
	if err := store.CreateUpload("u1"); err != nil {
		t.Fatalf("CreateUpload: %v", err)
	}
	n, err := store.AppendUpload("u1", 0, bytes.NewReader(content[:5000]))
	if err != nil || n != 5000 {
		t.Fatalf("AppendUpload first chunk: n=%d err=%v", n, err)
	}
	if _, err := store.AppendUpload("u1", n, bytes.NewReader(content[5000:])); err != nil {
		t.Fatalf("AppendUpload second chunk: %v", err)
	}
	size, err := store.CommitUpload("u1", want)
	if err != nil {
		t.Fatalf("CommitUpload: %v", err)
	}
	if size != int64(len(content)) {
		t.Fatalf("CommitUpload size = %d, want %d", size, len(content))
	}
	if _, err := store.UploadSize("u1"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("upload file still present after commit: %v", err)
	}

	if !store.Exists(want) {
		t.Fatal("Exists reports committed blob as missing")
	}
	if got, err := store.Stat(want); err != nil || got != int64(len(content)) {
		t.Fatalf("Stat = %d, %v; want %d", got, err, len(content))
	}

	rc, err := store.Open(want)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer rc.Close()
	if _, err := rc.Seek(4000, io.SeekStart); err != nil {
		t.Fatalf("Seek: %v", err)
	}
	buf := make([]byte, 2000)
	if _, err := io.ReadFull(rc, buf); err != nil {
		t.Fatalf("Read after Seek: %v", err)
	}
	if !bytes.Equal(buf, content[4000:6000]) {
		t.Fatal("Read after Seek returned wrong bytes")
	}
	ra, ok := rc.(io.ReaderAt)
	if !ok {
		t.Fatal("Open result does not implement io.ReaderAt")
	}
	part := make([]byte, 100)
	if _, err := ra.ReadAt(part, 9000); err != nil {
		t.Fatalf("ReadAt: %v", err)
	}
	if !bytes.Equal(part, content[9000:9100]) {
		t.Fatal("ReadAt returned wrong bytes")
	}

	if _, err := store.Open(missing); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Open missing blob: err = %v, want ErrNotFound", err)
	}
	if _, err := store.Stat(missing); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Stat missing blob: err = %v, want ErrNotFound", err)
	}
	if store.Exists(missing) {
		t.Fatal("Exists reports missing blob as present")
	}

	manifest := []byte(`{"schemaVersion":2}`)
	manifestDigest := digest.FromBytes("sha256", manifest)
	if err := store.PutBytes(manifestDigest, manifest); err != nil {
		t.Fatalf("PutBytes: %v", err)
	}

	var seen []digest.Digest
	err = store.WalkBlobs(func(d digest.Digest, size int64, _ time.Time) error {
		seen = append(seen, d)
		if d == want && size != int64(len(content)) {
			return fmt.Errorf("WalkBlobs size for %s = %d", d, size)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("WalkBlobs: %v", err)
	}
	if len(seen) != 2 {
		t.Fatalf("WalkBlobs found %d blobs, want 2: %v", len(seen), seen)
	}

	for _, d := range []digest.Digest{want, manifestDigest} {
		if err := store.Delete(d); err != nil {
			t.Fatalf("Delete %s: %v", d, err)
		}
		if store.Exists(d) {
			t.Fatalf("blob %s still present after Delete", d)
		}
	}
	if err := store.Delete(want); err != nil {
		t.Fatalf("Delete of missing blob: %v", err)
	}
}
