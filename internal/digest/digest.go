// Package digest parses and computes OCI content digests.
package digest

import (
	"crypto/sha256"
	"crypto/sha512"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"strings"
)

// Digest is a content digest such as "sha256:abc…".
type Digest string

var (
	// ErrInvalid is returned for malformed digests.
	ErrInvalid = errors.New("invalid digest")
	// ErrMismatch is returned when content does not hash to the expected digest.
	ErrMismatch = errors.New("digest mismatch")
)

var hexLen = map[string]int{"sha256": 64, "sha512": 128}

// Parse validates s and returns it as a Digest.
func Parse(s string) (Digest, error) {
	alg, h, ok := strings.Cut(s, ":")
	if !ok {
		return "", fmt.Errorf("%w: %q", ErrInvalid, s)
	}
	n, known := hexLen[alg]
	if !known || len(h) != n {
		return "", fmt.Errorf("%w: %q", ErrInvalid, s)
	}
	for _, c := range h {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return "", fmt.Errorf("%w: %q", ErrInvalid, s)
		}
	}
	return Digest(s), nil
}

// Algorithm returns the part before the colon.
func (d Digest) Algorithm() string {
	alg, _, _ := strings.Cut(string(d), ":")
	return alg
}

// Hex returns the part after the colon.
func (d Digest) Hex() string {
	_, h, _ := strings.Cut(string(d), ":")
	return h
}

func (d Digest) String() string { return string(d) }

// Hasher computes a digest incrementally.
type Hasher struct {
	hash.Hash
	alg string
}

// NewHasher returns a hasher for the given algorithm.
func NewHasher(alg string) (*Hasher, error) {
	switch alg {
	case "sha256":
		return &Hasher{sha256.New(), alg}, nil
	case "sha512":
		return &Hasher{sha512.New(), alg}, nil
	}
	return nil, fmt.Errorf("%w: unsupported algorithm %q", ErrInvalid, alg)
}

// Digest returns the digest of everything written so far.
func (h *Hasher) Digest() Digest {
	return Digest(h.alg + ":" + hex.EncodeToString(h.Sum(nil)))
}

// FromBytes hashes data with the given algorithm (sha256 if unknown).
func FromBytes(alg string, data []byte) Digest {
	h, err := NewHasher(alg)
	if err != nil {
		h, _ = NewHasher("sha256")
	}
	h.Write(data)
	return h.Digest()
}
