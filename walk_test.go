package fs

import (
	"errors"
	stdfs "io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"
)

// fixture: a small tree used by several tests.
//
//	root/
//	  a.txt
//	  .hidden
//	  sub/
//	    b.txt
//	    .secret
//	  vendor/
//	    pkg/
//	      v.go
//	  deep/
//	    deeper/
//	      x
func makeWalkFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	dirs := []string{
		filepath.Join(root, "sub"),
		filepath.Join(root, "vendor", "pkg"),
		filepath.Join(root, "deep", "deeper"),
	}
	for _, d := range dirs {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatalf("MkdirAll: %v", err)
		}
	}
	files := []string{
		filepath.Join(root, "a.txt"),
		filepath.Join(root, ".hidden"),
		filepath.Join(root, "sub", "b.txt"),
		filepath.Join(root, "sub", ".secret"),
		filepath.Join(root, "vendor", "pkg", "v.go"),
		filepath.Join(root, "deep", "deeper", "x"),
	}
	for _, p := range files {
		if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
			t.Fatalf("WriteFile %s: %v", p, err)
		}
	}
	return root
}

func collectWalk(root string, opts ...WalkOption) ([]string, error) {
	var paths []string
	err := Walk(root, func(path string, _ stdfs.DirEntry, _ error) error {
		paths = append(paths, path)
		return nil
	}, opts...)
	sort.Strings(paths)
	return paths, err
}

// --- core walking ---

func TestWalk_AllEntries(t *testing.T) {
	t.Parallel()
	root := makeWalkFixture(t)
	got, err := collectWalk(root)
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}
	// root + sub + vendor + vendor/pkg + deep + deep/deeper = 6 dirs;
	// a.txt + .hidden + sub/b.txt + sub/.secret + vendor/pkg/v.go +
	// deep/deeper/x = 6 files; total = 12.
	if len(got) != 12 {
		t.Errorf("got %d paths, want 12: %v", len(got), got)
	}
}

func TestWalk_SkipDirFromFn(t *testing.T) {
	t.Parallel()
	root := makeWalkFixture(t)
	var visited []string
	err := Walk(root, func(path string, d stdfs.DirEntry, _ error) error {
		visited = append(visited, path)
		if d.IsDir() && d.Name() == "vendor" {
			return filepath.SkipDir
		}
		return nil
	})
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}
	for _, p := range visited {
		if strings.Contains(p, "vendor"+string(filepath.Separator)+"pkg") {
			t.Errorf("vendor subtree should be pruned, got %s", p)
		}
	}
}

func TestWalk_SkipAllFromFn(t *testing.T) {
	t.Parallel()
	root := makeWalkFixture(t)
	var count int
	err := Walk(root, func(_ string, _ stdfs.DirEntry, _ error) error {
		count++
		if count >= 2 {
			return filepath.SkipAll
		}
		return nil
	})
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}
	if count != 2 {
		t.Errorf("count = %d, want 2 (SkipAll on 2nd entry should stop)", count)
	}
}

// --- WalkSkipHidden ---

func TestWalk_SkipHidden(t *testing.T) {
	t.Parallel()
	root := makeWalkFixture(t)
	got, err := collectWalk(root, WalkSkipHidden(true))
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}
	for _, p := range got {
		base := filepath.Base(p)
		if strings.HasPrefix(base, ".") {
			t.Errorf("hidden entry leaked: %s", p)
		}
	}
}

// --- WithSkipNames ---

func TestWalk_SkipNames(t *testing.T) {
	t.Parallel()
	root := makeWalkFixture(t)
	got, err := collectWalk(root, WithSkipNames([]string{"vendor", "deeper"}))
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}
	for _, p := range got {
		if strings.Contains(p, string(filepath.Separator)+"vendor") {
			t.Errorf("vendor subtree should be pruned: %s", p)
		}
		if strings.Contains(p, string(filepath.Separator)+"deeper") {
			t.Errorf("deeper subtree should be pruned: %s", p)
		}
	}
}

// --- WithSkipPatterns ---

func TestWalk_SkipPatterns(t *testing.T) {
	t.Parallel()
	root := makeWalkFixture(t)
	got, err := collectWalk(root, WithSkipPatterns([]string{"*.txt", "*.go"}))
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}
	for _, p := range got {
		if strings.HasSuffix(p, ".txt") || strings.HasSuffix(p, ".go") {
			t.Errorf("pattern-skipped entry leaked: %s", p)
		}
	}
}

// --- WithMaxDepth ---

func TestWalk_MaxDepthZero(t *testing.T) {
	t.Parallel()
	root := makeWalkFixture(t)
	// Default (n<=0) is unbounded. Verify by using n=1: only root +
	// immediate children should be visited.
	got, err := collectWalk(root, WithMaxDepth(1))
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}
	for _, p := range got {
		rel, _ := filepath.Rel(root, p)
		// Sub-files like "sub/b.txt" have a separator; reject those.
		if strings.Contains(rel, string(filepath.Separator)) {
			t.Errorf("entry past depth 1: %s", rel)
		}
	}
}

// --- WithErrorHandler ---

func TestWalk_ErrorHandlerSwallow(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	// Walking a path that doesn't exist; the handler swallows.
	missing := filepath.Join(root, "missing")
	var seenErr error
	err := Walk(missing, func(_ string, _ stdfs.DirEntry, _ error) error {
		t.Error("fn should not be called for missing root")
		return nil
	}, WithErrorHandler(func(_ string, e error) error {
		seenErr = e
		return nil // swallow
	}))
	if err != nil {
		t.Errorf("Walk = %v, want nil (handler swallows)", err)
	}
	if seenErr == nil {
		t.Error("error handler not invoked")
	}
}

// --- WalkFollowSymlinks ---

func TestWalk_FollowSymlinks(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation typically requires elevation on Windows")
	}
	t.Parallel()
	root := t.TempDir()
	target := filepath.Join(root, "real")
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if err := os.WriteFile(filepath.Join(target, "data"), []byte("x"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}
	link := filepath.Join(root, "link")
	if err := os.Symlink(target, link); err != nil {
		t.Fatalf("Symlink: %v", err)
	}

	// Without follow: link itself is visited but not its contents.
	withoutFollow, _ := collectWalk(root)
	hasInsideLink := false
	for _, p := range withoutFollow {
		if strings.HasPrefix(p, link+string(filepath.Separator)) {
			hasInsideLink = true
		}
	}
	if hasInsideLink {
		t.Error("default walk should not descend through symlinks")
	}

	// With follow: link is descended.
	withFollow, _ := collectWalk(root, WalkFollowSymlinks(true))
	descended := false
	for _, p := range withFollow {
		if strings.HasPrefix(p, link+string(filepath.Separator)) {
			descended = true
		}
	}
	if !descended {
		t.Error("WalkFollowSymlinks did not descend through symlink")
	}
}

func TestWalk_FollowSymlinksMaxDepth(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation typically requires elevation on Windows")
	}
	t.Parallel()
	root := t.TempDir()
	deep := filepath.Join(root, "a", "b", "c")
	if err := os.MkdirAll(deep, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	var visits []string
	err := Walk(root, func(p string, _ stdfs.DirEntry, _ error) error {
		visits = append(visits, p)
		return nil
	}, WalkFollowSymlinks(true), WithMaxDepth(1))
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}
	for _, p := range visits {
		rel, _ := filepath.Rel(root, p)
		if strings.Count(rel, string(filepath.Separator)) > 1 {
			t.Errorf("entry past depth 1 visited: %s", rel)
		}
	}
}

func TestWalk_FollowSymlinksMissingRoot(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	err := Walk(filepath.Join(dir, "missing"), func(string, stdfs.DirEntry, error) error {
		return nil
	}, WalkFollowSymlinks(true))
	if err == nil {
		t.Error("expected error walking missing root with follow-symlinks")
	}
}

func TestWalk_FollowSymlinksErrorHandlerSwallow(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	err := Walk(filepath.Join(dir, "missing"), func(string, stdfs.DirEntry, error) error {
		return nil
	}, WalkFollowSymlinks(true), WithErrorHandler(func(string, error) error { return nil }))
	if err != nil {
		t.Errorf("error-handler swallow: %v", err)
	}
}

func TestWalk_SymlinkLoopDetected(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation typically requires elevation on Windows")
	}
	t.Parallel()
	root := t.TempDir()
	loopDir := filepath.Join(root, "loopy")
	if err := os.Mkdir(loopDir, 0o755); err != nil {
		t.Fatalf("setup: %v", err)
	}
	// loopy/back -> loopy (creates a cycle)
	if err := os.Symlink(loopDir, filepath.Join(loopDir, "back")); err != nil {
		t.Fatalf("Symlink: %v", err)
	}

	// With follow, the loop should be detected and the walk must terminate.
	count := 0
	err := Walk(root, func(_ string, _ stdfs.DirEntry, _ error) error {
		count++
		if count > 100 {
			return errors.New("walk did not terminate")
		}
		return nil
	}, WalkFollowSymlinks(true))
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}
	if count > 100 {
		t.Errorf("walk did not terminate (count=%d)", count)
	}
}

// --- Integration: Find/FindByRegex/FindFunc honor WalkOption ---

func TestFind_HonorsSkipHidden(t *testing.T) {
	t.Parallel()
	root := makeWalkFixture(t)
	got, err := Find(root, "*", WalkSkipHidden(true))
	if err != nil {
		t.Fatalf("Find: %v", err)
	}
	for _, p := range got {
		if strings.HasPrefix(filepath.Base(p), ".") {
			t.Errorf("hidden entry returned by Find: %s", p)
		}
	}
}

func TestFind_HonorsMaxDepth(t *testing.T) {
	t.Parallel()
	root := makeWalkFixture(t)
	got, err := Find(root, "*.go", WithMaxDepth(1))
	if err != nil {
		t.Fatalf("Find: %v", err)
	}
	// vendor/pkg/v.go is 3 deep; with maxDepth=1 it should be excluded.
	for _, p := range got {
		if strings.HasSuffix(p, "v.go") {
			t.Errorf("file beyond maxDepth=1 returned: %s", p)
		}
	}
}

// --- Walk with WalkFollowSymlinks behaviors ---

func TestWalk_FollowSymlinksFnSkipDir(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "skip-me"), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "skip-me", "x"), []byte("y"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	visits := 0
	err := Walk(root, func(p string, d stdfs.DirEntry, _ error) error {
		visits++
		if d.IsDir() && filepath.Base(p) == "skip-me" {
			return filepath.SkipDir
		}
		return nil
	}, WalkFollowSymlinks(true))
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}
	if visits == 0 {
		t.Error("walk visited zero entries")
	}
}

func TestWalk_FollowSymlinksFnError(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "f"), nil, 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}
	sentinel := errors.New("fn-stop")
	err := Walk(root, func(_ string, _ stdfs.DirEntry, _ error) error {
		return sentinel
	}, WalkFollowSymlinks(true))
	if !errors.Is(err, sentinel) {
		t.Errorf("got %v, want sentinel", err)
	}
}

func TestWalk_FollowSymlinks_MissingTarget(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	root := filepath.Join(dir, "root")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatalf("setup: %v", err)
	}
	// Create a dangling symlink (target missing).
	if err := os.Symlink(filepath.Join(dir, "missing"), filepath.Join(root, "lnk")); err != nil {
		t.Skipf("symlinks not supported: %v", err)
	}
	err := Walk(root, func(_ string, _ stdfs.DirEntry, _ error) error {
		return nil
	}, WalkFollowSymlinks(true))
	if err == nil {
		t.Fatal("expected error from dangling symlink stat")
	}
}

func TestWalk_FollowSymlinks_ErrorHandlerSwallowsStatError(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	root := filepath.Join(dir, "root")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if err := os.Symlink(filepath.Join(dir, "missing"), filepath.Join(root, "lnk")); err != nil {
		t.Skipf("symlinks not supported: %v", err)
	}
	err := Walk(root, func(_ string, _ stdfs.DirEntry, _ error) error {
		return nil
	}, WalkFollowSymlinks(true), WithErrorHandler(func(_ string, _ error) error { return nil }))
	if err != nil {
		t.Errorf("Walk: %v", err)
	}
}

func TestWalk_FollowSymlinks_LoopDetection(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	root := filepath.Join(dir, "root")
	sub := filepath.Join(root, "a")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatalf("setup: %v", err)
	}
	// Create a symlink that points back to root from inside.
	if err := os.Symlink(root, filepath.Join(sub, "back")); err != nil {
		t.Skipf("symlinks not supported: %v", err)
	}
	err := Walk(root, func(_ string, _ stdfs.DirEntry, _ error) error {
		return nil
	}, WalkFollowSymlinks(true))
	if err != nil {
		t.Errorf("Walk: %v", err)
	}
}
