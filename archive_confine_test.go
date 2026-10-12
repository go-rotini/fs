package fs

import (
	"archive/tar"
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

// tarEntry is one entry of a test archive: a regular file when typ is 0.
type tarEntry struct {
	name, link, body string
	typ              byte
}

func tarOf(t *testing.T, entries ...tarEntry) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	for _, e := range entries {
		hdr := &tar.Header{Name: e.name, Mode: 0o644, Linkname: e.link, ModTime: time.Now(), Typeflag: e.typ}
		switch e.typ {
		case 0:
			hdr.Typeflag, hdr.Size = tar.TypeReg, int64(len(e.body))
		case tar.TypeDir:
			hdr.Mode = 0o755
		}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatalf("WriteHeader %s: %v", e.name, err)
		}
		if e.body != "" {
			if _, err := tw.Write([]byte(e.body)); err != nil {
				t.Fatalf("Write %s: %v", e.name, err)
			}
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	return &buf
}

func skipWithoutSymlinks(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation typically requires elevation on Windows")
	}
}

// sandbox returns an empty extraction directory inside a parent the test can inspect for
// anything written outside it.
func sandbox(t *testing.T) (parent, dst string) {
	t.Helper()
	parent = t.TempDir()
	dst = filepath.Join(parent, "dst")
	if err := os.Mkdir(dst, 0o755); err != nil {
		t.Fatal(err)
	}
	return parent, dst
}

func assertAbsent(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Lstat(path); err == nil {
		t.Errorf("%s was written outside the destination", path)
	}
}

// Symlinks that each look inside the destination can chain to its parent: d -> . and
// s -> d/d/d lead back to dst, so a -> s/.. is dst's parent. Neither the file written through
// a nor the escaping link may survive.
func TestExtractArchive_ChainedSymlinkEscape(t *testing.T) {
	skipWithoutSymlinks(t)
	t.Parallel()
	parent, dst := sandbox(t)
	err := ExtractArchive(tarOf(t,
		tarEntry{name: "d", link: ".", typ: tar.TypeSymlink},
		tarEntry{name: "s", link: "d/d/d", typ: tar.TypeSymlink},
		tarEntry{name: "a", link: "s/..", typ: tar.TypeSymlink},
		tarEntry{name: "a/evil.txt", body: "escaped"},
	), dst)
	if !errors.Is(err, ErrEscapesRoot) {
		t.Errorf("got %v, want ErrEscapesRoot", err)
	}
	assertAbsent(t, filepath.Join(parent, "evil.txt"))
	if _, lerr := os.Lstat(filepath.Join(dst, "a")); lerr == nil {
		if fi, _ := os.Lstat(filepath.Join(dst, "a")); fi.Mode()&os.ModeSymlink != 0 {
			t.Error("the escaping symlink a was left in the destination")
		}
	}
}

// A symlink already in the destination that leads outside is not followed by a write.
func TestExtractArchive_ExistingSymlinkNotFollowed(t *testing.T) {
	skipWithoutSymlinks(t)
	t.Parallel()
	parent, dst := sandbox(t)
	outside := filepath.Join(parent, "outside")
	if err := os.Mkdir(outside, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(dst, "out")); err != nil {
		t.Fatal(err)
	}
	for _, e := range []tarEntry{
		{name: "out/file.txt", body: "x"},
		{name: "out/sub", typ: tar.TypeDir},
		{name: "out/hard", link: "real.txt", typ: tar.TypeLink},
	} {
		entries := []tarEntry{{name: "real.txt", body: "r"}, e}
		if err := ExtractArchive(tarOf(t, entries...), dst); !errors.Is(err, ErrEscapesRoot) {
			t.Errorf("%s: got %v, want ErrEscapesRoot", e.name, err)
		}
	}
	if names, _ := os.ReadDir(outside); len(names) != 0 {
		t.Errorf("wrote %v outside the destination", names)
	}
}

// An entry at a path that holds a symlink replaces the link instead of writing through it.
func TestExtractArchive_EntryReplacesExistingSymlink(t *testing.T) {
	skipWithoutSymlinks(t)
	t.Parallel()
	parent, dst := sandbox(t)
	victim := filepath.Join(parent, "victim.txt")
	if err := os.WriteFile(victim, []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(victim, filepath.Join(dst, "f.txt")); err != nil {
		t.Fatal(err)
	}
	if err := ExtractArchive(tarOf(t, tarEntry{name: "f.txt", body: "new"}), dst); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(victim); string(got) != "keep" {
		t.Errorf("the file outside became %q", got)
	}
	if fi, err := os.Lstat(filepath.Join(dst, "f.txt")); err != nil || fi.Mode()&os.ModeSymlink != 0 {
		t.Errorf("f.txt is still a link: %v", err)
	}
}

// A hard link may not reach a file outside through a symlink already in the destination.
func TestExtractArchive_HardlinkThroughSymlinkRefused(t *testing.T) {
	skipWithoutSymlinks(t)
	t.Parallel()
	parent, dst := sandbox(t)
	if err := os.WriteFile(filepath.Join(parent, "secret"), []byte("s"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(parent, filepath.Join(dst, "up")); err != nil {
		t.Fatal(err)
	}
	err := ExtractArchive(tarOf(t, tarEntry{name: "h", link: "up/secret", typ: tar.TypeLink}), dst)
	if !errors.Is(err, ErrEscapesRoot) {
		t.Errorf("got %v, want ErrEscapesRoot", err)
	}
	assertAbsent(t, filepath.Join(dst, "h"))
}

// Links that stay inside still extract, whatever order the archive lists them in.
func TestExtractArchive_InsideSymlinksExtract(t *testing.T) {
	skipWithoutSymlinks(t)
	t.Parallel()
	_, dst := sandbox(t)
	err := ExtractArchive(tarOf(t,
		tarEntry{name: "lib", link: "lib64", typ: tar.TypeSymlink}, // before its target
		tarEntry{name: "lib64", typ: tar.TypeDir},
		tarEntry{name: "lib64/x.so", body: "elf"},
		tarEntry{name: "bin/tool", link: "../lib/x.so", typ: tar.TypeSymlink},
		tarEntry{name: "dangling", link: "not/yet/there", typ: tar.TypeSymlink},
	), dst)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := os.ReadFile(filepath.Join(dst, "bin", "tool")); err != nil || string(got) != "elf" {
		t.Errorf("bin/tool -> %q, %v", got, err)
	}
	if target, err := os.Readlink(filepath.Join(dst, "dangling")); err != nil || target != "not/yet/there" {
		t.Errorf("dangling -> %q, %v", target, err)
	}
}

// A later entry at a symlink's path replaces it, as any later entry replaces an earlier one.
func TestExtractArchive_LaterEntryReplacesSymlink(t *testing.T) {
	skipWithoutSymlinks(t)
	t.Parallel()
	_, dst := sandbox(t)
	err := ExtractArchive(tarOf(t,
		tarEntry{name: "x", link: "y", typ: tar.TypeSymlink},
		tarEntry{name: "x", body: "file"},
	), dst)
	if err != nil {
		t.Fatal(err)
	}
	if fi, err := os.Lstat(filepath.Join(dst, "x")); err != nil || !fi.Mode().IsRegular() {
		t.Errorf("x: %v, %v; want the regular file", fi, err)
	}
}

// A symlink loop is reported rather than followed forever.
func TestExtractArchive_SymlinkLoop(t *testing.T) {
	skipWithoutSymlinks(t)
	t.Parallel()
	_, dst := sandbox(t)
	if err := os.Symlink("b", filepath.Join(dst, "a")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("a", filepath.Join(dst, "b")); err != nil {
		t.Fatal(err)
	}
	err := ExtractArchive(tarOf(t, tarEntry{name: "a/f", body: "x"}), dst)
	if !errors.Is(err, ErrSymlinkLoop) {
		t.Errorf("got %v, want ErrSymlinkLoop", err)
	}
}
