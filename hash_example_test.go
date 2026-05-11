package fs_test

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"log"
	"path/filepath"

	"github.com/go-rotini/fs"
)

// Hash streams a file through the chosen algorithm and returns the
// hex-encoded digest. The zero value of [fs.HashAlgo] is
// [fs.HashSHA256], which is the package default.
func ExampleHash() {
	tmp, cleanup, _ := fs.TempDir("", "fs-example-*")
	defer cleanup()

	path := filepath.Join(tmp, "in")
	_ = fs.WriteFile(path, []byte("abc"))

	digest, err := fs.Hash(path, fs.HashSHA256)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(digest)
	// Output: ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad
}

// HashCompare returns [fs.ErrHashMismatch] when a file's digest
// differs from the expected hex string. The comparison is
// constant-time via [crypto/subtle.ConstantTimeCompare].
func ExampleHashCompare() {
	tmp, cleanup, _ := fs.TempDir("", "fs-example-*")
	defer cleanup()

	path := filepath.Join(tmp, "in")
	_ = fs.WriteFile(path, []byte("abc"))

	// Correct digest.
	if err := fs.HashCompare(path, "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad", fs.HashSHA256); err != nil {
		log.Fatal(err)
	}
	fmt.Println("first: match")

	// Tampered digest.
	err := fs.HashCompare(path, "0000000000000000000000000000000000000000000000000000000000000000", fs.HashSHA256)
	if errors.Is(err, fs.ErrHashMismatch) {
		fmt.Println("second: mismatch")
	}
	// Output:
	// first: match
	// second: mismatch
}

// HashWriter folds a hash computation into a copy operation. Pair
// it with [io.MultiWriter] to hash bytes you're also writing
// elsewhere; handy for "download + integrity-check" flows.
func ExampleHashWriter() {
	hw := fs.HashWriter(fs.HashSHA256)

	// In real code dst is a file; io.Discard keeps the example
	// hermetic.
	if _, err := io.Copy(io.MultiWriter(io.Discard, hw), bytes.NewReader([]byte("abc"))); err != nil {
		log.Fatal(err)
	}
	fmt.Println(hw.Hex())
	// Output: ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad
}
