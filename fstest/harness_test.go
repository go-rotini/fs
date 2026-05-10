package fstest

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/go-rotini/fs"
)

func TestTestHarness_RootIsTempDir(t *testing.T) {
	t.Parallel()
	h := NewTestHarness(t)
	if h.Root() == "" {
		t.Error("Root empty")
	}
	if !filepath.IsAbs(h.Root()) {
		t.Errorf("Root not absolute: %s", h.Root())
	}
	info, err := os.Stat(h.Root())
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if !info.IsDir() {
		t.Error("Root is not a directory")
	}
}

func TestTestHarness_PathRelative(t *testing.T) {
	t.Parallel()
	h := NewTestHarness(t)
	got := h.Path("a/b/c.txt")
	want := filepath.Join(h.Root(), "a", "b", "c.txt")
	if got != want {
		t.Errorf("got %s, want %s", got, want)
	}
}

func TestTestHarness_PathAbsoluteStripped(t *testing.T) {
	t.Parallel()
	h := NewTestHarness(t)
	got := h.Path("/etc/passwd")
	want := filepath.Join(h.Root(), "etc", "passwd")
	if got != want {
		t.Errorf("absolute path leaked outside harness: got %s, want %s", got, want)
	}
}

func TestTestHarness_PathEmpty(t *testing.T) {
	t.Parallel()
	h := NewTestHarness(t)
	if h.Path("") == "" {
		t.Error("Path('') should still return harness root")
	}
}

func TestTestHarness_WriteRead(t *testing.T) {
	t.Parallel()
	h := NewTestHarness(t)
	path := h.Write("config.yaml", []byte("key: value"))
	if !filepath.IsAbs(path) {
		t.Errorf("Write returned non-absolute path: %s", path)
	}
	got := h.Read("config.yaml")
	if string(got) != "key: value" {
		t.Errorf("Read returned %q, want %q", got, "key: value")
	}
}

func TestTestHarness_WriteCreatesParents(t *testing.T) {
	t.Parallel()
	h := NewTestHarness(t)
	h.Write("a/b/c/d.txt", []byte("nested"))
	got := h.Read("a/b/c/d.txt")
	if string(got) != "nested" {
		t.Errorf("got %q, want nested", got)
	}
}

func TestTestHarness_WriteString(t *testing.T) {
	t.Parallel()
	h := NewTestHarness(t)
	h.WriteString("note.txt", "hello world")
	got := h.Read("note.txt")
	if string(got) != "hello world" {
		t.Errorf("got %q", got)
	}
}

func TestTestHarness_Mkdir(t *testing.T) {
	t.Parallel()
	h := NewTestHarness(t)
	p := h.Mkdir("data/cache")
	info, err := os.Stat(p)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if !info.IsDir() {
		t.Error("Mkdir target is not a directory")
	}
}

func TestTestHarness_Symlink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation typically requires elevation on Windows")
	}
	t.Parallel()
	h := NewTestHarness(t)
	h.WriteString("real.txt", "payload")
	h.Symlink("link.txt", h.Path("real.txt"))

	got, err := os.ReadFile(h.Path("link.txt"))
	if err != nil {
		t.Fatalf("ReadFile via symlink: %v", err)
	}
	if string(got) != "payload" {
		t.Errorf("got %q, want payload", got)
	}
}

func TestTestHarness_Remove(t *testing.T) {
	t.Parallel()
	h := NewTestHarness(t)
	h.WriteString("temp.txt", "x")
	if !fs.Exists(h.Path("temp.txt")) {
		t.Fatal("setup failed")
	}
	h.Remove("temp.txt")
	if fs.Exists(h.Path("temp.txt")) {
		t.Error("Remove did not delete file")
	}
}

func TestTestHarness_RemoveTree(t *testing.T) {
	t.Parallel()
	h := NewTestHarness(t)
	h.WriteString("sub/a.txt", "a")
	h.WriteString("sub/b.txt", "b")
	h.Mkdir("sub/deep")
	h.Remove("sub")
	if fs.Exists(h.Path("sub")) {
		t.Error("Remove did not delete tree")
	}
}

func TestTestHarness_RemoveMissingTolerated(t *testing.T) {
	t.Parallel()
	h := NewTestHarness(t)
	// Removing something that doesn't exist must not fail the test.
	h.Remove("never-existed")
}

func TestTestHarness_AllMethodsHappyPath(t *testing.T) {
	t.Parallel()
	h := NewTestHarness(t)
	if h.Path("a/b/c") == "" {
		t.Error("Path empty")
	}
	h.Mkdir("dir")
	h.WriteString("file.txt", "x")
	if string(h.Read("file.txt")) != "x" {
		t.Error("read content mismatch")
	}
	if runtime.GOOS != "windows" {
		h.Symlink("link", h.Path("file.txt"))
	}
	h.Remove("dir")
}

// --- Snapshot ---

func TestTestHarness_SnapshotDeterministic(t *testing.T) {
	t.Parallel()
	h := NewTestHarness(t)
	h.WriteString("c.txt", "C")
	h.WriteString("a.txt", "A")
	h.Mkdir("b")
	h.WriteString("b/inner.txt", "BI")

	first := h.Snapshot()
	second := h.Snapshot()
	if first != second {
		t.Errorf("snapshot non-deterministic:\n--- first ---\n%s\n--- second ---\n%s", first, second)
	}
}

func TestTestHarness_SnapshotSorted(t *testing.T) {
	t.Parallel()
	h := NewTestHarness(t)
	h.WriteString("z.txt", "z")
	h.WriteString("a.txt", "a")
	h.WriteString("m.txt", "m")

	snap := h.Snapshot()
	idxA := strings.Index(snap, "a.txt")
	idxM := strings.Index(snap, "m.txt")
	idxZ := strings.Index(snap, "z.txt")
	if !(idxA < idxM && idxM < idxZ) {
		t.Errorf("snapshot not sorted:\n%s", snap)
	}
}

func TestTestHarness_SnapshotRendersDirsAndFiles(t *testing.T) {
	t.Parallel()
	h := NewTestHarness(t)
	h.WriteString("a.txt", "alpha")
	h.Mkdir("sub")

	snap := h.Snapshot()
	if !strings.Contains(snap, "FILE a.txt") {
		t.Errorf("file entry missing:\n%s", snap)
	}
	if !strings.Contains(snap, "DIR  sub") {
		t.Errorf("dir entry missing:\n%s", snap)
	}
	if !strings.Contains(snap, "alpha") {
		t.Errorf("file content missing:\n%s", snap)
	}
}

func TestTestHarness_SnapshotSymlinks(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation typically requires elevation on Windows")
	}
	t.Parallel()
	h := NewTestHarness(t)
	h.WriteString("real.txt", "x")
	h.Symlink("link.txt", h.Path("real.txt"))

	snap := h.Snapshot()
	if !strings.Contains(snap, "LINK link.txt -> ") {
		t.Errorf("link entry missing or wrong format:\n%s", snap)
	}
}

func TestNewHarnessAt(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("alpha"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}
	h := NewHarnessAt(t, root)
	if h.Root() != root {
		t.Errorf("Root = %s, want %s", h.Root(), root)
	}
	if string(h.Read("a.txt")) != "alpha" {
		t.Error("NewHarnessAt did not see pre-existing file")
	}
	if !strings.Contains(h.Snapshot(), "FILE a.txt") {
		t.Error("Snapshot missing pre-existing file")
	}
}
