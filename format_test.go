package fs

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// --- Magic ---

func TestMagic_FirstNBytes(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "f")
	if err := os.WriteFile(path, []byte("PNG\x89... rest of file"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	got, err := Magic(path, 4)
	if err != nil {
		t.Fatalf("Magic: %v", err)
	}
	if string(got) != "PNG\x89" {
		t.Errorf("got %q, want PNG\\x89", got)
	}
}

func TestMagic_ShortFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "f")
	if err := os.WriteFile(path, []byte("ab"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	got, err := Magic(path, 16)
	if err != nil {
		t.Fatalf("Magic on short file: %v", err)
	}
	if string(got) != "ab" {
		t.Errorf("got %q, want ab (no padding when file is shorter)", got)
	}
}

func TestMagic_EmptyFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "f")
	if err := os.WriteFile(path, nil, 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	got, err := Magic(path, 4)
	if err != nil {
		t.Fatalf("Magic on empty file: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("got %v, want empty slice", got)
	}
}

func TestMagic_ZeroN(t *testing.T) {
	t.Parallel()
	got, err := Magic("/this/path/does/not/exist", 0)
	if err != nil {
		t.Errorf("Magic with n=0 should not open the file or error: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("got %v, want empty", got)
	}
}

func TestMagic_Missing(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	_, err := Magic(filepath.Join(dir, "missing"), 4)
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("got %v, want ErrNotFound", err)
	}
}

// --- ExtFormat ---

func TestExtFormat_KnownExtensions(t *testing.T) {
	t.Parallel()
	cases := []struct {
		path, want string
	}{
		{"config.json", "json"},
		{"config.yaml", "yaml"},
		{"config.yml", "yaml"},
		{"config.toml", "toml"},
		{"data.csv", "csv"},
		{"page.html", "html"},
		{"page.HTM", "html"}, // case-insensitive
		{"README.md", "markdown"},
		{"archive.tar", "tar"},
		{"archive.tar.gz", "gz"}, // last extension only
		{"archive.tgz", "gz"},
		{"archive.zip", "zip"},
	}
	for _, c := range cases {
		if got := ExtFormat(c.path); got != c.want {
			t.Errorf("ExtFormat(%q) = %q, want %q", c.path, got, c.want)
		}
	}
}

func TestExtFormat_Unknown(t *testing.T) {
	t.Parallel()
	for _, p := range []string{"file", "file.unknownext", "file.abcdef"} {
		if got := ExtFormat(p); got != "" {
			t.Errorf("ExtFormat(%q) = %q, want empty", p, got)
		}
	}
}

func TestExtFormat_NoExtension(t *testing.T) {
	t.Parallel()
	if got := ExtFormat("Makefile"); got != "" {
		t.Errorf("got %q, want empty", got)
	}
}

// --- FormatError color, nil, multi ---

func TestFormatError_Color(t *testing.T) {
	t.Parallel()
	pe := &PathError{Op: "x", Path: "/y", Cause: errors.New("boom")}
	plain := FormatError(pe, false)
	col := FormatError(pe, true)
	if plain == col {
		t.Error("colorless and colorized output should differ")
	}
	if !strings.Contains(col, "\x1b[") {
		t.Errorf("colored output missing ANSI escape: %q", col)
	}
}

func TestFormatError_NilCoverage(t *testing.T) {
	t.Parallel()
	if got := FormatError(nil); got != "" {
		t.Errorf("FormatError(nil) = %q, want empty", got)
	}
}

func TestFormatError_MultiError(t *testing.T) {
	t.Parallel()
	m := &MultiError{}
	m.Append(errors.New("a"))
	m.Append(errors.New("b"))
	got := FormatError(m, false)
	if !strings.Contains(got, "a") || !strings.Contains(got, "b") {
		t.Errorf("FormatError(multi) = %q; missing branches", got)
	}
}
