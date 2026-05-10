package fs

import (
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// --- Match ---

func TestMatch_Simple(t *testing.T) {
	t.Parallel()
	cases := []struct {
		pattern, name string
		want          bool
	}{
		{"*.go", "main.go", true},
		{"*.go", "main.txt", false},
		{"foo?", "fooa", true},
		{"foo?", "foo", false},
		{"[abc].txt", "a.txt", true},
		{"[abc].txt", "d.txt", false},
	}
	for _, c := range cases {
		got, err := Match(c.pattern, c.name)
		if err != nil {
			t.Errorf("Match(%q, %q): %v", c.pattern, c.name, err)
		}
		if got != c.want {
			t.Errorf("Match(%q, %q) = %v, want %v", c.pattern, c.name, got, c.want)
		}
	}
}

func TestMatch_BadPattern(t *testing.T) {
	t.Parallel()
	_, err := Match("[", "anything")
	if err == nil {
		t.Error("expected error on malformed pattern")
	}
	if !strings.Contains(err.Error(), "match") {
		t.Errorf("error %q should mention op", err)
	}
}

// --- Glob ---

func TestGlob_BasicMatches(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	for _, name := range []string{"a.go", "b.go", "c.txt"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o644); err != nil {
			t.Fatalf("setup: %v", err)
		}
	}

	got, err := Glob(filepath.Join(dir, "*.go"))
	if err != nil {
		t.Fatalf("Glob: %v", err)
	}
	sort.Strings(got)
	if len(got) != 2 {
		t.Errorf("got %d, want 2: %v", len(got), got)
	}
}

func TestGlob_NoMatchEmpty(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	got, err := Glob(filepath.Join(dir, "nothing.*"))
	if err != nil {
		t.Errorf("no-match should not error: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("got %v, want empty", got)
	}
}

func TestGlob_BadPattern(t *testing.T) {
	t.Parallel()
	_, err := Glob("[")
	if err == nil {
		t.Error("expected error on malformed pattern")
	}
}

func TestGlob_HomeExpansion(t *testing.T) {
	if os.Getenv("HOME") == "" {
		t.Skip("HOME unset")
	}
	t.Parallel()
	// Tilde-prefix should expand to HOME, then list HOME's contents
	// matching the glob. Just verify Glob doesn't error and that
	// expansion happened (the resulting paths should not start with ~).
	got, err := Glob("~/*")
	if err != nil {
		t.Fatalf("Glob: %v", err)
	}
	for _, p := range got {
		if strings.HasPrefix(p, "~") {
			t.Errorf("path starts with ~: %s (expansion failed)", p)
		}
	}
}

func TestGlob_VarExpansion(t *testing.T) {
	// No t.Parallel: t.Setenv is incompatible with parallel tests.
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "x.txt"), []byte("y"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}
	t.Setenv("ROTINI_TEST_DIR", dir)

	got, err := Glob("$ROTINI_TEST_DIR/*.txt")
	if err != nil {
		t.Fatalf("Glob: %v", err)
	}
	if len(got) != 1 {
		t.Errorf("got %d matches, want 1: %v", len(got), got)
	}
}

func TestGlob_StrictExpansionUnsetVar(t *testing.T) {
	t.Parallel()
	_, err := Glob("$ROTINI_THIS_VAR_DOES_NOT_EXIST/*", WithStrictExpansion())
	if err == nil {
		t.Error("expected error from WithStrictExpansion on unset var")
	}
}

// --- GlobAny ---

func TestGlobAny_Union(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	for _, n := range []string{"a.go", "b.go", "c.txt", "d.md"} {
		if err := os.WriteFile(filepath.Join(dir, n), []byte("x"), 0o644); err != nil {
			t.Fatalf("setup: %v", err)
		}
	}

	got, err := GlobAny([]string{
		filepath.Join(dir, "*.go"),
		filepath.Join(dir, "*.md"),
	})
	if err != nil {
		t.Fatalf("GlobAny: %v", err)
	}
	if len(got) != 3 {
		t.Errorf("got %d, want 3: %v", len(got), got)
	}
}

func TestGlobAny_Deduplicates(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.go"), []byte("x"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}

	// Both patterns match a.go; result should contain it once.
	got, err := GlobAny([]string{
		filepath.Join(dir, "*.go"),
		filepath.Join(dir, "a.*"),
	})
	if err != nil {
		t.Fatalf("GlobAny: %v", err)
	}
	if len(got) != 1 {
		t.Errorf("got %d, want 1 (deduplicated): %v", len(got), got)
	}
}

func TestGlobAny_OrderPreserved(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	for _, n := range []string{"alpha.go", "beta.txt"} {
		if err := os.WriteFile(filepath.Join(dir, n), []byte("x"), 0o644); err != nil {
			t.Fatalf("setup: %v", err)
		}
	}

	got, err := GlobAny([]string{
		filepath.Join(dir, "*.txt"),
		filepath.Join(dir, "*.go"),
	})
	if err != nil {
		t.Fatalf("GlobAny: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d, want 2", len(got))
	}
	if !strings.HasSuffix(got[0], "beta.txt") {
		t.Errorf("first result should be beta.txt (first pattern's match): %s", got[0])
	}
}

func TestGlobAny_EmptyInput(t *testing.T) {
	t.Parallel()
	got, err := GlobAny(nil)
	if err != nil {
		t.Errorf("GlobAny(nil) = %v, want nil", err)
	}
	if len(got) != 0 {
		t.Errorf("got %v, want empty", got)
	}
}

func TestGlobAny_BadPatternReturnsPartial(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.go"), []byte("x"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}

	got, err := GlobAny([]string{
		filepath.Join(dir, "*.go"),
		"[", // malformed
	})
	if err == nil {
		t.Error("expected error from malformed pattern")
	}
	if !errors.Is(err, &PathError{}) && !strings.Contains(err.Error(), "glob") {
		// At least sanity-check the error is recognizable.
		t.Logf("error: %v", err)
	}
	// Partial result should include the first pattern's match.
	if len(got) != 1 {
		t.Errorf("partial result = %v, want one entry from first pattern", got)
	}
}

// --- Glob expand error paths ---

func TestGlob_BadExpand(t *testing.T) {
	t.Parallel()
	_, err := Glob("$ROTINI_NEVER_SET_AT_ALL/*", WithStrictExpansion())
	if err == nil {
		t.Error("expected strict-expansion error")
	}
}

func TestGlobAny_PartialBadOnFirstPattern(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.go"), nil, 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}
	got, err := GlobAny([]string{"["}, WithStrictExpansion())
	if err == nil {
		t.Error("expected error from malformed pattern")
	}
	if len(got) != 0 {
		t.Errorf("partial = %v, want empty", got)
	}
}
