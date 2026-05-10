package fstest

import (
	stdfs "io/fs"
	"testing"
)

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
