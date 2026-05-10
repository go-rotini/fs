package fs

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// --- TempFile ---

func TestTempFile_Basic(t *testing.T) {
	t.Parallel()
	f, cleanup, err := TempFile("", "rotini-tf-*")
	if err != nil {
		t.Fatalf("TempFile: %v", err)
	}
	defer func() { _ = cleanup() }()

	if f == nil {
		t.Fatal("nil file")
	}
	if !strings.Contains(filepath.Base(f.Name()), "rotini-tf-") {
		t.Errorf("name = %s; pattern not honored", f.Name())
	}
	if !Exists(f.Name()) {
		t.Error("file does not exist on disk")
	}
}

func TestTempFile_CleanupRemoves(t *testing.T) {
	t.Parallel()
	f, cleanup, err := TempFile("", "rotini-tf-*")
	if err != nil {
		t.Fatalf("TempFile: %v", err)
	}
	name := f.Name()
	if err := cleanup(); err != nil {
		t.Errorf("cleanup: %v", err)
	}
	if Exists(name) {
		t.Error("file still exists after cleanup")
	}
}

func TestTempFile_CleanupIdempotent(t *testing.T) {
	t.Parallel()
	_, cleanup, err := TempFile("", "rotini-tf-*")
	if err != nil {
		t.Fatalf("TempFile: %v", err)
	}
	if err := cleanup(); err != nil {
		t.Errorf("first cleanup: %v", err)
	}
	if err := cleanup(); err != nil {
		t.Errorf("second cleanup: %v", err)
	}
	if err := cleanup(); err != nil {
		t.Errorf("third cleanup: %v", err)
	}
}

func TestTempFile_CleanupAfterCallerClose(t *testing.T) {
	t.Parallel()
	f, cleanup, err := TempFile("", "rotini-tf-*")
	if err != nil {
		t.Fatalf("TempFile: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	// Cleanup must tolerate the file already being closed.
	if err := cleanup(); err != nil {
		t.Errorf("cleanup after caller-close: %v", err)
	}
	if Exists(f.Name()) {
		t.Error("file still exists after cleanup")
	}
}

func TestTempFile_CleanupAfterManualRemove(t *testing.T) {
	t.Parallel()
	f, cleanup, err := TempFile("", "rotini-tf-*")
	if err != nil {
		t.Fatalf("TempFile: %v", err)
	}
	name := f.Name()
	_ = f.Close()
	if rerr := os.Remove(name); rerr != nil {
		t.Fatalf("Remove: %v", rerr)
	}
	// Missing file should not produce an error from cleanup.
	if err := cleanup(); err != nil {
		t.Errorf("cleanup on already-missing file: %v", err)
	}
}

func TestTempFile_BadDir(t *testing.T) {
	t.Parallel()
	_, _, err := TempFile("/this/path/should/not/exist/at/all", "rotini-*")
	if err == nil {
		t.Error("expected error for bad parent dir")
	}
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("got %v, want ErrNotFound", err)
	}
}

// --- TempDir ---

func TestTempDir_Basic(t *testing.T) {
	t.Parallel()
	dir, cleanup, err := TempDir("", "rotini-td-*")
	if err != nil {
		t.Fatalf("TempDir: %v", err)
	}
	defer func() { _ = cleanup() }()

	if !IsDir(dir) {
		t.Error("dir not created")
	}
	if !strings.Contains(filepath.Base(dir), "rotini-td-") {
		t.Errorf("dir name = %s; pattern not honored", dir)
	}
}

func TestTempDir_CleanupRemovesTree(t *testing.T) {
	t.Parallel()
	dir, cleanup, err := TempDir("", "rotini-td-*")
	if err != nil {
		t.Fatalf("TempDir: %v", err)
	}
	// Populate with content.
	if err := os.MkdirAll(filepath.Join(dir, "sub", "deep"), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "sub", "deep", "f"), []byte("x"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if err := cleanup(); err != nil {
		t.Errorf("cleanup: %v", err)
	}
	if Exists(dir) {
		t.Error("temp dir still exists after cleanup")
	}
}

func TestTempDir_CleanupIdempotent(t *testing.T) {
	t.Parallel()
	_, cleanup, err := TempDir("", "rotini-td-*")
	if err != nil {
		t.Fatalf("TempDir: %v", err)
	}
	if err := cleanup(); err != nil {
		t.Errorf("first cleanup: %v", err)
	}
	if err := cleanup(); err != nil {
		t.Errorf("second cleanup: %v", err)
	}
}

// TempFileT and TempDirT live in [github.com/go-rotini/fs/fstest];
// see fstest/temp_test.go for their tests.
