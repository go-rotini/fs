//go:build windows

package fs

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLongPath_AddsPrefix(t *testing.T) {
	t.Parallel()
	long := `C:\` + strings.Repeat("a", 300)
	got, err := LongPath(long)
	if err != nil {
		t.Fatalf("LongPath: %v", err)
	}
	if !strings.HasPrefix(got, `\\?\`) {
		t.Errorf("expected \\?\\ prefix; got %q", got)
	}
}

func TestLongPath_AlreadyPrefixed(t *testing.T) {
	t.Parallel()
	in := `\\?\C:\` + strings.Repeat("a", 300)
	got, err := LongPath(in)
	if err != nil {
		t.Fatalf("LongPath: %v", err)
	}
	if got != in {
		t.Errorf("already-prefixed path was modified: %q", got)
	}
}

func TestLongPath_UNC(t *testing.T) {
	t.Parallel()
	in := `\\server\share\` + strings.Repeat("a", 300)
	got, err := LongPath(in)
	if err != nil {
		t.Fatalf("LongPath: %v", err)
	}
	if !strings.HasPrefix(got, `\\?\UNC\`) {
		t.Errorf("UNC long path missing \\?\\UNC\\ prefix: %q", got)
	}
}

// Locks and no-follow opens work on paths past MAX_PATH, as os.OpenFile does.
func TestLock_LongPath(t *testing.T) {
	t.Parallel()
	dir := filepath.Join(t.TempDir(), strings.Repeat("d", 100), strings.Repeat("e", 100), strings.Repeat("f", 100))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "app.lock")
	if len(path) < maxPath {
		t.Fatalf("test path is only %d characters", len(path))
	}
	h, err := Lock(path)
	if err != nil {
		t.Fatalf("Lock: %v", err)
	}
	if err := h.Release(); err != nil {
		t.Fatalf("Release: %v", err)
	}
	f, err := OpenNoFollow(path, os.O_RDONLY, 0)
	if err != nil {
		t.Fatalf("OpenNoFollow: %v", err)
	}
	_ = f.Close()
}

// A short relative path is measured with the working directory it is relative to, so a lock
// beside a deep working directory still gets the long-path prefix.
func TestLongPath_RelativeUnderDeepCwd(t *testing.T) {
	base := t.TempDir()
	if len(base) > 200 {
		t.Skipf("temp dir is already %d characters", len(base))
	}
	dir := filepath.Join(base, strings.Repeat("d", 200-len(base)))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)
	rel := strings.Repeat("r", maxPath-len(dir)) + ".lock"
	got, err := LongPath(rel)
	if err != nil {
		t.Fatalf("LongPath: %v", err)
	}
	if want := `\\?\` + filepath.Join(dir, rel); got != want {
		t.Errorf("LongPath(%q) = %q, want %q", rel, got, want)
	}
	h, err := Lock(rel)
	if err != nil {
		t.Fatalf("Lock: %v", err)
	}
	if err := h.Release(); err != nil {
		t.Fatalf("Release: %v", err)
	}
	f, err := OpenNoFollow(rel, os.O_RDONLY, 0)
	if err != nil {
		t.Fatalf("OpenNoFollow: %v", err)
	}
	_ = f.Close()
}

func TestLongPath_ShortRelativeUnchanged(t *testing.T) {
	t.Parallel()
	if got, err := LongPath("app.lock"); err != nil || got != "app.lock" {
		t.Errorf(`LongPath("app.lock") = %q, %v; want it unchanged`, got, err)
	}
}
