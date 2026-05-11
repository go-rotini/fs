package fs

// MD5 and SHA-1 are exposed for non-security-sensitive uses
// (legacy compatibility, content-addressed caches, file-integrity
// checks against published digests). Both are documented as broken
// for security purposes; gosec G501/G505/G401 are silenced because
// the package-level documentation is the appropriate warning.

//nolint:gosec // MD5 / SHA-1 supported for non-security uses; documented as broken
import (
	"crypto/md5"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/sha512"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"hash"
	"io"
	"os"
)

// HashAlgo selects the hash function used by [Hash], [HashCompare],
// and [HashWriter]. The zero value is [HashSHA256], the package
// default.
type HashAlgo int

const (
	// HashSHA256 selects SHA-256 (default).
	HashSHA256 HashAlgo = iota
	// HashSHA512 selects SHA-512.
	HashSHA512
	// HashSHA1 selects SHA-1. Cryptographically broken; use only for
	// non-security-sensitive applications (legacy compatibility,
	// content-addressed caches, etc.).
	HashSHA1
	// HashMD5 selects MD5. Cryptographically broken; use only for
	// non-security-sensitive applications.
	HashMD5
)

// String returns a lowercase canonical name for algo, e.g., "sha256".
func (a HashAlgo) String() string {
	switch a {
	case HashSHA256:
		return "sha256"
	case HashSHA512:
		return "sha512"
	case HashSHA1:
		return "sha1"
	case HashMD5:
		return "md5"
	default:
		return unknownLabel
	}
}

func newHasher(algo HashAlgo) hash.Hash {
	switch algo {
	case HashSHA512:
		return sha512.New()
	case HashSHA1:
		return sha1.New() //nolint:gosec // documented as non-security
	case HashMD5:
		return md5.New() //nolint:gosec // documented as non-security
	default: // HashSHA256 + any unknown value
		return sha256.New()
	}
}

const (
	opHash        = "hash"
	opHashCompare = "hashcompare"
)

// Hash streams path through algo and returns the hex-encoded digest.
// Empty files hash to algo's zero-length digest (e.g., SHA-256:
// `e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855`).
func Hash(path string, algo HashAlgo) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", wrapPathError(opHash, path, err)
	}
	defer f.Close()

	h := newHasher(algo)
	if _, err := io.Copy(h, f); err != nil {
		return "", wrapPathError(opHash, path, err)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// HashCompare returns nil if path's hash matches expected (hex).
// Comparison is constant-time via [crypto/subtle.ConstantTimeCompare]
// ; protects integrity-check call sites against timing oracles.
// Returns [ErrHashMismatch] on mismatch.
//
// expected may be upper or lower case; case is normalized by the
// hex-decoding step. A malformed expected string returns a wrapped
// hex-decode error.
func HashCompare(path, expected string, algo HashAlgo) error {
	got, err := Hash(path, algo)
	if err != nil {
		return err
	}

	expectedBytes, err := hex.DecodeString(expected)
	if err != nil {
		return wrapPathError(opHashCompare, path, fmt.Errorf("invalid expected hex: %w", err))
	}
	gotBytes, err := hex.DecodeString(got)
	if err != nil {
		// Defensive: hex we just produced should always decode.
		return wrapPathError(opHashCompare, path, err)
	}

	if subtle.ConstantTimeCompare(gotBytes, expectedBytes) != 1 {
		return wrapPathError(opHashCompare, path, ErrHashMismatch)
	}
	return nil
}

// HashingWriter is an [io.Writer] that computes a running hash of
// every byte written through it. [HashingWriter.Hex] returns the
// hex digest of what's been written so far; [HashingWriter.Reset]
// clears the running state.
//
// The type is concrete rather than an interface because there is
// exactly one implementation; callers who want abstraction can
// declare a one-method interface at their use site (the Go-idiomatic
// "accept interfaces, return concrete types" pattern).
type HashingWriter struct {
	h hash.Hash
}

// HashWriter returns a fresh [*HashingWriter] for algo. The typical
// use is to fold a hash computation into a copy operation:
//
//	h := fs.HashWriter(fs.HashSHA256)
//	if _, err := io.Copy(io.MultiWriter(dst, h), src); err != nil { ... }
//	digest := h.Hex()
func HashWriter(algo HashAlgo) *HashingWriter {
	return &HashingWriter{h: newHasher(algo)}
}

// Write writes p into the underlying hash. The error return is for
// [io.Writer] conformance only; [hash.Hash.Write] never errors.
func (w *HashingWriter) Write(p []byte) (int, error) {
	return w.h.Write(p)
}

// Hex returns the hex-encoded digest of every byte written since
// the last [HashingWriter.Reset] (or since construction). Named Hex
// rather than Sum so it doesn't shadow [hash.Hash.Sum], which has a
// different signature and returns bytes rather than a hex string.
func (w *HashingWriter) Hex() string { return hex.EncodeToString(w.h.Sum(nil)) }

// Reset clears the running state.
func (w *HashingWriter) Reset() { w.h.Reset() }
