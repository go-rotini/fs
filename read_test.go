package fs

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// --- ReadFile ---

func TestReadFile_Basic(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "f")
	if err := os.WriteFile(path, []byte("hello"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	got, err := ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(got) != "hello" {
		t.Errorf("got %q, want hello", got)
	}
}

func TestReadFile_Missing(t *testing.T) {
	t.Parallel()
	_, err := ReadFile(filepath.Join(t.TempDir(), "missing"))
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("got %v, want ErrNotFound", err)
	}
}

func TestReadFile_OverDefaultCap(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "big")
	// Use a deliberately small per-test cap rather than writing a
	// 100 MiB fixture file.
	if err := os.WriteFile(path, bytes.Repeat([]byte("x"), 1024), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	_, err := ReadFile(path, WithMaxSize(512))
	if !errors.Is(err, ErrFileTooLarge) {
		t.Errorf("got %v, want ErrFileTooLarge", err)
	}
}

func TestReadFile_AtCap_OK(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "f")
	data := bytes.Repeat([]byte("x"), 512)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	got, err := ReadFile(path, WithMaxSize(512))
	if err != nil {
		t.Fatalf("ReadFile at cap: %v", err)
	}
	if len(got) != 512 {
		t.Errorf("len = %d, want 512", len(got))
	}
}

func TestReadFile_Unbounded(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "f")
	data := bytes.Repeat([]byte("y"), 8192)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	got, err := ReadFile(path, WithMaxSize(0))
	if err != nil {
		t.Fatalf("ReadFile unbounded: %v", err)
	}
	if !bytes.Equal(got, data) {
		t.Errorf("contents differ; len got=%d want=%d", len(got), len(data))
	}
}

func TestReadFile_NegativeMaxSizeClampedToZero(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "f")
	if err := os.WriteFile(path, []byte("data"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	got, err := ReadFile(path, WithMaxSize(-1))
	if err != nil || string(got) != "data" {
		t.Errorf("got (%q, %v); negative maxSize should be unbounded", got, err)
	}
}

// TestReadFile_FIFOEnforced lives in read_unix_test.go (FIFOs are
// POSIX-only and the test creates one via syscall.Mkfifo).

func TestReadFileMax(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "f")
	if err := os.WriteFile(path, []byte("hello"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	got, err := ReadFileMax(path, 1024)
	if err != nil || string(got) != "hello" {
		t.Errorf("got (%q, %v)", got, err)
	}
	_, err = ReadFileMax(path, 2)
	if !errors.Is(err, ErrFileTooLarge) {
		t.Errorf("got %v, want ErrFileTooLarge", err)
	}
}

func TestReadFile_WithExpand(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "f")
	if err := os.WriteFile(path, []byte("expanded"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	t.Setenv("FS_TEST_DIR", dir)
	got, err := ReadFile("$FS_TEST_DIR/f", WithExpand())
	if err != nil {
		t.Fatalf("ReadFile expand: %v", err)
	}
	if string(got) != "expanded" {
		t.Errorf("got %q", got)
	}
}

// --- ReadLines ---

func TestReadLines_LF(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "f")
	if err := os.WriteFile(path, []byte("a\nb\nc\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	got, err := ReadLines(path)
	if err != nil {
		t.Fatalf("ReadLines: %v", err)
	}
	want := []string{"a", "b", "c"}
	if !equalStrings(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestReadLines_CRLF(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "f")
	if err := os.WriteFile(path, []byte("a\r\nb\r\nc\r\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	got, err := ReadLines(path)
	if err != nil {
		t.Fatalf("ReadLines: %v", err)
	}
	want := []string{"a", "b", "c"}
	if !equalStrings(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestReadLines_BareCR(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "f")
	if err := os.WriteFile(path, []byte("a\rb\rc"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	got, err := ReadLines(path)
	if err != nil {
		t.Fatalf("ReadLines: %v", err)
	}
	want := []string{"a", "b", "c"}
	if !equalStrings(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestReadLines_StripBOM(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "f")
	in := append([]byte{0xEF, 0xBB, 0xBF}, []byte("hello\nworld\n")...)
	if err := os.WriteFile(path, in, 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	got, err := ReadLines(path)
	if err != nil {
		t.Fatalf("ReadLines: %v", err)
	}
	if len(got) != 2 || got[0] != "hello" || got[1] != "world" {
		t.Errorf("got %v; BOM not stripped", got)
	}
}

func TestReadLines_TrailingNoNewline(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "f")
	if err := os.WriteFile(path, []byte("a\nb"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	got, err := ReadLines(path)
	if err != nil {
		t.Fatalf("ReadLines: %v", err)
	}
	want := []string{"a", "b"}
	if !equalStrings(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestReadLines_Empty(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "f")
	if err := os.WriteFile(path, nil, 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	got, err := ReadLines(path)
	if err != nil {
		t.Fatalf("ReadLines: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("got %v, want []", got)
	}
}

// --- ReadFirstLine ---

func TestReadFirstLine_Basic(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "f")
	if err := os.WriteFile(path, []byte("first\nsecond\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	got, err := ReadFirstLine(path)
	if err != nil || got != "first" {
		t.Errorf("got (%q, %v)", got, err)
	}
}

func TestReadFirstLine_NoTerminator(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "f")
	if err := os.WriteFile(path, []byte("only line"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	got, err := ReadFirstLine(path)
	if err != nil || got != "only line" {
		t.Errorf("got (%q, %v)", got, err)
	}
}

func TestReadFirstLine_CRLF(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "f")
	if err := os.WriteFile(path, []byte("first\r\nsecond\r\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	got, err := ReadFirstLine(path)
	if err != nil || got != "first" {
		t.Errorf("got (%q, %v); CRLF should strip both", got, err)
	}
}

func TestReadFirstLine_Empty(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "f")
	if err := os.WriteFile(path, nil, 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	_, err := ReadFirstLine(path)
	if !errors.Is(err, ErrEmptyFile) {
		t.Errorf("got %v, want ErrEmptyFile", err)
	}
}

func TestReadFirstLine_StripBOM(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "f")
	in := append([]byte{0xEF, 0xBB, 0xBF}, []byte("hello\n")...)
	if err := os.WriteFile(path, in, 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	got, err := ReadFirstLine(path)
	if err != nil || got != "hello" {
		t.Errorf("got (%q, %v); BOM not stripped", got, err)
	}
}

// --- OpenLines ---

func TestOpenLines_Basic(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "f")
	if err := os.WriteFile(path, []byte("one\ntwo\nthree\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	seq, closeFn, err := OpenLines(path)
	if err != nil {
		t.Fatalf("OpenLines: %v", err)
	}
	defer func() { _ = closeFn() }()

	var got []string
	for line := range seq {
		got = append(got, line)
	}
	want := []string{"one", "two", "three"}
	if !equalStrings(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestOpenLines_EarlyBreak(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "f")
	if err := os.WriteFile(path, []byte("a\nb\nc\nd\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	seq, closeFn, err := OpenLines(path)
	if err != nil {
		t.Fatalf("OpenLines: %v", err)
	}
	defer func() { _ = closeFn() }()

	count := 0
	for line := range seq {
		count++
		if line == "b" {
			break
		}
	}
	if count != 2 {
		t.Errorf("count = %d, want 2 (early break)", count)
	}
}

func TestOpenLines_BOMStripped(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "f")
	in := append([]byte{0xEF, 0xBB, 0xBF}, []byte("a\nb\n")...)
	if err := os.WriteFile(path, in, 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	seq, closeFn, err := OpenLines(path)
	if err != nil {
		t.Fatalf("OpenLines: %v", err)
	}
	defer func() { _ = closeFn() }()

	var got []string
	for line := range seq {
		got = append(got, line)
	}
	if len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Errorf("got %v", got)
	}
}

func TestOpenLines_Missing(t *testing.T) {
	t.Parallel()
	_, _, err := OpenLines(filepath.Join(t.TempDir(), "missing"))
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("got %v, want ErrNotFound", err)
	}
}

// --- OpenChunked ---

func TestOpenChunked_Basic(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "f")
	data := bytes.Repeat([]byte("abc"), 100) // 300 bytes
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	seq, closeFn, err := OpenChunked(path, 64)
	if err != nil {
		t.Fatalf("OpenChunked: %v", err)
	}
	defer func() { _ = closeFn() }()

	var assembled []byte
	for chunk, ierr := range seq {
		if ierr != nil {
			t.Fatalf("iter err: %v", ierr)
		}
		assembled = append(assembled, chunk...)
	}
	if !bytes.Equal(assembled, data) {
		t.Errorf("assembled differs; len got=%d want=%d", len(assembled), len(data))
	}
}

func TestOpenChunked_LastChunkPartial(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "f")
	// 70 bytes against 32-byte chunks: expect 32, 32, 6.
	data := bytes.Repeat([]byte("a"), 70)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	seq, closeFn, err := OpenChunked(path, 32)
	if err != nil {
		t.Fatalf("OpenChunked: %v", err)
	}
	defer func() { _ = closeFn() }()

	var sizes []int
	for chunk, ierr := range seq {
		if ierr != nil {
			t.Fatalf("iter err: %v", ierr)
		}
		sizes = append(sizes, len(chunk))
	}
	want := []int{32, 32, 6}
	if len(sizes) != len(want) {
		t.Fatalf("got %v, want %v", sizes, want)
	}
	for i := range sizes {
		if sizes[i] != want[i] {
			t.Errorf("chunk %d size = %d, want %d", i, sizes[i], want[i])
		}
	}
}

func TestOpenChunked_EarlyBreak(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "f")
	data := bytes.Repeat([]byte("x"), 1024)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	seq, closeFn, err := OpenChunked(path, 32)
	if err != nil {
		t.Fatalf("OpenChunked: %v", err)
	}
	defer func() { _ = closeFn() }()

	count := 0
	for range seq {
		count++
		if count == 2 {
			break
		}
	}
	if count != 2 {
		t.Errorf("count = %d, want 2", count)
	}
}

func TestOpenChunked_DefaultSize(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "f")
	if err := os.WriteFile(path, []byte("short"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	seq, closeFn, err := OpenChunked(path, 0) // → default 64 KiB
	if err != nil {
		t.Fatalf("OpenChunked: %v", err)
	}
	defer func() { _ = closeFn() }()
	var assembled []byte
	for chunk := range seq {
		assembled = append(assembled, chunk...)
	}
	if string(assembled) != "short" {
		t.Errorf("got %q", assembled)
	}
}

// --- ReadAt ---

func TestReadAt_Basic(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "f")
	if err := os.WriteFile(path, []byte("0123456789"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	got, err := ReadAt(path, 3, 4)
	if err != nil {
		t.Fatalf("ReadAt: %v", err)
	}
	if string(got) != "3456" {
		t.Errorf("got %q, want 3456", got)
	}
}

func TestReadAt_PastEOF_Strict(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "f")
	if err := os.WriteFile(path, []byte("abc"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	_, err := ReadAt(path, 0, 100)
	if !errors.Is(err, ErrShortRead) {
		t.Errorf("got %v, want ErrShortRead", err)
	}
}

func TestReadAt_PastEOF_AllowShort(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "f")
	if err := os.WriteFile(path, []byte("abc"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	got, err := ReadAt(path, 0, 100, WithAllowShort(true))
	if err != nil {
		t.Fatalf("ReadAt: %v", err)
	}
	if string(got) != "abc" {
		t.Errorf("got %q, want abc", got)
	}
}

func TestReadAt_Missing(t *testing.T) {
	t.Parallel()
	_, err := ReadAt(filepath.Join(t.TempDir(), "missing"), 0, 1)
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("got %v, want ErrNotFound", err)
	}
}

func TestReadAt_NegativeN(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "f")
	if err := os.WriteFile(path, []byte("abc"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	_, err := ReadAt(path, 0, -1)
	if !errors.Is(err, ErrInvalidPath) {
		t.Errorf("got %v, want ErrInvalidPath", err)
	}
}

// --- Helpers ---

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// --- splitLines (internal helper) ---

func TestSplitLines(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in   string
		want []string
	}{
		{"", []string{}},
		{"a", []string{"a"}},
		{"a\n", []string{"a"}},
		{"a\nb", []string{"a", "b"}},
		{"a\nb\n", []string{"a", "b"}},
		{"a\r\nb\r\n", []string{"a", "b"}},
		{"a\rb\rc", []string{"a", "b", "c"}},
		{"\n\n", []string{"", ""}},
	}
	for _, c := range cases {
		got := splitLines([]byte(c.in))
		if !equalStrings(got, c.want) {
			t.Errorf("splitLines(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

// DefaultMaxReadSize sanity check.

func TestDefaultMaxReadSize(t *testing.T) {
	t.Parallel()
	if DefaultMaxReadSize != 100<<20 {
		t.Errorf("DefaultMaxReadSize = %d, want 100 MiB", DefaultMaxReadSize)
	}
}
