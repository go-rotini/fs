package fs

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// --- Remove ---

func TestRemove_File(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "f")
	if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if err := Remove(path); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if Exists(path) {
		t.Error("file still exists after Remove")
	}
}

func TestRemove_EmptyDir(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	sub := filepath.Join(dir, "sub")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatalf("Mkdir: %v", err)
	}
	if err := Remove(sub); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if Exists(sub) {
		t.Error("dir still exists after Remove")
	}
}

func TestRemove_NonEmptyDir(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	sub := filepath.Join(dir, "sub")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatalf("Mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(sub, "f"), []byte("x"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	err := Remove(sub)
	if err == nil {
		t.Error("expected error removing non-empty dir")
	}
}

func TestRemove_MissingIdempotent(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "missing")
	if err := Remove(path); err != nil {
		t.Errorf("Remove(missing) = %v, want nil (idempotent)", err)
	}
}

func TestRemove_MissingStrict(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "missing")
	err := Remove(path, WithStrict(true))
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("got %v, want ErrNotFound", err)
	}
}

// --- RemoveAll ---

func TestRemoveAll_RecursiveTree(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	root := filepath.Join(dir, "root")
	deep := filepath.Join(root, "a", "b", "c")
	if err := os.MkdirAll(deep, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(deep, "leaf"), []byte("x"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if err := RemoveAll(root); err != nil {
		t.Fatalf("RemoveAll: %v", err)
	}
	if Exists(root) {
		t.Error("root still exists after RemoveAll")
	}
}

func TestRemoveAll_MissingIdempotent(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	missing := filepath.Join(dir, "missing")
	if err := RemoveAll(missing); err != nil {
		t.Errorf("RemoveAll(missing) = %v, want nil", err)
	}
}

func TestRemoveAll_MissingStrict(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	missing := filepath.Join(dir, "missing")
	err := RemoveAll(missing, WithStrict(true))
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("got %v, want ErrNotFound", err)
	}
}

// --- RemoveContents ---

func TestRemoveContents_KeepsDir(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	root := filepath.Join(dir, "root")
	if err := os.MkdirAll(filepath.Join(root, "sub"), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "f"), []byte("x"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "sub", "f2"), []byte("y"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	if err := RemoveContents(root); err != nil {
		t.Fatalf("RemoveContents: %v", err)
	}
	if !IsDir(root) {
		t.Error("root removed; should remain")
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("entries remain: %v", entries)
	}
}

func TestRemoveContents_OnFileErrors(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "f")
	if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	err := RemoveContents(path)
	if !errors.Is(err, ErrNotDir) {
		t.Errorf("got %v, want ErrNotDir", err)
	}
}

func TestRemoveContents_MissingIdempotent(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	missing := filepath.Join(dir, "missing")
	if err := RemoveContents(missing); err != nil {
		t.Errorf("RemoveContents(missing) = %v, want nil", err)
	}
}

func TestRemoveContents_MissingStrict(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	missing := filepath.Join(dir, "missing")
	err := RemoveContents(missing, WithStrict(true))
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("got %v, want ErrNotFound", err)
	}
}

func TestRemoveContents_EmptyDirNoop(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := RemoveContents(dir); err != nil {
		t.Errorf("RemoveContents on empty dir = %v, want nil", err)
	}
	if !IsDir(dir) {
		t.Error("empty dir removed")
	}
}

// --- Aggregation: best-effort over multiple entries ---

func TestRemoveContents_MultipleEntries(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	for _, name := range []string{"a", "b", "c", "d", "e"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o644); err != nil {
			t.Fatalf("WriteFile %s: %v", name, err)
		}
	}
	if err := RemoveContents(dir); err != nil {
		t.Fatalf("RemoveContents: %v", err)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 0 {
		t.Errorf("entries remain: %v", entries)
	}
}

// --- RemoveAll with WithStrict ---

func TestRemoveAll_StrictMissing(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	err := RemoveAll(filepath.Join(dir, "missing"), WithStrict(true))
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("got %v, want ErrNotFound", err)
	}
}
