// Package storage keeps in-progress uploads on the local filesystem and stores
// finished blobs either on the local filesystem or in an S3-compatible bucket.
//
// Blobs are content-addressed and immutable: a blob is only ever created from a
// fully written and verified file, so a reader never sees a partial blob.
package storage

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/p-arndt/doreg/internal/digest"
)

// ErrNotFound is returned when a blob or upload does not exist.
var ErrNotFound = errors.New("not found")

// FS stores in-progress uploads under <root>/uploads and finished blobs in a
// blobBackend.
type FS struct {
	root    string
	backend blobBackend
	mu      sync.Mutex
	locks   map[digest.Digest]*digestLock
}

type digestLock struct {
	sync.Mutex
	refs int
}

// Lock serialises work on one digest. Upload finalisation (rename + metadata
// row) and GC deletion (row delete + unlink) both hold it, so GC can never unlink
// a file that a concurrent push just registered.
func (s *FS) Lock(d digest.Digest) (unlock func()) {
	s.mu.Lock()
	l := s.locks[d]
	if l == nil {
		l = &digestLock{}
		s.locks[d] = l
	}
	l.refs++
	s.mu.Unlock()
	l.Lock()
	return func() {
		l.Unlock()
		s.mu.Lock()
		l.refs--
		if l.refs == 0 {
			delete(s.locks, d)
		}
		s.mu.Unlock()
	}
}

// New creates a local-only store below root.
func New(root string) (*FS, error) {
	for _, dir := range []string{"blobs", "uploads", "tmp"} {
		if err := os.MkdirAll(filepath.Join(root, dir), 0o750); err != nil {
			return nil, err
		}
	}
	return newFS(root, localBackend{root: root})
}

// newFS creates the upload and temp directories below root and wraps backend.
func newFS(root string, backend blobBackend) (*FS, error) {
	for _, dir := range []string{"uploads", "tmp"} {
		if err := os.MkdirAll(filepath.Join(root, dir), 0o750); err != nil {
			return nil, err
		}
	}
	return &FS{root: root, backend: backend, locks: map[digest.Digest]*digestLock{}}, nil
}

// Root returns the data directory.
func (s *FS) Root() string { return s.root }

func (s *FS) uploadPath(id string) string {
	return filepath.Join(s.root, "uploads", id)
}

// Open returns a reader for the blob. The reader supports seeking, so it can be
// passed to http.ServeContent.
func (s *FS) Open(d digest.Digest) (io.ReadSeekCloser, error) {
	return s.backend.open(d)
}

// Stat returns the blob size.
func (s *FS) Stat(d digest.Digest) (int64, error) {
	return s.backend.stat(d)
}

// Exists reports whether the blob is present.
func (s *FS) Exists(d digest.Digest) bool {
	_, err := s.Stat(d)
	return err == nil
}

// Delete removes the blob. Deleting a missing blob is not an error.
func (s *FS) Delete(d digest.Digest) error {
	return s.backend.delete(d)
}

// PutBytes writes small content (manifests) as a blob after verifying its digest.
func (s *FS) PutBytes(d digest.Digest, data []byte) error {
	if digest.FromBytes(d.Algorithm(), data) != d {
		return digest.ErrMismatch
	}
	if s.Exists(d) {
		return nil
	}
	tmp, err := os.CreateTemp(filepath.Join(s.root, "tmp"), "put-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return s.place(tmp.Name(), d)
}

// place moves the local file src into the backend as blob d. src is consumed.
func (s *FS) place(src string, d digest.Digest) error {
	return s.backend.put(d, src)
}

// CreateUpload creates an empty upload file.
func (s *FS) CreateUpload(id string) error {
	f, err := os.OpenFile(s.uploadPath(id), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o640)
	if err != nil {
		return err
	}
	return f.Close()
}

// AppendUpload appends r to the upload, which must currently be exactly offset
// bytes long. It returns the new size.
func (s *FS) AppendUpload(id string, offset int64, r io.Reader) (int64, error) {
	f, err := os.OpenFile(s.uploadPath(id), os.O_WRONLY, 0)
	if errors.Is(err, fs.ErrNotExist) {
		return 0, ErrNotFound
	}
	if err != nil {
		return 0, err
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return 0, err
	}
	if fi.Size() != offset {
		return fi.Size(), fmt.Errorf("upload offset %d does not match size %d", offset, fi.Size())
	}
	if _, err := f.Seek(offset, io.SeekStart); err != nil {
		return 0, err
	}
	n, err := io.Copy(f, r)
	if err != nil {
		// Drop the partial chunk so the client can retry from the last good offset.
		_ = f.Truncate(offset)
		return offset, err
	}
	if err := f.Sync(); err != nil {
		return 0, err
	}
	return offset + n, nil
}

// UploadSize returns the current size of an upload.
func (s *FS) UploadSize(id string) (int64, error) {
	fi, err := os.Stat(s.uploadPath(id))
	if errors.Is(err, fs.ErrNotExist) {
		return 0, ErrNotFound
	}
	if err != nil {
		return 0, err
	}
	return fi.Size(), nil
}

// CommitUpload verifies the upload against want and moves it into the blob store.
// It returns the blob size.
func (s *FS) CommitUpload(id string, want digest.Digest) (int64, error) {
	path := s.uploadPath(id)
	f, err := os.Open(path)
	if errors.Is(err, fs.ErrNotExist) {
		return 0, ErrNotFound
	}
	if err != nil {
		return 0, err
	}
	h, err := digest.NewHasher(want.Algorithm())
	if err != nil {
		f.Close()
		return 0, err
	}
	size, err := io.Copy(h, f)
	f.Close()
	if err != nil {
		return 0, err
	}
	if h.Digest() != want {
		return 0, digest.ErrMismatch
	}
	if s.Exists(want) {
		// Identical content is already stored; keep the existing file.
		return size, os.Remove(path)
	}
	return size, s.place(path, want)
}

// DeleteUpload removes an upload file.
func (s *FS) DeleteUpload(id string) error {
	err := os.Remove(s.uploadPath(id))
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	return err
}

// WalkBlobs calls fn for every stored blob. Used by GC to find blobs the
// metadata database does not know about.
func (s *FS) WalkBlobs(fn func(d digest.Digest, size int64, mod time.Time) error) error {
	return s.backend.walk(fn)
}

// UploadIDs lists the ids of all upload files with their modification times.
func (s *FS) UploadIDs() ([]UploadFile, error) {
	entries, err := os.ReadDir(filepath.Join(s.root, "uploads"))
	if err != nil {
		return nil, err
	}
	out := make([]UploadFile, 0, len(entries))
	for _, e := range entries {
		info, err := e.Info()
		if err != nil {
			continue
		}
		out = append(out, UploadFile{ID: e.Name(), ModUnix: info.ModTime().Unix()})
	}
	return out, nil
}

// UploadFile describes an upload file on disk.
type UploadFile struct {
	ID      string
	ModUnix int64
}
