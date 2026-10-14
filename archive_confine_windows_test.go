//go:build windows

package fs

import (
	"archive/tar"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
)

// junction makes link a directory junction to target. Unlike a symlink, a junction needs no
// elevation.
func junction(t *testing.T, link, target string) {
	t.Helper()
	if out, err := exec.Command("cmd", "/c", "mklink", "/J", link, target).CombinedOutput(); err != nil {
		t.Fatalf("mklink /J %s %s: %v: %s", link, target, err, out)
	}
}

// A directory junction already in the destination that leads outside is resolved like a
// symlink, so nothing is written through it.
func TestExtractArchive_ExistingJunctionNotFollowed(t *testing.T) {
	t.Parallel()
	parent, dst := sandbox(t)
	outside := filepath.Join(parent, "outside")
	if err := os.Mkdir(outside, 0o755); err != nil {
		t.Fatal(err)
	}
	junction(t, filepath.Join(dst, "out"), outside)
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
	src := fstest.MapFS{"out/file.txt": {Data: []byte("x")}}
	if err := ScaffoldApply(src, dst, nil); !errors.Is(err, ErrEscapesRoot) {
		t.Errorf("ScaffoldApply: got %v, want ErrEscapesRoot", err)
	}
	if err := ScaffoldExtract(src, dst); !errors.Is(err, ErrEscapesRoot) {
		t.Errorf("ScaffoldExtract: got %v, want ErrEscapesRoot", err)
	}
	if names, _ := os.ReadDir(outside); len(names) != 0 {
		t.Errorf("wrote %v outside the destination", names)
	}
}

// A junction that leads elsewhere inside the destination is followed.
func TestExtractArchive_InsideJunction(t *testing.T) {
	t.Parallel()
	_, dst := sandbox(t)
	inside := filepath.Join(dst, "real")
	if err := os.Mkdir(inside, 0o755); err != nil {
		t.Fatal(err)
	}
	junction(t, filepath.Join(dst, "j"), inside)
	if err := ExtractArchive(tarOf(t, tarEntry{name: "j/x.txt", body: "x"}), dst); err != nil {
		t.Fatalf("ExtractArchive: %v", err)
	}
	if got, err := os.ReadFile(filepath.Join(inside, "x.txt")); err != nil || string(got) != "x" {
		t.Errorf("real/x.txt = %q, %v; want %q", got, err, "x")
	}
}

// A file entry at a junction's path replaces the junction instead of writing through it.
func TestExtractArchive_EntryReplacesJunction(t *testing.T) {
	t.Parallel()
	parent, dst := sandbox(t)
	outside := filepath.Join(parent, "outside")
	if err := os.Mkdir(outside, 0o755); err != nil {
		t.Fatal(err)
	}
	junction(t, filepath.Join(dst, "j"), outside)
	if err := ExtractArchive(tarOf(t, tarEntry{name: "j", body: "x"}), dst); err != nil {
		t.Fatalf("ExtractArchive: %v", err)
	}
	info, err := os.Lstat(filepath.Join(dst, "j"))
	if err != nil || !info.Mode().IsRegular() {
		t.Errorf("dst/j: %v, %v; want a regular file", info, err)
	}
	if names, _ := os.ReadDir(outside); len(names) != 0 {
		t.Errorf("wrote %v outside the destination", names)
	}
}

// EvalSymlinksWithin follows a directory junction: one leading outside parent escapes, one
// leading inside resolves to its target.
func TestEvalSymlinksWithin_Junction(t *testing.T) {
	t.Parallel()
	parent, dst := sandbox(t)
	outside := filepath.Join(parent, "outside")
	inside := filepath.Join(dst, "real")
	for _, d := range []string{outside, inside} {
		if err := os.Mkdir(d, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(d, "f"), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	junction(t, filepath.Join(dst, "out"), outside)
	junction(t, filepath.Join(dst, "in"), inside)
	if _, err := EvalSymlinksWithin(dst, filepath.Join(dst, "out", "f")); !errors.Is(err, ErrEscapesRoot) {
		t.Errorf("through the outside junction: got %v, want ErrEscapesRoot", err)
	}
	got, err := EvalSymlinksWithin(dst, filepath.Join(dst, "in", "f"))
	if err != nil {
		t.Fatalf("through the inside junction: %v", err)
	}
	want, err := filepath.EvalSymlinks(filepath.Join(inside, "f"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.EqualFold(got, want) {
		t.Errorf("got %q, want %q", got, want)
	}
}
