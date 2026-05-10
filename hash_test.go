package fs

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Known digests for the input "abc" (NIST/RFC test vectors).
const (
	abcSHA256 = "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad"
	abcSHA512 = "ddaf35a193617abacc417349ae20413112e6fa4e89a97ea20a9eeee64b55d39a2192992a274fc1a836ba3c23a3feebbd454d4423643ce80e2a9ac94fa54ca49f"
	abcSHA1   = "a9993e364706816aba3e25717850c26c9cd0d89d"
	abcMD5    = "900150983cd24fb0d6963f7d28e17f72"

	// Empty input.
	emptySHA256 = "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
	emptyMD5    = "d41d8cd98f00b204e9800998ecf8427e"
)

func writeFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	return p
}

// --- Hash ---

func TestHash_RFCVectors(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := writeFile(t, dir, "abc.txt", "abc")

	cases := []struct {
		algo HashAlgo
		want string
	}{
		{HashSHA256, abcSHA256},
		{HashSHA512, abcSHA512},
		{HashSHA1, abcSHA1},
		{HashMD5, abcMD5},
	}
	for _, c := range cases {
		got, err := Hash(path, c.algo)
		if err != nil {
			t.Errorf("Hash(%s): %v", c.algo, err)
			continue
		}
		if got != c.want {
			t.Errorf("%s: got %s, want %s", c.algo, got, c.want)
		}
	}
}

func TestHash_EmptyFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := writeFile(t, dir, "empty", "")

	got, err := Hash(path, HashSHA256)
	if err != nil {
		t.Fatalf("Hash: %v", err)
	}
	if got != emptySHA256 {
		t.Errorf("empty SHA256 = %s, want %s", got, emptySHA256)
	}
}

func TestHash_DefaultAlgo(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := writeFile(t, dir, "abc", "abc")

	// Zero value of HashAlgo is HashSHA256.
	var algo HashAlgo
	got, err := Hash(path, algo)
	if err != nil {
		t.Fatalf("Hash: %v", err)
	}
	if got != abcSHA256 {
		t.Errorf("default algo SHA256 mismatch: got %s", got)
	}
}

func TestHash_MissingFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	_, err := Hash(filepath.Join(dir, "missing"), HashSHA256)
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("got %v, want ErrNotFound", err)
	}
}

func TestHash_LargeFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	// 1MB of zero bytes — verifies streaming works (single read won't
	// suffice in all configurations; io.Copy chunks).
	path := filepath.Join(dir, "big")
	if err := os.WriteFile(path, make([]byte, 1<<20), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	got, err := Hash(path, HashSHA256)
	if err != nil {
		t.Fatalf("Hash: %v", err)
	}
	// SHA-256 of 1MB of zero bytes — known constant.
	const want = "30e14955ebf1352266dc2ff8067e68104607e750abb9d3b36582b8af909fcb58"
	if got != want {
		t.Errorf("got %s, want %s", got, want)
	}
}

// --- HashCompare ---

func TestHashCompare_Match(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := writeFile(t, dir, "abc", "abc")

	if err := HashCompare(path, abcSHA256, HashSHA256); err != nil {
		t.Errorf("expected match, got %v", err)
	}
}

func TestHashCompare_MatchUppercase(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := writeFile(t, dir, "abc", "abc")

	if err := HashCompare(path, strings.ToUpper(abcSHA256), HashSHA256); err != nil {
		t.Errorf("uppercase hex should still match: %v", err)
	}
}

func TestHashCompare_Mismatch(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := writeFile(t, dir, "abc", "abc")

	wrong := strings.Repeat("0", 64) // valid hex, wrong digest
	err := HashCompare(path, wrong, HashSHA256)
	if !errors.Is(err, ErrHashMismatch) {
		t.Errorf("got %v, want ErrHashMismatch", err)
	}
}

func TestHashCompare_BadHex(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := writeFile(t, dir, "abc", "abc")

	err := HashCompare(path, "not-hex!!", HashSHA256)
	if err == nil {
		t.Fatal("expected error for malformed hex")
	}
	if errors.Is(err, ErrHashMismatch) {
		t.Errorf("malformed hex should not surface as ErrHashMismatch: %v", err)
	}
}

// --- HashWriter ---

func TestHashWriter_StreamMatchesHash(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := writeFile(t, dir, "abc", "abc")

	hw := HashWriter(HashSHA256)
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer f.Close()
	if _, err := io.Copy(hw, f); err != nil {
		t.Fatalf("Copy: %v", err)
	}
	if hw.Sum() != abcSHA256 {
		t.Errorf("stream sum = %s, want %s", hw.Sum(), abcSHA256)
	}
}

func TestHashWriter_TeeWithMultiWriter(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	src := writeFile(t, dir, "abc", "abc")
	dst := filepath.Join(dir, "copy")

	hw := HashWriter(HashSHA256)
	sf, err := os.Open(src)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer sf.Close()
	df, err := os.Create(dst)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	defer df.Close()

	if _, err := io.Copy(io.MultiWriter(df, hw), sf); err != nil {
		t.Fatalf("Copy: %v", err)
	}
	if err := df.Sync(); err != nil {
		t.Fatalf("Sync: %v", err)
	}

	// Hash captured during the copy must match a re-hash of the dst.
	want, err := Hash(dst, HashSHA256)
	if err != nil {
		t.Fatalf("Hash: %v", err)
	}
	if hw.Sum() != want {
		t.Errorf("streaming sum diverged from on-disk: %s vs %s", hw.Sum(), want)
	}
}

func TestHashWriter_Reset(t *testing.T) {
	t.Parallel()
	hw := HashWriter(HashMD5)
	if _, err := hw.Write([]byte("garbage")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	hw.Reset()
	// After reset, an empty hash should produce algo's empty digest.
	if hw.Sum() != emptyMD5 {
		t.Errorf("after Reset, Sum = %s, want %s", hw.Sum(), emptyMD5)
	}
	if _, err := hw.Write([]byte("abc")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if hw.Sum() != abcMD5 {
		t.Errorf("after Reset+abc, Sum = %s, want %s", hw.Sum(), abcMD5)
	}
}

// --- HashAlgo.String ---

func TestHashAlgo_String(t *testing.T) {
	t.Parallel()
	cases := []struct {
		a    HashAlgo
		want string
	}{
		{HashSHA256, "sha256"},
		{HashSHA512, "sha512"},
		{HashSHA1, "sha1"},
		{HashMD5, "md5"},
	}
	for _, c := range cases {
		if got := c.a.String(); got != c.want {
			t.Errorf("got %s, want %s", got, c.want)
		}
	}
}
