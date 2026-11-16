package storage

import (
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/p-arndt/doreg/internal/digest"
)

// blobBackend stores finished, verified blobs. Uploads never go through it.
type blobBackend interface {
	// open returns a seekable reader, or ErrNotFound.
	open(d digest.Digest) (io.ReadSeekCloser, error)
	// stat returns the blob size, or ErrNotFound.
	stat(d digest.Digest) (int64, error)
	// delete removes the blob; a missing blob is not an error.
	delete(d digest.Digest) error
	// put stores the local file src as blob d. src is consumed on success.
	put(d digest.Digest, src string) error
	// walk calls fn for every blob in the backend.
	walk(fn func(d digest.Digest, size int64, mod time.Time) error) error
}

// localBackend keeps blobs under <root>/blobs/<alg>/<xx>/<hex>.
type localBackend struct {
	root string
}

func (b localBackend) blobPath(d digest.Digest) string {
	hex := d.Hex()
	return filepath.Join(b.root, "blobs", d.Algorithm(), hex[:2], hex)
}

func (b localBackend) open(d digest.Digest) (io.ReadSeekCloser, error) {
	f, err := os.Open(b.blobPath(d))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return f, nil
}

func (b localBackend) stat(d digest.Digest) (int64, error) {
	fi, err := os.Stat(b.blobPath(d))
	if errors.Is(err, fs.ErrNotExist) {
		return 0, ErrNotFound
	}
	if err != nil {
		return 0, err
	}
	return fi.Size(), nil
}

func (b localBackend) delete(d digest.Digest) error {
	err := os.Remove(b.blobPath(d))
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	return err
}

func (b localBackend) put(d digest.Digest, src string) error {
	dst := b.blobPath(d)
	if err := os.MkdirAll(filepath.Dir(dst), 0o750); err != nil {
		return err
	}
	return os.Rename(src, dst)
}

func (b localBackend) walk(fn func(d digest.Digest, size int64, mod time.Time) error) error {
	base := filepath.Join(b.root, "blobs")
	return filepath.WalkDir(base, func(path string, e fs.DirEntry, err error) error {
		if err != nil || e.IsDir() {
			return err
		}
		rel, err := filepath.Rel(base, path)
		if err != nil {
			return err
		}
		parts := strings.Split(filepath.ToSlash(rel), "/")
		if len(parts) != 3 {
			return nil
		}
		d, err := digest.Parse(parts[0] + ":" + parts[2])
		if err != nil {
			return nil
		}
		info, err := e.Info()
		if err != nil {
			return err
		}
		return fn(d, info.Size(), info.ModTime())
	})
}

// Open returns the store for root. Blobs go to the S3 bucket named by S3_BUCKET
// when it is set, and to the local filesystem otherwise. The S3 settings are
// read from the environment; see S3Config.
func Open(root string) (*FS, error) {
	if os.Getenv("S3_BUCKET") == "" {
		return New(root)
	}
	return newS3FS(root, s3ConfigFromEnv())
}
