package fs

import (
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// --- FindUp ---

func TestFindUp_DirectMatch(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	deep := filepath.Join(root, "a", "b", "c")
	if err := os.MkdirAll(deep, 0o755); err != nil {
		t.Fatalf("setup: %v", err)
	}
	marker := filepath.Join(root, ".markerfile")
	if err := os.WriteFile(marker, []byte("x"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}

	got, ok, err := FindUp(".markerfile", deep)
	if err != nil {
		t.Fatalf("FindUp: %v", err)
	}
	if !ok {
		t.Fatal("ok=false; want true")
	}
	if !sameRealPath(t, got, marker) {
		t.Errorf("got %s, want %s", got, marker)
	}
}

func TestFindUp_NoMatch(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	got, ok, err := FindUp("nonexistent", root, WithStopAt(root))
	if err != nil {
		t.Fatalf("FindUp: %v", err)
	}
	if ok || got != "" {
		t.Errorf("got (%q, %v), want ('', false)", got, ok)
	}
}

func TestFindUp_GlobMatch(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "settings.toml"), []byte("x"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}
	sub := filepath.Join(root, "child")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatalf("setup: %v", err)
	}

	got, ok, err := FindUp("*.toml", sub)
	if err != nil {
		t.Fatalf("FindUp: %v", err)
	}
	if !ok {
		t.Fatal("expected match")
	}
	if filepath.Base(got) != "settings.toml" {
		t.Errorf("got %s, want settings.toml", got)
	}
}

func TestFindUp_MaxAncestors(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	chain := root
	for _, name := range []string{"l1", "l2", "l3", "l4", "l5"} {
		chain = filepath.Join(chain, name)
		if err := os.Mkdir(chain, 0o755); err != nil {
			t.Fatalf("setup: %v", err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, ".marker"), []byte("x"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}

	_, ok, err := FindUp(".marker", chain, WithMaxAncestors(2))
	if err != nil {
		t.Fatalf("FindUp: %v", err)
	}
	if ok {
		t.Error("found marker despite max-ancestors=2 (5 levels deep)")
	}

	_, ok, _ = FindUp(".marker", chain, WithMaxAncestors(10))
	if !ok {
		t.Error("did not find marker with max-ancestors=10")
	}
}

func TestFindUp_StopAt(t *testing.T) {
	t.Parallel()
	outer := t.TempDir()
	mid := filepath.Join(outer, "mid")
	inner := filepath.Join(mid, "inner")
	if err := os.MkdirAll(inner, 0o755); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if err := os.WriteFile(filepath.Join(outer, ".marker"), []byte("x"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}

	_, ok, err := FindUp(".marker", inner, WithStopAt(mid))
	if err != nil {
		t.Fatalf("FindUp: %v", err)
	}
	if ok {
		t.Error("walk crossed stopAt boundary")
	}
}

// --- FindUpAll ---

func TestFindUpAll_MultiLevel(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	mid := filepath.Join(root, "mid")
	leaf := filepath.Join(mid, "leaf")
	if err := os.MkdirAll(leaf, 0o755); err != nil {
		t.Fatalf("setup: %v", err)
	}
	for _, p := range []string{
		filepath.Join(root, ".envrc"),
		filepath.Join(mid, ".envrc"),
		filepath.Join(leaf, ".envrc"),
	} {
		if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
			t.Fatalf("setup: %v", err)
		}
	}

	got, err := FindUpAll(".envrc", leaf, WithStopAt(root))
	if err != nil {
		t.Fatalf("FindUpAll: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("got %d matches, want 3 (got: %v)", len(got), got)
	}
	if filepath.Dir(got[0]) != filepath.Clean(leaf) || filepath.Dir(got[2]) != filepath.Clean(root) {
		t.Errorf("matches not in leaf-to-root order: %v", got)
	}
}

// --- ProjectRoot ---

func TestProjectRoot_GitMarker(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, ".git"), 0o755); err != nil {
		t.Fatalf("setup: %v", err)
	}
	deep := filepath.Join(root, "src", "pkg")
	if err := os.MkdirAll(deep, 0o755); err != nil {
		t.Fatalf("setup: %v", err)
	}

	got, err := ProjectRoot(deep)
	if err != nil {
		t.Fatalf("ProjectRoot: %v", err)
	}
	if !sameRealPath(t, got, root) {
		t.Errorf("got %s, want %s", got, root)
	}
}

func TestProjectRoot_CustomMarkers(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, ".myapp.json"), []byte("{}"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}
	deep := filepath.Join(root, "x")
	if err := os.Mkdir(deep, 0o755); err != nil {
		t.Fatalf("setup: %v", err)
	}

	got, err := ProjectRoot(deep, WithProjectMarkers([]string{".myapp.json"}))
	if err != nil {
		t.Fatalf("ProjectRoot: %v", err)
	}
	if !sameRealPath(t, got, root) {
		t.Errorf("got %s, want %s", got, root)
	}
}

func TestProjectRoot_NotFound(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	deep := filepath.Join(root, "x")
	if err := os.Mkdir(deep, 0o755); err != nil {
		t.Fatalf("setup: %v", err)
	}
	_, err := ProjectRoot(deep, WithStopAt(root))
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("got %v, want ErrNotFound", err)
	}
}

func TestProjectRoot_Memoized(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, ".git"), 0o755); err != nil {
		t.Fatalf("setup: %v", err)
	}

	first, err := ProjectRoot(root)
	if err != nil {
		t.Fatalf("ProjectRoot 1st: %v", err)
	}
	second, err := ProjectRoot(root)
	if err != nil {
		t.Fatalf("ProjectRoot 2nd: %v", err)
	}
	if first != second {
		t.Errorf("memoized result diverged: %q vs %q", first, second)
	}

	// Cache is keyed on inputs; sorted markers should hit the same entry.
	third, err := ProjectRoot(root, WithProjectMarkers([]string{"go.mod", ".git", "package.json", "Cargo.toml"}))
	if err != nil {
		t.Fatalf("ProjectRoot 3rd: %v", err)
	}
	if third != first {
		t.Errorf("re-ordered marker list missed the cache: %q vs %q", third, first)
	}
}

// --- FirstExisting ---

func TestFirstExisting_FirstHit(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	a := filepath.Join(dir, "missing-a")
	b := filepath.Join(dir, "real")
	c := filepath.Join(dir, "real-2")
	if err := os.WriteFile(b, []byte("x"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if err := os.WriteFile(c, []byte("x"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}

	got, ok := FirstExisting([]string{a, b, c})
	if !ok {
		t.Fatal("ok=false; want true")
	}
	if got != b {
		t.Errorf("got %s, want %s (should pick first hit, not last)", got, b)
	}
}

func TestFirstExisting_AllMissing(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	got, ok := FirstExisting([]string{
		filepath.Join(dir, "a"),
		filepath.Join(dir, "b"),
	})
	if ok || got != "" {
		t.Errorf("got (%q, %v), want ('', false)", got, ok)
	}
}

func TestFirstExisting_Empty(t *testing.T) {
	t.Parallel()
	got, ok := FirstExisting(nil)
	if ok || got != "" {
		t.Errorf("got (%q, %v), want ('', false)", got, ok)
	}
}

// --- Find / FindByRegex / FindFunc ---

func TestFind_BasenameGlob(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	for _, p := range []string{
		filepath.Join(root, "a.go"),
		filepath.Join(root, "b.go"),
		filepath.Join(root, "c.txt"),
		filepath.Join(root, "sub", "d.go"),
	} {
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatalf("setup: %v", err)
		}
		if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
			t.Fatalf("setup: %v", err)
		}
	}

	got, err := Find(root, "*.go")
	if err != nil {
		t.Fatalf("Find: %v", err)
	}
	if len(got) != 3 {
		t.Errorf("got %d, want 3 (got: %v)", len(got), got)
	}
	for _, m := range got {
		if !strings.HasSuffix(m, ".go") {
			t.Errorf("non-matching: %s", m)
		}
	}
}

func TestFindByRegex_Basename(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	for _, name := range []string{"alpha.txt", "beta.log", "gamma.txt", "data42.bin"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte("x"), 0o644); err != nil {
			t.Fatalf("setup: %v", err)
		}
	}
	re := regexp.MustCompile(`^(alpha|beta|gamma)\.`)

	got, err := FindByRegex(root, re)
	if err != nil {
		t.Fatalf("FindByRegex: %v", err)
	}
	if len(got) != 3 {
		t.Errorf("got %d, want 3 (got: %v)", len(got), got)
	}
}

func TestFindFunc_Predicate(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "small"), []byte("ab"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "large"), make([]byte, 1024), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}

	got, err := FindFunc(root, func(_ string, info os.FileInfo) bool {
		return !info.IsDir() && info.Size() >= 100
	})
	if err != nil {
		t.Fatalf("FindFunc: %v", err)
	}
	if len(got) != 1 || filepath.Base(got[0]) != "large" {
		t.Errorf("got %v, want exactly [.../large]", got)
	}
}

func TestFind_MissingRoot(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	_, err := Find(filepath.Join(dir, "missing"), "*")
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("got %v, want ErrNotFound", err)
	}
}

// sameRealPath compares paths after resolving symlinks. macOS's
// /var/folders TempDir lives behind /private/var/folders, so direct
// string equality on TempDir-derived paths will fail.
func sameRealPath(t *testing.T, a, b string) bool {
	t.Helper()
	ra, err := filepath.EvalSymlinks(a)
	if err != nil {
		t.Fatalf("EvalSymlinks %s: %v", a, err)
	}
	rb, err := filepath.EvalSymlinks(b)
	if err != nil {
		t.Fatalf("EvalSymlinks %s: %v", b, err)
	}
	return ra == rb
}

// --- FindUp / FindUpAll error paths ---

func TestFindUp_BadGlob(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "x"), nil, 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}
	_, _, err := FindUp("[", dir, WithStopAt(dir))
	if err == nil {
		t.Error("expected error for malformed glob")
	}
}

func TestFindUpAll_BadGlob(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "x"), nil, 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}
	_, err := FindUpAll("[", dir, WithStopAt(dir))
	if err == nil {
		t.Error("expected error for malformed glob")
	}
}

func TestFindUp_AbsError(t *testing.T) {
	t.Parallel()
	// On most platforms filepath.Abs doesn't fail for any string. This
	// exercises matchInDir's read-fail path by giving FindUp a startDir
	// whose ancestors include a missing component. ReadDir fails and
	// matchInDir folds it to "no match", so the walk traverses cleanly.
	_, ok, err := FindUp("anything", "/__definitely_does_not_exist_qwxz")
	if err != nil {
		t.Fatalf("FindUp: %v", err)
	}
	if ok {
		t.Errorf("got ok=%v, want false on bogus tree", ok)
	}
}

func TestFindUpAll_AbsError(t *testing.T) {
	t.Parallel()
	matches, err := FindUpAll("anything", "/__definitely_does_not_exist_qwxz")
	if err != nil {
		t.Fatalf("FindUpAll: %v", err)
	}
	if len(matches) != 0 {
		t.Errorf("got %v, want empty", matches)
	}
}

func TestFindUpAll_StopAt(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "a", "b"), 0o755); err != nil {
		t.Fatalf("setup: %v", err)
	}
	matches, err := FindUpAll("doesnotexist", filepath.Join(dir, "a", "b"), WithStopAt(filepath.Join(dir, "a")))
	if err != nil {
		t.Fatalf("FindUpAll: %v", err)
	}
	if len(matches) != 0 {
		t.Errorf("got %v, want empty", matches)
	}
}

// --- Find / FindByRegex / FindFunc misc ---

func TestFind_BadPattern(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "f"), []byte("x"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}
	_, err := Find(dir, "[")
	if err == nil {
		t.Fatal("expected error from bad glob")
	}
}

func TestFindFunc_PredicateRoot(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("x"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "b.dat"), []byte("y"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}
	got, err := FindFunc(dir, func(_ string, info os.FileInfo) bool {
		return strings.HasSuffix(info.Name(), ".txt")
	})
	if err != nil {
		t.Fatalf("FindFunc: %v", err)
	}
	if len(got) != 1 {
		t.Errorf("got %d matches, want 1: %v", len(got), got)
	}
}

func TestFindFunc_MissingRoot(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	_, err := FindFunc(filepath.Join(dir, "missing"), func(string, os.FileInfo) bool { return true })
	if err == nil {
		t.Fatal("expected error for missing root")
	}
}

func TestMatchInDir_BadPattern(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a"), []byte("x"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}
	// "[" is an unmatched bracket; filepath.Match returns ErrBadPattern.
	_, _, err := FindUp("[", dir)
	if err == nil {
		t.Error("expected error from bad glob")
	}
}

func TestFindWithExtensions(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".app.config.toml"), []byte("x"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}

	// First match in extension order wins; a leading "." on an ext is ignored.
	got, ok := FindWithExtensions(dir, ".app.config", []string{"yml", "yaml", ".toml", "json"})
	if !ok {
		t.Fatal("FindWithExtensions did not find the .toml file")
	}
	if want := filepath.Join(dir, ".app.config.toml"); got != want {
		t.Fatalf("got %q, want %q", got, want)
	}

	if _, ok := FindWithExtensions(dir, "absent", []string{"yaml", "json"}); ok {
		t.Error("expected no match for an absent stem")
	}
}
