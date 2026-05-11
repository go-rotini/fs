package fs

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestRemoveAllNoFollow_DoesNotFollowSymlinks(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("symlinks require admin / developer mode on Windows")
	}

	dir := t.TempDir()

	// victim holds a file we DON'T want touched.
	victim := filepath.Join(dir, "victim")
	if err := os.MkdirAll(victim, 0o755); err != nil {
		t.Fatalf("MkdirAll victim: %v", err)
	}
	importantFile := filepath.Join(victim, "important.txt")
	if err := os.WriteFile(importantFile, []byte("DO NOT DELETE"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	// target holds a symlink pointing to victim. If RemoveAllNoFollow
	// follows the symlink, important.txt vanishes.
	target := filepath.Join(dir, "target")
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatalf("MkdirAll target: %v", err)
	}
	mustWrite(t, filepath.Join(target, "doomed.txt"), "ok to delete")
	if err := os.Symlink(victim, filepath.Join(target, "link-to-victim")); err != nil {
		t.Fatalf("Symlink: %v", err)
	}

	if err := RemoveAllNoFollow(target); err != nil {
		t.Fatalf("RemoveAllNoFollow: %v", err)
	}

	if _, err := os.Stat(importantFile); err != nil {
		t.Errorf("important file was wrongly deleted: %v", err)
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Errorf("target dir not removed; stat err=%v", err)
	}
}

func TestRemoveAllNoFollow_MissingPathOK(t *testing.T) {
	t.Parallel()
	if err := RemoveAllNoFollow(filepath.Join(t.TempDir(), "missing")); err != nil {
		t.Errorf("missing path err = %v; want nil", err)
	}
}

func TestRemoveAllNoFollow_SingleFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	file := filepath.Join(dir, "single.txt")
	mustWrite(t, file, "x")
	if err := RemoveAllNoFollow(file); err != nil {
		t.Fatalf("RemoveAllNoFollow: %v", err)
	}
	if Exists(file) {
		t.Error("file not removed")
	}
}

func TestRemoveAllNoFollow_RemovesNestedTree(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	tree := filepath.Join(root, "tree")
	for _, sub := range []string{"a/b/c", "x/y"} {
		if err := os.MkdirAll(filepath.Join(tree, sub), 0o755); err != nil {
			t.Fatalf("MkdirAll: %v", err)
		}
	}
	mustWrite(t, filepath.Join(tree, "a/b/c/leaf.txt"), "hi")
	mustWrite(t, filepath.Join(tree, "x/y/leaf.txt"), "bye")

	if err := RemoveAllNoFollow(tree); err != nil {
		t.Fatalf("RemoveAllNoFollow: %v", err)
	}
	if _, err := os.Stat(tree); !os.IsNotExist(err) {
		t.Errorf("tree not removed; stat err=%v", err)
	}
}
