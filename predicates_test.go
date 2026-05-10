package fs

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// --- Cross-platform predicates ---

func TestExists(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	file := filepath.Join(dir, "f")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if !Exists(file) {
		t.Errorf("Exists(%q) = false, want true", file)
	}
	if !Exists(dir) {
		t.Errorf("Exists(dir) = false, want true")
	}
	if Exists(filepath.Join(dir, "missing")) {
		t.Errorf("Exists(missing) = true, want false")
	}
	if Exists("") {
		t.Errorf("Exists(\"\") = true, want false")
	}
}

func TestIsFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	file := filepath.Join(dir, "f")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if !IsFile(file) {
		t.Errorf("IsFile(file) = false")
	}
	if IsFile(dir) {
		t.Errorf("IsFile(dir) = true, want false")
	}
	if IsFile(filepath.Join(dir, "missing")) {
		t.Errorf("IsFile(missing) = true")
	}
}

func TestIsDir(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	file := filepath.Join(dir, "f")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if !IsDir(dir) {
		t.Errorf("IsDir(dir) = false")
	}
	if IsDir(file) {
		t.Errorf("IsDir(file) = true, want false")
	}
	if IsDir(filepath.Join(dir, "missing")) {
		t.Errorf("IsDir(missing) = true")
	}
}

func TestIsSymlink(t *testing.T) {
	if runtime.GOOS == goosWindows {
		t.Skip("Windows symlinks require admin/developer mode")
	}
	t.Parallel()
	dir := t.TempDir()
	target := filepath.Join(dir, "target")
	if err := os.WriteFile(target, []byte("x"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	link := filepath.Join(dir, "link")
	if err := os.Symlink(target, link); err != nil {
		t.Fatalf("Symlink: %v", err)
	}
	if !IsSymlink(link) {
		t.Errorf("IsSymlink(link) = false")
	}
	if IsSymlink(target) {
		t.Errorf("IsSymlink(regular file) = true")
	}
}

// --- Extended type predicates (negative cases run cross-platform) ---

func TestIsBlockDevice_NotARegularFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	regular := filepath.Join(dir, "f")
	if err := os.WriteFile(regular, []byte("x"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if IsBlockDevice(regular) {
		t.Errorf("IsBlockDevice(regular file) = true")
	}
	// Block devices are root-owned and rare in test environments;
	// the positive case is exercised on POSIX in predicates_unix_test.go
	// against well-known nodes.
}

// --- Permission predicates ---

func TestIsReadable(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	file := filepath.Join(dir, "f")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if !IsReadable(file) {
		t.Errorf("IsReadable(0o644 file) = false")
	}
	if IsReadable(filepath.Join(dir, "missing")) {
		t.Errorf("IsReadable(missing) = true")
	}
}

func TestIsWritable(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	file := filepath.Join(dir, "f")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if !IsWritable(file) {
		t.Errorf("IsWritable(0o644 file) = false")
	}

	if runtime.GOOS != goosWindows {
		// 0o400 file: readable but not writable for owner.
		ro := filepath.Join(dir, "ro")
		if err := os.WriteFile(ro, []byte("x"), 0o400); err != nil {
			t.Fatalf("WriteFile ro: %v", err)
		}
		if IsWritable(ro) {
			t.Errorf("IsWritable(0o400 file) = true (running as root?)")
		}
	}
}

func TestIsExecutable(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	plain := filepath.Join(dir, "f")
	if err := os.WriteFile(plain, []byte("x"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if IsExecutable(plain) {
		t.Errorf("IsExecutable(0o644) = true")
	}

	if runtime.GOOS == goosWindows {
		// Windows: extension-driven.
		exe := filepath.Join(dir, "tool.exe")
		if err := os.WriteFile(exe, []byte("x"), 0o644); err != nil {
			t.Fatalf("WriteFile exe: %v", err)
		}
		if !IsExecutable(exe) {
			t.Errorf("IsExecutable(.exe) = false")
		}
		nonExe := filepath.Join(dir, "data.dat")
		if err := os.WriteFile(nonExe, []byte("x"), 0o644); err != nil {
			t.Fatalf("WriteFile dat: %v", err)
		}
		if IsExecutable(nonExe) {
			t.Errorf("IsExecutable(.dat) = true")
		}
		return
	}

	// POSIX: +x bit.
	if err := os.Chmod(plain, 0o755); err != nil {
		t.Fatalf("Chmod: %v", err)
	}
	if !IsExecutable(plain) {
		t.Errorf("IsExecutable(0o755) = false")
	}
}
