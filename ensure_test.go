package fs

import (
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
)

// --- EnsureFile ---

func TestEnsureFile_CreatesIfMissing(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "f")

	created, err := EnsureFile(path, []byte("default-content"))
	if err != nil {
		t.Fatalf("EnsureFile: %v", err)
	}
	if !created {
		t.Error("created = false on first call; want true")
	}
	got, _ := os.ReadFile(path)
	if string(got) != "default-content" {
		t.Errorf("content = %q, want default-content", got)
	}
}

func TestEnsureFile_NoOpIfExists(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "f")
	if err := os.WriteFile(path, []byte("existing"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	created, err := EnsureFile(path, []byte("should-not-overwrite"))
	if err != nil {
		t.Fatalf("EnsureFile: %v", err)
	}
	if created {
		t.Error("created = true; existing file should not have been recreated")
	}
	got, _ := os.ReadFile(path)
	if string(got) != "existing" {
		t.Errorf("content = %q; existing was overwritten", got)
	}
}

func TestEnsureFile_Concurrent(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "f")

	const n = 16
	var (
		wg          sync.WaitGroup
		createdSeen atomic.Int32
		errs        atomic.Int32
	)
	for i := range n {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			payload := []byte{byte(i)}
			created, err := EnsureFile(path, payload)
			if err != nil {
				errs.Add(1)
				return
			}
			if created {
				createdSeen.Add(1)
			}
		}(i)
	}
	wg.Wait()

	if errs.Load() != 0 {
		t.Errorf("got %d errors; want 0", errs.Load())
	}
	if c := createdSeen.Load(); c != 1 {
		t.Errorf("createdSeen = %d; want exactly 1 (O_EXCL race-safe semantics)", c)
	}
	if !Exists(path) {
		t.Error("file does not exist after concurrent EnsureFile")
	}
}

func TestEnsureFile_HonorsMkdirAll(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	deep := filepath.Join(dir, "a", "b", "c", "f")

	created, err := EnsureFile(deep, []byte("x"), WithMkdirAll(true))
	if err != nil {
		t.Fatalf("EnsureFile: %v", err)
	}
	if !created {
		t.Error("created = false")
	}
	if !Exists(deep) {
		t.Error("deep file not created")
	}
}

// --- EnsureDir ---

func TestEnsureDir_CreatesIfMissing(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	target := filepath.Join(dir, "a", "b")

	created, err := EnsureDir(target, 0o755)
	if err != nil {
		t.Fatalf("EnsureDir: %v", err)
	}
	if !created {
		t.Error("created = false on first call")
	}
	if !IsDir(target) {
		t.Error("dir not created")
	}
}

func TestEnsureDir_NoOpIfExists(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	created, err := EnsureDir(dir, 0o755)
	if err != nil {
		t.Fatalf("EnsureDir: %v", err)
	}
	if created {
		t.Error("created = true on existing dir")
	}
}

func TestEnsureDir_OnRegularFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "f")
	if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	// path is a file; EnsureDir should treat it as "missing dir" and
	// try MkdirAll, which will error since the path exists as non-dir.
	_, err := EnsureDir(path, 0o755)
	if err == nil {
		t.Error("expected error when path exists as a file")
	}
}
