package fs

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// --- Chmod ---

func TestChmod_Basic(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX perms only")
	}
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "f")
	if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if err := Chmod(path, Mode0600); err != nil {
		t.Fatalf("Chmod: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("perm = %o, want 0600", info.Mode().Perm())
	}
}

func TestChmod_Missing(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	err := Chmod(filepath.Join(dir, "missing"), Mode0644)
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("got %v, want ErrNotFound", err)
	}
}

// --- EnsurePerm ---

func TestEnsurePerm_AlreadyMatching(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX perms only")
	}
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "f")
	if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	// Should be a no-op (no chmod syscall, just stat).
	if err := EnsurePerm(path, Mode0600); err != nil {
		t.Errorf("EnsurePerm on already-matching: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("perm = %o, want 0600 (unchanged)", info.Mode().Perm())
	}
}

func TestEnsurePerm_AppliesChange(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX perms only")
	}
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "f")
	if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if err := EnsurePerm(path, Mode0600); err != nil {
		t.Fatalf("EnsurePerm: %v", err)
	}
	info, _ := os.Stat(path)
	if info.Mode().Perm() != 0o600 {
		t.Errorf("perm = %o, want 0600", info.Mode().Perm())
	}
}

func TestEnsurePerm_Missing(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	err := EnsurePerm(filepath.Join(dir, "missing"), Mode0644)
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("got %v, want ErrNotFound", err)
	}
}

func TestEnsurePerm_IgnoresNonPermBits(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX perms only")
	}
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "f")
	if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	// Pass a mode with a type bit set; only permission bits should
	// matter — and they already match, so no syscall.
	if err := EnsurePerm(path, os.ModeDir|0o600); err != nil {
		t.Errorf("EnsurePerm: %v", err)
	}
	info, _ := os.Stat(path)
	if info.Mode().Perm() != 0o600 {
		t.Errorf("perm = %o, want 0600", info.Mode().Perm())
	}
}
