package fs

import (
	stdfs "io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

func TestGitignore_LiteralMatch(t *testing.T) {
	t.Parallel()
	g := NewGitignore([]string{"foo.txt"})
	cases := map[string]bool{
		"foo.txt":         true,
		"bar.txt":         false,
		"sub/foo.txt":     true, // unanchored — matches at any depth
		"sub/sub/foo.txt": true,
		"foo.txt.bak":     false, // different name
	}
	for in, want := range cases {
		if got := g.Match(in, false); got != want {
			t.Errorf("Match(%q) = %v; want %v", in, got, want)
		}
	}
}

func TestGitignore_AnchoredLeading(t *testing.T) {
	t.Parallel()
	g := NewGitignore([]string{"/foo"})
	cases := map[string]bool{
		"foo":     true,
		"sub/foo": false, // anchored — only matches at root level
	}
	for in, want := range cases {
		if got := g.Match(in, false); got != want {
			t.Errorf("Match(%q) = %v; want %v", in, got, want)
		}
	}
}

func TestGitignore_AnchoredInternalSlash(t *testing.T) {
	t.Parallel()
	g := NewGitignore([]string{"src/build"})
	cases := map[string]bool{
		"src/build":     true,
		"src/build/x":   true,  // parent-dir ignore
		"sub/src/build": false, // anchored
	}
	for in, want := range cases {
		if got := g.Match(in, false); got != want {
			t.Errorf("Match(%q) = %v; want %v", in, got, want)
		}
	}
}

func TestGitignore_DirOnly(t *testing.T) {
	t.Parallel()
	g := NewGitignore([]string{"node_modules/"})

	if got := g.Match("node_modules", true); !got {
		t.Error("node_modules dir not matched")
	}
	if got := g.Match("node_modules", false); got {
		t.Error("dir-only pattern matched a file")
	}
	if got := g.Match("sub/node_modules", true); !got {
		t.Error("unanchored dir-only pattern did not match at depth")
	}
}

func TestGitignore_Negation(t *testing.T) {
	t.Parallel()
	g := NewGitignore([]string{
		"*.log",
		"!important.log",
	})

	if !g.Match("foo.log", false) {
		t.Error("*.log did not match foo.log")
	}
	if g.Match("important.log", false) {
		t.Error("!important.log was not honored")
	}
}

func TestGitignore_StarWildcard(t *testing.T) {
	t.Parallel()
	g := NewGitignore([]string{"*.go"})
	cases := map[string]bool{
		"main.go":      true,
		"pkg/util.go":  true,
		"main.go.bak":  false,
		"main_test.go": true,
	}
	for in, want := range cases {
		if got := g.Match(in, false); got != want {
			t.Errorf("Match(%q) = %v; want %v", in, got, want)
		}
	}
}

func TestGitignore_DoubleStarRecursive(t *testing.T) {
	t.Parallel()
	g := NewGitignore([]string{"foo/**/bar"})
	cases := map[string]bool{
		"foo/bar":       true, // zero intermediate segments
		"foo/x/bar":     true,
		"foo/x/y/z/bar": true,
		"foo/bar/baz":   true,  // parent-dir match
		"other/foo/bar": false, // anchored
	}
	for in, want := range cases {
		if got := g.Match(in, false); got != want {
			t.Errorf("Match(%q) = %v; want %v", in, got, want)
		}
	}
}

func TestGitignore_QuestionMark(t *testing.T) {
	t.Parallel()
	g := NewGitignore([]string{"a?c"})
	cases := map[string]bool{
		"abc": true,
		"ac":  false,
		"axc": true,
		"a/c": false, // ? doesn't match /
	}
	for in, want := range cases {
		if got := g.Match(in, false); got != want {
			t.Errorf("Match(%q) = %v; want %v", in, got, want)
		}
	}
}

func TestGitignore_CharClass(t *testing.T) {
	t.Parallel()
	g := NewGitignore([]string{"foo.[ab]"})
	cases := map[string]bool{
		"foo.a": true,
		"foo.b": true,
		"foo.c": false,
	}
	for in, want := range cases {
		if got := g.Match(in, false); got != want {
			t.Errorf("Match(%q) = %v; want %v", in, got, want)
		}
	}
}

func TestGitignore_CommentsAndBlankLines(t *testing.T) {
	t.Parallel()
	g := NewGitignore([]string{
		"# a comment",
		"",
		"   ",
		"*.tmp",
	})
	if !g.Match("scratch.tmp", false) {
		t.Error("pattern after comment+blank was not honored")
	}
	if g.Match("# a comment", false) {
		t.Error("comment line was interpreted as a pattern")
	}
}

func TestGitignore_NilSafe(t *testing.T) {
	t.Parallel()
	var g *Gitignore
	if g.Match("anything", false) {
		t.Error("nil Gitignore.Match returned true")
	}
}

func TestLoadGitignore(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, ".gitignore")
	body := "*.log\n!keep.log\n# comment\nbuild/\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	g, err := LoadGitignore(path)
	if err != nil {
		t.Fatalf("LoadGitignore: %v", err)
	}
	if !g.Match("foo.log", false) {
		t.Error("foo.log not ignored")
	}
	if g.Match("keep.log", false) {
		t.Error("keep.log was wrongly ignored")
	}
	if !g.Match("build", true) {
		t.Error("build/ dir not ignored")
	}
}

func TestLoadGitignore_MissingFile(t *testing.T) {
	t.Parallel()
	if _, err := LoadGitignore(filepath.Join(t.TempDir(), "nope")); err == nil {
		t.Error("expected error for missing file")
	}
}

func TestWalkGitignore_PrunesIgnoredDirs(t *testing.T) {
	t.Parallel()
	root := t.TempDir()

	mustMkdir(t, filepath.Join(root, "src"))
	mustMkdir(t, filepath.Join(root, "node_modules", "react"))
	mustMkdir(t, filepath.Join(root, "vendor", "github.com", "foo"))
	mustWrite(t, filepath.Join(root, "src", "main.go"), "package main")
	mustWrite(t, filepath.Join(root, "node_modules", "react", "index.js"), "//")
	mustWrite(t, filepath.Join(root, "vendor", "github.com", "foo", "lib.go"), "package foo")
	mustWrite(t, filepath.Join(root, "README.md"), "# hi")

	g := NewGitignore([]string{"node_modules/", "vendor/"})

	var visited []string
	err := Walk(root, func(path string, d stdfs.DirEntry, werr error) error {
		if werr != nil {
			return werr
		}
		if d.IsDir() {
			return nil
		}
		rel, _ := filepath.Rel(root, path)
		visited = append(visited, filepath.ToSlash(rel))
		return nil
	}, WithWalkGitignore(g))
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}

	sort.Strings(visited)
	want := []string{"README.md", "src/main.go"}
	if strings.Join(visited, ",") != strings.Join(want, ",") {
		t.Errorf("visited = %v; want %v", visited, want)
	}
}

// mustMkdir / mustWrite are local test helpers.

func mustMkdir(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatalf("MkdirAll(%s): %v", path, err)
	}
}

func mustWrite(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("WriteFile(%s): %v", path, err)
	}
}
