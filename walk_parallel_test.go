package fs

import (
	"errors"
	stdfs "io/fs"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"sync/atomic"
	"testing"
)

func TestWalkParallel_VisitsEveryEntry(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	mustMkdir(t, filepath.Join(root, "a"))
	mustMkdir(t, filepath.Join(root, "b", "c"))
	mustWrite(t, filepath.Join(root, "a", "x.txt"), "x")
	mustWrite(t, filepath.Join(root, "b", "y.txt"), "y")
	mustWrite(t, filepath.Join(root, "b", "c", "z.txt"), "z")

	var (
		mu      sync.Mutex
		visited []string
	)
	err := WalkParallel(root, func(path string, _ stdfs.DirEntry) error {
		rel, _ := filepath.Rel(root, path)
		mu.Lock()
		visited = append(visited, filepath.ToSlash(rel))
		mu.Unlock()
		return nil
	}, 4)
	if err != nil {
		t.Fatalf("WalkParallel: %v", err)
	}

	sort.Strings(visited)
	want := []string{".", "a", "a/x.txt", "b", "b/c", "b/c/z.txt", "b/y.txt"}
	if !equalStringSlice(visited, want) {
		t.Errorf("visited = %v; want %v", visited, want)
	}
}

func TestWalkParallel_PropagatesError(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "a.txt"), "a")
	mustWrite(t, filepath.Join(root, "b.txt"), "b")

	want := errors.New("boom")
	var hits atomic.Int32
	err := WalkParallel(root, func(path string, _ stdfs.DirEntry) error {
		if filepath.Base(path) == "a.txt" {
			hits.Add(1)
			return want
		}
		return nil
	}, 2)
	if !errors.Is(err, want) {
		t.Errorf("err = %v; want %v", err, want)
	}
	if hits.Load() == 0 {
		t.Error("error was not triggered")
	}
}

func TestWalkParallel_MissingRoot(t *testing.T) {
	t.Parallel()
	if err := WalkParallel(filepath.Join(t.TempDir(), "nope"), func(_ string, _ stdfs.DirEntry) error { return nil }, 1); err == nil {
		t.Error("expected error for missing root")
	}
}

func TestWalkParallel_DefaultWorkers(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "a.txt"), "a")
	var count atomic.Int32
	err := WalkParallel(root, func(_ string, _ stdfs.DirEntry) error {
		count.Add(1)
		return nil
	}, 0)
	if err != nil {
		t.Fatalf("WalkParallel: %v", err)
	}
	if count.Load() != 2 { // root dir + one file
		t.Errorf("count = %d; want 2", count.Load())
	}
}

// Avoid an "imported and not used" failure if other helpers in this
// file are pruned later.
var _ = os.PathSeparator
