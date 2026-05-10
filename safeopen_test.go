package fs

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// --- OpenNoFollow ---

func TestOpenNoFollow_RegularFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "f")
	if err := os.WriteFile(path, []byte("hello"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	f, err := OpenNoFollow(path, os.O_RDONLY, 0)
	if err != nil {
		t.Fatalf("OpenNoFollow: %v", err)
	}
	defer f.Close()

	got, err := io.ReadAll(f)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if string(got) != "hello" {
		t.Errorf("got %q, want hello", got)
	}
}

func TestOpenNoFollow_RefusesSymlink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation typically requires elevation on Windows")
	}
	t.Parallel()
	dir := t.TempDir()
	target := filepath.Join(dir, "real")
	if err := os.WriteFile(target, []byte("x"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	link := filepath.Join(dir, "lnk")
	if err := os.Symlink(target, link); err != nil {
		t.Fatalf("Symlink: %v", err)
	}

	_, err := OpenNoFollow(link, os.O_RDONLY, 0)
	if !errors.Is(err, ErrSymlinkLoop) {
		t.Errorf("got %v, want ErrSymlinkLoop", err)
	}
}

func TestOpenNoFollow_FollowsIntermediateSymlink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation typically requires elevation on Windows")
	}
	t.Parallel()
	dir := t.TempDir()
	realDir := filepath.Join(dir, "realdir")
	if err := os.Mkdir(realDir, 0o755); err != nil {
		t.Fatalf("Mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(realDir, "f"), []byte("ok"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	linkDir := filepath.Join(dir, "linkdir")
	if err := os.Symlink(realDir, linkDir); err != nil {
		t.Fatalf("Symlink: %v", err)
	}

	// linkdir/f: linkdir is a symlink (intermediate) but f itself is
	// a regular file. OpenNoFollow must succeed (only the FINAL
	// component is checked).
	f, err := OpenNoFollow(filepath.Join(linkDir, "f"), os.O_RDONLY, 0)
	if err != nil {
		t.Fatalf("OpenNoFollow: %v", err)
	}
	_ = f.Close()
}

func TestOpenNoFollow_Missing(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	_, err := OpenNoFollow(filepath.Join(dir, "missing"), os.O_RDONLY, 0)
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("got %v, want ErrNotFound", err)
	}
}

// --- OpenAt ---

func TestOpenAt_BasicRead(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "f"), []byte("payload"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	dirF, err := os.Open(dir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer dirF.Close()

	f, err := OpenAt(dirF, "f", os.O_RDONLY, 0)
	if err != nil {
		t.Fatalf("OpenAt: %v", err)
	}
	defer f.Close()

	got, err := io.ReadAll(f)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if string(got) != "payload" {
		t.Errorf("got %q, want payload", got)
	}
}

func TestOpenAt_NilDir(t *testing.T) {
	t.Parallel()
	_, err := OpenAt(nil, "f", os.O_RDONLY, 0)
	if !errors.Is(err, ErrInvalidPath) {
		t.Errorf("got %v, want ErrInvalidPath", err)
	}
}

func TestOpenAt_MissingChild(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	dirF, err := os.Open(dir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer dirF.Close()

	_, err = OpenAt(dirF, "missing", os.O_RDONLY, 0)
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("got %v, want ErrNotFound", err)
	}
}

// TestOpenAt_HoldsInodeAfterRename verifies the POSIX guarantee: an
// open dir FD continues to refer to the original inode even after
// the directory's pathname changes. This is the primary
// race-resistance property OpenAt provides over Join + OpenFile —
// an attacker that renames the parent between calls cannot redirect
// the open. On Windows, OpenAt is a Join + OpenFile fallback and
// this test is skipped.
func TestOpenAt_HoldsInodeAfterRename(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("OpenAt not race-safe on Windows; behavior not guaranteed")
	}
	t.Parallel()
	root := t.TempDir()
	original := filepath.Join(root, "original")
	renamed := filepath.Join(root, "renamed")
	if err := os.MkdirAll(original, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(original, "f"), []byte("from-original"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	dirF, err := os.Open(original)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer dirF.Close()

	// Rename the directory's path and create a new directory at the
	// old path. dirF still points at the original inode.
	if err := os.Rename(original, renamed); err != nil {
		t.Fatalf("Rename: %v", err)
	}
	if err := os.MkdirAll(original, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(original, "f"), []byte("from-impostor"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	// OpenAt against the held FD must resolve through the original
	// inode (now reachable via "renamed/"), not the impostor at the
	// old path.
	f, err := OpenAt(dirF, "f", os.O_RDONLY, 0)
	if err != nil {
		t.Fatalf("OpenAt: %v", err)
	}
	defer f.Close()
	got, _ := io.ReadAll(f)
	if string(got) != "from-original" {
		t.Errorf("OpenAt resolved via the path rather than the held FD: got %q", got)
	}
}
