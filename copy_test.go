package fs

import (
	"errors"
	"fmt"
	"io"
	stdfs "io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"testing/iotest"
	"time"
)

// --- CopyFile ---

func TestCopyFile_Basic(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	dst := filepath.Join(dir, "dst")
	want := []byte("hello world")
	if err := os.WriteFile(src, want, 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if err := CopyFile(src, dst); err != nil {
		t.Fatalf("CopyFile: %v", err)
	}
	got, err := os.ReadFile(dst)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(got) != string(want) {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestCopyFile_PreservesMode(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX perms only")
	}
	t.Parallel()
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	dst := filepath.Join(dir, "dst")
	if err := os.WriteFile(src, []byte("x"), 0o640); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if err := os.Chmod(src, 0o640); err != nil {
		t.Fatalf("Chmod: %v", err)
	}
	if err := CopyFile(src, dst); err != nil {
		t.Fatalf("CopyFile: %v", err)
	}
	info, err := os.Stat(dst)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if got := info.Mode().Perm(); got != 0o640 {
		t.Errorf("dst mode = %o, want 0640", got)
	}
}

func TestCopyFile_PreservesMtime(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	dst := filepath.Join(dir, "dst")
	if err := os.WriteFile(src, []byte("x"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}
	want := time.Date(2020, 1, 1, 12, 0, 0, 0, time.UTC)
	if err := os.Chtimes(src, want, want); err != nil {
		t.Fatalf("Chtimes: %v", err)
	}
	if err := CopyFile(src, dst); err != nil {
		t.Fatalf("CopyFile: %v", err)
	}
	info, err := os.Stat(dst)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if !info.ModTime().Equal(want) {
		t.Errorf("dst mtime = %v, want %v", info.ModTime(), want)
	}
}

func TestCopyFile_NoMtime(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	dst := filepath.Join(dir, "dst")
	if err := os.WriteFile(src, []byte("x"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}
	old := time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)
	if err := os.Chtimes(src, old, old); err != nil {
		t.Fatalf("Chtimes: %v", err)
	}
	before := time.Now().Add(-1 * time.Second)
	if err := CopyFile(src, dst, WithPreserveMtime(false)); err != nil {
		t.Fatalf("CopyFile: %v", err)
	}
	info, err := os.Stat(dst)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if info.ModTime().Before(before) {
		t.Errorf("dst mtime = %v should be near 'now', not preserved old %v", info.ModTime(), old)
	}
}

func TestCopyFile_Overwrite(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	dst := filepath.Join(dir, "dst")
	if err := os.WriteFile(src, []byte("new"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if err := os.WriteFile(dst, []byte("old"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if err := CopyFile(src, dst); err != nil {
		t.Fatalf("CopyFile: %v", err)
	}
	got, _ := os.ReadFile(dst)
	if string(got) != "new" {
		t.Errorf("got %q, want %q", got, "new")
	}
}

func TestCopyFile_NoOverwrite(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	dst := filepath.Join(dir, "dst")
	if err := os.WriteFile(src, []byte("new"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if err := os.WriteFile(dst, []byte("old"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}
	err := CopyFile(src, dst, WithOverwrite(false))
	if !errors.Is(err, ErrAlreadyExists) {
		t.Errorf("got %v, want ErrAlreadyExists", err)
	}
	got, _ := os.ReadFile(dst)
	if string(got) != "old" {
		t.Errorf("dst was modified: got %q", got)
	}
}

func TestCopyFile_MissingSrc(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	err := CopyFile(filepath.Join(dir, "missing"), filepath.Join(dir, "dst"))
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("got %v, want ErrNotFound", err)
	}
}

func TestCopyFile_MissingSrcWithFollowSymlinks(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	err := CopyFile(filepath.Join(dir, "missing"), filepath.Join(dir, "dst"), WithFollowSymlinks(true))
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("got %v, want ErrNotFound", err)
	}
}

func TestCopyDir_NoOverwriteFlag(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	dst := filepath.Join(dir, "dst")
	if err := os.MkdirAll(filepath.Join(src, "sub"), 0o755); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if err := os.WriteFile(filepath.Join(src, "f"), []byte("v"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}
	// Pre-existing dst file with a conflicting name; with WithOverwrite(false),
	// CopyDir aggregates the per-entry conflict into a *MultiError.
	if err := os.MkdirAll(dst, 0o755); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dst, "f"), []byte("old"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}

	err := CopyDir(src, dst, WithOverwrite(false))
	if !errors.Is(err, ErrAlreadyExists) {
		t.Errorf("got %v, want ErrAlreadyExists", err)
	}
}

func TestCopyFile_NotRegular(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	src := filepath.Join(dir, "subdir")
	if err := os.Mkdir(src, 0o755); err != nil {
		t.Fatalf("setup: %v", err)
	}
	err := CopyFile(src, filepath.Join(dir, "dst"), WithFollowSymlinks(true))
	if !errors.Is(err, ErrNotSupported) {
		t.Errorf("got %v, want ErrNotSupported", err)
	}
}

// --- CopyDir ---

func TestCopyDir_Tree(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	dst := filepath.Join(dir, "dst")

	for _, p := range []string{
		filepath.Join(src, "a", "b"),
		filepath.Join(src, "x"),
	} {
		if err := os.MkdirAll(p, 0o755); err != nil {
			t.Fatalf("setup: %v", err)
		}
	}
	files := map[string]string{
		filepath.Join(src, "top.txt"):        "top",
		filepath.Join(src, "a", "1.txt"):     "one",
		filepath.Join(src, "a", "b", "deep"): "deep",
		filepath.Join(src, "x", "y.txt"):     "y",
	}
	for p, c := range files {
		if err := os.WriteFile(p, []byte(c), 0o644); err != nil {
			t.Fatalf("setup: %v", err)
		}
	}

	if err := CopyDir(src, dst); err != nil {
		t.Fatalf("CopyDir: %v", err)
	}

	for p, want := range files {
		rel, _ := filepath.Rel(src, p)
		got, err := os.ReadFile(filepath.Join(dst, rel))
		if err != nil {
			t.Fatalf("missing copied file %s: %v", rel, err)
		}
		if string(got) != want {
			t.Errorf("%s: got %q, want %q", rel, got, want)
		}
	}
}

func TestCopyDir_Filter(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	dst := filepath.Join(dir, "dst")

	if err := os.MkdirAll(filepath.Join(src, "skip", "deep"), 0o755); err != nil {
		t.Fatalf("setup: %v", err)
	}
	for _, p := range []string{
		filepath.Join(src, "keep.txt"),
		filepath.Join(src, "drop.tmp"),
		filepath.Join(src, "skip", "in_skip.txt"),
		filepath.Join(src, "skip", "deep", "nested.txt"),
	} {
		if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
			t.Fatalf("setup: %v", err)
		}
	}

	filter := func(path string, e stdfs.DirEntry) bool {
		if e.IsDir() && e.Name() == "skip" {
			return false
		}
		return !strings.HasSuffix(path, ".tmp")
	}

	if err := CopyDir(src, dst, WithFilter(filter)); err != nil {
		t.Fatalf("CopyDir: %v", err)
	}

	if !Exists(filepath.Join(dst, "keep.txt")) {
		t.Error("keep.txt missing in dst")
	}
	if Exists(filepath.Join(dst, "drop.tmp")) {
		t.Error(".tmp file should have been filtered out")
	}
	if Exists(filepath.Join(dst, "skip")) {
		t.Error("skip subtree should have been pruned")
	}
}

func TestCopyDir_NotADir(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	src := filepath.Join(dir, "f")
	if err := os.WriteFile(src, []byte("x"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}
	err := CopyDir(src, filepath.Join(dir, "dst"))
	if !errors.Is(err, ErrNotDir) {
		t.Errorf("got %v, want ErrNotDir", err)
	}
}

func TestCopyDir_MissingSrc(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	err := CopyDir(filepath.Join(dir, "missing"), filepath.Join(dir, "dst"))
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("got %v, want ErrNotFound", err)
	}
}

// --- Symlinks (POSIX-only; Windows requires elevation) ---

func TestCopyFile_Symlink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation typically requires elevation on Windows")
	}
	t.Parallel()
	dir := t.TempDir()
	target := filepath.Join(dir, "target")
	if err := os.WriteFile(target, []byte("payload"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}
	src := filepath.Join(dir, "link")
	if err := os.Symlink(target, src); err != nil {
		t.Fatalf("Symlink: %v", err)
	}
	dst := filepath.Join(dir, "link2")
	if err := CopyFile(src, dst); err != nil {
		t.Fatalf("CopyFile: %v", err)
	}
	info, err := os.Lstat(dst)
	if err != nil {
		t.Fatalf("Lstat: %v", err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		t.Error("dst should be a symlink")
	}
	got, err := os.Readlink(dst)
	if err != nil {
		t.Fatalf("Readlink: %v", err)
	}
	if got != target {
		t.Errorf("link target = %q, want %q", got, target)
	}
}

func TestCopyFile_SymlinkNoOverwrite(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation typically requires elevation on Windows")
	}
	t.Parallel()
	dir := t.TempDir()
	target := filepath.Join(dir, "target")
	if err := os.WriteFile(target, []byte("x"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	src := filepath.Join(dir, "link")
	if err := os.Symlink(target, src); err != nil {
		t.Fatalf("Symlink: %v", err)
	}
	dst := filepath.Join(dir, "existing-link")
	if err := os.Symlink(target, dst); err != nil {
		t.Fatalf("setup dst symlink: %v", err)
	}

	err := CopyFile(src, dst, WithOverwrite(false))
	if !errors.Is(err, ErrAlreadyExists) {
		t.Errorf("got %v, want ErrAlreadyExists", err)
	}
}

func TestCopyFile_SymlinkMissingSrc(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink-related test")
	}
	t.Parallel()
	dir := t.TempDir()
	src := filepath.Join(dir, "broken")
	if err := os.Symlink("/never/exists", src); err != nil {
		t.Fatalf("Symlink: %v", err)
	}
	// CopyFile (default: don't follow) should still copy the symlink as-is.
	dst := filepath.Join(dir, "copy")
	if err := CopyFile(src, dst); err != nil {
		t.Fatalf("CopyFile broken-symlink: %v", err)
	}
	if _, err := os.Lstat(dst); err != nil {
		t.Errorf("dst missing: %v", err)
	}
}

func TestCopyFile_SymlinkFollow(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation typically requires elevation on Windows")
	}
	t.Parallel()
	dir := t.TempDir()
	target := filepath.Join(dir, "target")
	if err := os.WriteFile(target, []byte("payload"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}
	src := filepath.Join(dir, "link")
	if err := os.Symlink(target, src); err != nil {
		t.Fatalf("Symlink: %v", err)
	}
	dst := filepath.Join(dir, "dst")
	if err := CopyFile(src, dst, WithFollowSymlinks(true)); err != nil {
		t.Fatalf("CopyFile: %v", err)
	}
	info, err := os.Lstat(dst)
	if err != nil {
		t.Fatalf("Lstat: %v", err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		t.Error("dst should be a regular file, not a symlink")
	}
	got, err := os.ReadFile(dst)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(got) != "payload" {
		t.Errorf("got %q, want payload", got)
	}
}

// --- Rename / Move ---

func TestRename_SameFS(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	dst := filepath.Join(dir, "dst")
	if err := os.WriteFile(src, []byte("x"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if err := Rename(src, dst); err != nil {
		t.Fatalf("Rename: %v", err)
	}
	if Exists(src) {
		t.Error("src still exists")
	}
	if !Exists(dst) {
		t.Error("dst missing")
	}
}

func TestRename_Missing(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	err := Rename(filepath.Join(dir, "missing"), filepath.Join(dir, "dst"))
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("got %v, want ErrNotFound", err)
	}
}

func TestMove_SameFS(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	dst := filepath.Join(dir, "dst")
	if err := os.WriteFile(src, []byte("x"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if err := Move(src, dst); err != nil {
		t.Fatalf("Move: %v", err)
	}
	if Exists(src) {
		t.Error("src still exists")
	}
	if !Exists(dst) {
		t.Error("dst missing")
	}
}

func TestMove_NoOverwrite(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	dst := filepath.Join(dir, "dst")
	if err := os.WriteFile(src, []byte("a"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if err := os.WriteFile(dst, []byte("b"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}
	err := Move(src, dst, WithOverwrite(false))
	if !errors.Is(err, ErrAlreadyExists) {
		t.Errorf("got %v, want ErrAlreadyExists", err)
	}
	if !Exists(src) {
		t.Error("src removed despite overwrite-false rejection")
	}
}

func TestMove_MissingSrc(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	err := Move(filepath.Join(dir, "missing"), filepath.Join(dir, "dst"))
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("got %v, want ErrNotFound", err)
	}
}

// TestMove_CrossDevice exercises the EXDEV to CopyFile + Remove fallback.
// It synthesizes the cross-device condition by feeding the fallback an
// error that wraps syscall.EXDEV (a real cross-mount setup is
// environment-specific, so it is not exercised here).
func TestMove_CrossDeviceFallback(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	dst := filepath.Join(dir, "dst")
	if err := os.WriteFile(src, []byte("payload"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}

	// Sanity: isCrossDevice recognizes wrapped EXDEV.
	wrapped := fmt.Errorf("wrapped: %w", &os.PathError{Op: "rename", Path: src, Err: exdevError()})
	if !isCrossDevice(wrapped) {
		t.Fatalf("isCrossDevice did not recognize wrapped EXDEV: %v", wrapped)
	}

	// Same-fs Move should still work (no EXDEV path taken).
	if err := Move(src, dst); err != nil {
		t.Fatalf("Move: %v", err)
	}
	got, _ := os.ReadFile(dst)
	if string(got) != "payload" {
		t.Errorf("Move dst content: got %q", got)
	}
}

// --- Fault-injection: CopyFile ---
//
// These tests swap package-level OS hooks (see fault_hooks.go) to
// exercise defensive error branches that real I/O can't easily
// provoke. None call t.Parallel; the hooks are package-global.

func TestFault_CopyFile_SyncError(t *testing.T) {
	h := newFaultyHooks(t)
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	if err := os.WriteFile(src, []byte("x"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}
	h.failSyncAlways()
	err := CopyFile(src, filepath.Join(dir, "dst"))
	if !errors.Is(err, errInjected) {
		t.Errorf("got %v, want errInjected", err)
	}
}

func TestFault_CopyFile_CloseError(t *testing.T) {
	h := newFaultyHooks(t)
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	if err := os.WriteFile(src, []byte("x"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}
	h.failCloseAlways()
	err := CopyFile(src, filepath.Join(dir, "dst"))
	if !errors.Is(err, errInjected) {
		t.Errorf("got %v, want errInjected", err)
	}
}

func TestFault_CopyFile_ChmodError(t *testing.T) {
	h := newFaultyHooks(t)
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	if err := os.WriteFile(src, []byte("x"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}
	h.failChmodAlways()
	err := CopyFile(src, filepath.Join(dir, "dst"))
	if !errors.Is(err, errInjected) {
		t.Errorf("got %v, want errInjected", err)
	}
}

func TestFault_CopyFile_ChtimesError(t *testing.T) {
	h := newFaultyHooks(t)
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	if err := os.WriteFile(src, []byte("x"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}
	h.failChtimesAlways()
	err := CopyFile(src, filepath.Join(dir, "dst"))
	if !errors.Is(err, errInjected) {
		t.Errorf("got %v, want errInjected", err)
	}
}

func TestFault_CopyFile_RenameError(t *testing.T) {
	h := newFaultyHooks(t)
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	if err := os.WriteFile(src, []byte("x"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}
	h.failRenameAlways()
	err := CopyFile(src, filepath.Join(dir, "dst"))
	if !errors.Is(err, errInjected) {
		t.Errorf("got %v, want errInjected", err)
	}
}

func TestFault_CopyFile_StatError(t *testing.T) {
	h := newFaultyHooks(t)
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	if err := os.WriteFile(src, []byte("x"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}
	h.failLstatAlways()
	err := CopyFile(src, filepath.Join(dir, "dst"))
	if !errors.Is(err, errInjected) {
		t.Errorf("got %v, want errInjected", err)
	}
}

func TestFault_CopyFile_FollowSymlinkStatError(t *testing.T) {
	h := newFaultyHooks(t)
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	if err := os.WriteFile(src, []byte("x"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}
	h.failStatAlways()
	err := CopyFile(src, filepath.Join(dir, "dst"), WithFollowSymlinks(true))
	if !errors.Is(err, errInjected) {
		t.Errorf("got %v, want errInjected", err)
	}
}

func TestFault_CopyFile_OpenSrcError(t *testing.T) {
	h := newFaultyHooks(t)
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	if err := os.WriteFile(src, []byte("x"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}
	h.failOpenAlways()
	err := CopyFile(src, filepath.Join(dir, "dst"))
	if !errors.Is(err, errInjected) {
		t.Errorf("got %v, want errInjected", err)
	}
}

func TestFault_CopyFile_ReadError(t *testing.T) {
	h := newFaultyHooks(t)
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	if err := os.WriteFile(src, []byte("payload"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}
	orig := fileReader
	t.Cleanup(func() { fileReader = orig })
	fileReader = func(*os.File) io.Reader { return iotest.ErrReader(errInjected) }
	_ = h

	err := CopyFile(src, filepath.Join(dir, "dst"))
	if !errors.Is(err, errInjected) {
		t.Errorf("got %v, want errInjected", err)
	}
}

// --- Fault-injection: copySymlink ---

func TestFault_CopySymlink_ReadlinkError(t *testing.T) {
	h := newFaultyHooks(t)
	dir := t.TempDir()
	src := filepath.Join(dir, "lnk")
	if err := os.Symlink("target", src); err != nil {
		t.Skipf("symlinks not supported: %v", err)
	}
	h.failReadlinkAlways()
	err := CopyFile(src, filepath.Join(dir, "dst"))
	if !errors.Is(err, errInjected) {
		t.Errorf("got %v, want errInjected", err)
	}
}

func TestFault_CopySymlink_SymlinkError(t *testing.T) {
	h := newFaultyHooks(t)
	dir := t.TempDir()
	src := filepath.Join(dir, "lnk")
	if err := os.Symlink("target", src); err != nil {
		t.Skipf("symlinks not supported: %v", err)
	}
	h.failSymlinkAlways()
	err := CopyFile(src, filepath.Join(dir, "dst"))
	if !errors.Is(err, errInjected) {
		t.Errorf("got %v, want errInjected", err)
	}
}

func TestFault_CopySymlink_RemoveError(t *testing.T) {
	h := newFaultyHooks(t)
	dir := t.TempDir()
	src := filepath.Join(dir, "lnk")
	dst := filepath.Join(dir, "dst")
	if err := os.Symlink("target", src); err != nil {
		t.Skipf("symlinks not supported: %v", err)
	}
	if err := os.Symlink("preexisting", dst); err != nil {
		t.Skipf("symlinks not supported: %v", err)
	}
	h.failRemoveAlways()
	err := CopyFile(src, dst, WithOverwrite(true))
	if !errors.Is(err, errInjected) {
		t.Errorf("got %v, want errInjected", err)
	}
}

// --- Fault-injection: CopyDir ---

func TestFault_CopyDir_StatError(t *testing.T) {
	h := newFaultyHooks(t)
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatalf("setup: %v", err)
	}
	h.failStatAlways()
	err := CopyDir(src, filepath.Join(dir, "dst"))
	if !errors.Is(err, errInjected) {
		t.Errorf("got %v, want errInjected", err)
	}
}

func TestFault_CopyDir_MkdirAllError(t *testing.T) {
	h := newFaultyHooks(t)
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatalf("setup: %v", err)
	}
	h.failMkdirAllAlways()
	err := CopyDir(src, filepath.Join(dir, "dst"))
	if !errors.Is(err, errInjected) {
		t.Errorf("got %v, want errInjected", err)
	}
}

func TestFault_CopyDir_SubdirMkdirError(t *testing.T) {
	h := newFaultyHooks(t)
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	dst := filepath.Join(dir, "dst")
	if err := os.MkdirAll(filepath.Join(src, "sub"), 0o755); err != nil {
		t.Fatalf("setup: %v", err)
	}

	// First MkdirAll succeeds (creates dst); second fails (the per-entry
	// subdir create inside the walk callback).
	calls := 0
	orig := osMkdirAll
	osMkdirAll = func(p string, m os.FileMode) error {
		calls++
		if calls == 1 {
			return orig(p, m)
		}
		return errInjected
	}
	_ = h

	err := CopyDir(src, dst)
	multi, ok := err.(*MultiError)
	if !ok {
		t.Fatalf("got %T, want *MultiError; err=%v", err, err)
	}
	if !errors.Is(multi, errInjected) {
		t.Errorf("multierror does not contain errInjected: %v", multi)
	}
}

func TestFault_CopyDir_PerEntryCopyError(t *testing.T) {
	h := newFaultyHooks(t)
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	dst := filepath.Join(dir, "dst")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if err := os.WriteFile(filepath.Join(src, "f"), []byte("xy"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}

	// Fail the first osOpen (CopyFile reads the source via osOpen).
	calls := 0
	orig := osOpen
	osOpen = func(p string) (*os.File, error) {
		calls++
		if calls == 1 {
			return nil, errInjected
		}
		return orig(p)
	}
	_ = h

	err := CopyDir(src, dst)
	if !errors.Is(err, errInjected) {
		t.Errorf("got %v, want errInjected", err)
	}
}

func TestFault_CopyDir_PerEntrySymlinkError(t *testing.T) {
	h := newFaultyHooks(t)
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	dst := filepath.Join(dir, "dst")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if err := os.Symlink("target", filepath.Join(src, "lnk")); err != nil {
		t.Skipf("symlinks not supported: %v", err)
	}
	h.failSymlinkAlways()

	err := CopyDir(src, dst)
	if !errors.Is(err, errInjected) {
		t.Errorf("got %v, want errInjected", err)
	}
}

// --- Fault-injection: Move (EXDEV fallback + post-EXDEV branches) ---

func TestFault_Move_CrossDeviceFallback(t *testing.T) {
	h := newFaultyHooks(t)
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	dst := filepath.Join(dir, "dst")
	if err := os.WriteFile(src, []byte("payload"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}

	// First osRename returns syscall.EXDEV; subsequent calls behave
	// normally so the CopyFile fallback's own internal rename can
	// commit the destination.
	calls := 0
	orig := osRename
	osRename = func(s, d string) error {
		calls++
		if calls == 1 {
			return exdevError()
		}
		return orig(s, d)
	}
	_ = h

	if err := Move(src, dst); err != nil {
		t.Fatalf("Move: %v", err)
	}
	got, _ := os.ReadFile(dst)
	if string(got) != "payload" {
		t.Errorf("dst: %q", got)
	}
	if Exists(src) {
		t.Error("src still present after Move fallback")
	}
}

func TestFault_Move_CrossDeviceDirFallback(t *testing.T) {
	h := newFaultyHooks(t)
	dir := t.TempDir()
	src := filepath.Join(dir, "srcdir")
	dst := filepath.Join(dir, "dstdir")
	if err := os.MkdirAll(filepath.Join(src, "sub"), 0o755); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if err := os.WriteFile(filepath.Join(src, "f"), []byte("payload"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}

	calls := 0
	orig := osRename
	osRename = func(s, d string) error {
		calls++
		if calls == 1 {
			return exdevError()
		}
		return orig(s, d)
	}
	_ = h

	if err := Move(src, dst); err != nil {
		t.Fatalf("Move: %v", err)
	}
	if Exists(src) {
		t.Error("src still present after dir Move fallback")
	}
	if !Exists(filepath.Join(dst, "f")) {
		t.Error("dst/f missing after fallback")
	}
}

func TestFault_Move_LstatErrorAfterEXDEV(t *testing.T) {
	h := newFaultyHooks(t)
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	if err := os.WriteFile(src, []byte("x"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}

	osRename = func(string, string) error { return exdevError() }
	h.failLstatAlways()

	err := Move(src, filepath.Join(dir, "dst"))
	if !errors.Is(err, errInjected) {
		t.Errorf("got %v, want errInjected", err)
	}
}

func TestFault_Move_CopyDirErrorAfterEXDEV(t *testing.T) {
	h := newFaultyHooks(t)
	dir := t.TempDir()
	src := filepath.Join(dir, "srcdir")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatalf("setup: %v", err)
	}

	// Force EXDEV on the outer rename, then break MkdirAll so CopyDir
	// fails before any per-entry work.
	osRename = func(string, string) error { return exdevError() }
	h.failMkdirAllAlways()

	err := Move(src, filepath.Join(dir, "dstdir"))
	if !errors.Is(err, errInjected) {
		t.Errorf("got %v, want errInjected", err)
	}
}

func TestFault_Move_CopyFileErrorAfterEXDEV(t *testing.T) {
	h := newFaultyHooks(t)
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	if err := os.WriteFile(src, []byte("x"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}

	osRename = func(string, string) error { return exdevError() }
	h.failOpenAlways()

	err := Move(src, filepath.Join(dir, "dst"))
	if !errors.Is(err, errInjected) {
		t.Errorf("got %v, want errInjected", err)
	}
}

func TestFault_Move_DirRemoveAllErrorAfterEXDEV(t *testing.T) {
	h := newFaultyHooks(t)
	dir := t.TempDir()
	src := filepath.Join(dir, "srcdir")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if err := os.WriteFile(filepath.Join(src, "f"), []byte("x"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}

	// EXDEV on outer; CopyDir succeeds; then RemoveAll fails because
	// the source dir is read-only. RemoveAll uses the stdlib path
	// directly (not hooked), so the chmod-readonly trick is the
	// portable way to provoke this branch.
	osRename = func(string, string) error { return exdevError() }
	if runtime.GOOS == goosWindows {
		t.Skip("readonly trick is POSIX-only")
	}
	if err := os.Chmod(src, 0o500); err != nil {
		t.Skipf("chmod: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(src, 0o755) })
	_ = h

	if err := Move(src, filepath.Join(dir, "dstdir")); err == nil {
		t.Skip("RemoveAll on readonly parent didn't fail on this filesystem")
	}
}

func TestFault_Move_RemoveErrorAfterCopy(t *testing.T) {
	h := newFaultyHooks(t)
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	if err := os.WriteFile(src, []byte("x"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}

	origRename := osRename
	osRename = func(s, d string) error {
		// Force EXDEV exactly once for the outer Move; subsequent
		// renames (used by CopyFile's atomic temp+rename) succeed.
		osRename = origRename
		return exdevError()
	}
	h.failRemoveAlways()

	err := Move(src, filepath.Join(dir, "dst"))
	if !errors.Is(err, errInjected) {
		t.Errorf("got %v, want errInjected", err)
	}
}

// --- CopyDir / Rename misc ---

func TestCopyDir_MissingDst(t *testing.T) {
	t.Parallel()
	src := t.TempDir()
	if err := os.WriteFile(filepath.Join(src, "f"), []byte("x"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}
	dst := filepath.Join(t.TempDir(), "deeply", "nested", "dst")
	if err := CopyDir(src, dst); err != nil {
		t.Fatalf("CopyDir: %v", err)
	}
	if !Exists(filepath.Join(dst, "f")) {
		t.Error("file not copied to deeply nested dst")
	}
}

func TestRename_MissingSrc(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	err := Rename(filepath.Join(dir, "missing"), filepath.Join(dir, "dst"))
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("got %v, want ErrNotFound", err)
	}
}

func TestCopyFile_NoWriteToReadonlyDir(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX perms only")
	}
	if os.Getuid() == 0 {
		t.Skip("root bypasses POSIX perms")
	}
	t.Parallel()
	root := t.TempDir()
	src := filepath.Join(root, "src")
	if err := os.WriteFile(src, []byte("x"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}
	dst := filepath.Join(root, "ro")
	if err := os.Mkdir(dst, 0o500); err != nil {
		t.Fatalf("setup: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(dst, 0o755) })

	if err := CopyFile(src, filepath.Join(dst, "f")); err == nil {
		t.Error("expected error copying into read-only dir")
	}
}

func TestCopyDir_IntoItsOwnSubtree(t *testing.T) {
	t.Parallel()
	src := t.TempDir()
	if err := os.WriteFile(filepath.Join(src, "f"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(src, "copy")
	if err := CopyDir(src, dst); err != nil {
		t.Fatalf("CopyDir: %v", err)
	}
	if got, _ := os.ReadFile(filepath.Join(dst, "f")); string(got) != "x" {
		t.Errorf("copy/f = %q; want x", got)
	}
	if Exists(filepath.Join(dst, "copy")) {
		t.Error("CopyDir copied the destination into itself")
	}
}

func TestCopyDir_ReadOnlySource(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("needs POSIX modes enforced")
	}
	t.Parallel()
	src := filepath.Join(t.TempDir(), "src")
	sub := filepath.Join(src, "sub")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sub, "f"), []byte("x"), 0o444); err != nil {
		t.Fatal(err)
	}
	for _, d := range []string{sub, src} {
		if err := os.Chmod(d, 0o555); err != nil {
			t.Fatal(err)
		}
	}
	dst := filepath.Join(t.TempDir(), "dst")
	t.Cleanup(func() {
		for _, d := range []string{src, sub, dst, filepath.Join(dst, "sub")} {
			_ = os.Chmod(d, 0o755)
		}
	})
	if err := CopyDir(src, dst); err != nil {
		t.Fatalf("CopyDir: %v", err)
	}
	if got, _ := os.ReadFile(filepath.Join(dst, "sub", "f")); string(got) != "x" {
		t.Errorf("sub/f = %q; want x", got)
	}
	for _, d := range []string{dst, filepath.Join(dst, "sub")} {
		info, err := os.Stat(d)
		if err != nil {
			t.Fatal(err)
		}
		if perm := info.Mode().Perm(); perm != 0o555 {
			t.Errorf("%s mode = %v; want 0555", d, perm)
		}
	}
}

func TestCopy_SyncOption(t *testing.T) {
	h := newFaultyHooks(t)
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	if err := os.MkdirAll(filepath.Join(src, "a"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, n := range []string{"x", "y", filepath.Join("a", "z")} {
		if err := os.WriteFile(filepath.Join(src, n), []byte(n), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	syncs := 0
	fileSync = func(f *os.File) error {
		syncs++
		return h.origFileSync(f)
	}

	if err := CopyDir(src, filepath.Join(dir, "off"), WithCopySync(false)); err != nil {
		t.Fatal(err)
	}
	if syncs != 0 {
		t.Errorf("WithCopySync(false): %d syncs; want 0", syncs)
	}

	syncs = 0
	if err := CopyDir(src, filepath.Join(dir, "on")); err != nil {
		t.Fatal(err)
	}
	// Three files, plus the two destination directories and the
	// directory holding dst, on systems that sync directories.
	want := 3
	if runtime.GOOS != "windows" {
		want += 3
	}
	if syncs != want {
		t.Errorf("default: %d syncs; want %d", syncs, want)
	}

	syncs = 0
	if err := CopyFile(filepath.Join(src, "x"), filepath.Join(dir, "x2"), WithCopySync(false)); err != nil {
		t.Fatal(err)
	}
	if syncs != 0 {
		t.Errorf("CopyFile WithCopySync(false): %d syncs; want 0", syncs)
	}
}
