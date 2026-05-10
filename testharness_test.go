package fs

import (
	stdfs "io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// --- TestHarness ---

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
	if !Exists(h.Path("temp.txt")) {
		t.Fatal("setup failed")
	}
	h.Remove("temp.txt")
	if Exists(h.Path("temp.txt")) {
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
	if Exists(h.Path("sub")) {
		t.Error("Remove did not delete tree")
	}
}

func TestTestHarness_RemoveMissingTolerated(t *testing.T) {
	t.Parallel()
	h := NewTestHarness(t)
	// Removing something that doesn't exist must not fail the test.
	h.Remove("never-existed")
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

// --- MockFS ---

func TestMockFS_ReadFile(t *testing.T) {
	t.Parallel()
	fsys := MockFS(map[string]string{
		"a.txt":     "alpha",
		"sub/b.txt": "beta",
	})
	data, err := stdfs.ReadFile(fsys, "a.txt")
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(data) != "alpha" {
		t.Errorf("got %q, want alpha", data)
	}
}

func TestMockFS_WalkCompat(t *testing.T) {
	t.Parallel()
	fsys := MockFS(map[string]string{
		"a.txt":     "a",
		"sub/b.txt": "b",
	})
	var paths []string
	if err := stdfs.WalkDir(fsys, ".", func(p string, _ stdfs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		paths = append(paths, p)
		return nil
	}); err != nil {
		t.Fatalf("WalkDir: %v", err)
	}
	// Should include "." plus both files plus the "sub" directory.
	if len(paths) < 3 {
		t.Errorf("walk visited %d entries, want >=3: %v", len(paths), paths)
	}
}

func TestMockFS_Empty(t *testing.T) {
	t.Parallel()
	fsys := MockFS(nil)
	if _, err := stdfs.ReadFile(fsys, "missing"); err == nil {
		t.Error("expected error reading from empty MockFS")
	}
}

// --- WithTempEnv ---

func TestWithTempEnv_RestoresOnCleanup(t *testing.T) {
	const key = "ROTINI_FS_TESTHARNESS_ENV"
	//nolint:usetesting // direct Setenv is the scenario WithTempEnv is meant to handle
	if err := os.Setenv(key, "outer"); err != nil {
		t.Fatalf("Setenv: %v", err)
	}
	t.Cleanup(func() { _ = os.Unsetenv(key) })

	t.Run("inner", func(t *testing.T) {
		WithTempEnv(t)
		// Mutate env inside the subtest via direct os.Setenv —
		// WithTempEnv's reason to exist.
		//nolint:usetesting // direct Setenv is the scenario under test
		if err := os.Setenv(key, "inner"); err != nil {
			t.Fatalf("Setenv: %v", err)
		}
		if got := os.Getenv(key); got != "inner" {
			t.Errorf("inside subtest got %q, want inner", got)
		}
	})

	// After the subtest's t.Cleanup runs, the variable should be back
	// to its outer value.
	if got := os.Getenv(key); got != "outer" {
		t.Errorf("after subtest got %q, want outer", got)
	}
}

func TestWithTempEnv_RestoresClearedVar(t *testing.T) {
	const key = "ROTINI_FS_TESTHARNESS_CLEAR"
	//nolint:usetesting // direct Setenv is the scenario WithTempEnv is meant to handle
	if err := os.Setenv(key, "set-by-outer"); err != nil {
		t.Fatalf("Setenv: %v", err)
	}
	t.Cleanup(func() { _ = os.Unsetenv(key) })

	t.Run("inner", func(t *testing.T) {
		WithTempEnv(t)
		if err := os.Unsetenv(key); err != nil {
			t.Fatalf("Unsetenv: %v", err)
		}
		if _, ok := os.LookupEnv(key); ok {
			t.Error("expected unset inside subtest")
		}
	})

	if got, ok := os.LookupEnv(key); !ok || got != "set-by-outer" {
		t.Errorf("after subtest got (%q, %v), want (set-by-outer, true)", got, ok)
	}
}

// --- TestHarness happy-path method coverage ---

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

func TestTestHarness_PathEmpty(t *testing.T) {
	t.Parallel()
	h := NewTestHarness(t)
	if h.Path("") == "" {
		t.Error("Path('') should still return harness root")
	}
}
