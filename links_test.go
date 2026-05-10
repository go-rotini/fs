package fs

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// --- Symlink ---

func TestSymlink_Basic(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation typically requires elevation on Windows")
	}
	t.Parallel()
	dir := t.TempDir()
	target := filepath.Join(dir, "target")
	if err := os.WriteFile(target, []byte("x"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	link := filepath.Join(dir, "link")
	if err := Symlink(target, link); err != nil {
		t.Fatalf("Symlink: %v", err)
	}
	got, err := os.Readlink(link)
	if err != nil || got != target {
		t.Errorf("Readlink = (%q, %v); want %q", got, err, target)
	}
}

func TestSymlink_IdempotentSameTarget(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("requires elevation")
	}
	t.Parallel()
	dir := t.TempDir()
	target := filepath.Join(dir, "t")
	if err := os.WriteFile(target, []byte("x"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	link := filepath.Join(dir, "lnk")
	if err := Symlink(target, link); err != nil {
		t.Fatalf("first Symlink: %v", err)
	}
	if err := Symlink(target, link); err != nil {
		t.Errorf("second Symlink (same target) should be nil; got %v", err)
	}
}

func TestSymlink_AlreadyExistsDifferentTarget(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("requires elevation")
	}
	t.Parallel()
	dir := t.TempDir()
	link := filepath.Join(dir, "lnk")
	if err := os.Symlink("/foo", link); err != nil {
		t.Fatalf("setup: %v", err)
	}
	err := Symlink("/bar", link)
	if !errors.Is(err, ErrAlreadyExists) {
		t.Errorf("got %v, want ErrAlreadyExists", err)
	}
}

func TestSymlink_AlreadyExistsAsRegularFile(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("requires elevation")
	}
	t.Parallel()
	dir := t.TempDir()
	link := filepath.Join(dir, "f")
	if err := os.WriteFile(link, []byte("x"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}
	err := Symlink("/anywhere", link)
	if !errors.Is(err, ErrAlreadyExists) {
		t.Errorf("got %v, want ErrAlreadyExists", err)
	}
}

// --- ReadLink ---

func TestReadLink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("requires elevation")
	}
	t.Parallel()
	dir := t.TempDir()
	link := filepath.Join(dir, "lnk")
	if err := os.Symlink("/foo/bar", link); err != nil {
		t.Fatalf("Symlink: %v", err)
	}
	got, err := ReadLink(link)
	if err != nil {
		t.Fatalf("ReadLink: %v", err)
	}
	if got != "/foo/bar" {
		t.Errorf("got %q, want /foo/bar", got)
	}
}

func TestReadLink_NotASymlink(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "f")
	if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	_, err := ReadLink(path)
	if err == nil {
		t.Error("expected error for non-symlink")
	}
}

// --- EvalSymlinks ---

func TestEvalSymlinks(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("requires elevation")
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
	got, err := EvalSymlinks(link)
	if err != nil {
		t.Fatalf("EvalSymlinks: %v", err)
	}
	resolvedTarget, _ := filepath.EvalSymlinks(target)
	if got != resolvedTarget {
		t.Errorf("got %q, want %q", got, resolvedTarget)
	}
}

// --- Hardlink ---

func TestHardlink_Basic(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	target := filepath.Join(dir, "target")
	link := filepath.Join(dir, "link")
	if err := os.WriteFile(target, []byte("payload"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if err := Hardlink(target, link); err != nil {
		t.Fatalf("Hardlink: %v", err)
	}
	same, err := SameFile(target, link)
	if err != nil {
		t.Fatalf("SameFile: %v", err)
	}
	if !same {
		t.Error("Hardlink did not produce shared inode")
	}
}

func TestHardlink_IdempotentSameInode(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	target := filepath.Join(dir, "target")
	link := filepath.Join(dir, "link")
	if err := os.WriteFile(target, []byte("x"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if err := Hardlink(target, link); err != nil {
		t.Fatalf("first Hardlink: %v", err)
	}
	if err := Hardlink(target, link); err != nil {
		t.Errorf("second Hardlink (same inode) should be nil; got %v", err)
	}
}

func TestHardlink_AlreadyExistsDifferentInode(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	target := filepath.Join(dir, "target")
	link := filepath.Join(dir, "link")
	if err := os.WriteFile(target, []byte("a"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	// link is an unrelated file (different inode).
	if err := os.WriteFile(link, []byte("b"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	err := Hardlink(target, link)
	if !errors.Is(err, ErrAlreadyExists) {
		t.Errorf("got %v, want ErrAlreadyExists", err)
	}
}

func TestHardlink_MissingTarget(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	err := Hardlink(filepath.Join(dir, "missing"), filepath.Join(dir, "link"))
	if err == nil {
		t.Error("expected error for missing target")
	}
}
