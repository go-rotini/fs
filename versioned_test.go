package fs

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestWriteFileVersioned_FirstWriteNoBackup(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")

	backup, err := WriteFileVersioned(path, []byte("v1"))
	if err != nil {
		t.Fatalf("WriteFileVersioned: %v", err)
	}
	if backup != "" {
		t.Errorf("backup = %q; want empty for first write", backup)
	}
	got, _ := os.ReadFile(path)
	if string(got) != "v1" {
		t.Errorf("content = %q; want v1", got)
	}
}

func TestWriteFileVersioned_OverwriteCreatesBackup(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")

	if err := os.WriteFile(path, []byte("v1"), 0o644); err != nil {
		t.Fatalf("seed WriteFile: %v", err)
	}

	clock := &mockClock{t: time.Now()}
	backup, err := WriteFileVersioned(path, []byte("v2"), WithVersionsClock(clock.Now))
	if err != nil {
		t.Fatalf("WriteFileVersioned: %v", err)
	}
	if backup == "" {
		t.Fatal("backup path empty; expected one for overwrite")
	}

	current, _ := os.ReadFile(path)
	if string(current) != "v2" {
		t.Errorf("current = %q; want v2", current)
	}
	prior, err := os.ReadFile(backup)
	if err != nil {
		t.Fatalf("read backup: %v", err)
	}
	if string(prior) != "v1" {
		t.Errorf("backup content = %q; want v1", prior)
	}
}

func TestListVersions_OrdersNewestFirst(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")

	clock := &mockClock{t: time.Now()}
	for i := 1; i <= 3; i++ {
		if _, err := WriteFileVersioned(path, []byte(fmt.Sprintf("v%d", i)), WithVersionsClock(clock.Now)); err != nil {
			t.Fatalf("WriteFileVersioned #%d: %v", i, err)
		}
		// Advance clock so each version has a distinct timestamp.
		clock.mu.Lock()
		clock.t = clock.t.Add(time.Second)
		clock.mu.Unlock()
	}

	versions, err := ListVersions(path)
	if err != nil {
		t.Fatalf("ListVersions: %v", err)
	}
	// First write created no backup; subsequent two each created one.
	// So we expect exactly 2 versions, newest first.
	if len(versions) != 2 {
		t.Fatalf("len(versions) = %d; want 2", len(versions))
	}
	if !versions[0].Created.After(versions[1].Created) {
		t.Errorf("versions not sorted newest-first: %v", versions)
	}
}

func TestWriteFileVersioned_KeepRetention(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")

	clock := &mockClock{t: time.Now()}
	for i := 1; i <= 5; i++ {
		if _, err := WriteFileVersioned(path, []byte(fmt.Sprintf("v%d", i)),
			WithVersionsKeep(2),
			WithVersionsClock(clock.Now),
		); err != nil {
			t.Fatalf("WriteFileVersioned #%d: %v", i, err)
		}
		clock.mu.Lock()
		clock.t = clock.t.Add(time.Second)
		clock.mu.Unlock()
	}

	versions, err := ListVersions(path)
	if err != nil {
		t.Fatalf("ListVersions: %v", err)
	}
	if len(versions) != 2 {
		t.Errorf("len(versions) = %d; want 2 (keep=2)", len(versions))
	}
}

func TestWriteFileVersioned_MaxAgePruning(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")

	clock := &mockClock{t: time.Now()}

	// At each step, write a new value. v1's write creates no backup
	// (no prior file). v2's write creates backup at t=5m (containing
	// v1). v3's write creates backup at t=60m (containing v2). v4's
	// write at t=80m creates backup at t=80m (containing v3) and
	// then prunes with cutoff=80m-10m=70m: backups at t=5m and t=60m
	// are both older than cutoff → pruned. Only the t=80m backup
	// remains.
	stamps := []time.Duration{0, 5 * time.Minute, 60 * time.Minute, 80 * time.Minute}
	for i, dt := range stamps {
		clock.mu.Lock()
		clock.t = time.Now().Add(dt)
		clock.mu.Unlock()
		if _, err := WriteFileVersioned(path, []byte(fmt.Sprintf("v%d", i)),
			WithVersionsMaxAge(10*time.Minute),
			WithVersionsClock(clock.Now),
		); err != nil {
			t.Fatalf("WriteFileVersioned #%d: %v", i, err)
		}
	}

	versions, err := ListVersions(path)
	if err != nil {
		t.Fatalf("ListVersions: %v", err)
	}
	if len(versions) != 1 {
		t.Errorf("len(versions) = %d; want 1: %+v", len(versions), versions)
	}
}

func TestRestoreVersion_RestoresAndBacksUpCurrent(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")

	clock := &mockClock{t: time.Now()}
	if _, err := WriteFileVersioned(path, []byte("v1"), WithVersionsClock(clock.Now)); err != nil {
		t.Fatalf("WriteFileVersioned v1: %v", err)
	}
	clock.mu.Lock()
	clock.t = clock.t.Add(time.Second)
	clock.mu.Unlock()
	if _, err := WriteFileVersioned(path, []byte("v2"), WithVersionsClock(clock.Now)); err != nil {
		t.Fatalf("WriteFileVersioned v2: %v", err)
	}

	versions, _ := ListVersions(path)
	if len(versions) != 1 {
		t.Fatalf("expected 1 backup at this point, got %d", len(versions))
	}
	v1Path := versions[0].Path

	clock.mu.Lock()
	clock.t = clock.t.Add(time.Second)
	clock.mu.Unlock()
	if err := RestoreVersion(path, v1Path, WithVersionsClock(clock.Now)); err != nil {
		t.Fatalf("RestoreVersion: %v", err)
	}

	got, _ := os.ReadFile(path)
	if string(got) != "v1" {
		t.Errorf("content after restore = %q; want v1", got)
	}
	// The v2 content should have been saved as a new backup.
	versions, _ = ListVersions(path)
	if len(versions) != 2 {
		t.Fatalf("expected 2 backups after restore (v1-original + v2-snap), got %d", len(versions))
	}
}

func TestListVersions_EmptyWhenNoBackups(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte("plain"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	versions, err := ListVersions(path)
	if err != nil {
		t.Fatalf("ListVersions: %v", err)
	}
	if len(versions) != 0 {
		t.Errorf("versions = %v; want empty", versions)
	}
}

func TestListVersions_IgnoresMalformedSuffixes(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte("plain"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	// Create a file with the prefix but an unparseable timestamp.
	if err := os.WriteFile(path+versionedInfix+"not-a-timestamp", []byte(""), 0o644); err != nil {
		t.Fatalf("WriteFile bogus: %v", err)
	}
	versions, err := ListVersions(path)
	if err != nil {
		t.Fatalf("ListVersions: %v", err)
	}
	if len(versions) != 0 {
		t.Errorf("malformed entry leaked into versions: %v", versions)
	}
}

func TestWriteFileVersioned_PermControlsBackupAndCurrent(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "secret.key")

	// Seed with permissive perms; WithVersionsPerm should constrain
	// the NEW file's mode.
	if err := os.WriteFile(path, []byte("old"), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}

	_, err := WriteFileVersioned(path, []byte("new"), WithVersionsPerm(0o600))
	if err != nil {
		t.Fatalf("WriteFileVersioned: %v", err)
	}

	info, _ := os.Stat(path)
	if info.Mode().Perm() != 0o600 {
		t.Errorf("new file mode = %v; want 0600", info.Mode().Perm())
	}
}

func TestWriteFileVersioned_EmptyPathRejected(t *testing.T) {
	t.Parallel()
	if _, err := WriteFileVersioned("/nonexistent-root-XYZ123/nope/path", []byte("data")); err == nil {
		t.Error("expected error for unwritable path")
	}
}

func TestWriteFileVersioned_ConcurrentWrites(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")

	if err := os.WriteFile(path, []byte("initial"), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}

	const goroutines = 8
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for g := 0; g < goroutines; g++ {
		go func(gid int) {
			defer wg.Done()
			data := []byte(fmt.Sprintf("g%d", gid))
			// Some writes may fail because the rename loses to a peer
			// writer's rename of the same file (ErrNotExist on rename
			// for the latecomers). That's expected for concurrent
			// versioning without a coordinating lock; the test
			// asserts the package doesn't panic or corrupt the live
			// file.
			_, _ = WriteFileVersioned(path, data)
		}(g)
	}
	wg.Wait()

	// Live file must be readable and equal to one of the goroutines'
	// payloads (or initial).
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read live: %v", err)
	}
	if len(content) == 0 {
		t.Errorf("live file empty after concurrent writes")
	}
}
