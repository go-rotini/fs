package fs

import (
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"testing"
)

func TestFindByContent_Substring(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "a.txt"), "hello\nworld\n")
	mustWrite(t, filepath.Join(root, "b.txt"), "world\nfoo\n")
	mustWrite(t, filepath.Join(root, "c.txt"), "nothing relevant\n")

	matches, err := FindByContent(root, "world")
	if err != nil {
		t.Fatalf("FindByContent: %v", err)
	}
	if len(matches) != 2 {
		t.Errorf("matches = %d; want 2: %+v", len(matches), matches)
	}
	for _, m := range matches {
		if m.Text != "world" {
			t.Errorf("Text = %q; want world", m.Text)
		}
		if m.Line <= 0 {
			t.Errorf("Line = %d; want > 0", m.Line)
		}
	}
}

func TestFindByContent_EmptySubstrRejected(t *testing.T) {
	t.Parallel()
	if _, err := FindByContent(t.TempDir(), ""); !errors.Is(err, ErrInvalidPath) {
		t.Errorf("err = %v; want ErrInvalidPath", err)
	}
}

func TestFindByContentRegex(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "code.go"), "package foo\nfunc Hello() {}\nfunc World() {}\n")

	re := regexp.MustCompile(`^func [A-Z]\w+\(\)`)
	matches, err := FindByContentRegex(root, re)
	if err != nil {
		t.Fatalf("FindByContentRegex: %v", err)
	}
	if len(matches) != 2 {
		t.Errorf("matches = %d; want 2: %+v", len(matches), matches)
	}
}

func TestFindByContent_BinaryFilesSkipped(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	// File with embedded NUL byte should be treated as binary and
	// skipped.
	binPath := filepath.Join(root, "binary.bin")
	if err := os.WriteFile(binPath, []byte("foo\x00bar"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	mustWrite(t, filepath.Join(root, "text.txt"), "foo\n")

	matches, err := FindByContent(root, "foo")
	if err != nil {
		t.Fatalf("FindByContent: %v", err)
	}
	for _, m := range matches {
		if m.Path == binPath {
			t.Error("binary file should have been skipped")
		}
	}
}
