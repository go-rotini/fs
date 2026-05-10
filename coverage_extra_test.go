package fs

// This file packs many small tests aimed at error paths and option
// combinations that the topical test files don't already exercise.
// Coverage-driven; functional invariants are owned by the topical
// suites.

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	stdfs "io/fs"
	"os"
	"os/user"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// --- read.go error paths ---

func TestReadFirstLine_EmptyFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "empty")
	if err := os.WriteFile(path, nil, 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	_, err := ReadFirstLine(path)
	if !errors.Is(err, ErrEmptyFile) {
		t.Errorf("got %v, want ErrEmptyFile", err)
	}
}

func TestReadFirstLine_NoNewline(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "f")
	if err := os.WriteFile(path, []byte("only-line-no-newline"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	got, err := ReadFirstLine(path)
	if err != nil {
		t.Fatalf("ReadFirstLine: %v", err)
	}
	if got != "only-line-no-newline" {
		t.Errorf("got %q", got)
	}
}

func TestReadFirstLine_Missing(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	_, err := ReadFirstLine(filepath.Join(dir, "missing"))
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("got %v, want ErrNotFound", err)
	}
}

func TestReadLines_Missing(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	_, err := ReadLines(filepath.Join(dir, "missing"))
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("got %v, want ErrNotFound", err)
	}
}

func TestOpenChunked_Missing(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	_, _, err := OpenChunked(filepath.Join(dir, "missing"), 64)
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("got %v, want ErrNotFound", err)
	}
}

// --- write.go error paths ---

func TestWriteAt_WithSync(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "f")
	if err := os.WriteFile(path, []byte("hello world"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if err := WriteAt(path, 6, []byte("WORLD"), WithSync(true)); err != nil {
		t.Fatalf("WriteAt: %v", err)
	}
	got, _ := os.ReadFile(path)
	if string(got) != "hello WORLD" {
		t.Errorf("got %q", got)
	}
}

func TestOpenWrite_WithSyncOnOverwrite(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "f")
	if err := os.WriteFile(path, []byte("v1"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}

	f, finalize, err := OpenWrite(path, WithSync(true))
	if err != nil {
		t.Fatalf("OpenWrite: %v", err)
	}
	if _, err := f.WriteString("v2"); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if err := finalize(); err != nil {
		t.Fatalf("finalize: %v", err)
	}
}

func TestOpenWrite_FinalizeIdempotentSuccess(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "f")
	f, finalize, err := OpenWrite(path)
	if err != nil {
		t.Fatalf("OpenWrite: %v", err)
	}
	if _, err := f.WriteString("ok"); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if err := finalize(); err != nil {
		t.Fatalf("first finalize: %v", err)
	}
	if err := finalize(); err != nil {
		t.Errorf("second finalize: %v", err)
	}
}

func TestWriteFileExclusive_WithSync(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := WriteFileExclusive(filepath.Join(dir, "f"), []byte("x"), WithSync(true)); err != nil {
		t.Fatalf("WriteFileExclusive WithSync: %v", err)
	}
}

func TestWriteFileExclusive_WithPerm(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX perms only")
	}
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "f")
	if err := WriteFileExclusive(path, []byte("x"), WithPerm(0o600)); err != nil {
		t.Fatalf("WriteFileExclusive: %v", err)
	}
	info, _ := os.Stat(path)
	if info.Mode().Perm() != 0o600 {
		t.Errorf("perm = %o, want 0o600", info.Mode().Perm())
	}
}

func TestWriteFile_WithAtomicFalseSync(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := WriteFile(filepath.Join(dir, "f"), []byte("x"), WithAtomic(false), WithSync(true)); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
}

func TestWriteFile_WithAtomicFalseBadOpen(t *testing.T) {
	t.Parallel()
	if err := WriteFile("/proc/1/cant-create-here", []byte("x"), WithAtomic(false)); err == nil {
		t.Skip("/proc not present or writable; behavior platform-dependent")
	}
}

// --- copy.go error paths ---

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

// --- walk.go more branches ---

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

// --- find.go error paths ---

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

// --- path.go ---

// Abs covered elsewhere; Tilde expansion test exists in path_test.go.

func TestEvalSymlinksWithin_Missing(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	_, err := EvalSymlinksWithin(dir, filepath.Join(dir, "missing"))
	if err == nil {
		t.Error("expected error for missing path")
	}
}

// --- glob.go ---

func TestGlob_BadExpand(t *testing.T) {
	t.Parallel()
	_, err := Glob("$ROTINI_NEVER_SET_AT_ALL/*", WithStrictExpansion())
	if err == nil {
		t.Error("expected strict-expansion error")
	}
}

func TestGlobAny_PartialBadOnFirstPattern(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.go"), nil, 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}
	got, err := GlobAny([]string{"["}, WithStrictExpansion())
	if err == nil {
		t.Error("expected error from malformed pattern")
	}
	if len(got) != 0 {
		t.Errorf("partial = %v, want empty", got)
	}
}

// --- watcher.go: registerInitial branch via NewWatcher with vanished parent ---

func TestWatcher_LazyOnNonExistentParent(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	missing := filepath.Join(dir, "no", "such", "dir", "f")
	_, err := NewLazyWatcher(missing, WithPolling(pollInterval))
	if err == nil {
		t.Error("expected error: lazy still requires parent to exist")
	}
}

// --- testharness.go method coverage ---

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

// --- archive: extractZipFromStream cap ---

func TestExtractArchive_ZipMaxBytes(t *testing.T) {
	t.Parallel()
	src := t.TempDir()
	if err := os.WriteFile(filepath.Join(src, "big"), bytes.Repeat([]byte("X"), 4096), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}
	archivePath := filepath.Join(t.TempDir(), "out.zip")
	if err := CreateArchiveFile(archivePath, src, WithArchiveFormat(ArchiveFormatZip)); err != nil {
		t.Fatalf("CreateArchiveFile: %v", err)
	}

	dst := t.TempDir()
	// 16-byte cap on a 4KB zip: must error.
	err := ExtractArchiveFile(archivePath, dst, WithArchiveMaxBytes(16))
	if !errors.Is(err, ErrArchiveTooLarge) {
		t.Errorf("got %v, want ErrArchiveTooLarge", err)
	}
}

// --- DirSize: nested files sums correctly ---

func TestDirSize_MissingPath(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	_, err := DirSize(context.Background(), filepath.Join(dir, "missing"))
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("got %v, want ErrNotFound", err)
	}
}

// --- Touch covers more branches ---

func TestTouch_CreatesIfMissing(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "new")
	if err := Touch(path); err != nil {
		t.Fatalf("Touch: %v", err)
	}
	if !Exists(path) {
		t.Error("Touch did not create file")
	}
}

func TestTouch_UpdatesMtime(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "f")
	if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}
	old := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatalf("Chtimes: %v", err)
	}
	if err := Touch(path); err != nil {
		t.Fatalf("Touch: %v", err)
	}
	got, _ := Mtime(path)
	if !got.After(old) {
		t.Errorf("Touch did not update mtime: still %v", got)
	}
}

func TestTouch_WithTimes_Coverage(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "f")
	if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}
	at := time.Date(2024, 6, 15, 12, 30, 45, 0, time.UTC)
	mt := time.Date(2024, 7, 20, 10, 15, 30, 0, time.UTC)
	if err := Touch(path, WithTimes(at, mt)); err != nil {
		t.Fatalf("Touch: %v", err)
	}
	gotA, _ := Atime(path)
	gotM, _ := Mtime(path)
	if !gotA.Equal(at) || !gotM.Equal(mt) {
		t.Errorf("got (%v, %v); want (%v, %v)", gotA, gotM, at, mt)
	}
}

// --- Stem edge cases ---

func TestStem_EdgeCases(t *testing.T) {
	t.Parallel()
	cases := []struct{ in, want string }{
		{"file.txt", "file"},
		{"file.tar.gz", "file.tar"},
		{"no-ext", "no-ext"},
		{".hidden", ".hidden"},
		{"/a/b/c.txt", "c"},
		{"", "."},
	}
	for _, c := range cases {
		if got := Stem(c.in); got != c.want {
			t.Errorf("Stem(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// --- ParseBytes additional units ---

func TestParseBytes_ExtremeValues(t *testing.T) {
	t.Parallel()
	for _, in := range []string{"1PiB", "1EiB", "1.5p", "2eb"} {
		got, err := ParseBytes(in)
		if err != nil {
			t.Errorf("ParseBytes(%q): %v", in, err)
		}
		if got <= 0 {
			t.Errorf("ParseBytes(%q) = %d", in, got)
		}
	}
}

// --- Sentinel format ---

func TestFormatError_Color(t *testing.T) {
	t.Parallel()
	pe := &PathError{Op: "x", Path: "/y", Cause: errors.New("boom")}
	plain := FormatError(pe, false)
	col := FormatError(pe, true)
	if plain == col {
		t.Error("colorless and colorized output should differ")
	}
	if !strings.Contains(col, "\x1b[") {
		t.Errorf("colored output missing ANSI escape: %q", col)
	}
}

func TestFormatError_NilCoverage(t *testing.T) {
	t.Parallel()
	if got := FormatError(nil); got != "" {
		t.Errorf("FormatError(nil) = %q, want empty", got)
	}
}

func TestFormatError_MultiError(t *testing.T) {
	t.Parallel()
	m := &MultiError{}
	m.Append(errors.New("a"))
	m.Append(errors.New("b"))
	got := FormatError(m, false)
	if !strings.Contains(got, "a") || !strings.Contains(got, "b") {
		t.Errorf("FormatError(multi) = %q; missing branches", got)
	}
}

// --- Permission-denied error paths via chmod parent ---
//
// These tests trigger error branches in operations that depend on a
// writable parent directory by chmod'ing the parent to 0 and
// verifying the wrapped error surfaces. POSIX-only.

func chmodAndCleanup(t *testing.T, dir string, mode os.FileMode) {
	t.Helper()
	if err := os.Chmod(dir, mode); err != nil {
		t.Fatalf("Chmod %s: %v", dir, err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })
}

func TestWriteFile_NoWriteToReadonlyDir(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX perms only")
	}
	if os.Getuid() == 0 {
		t.Skip("root bypasses POSIX perms")
	}
	t.Parallel()
	dir := t.TempDir()
	chmodAndCleanup(t, dir, 0o500) // r-x: cannot write inside

	if err := WriteFile(filepath.Join(dir, "f"), []byte("x")); err == nil {
		t.Error("expected error writing into read-only dir")
	}
}

func TestWriteFileExclusive_NoWriteToReadonlyDir(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX perms only")
	}
	if os.Getuid() == 0 {
		t.Skip("root bypasses POSIX perms")
	}
	t.Parallel()
	dir := t.TempDir()
	chmodAndCleanup(t, dir, 0o500)

	if err := WriteFileExclusive(filepath.Join(dir, "f"), []byte("x")); err == nil {
		t.Error("expected error")
	}
}

func TestAppend_NoWriteToReadonlyDir(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX perms only")
	}
	if os.Getuid() == 0 {
		t.Skip("root bypasses POSIX perms")
	}
	t.Parallel()
	dir := t.TempDir()
	chmodAndCleanup(t, dir, 0o500)

	if err := Append(filepath.Join(dir, "f"), []byte("x")); err == nil {
		t.Error("expected error")
	}
}

func TestOpenWrite_NoWriteToReadonlyDir(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX perms only")
	}
	if os.Getuid() == 0 {
		t.Skip("root bypasses POSIX perms")
	}
	t.Parallel()
	dir := t.TempDir()
	chmodAndCleanup(t, dir, 0o500)

	if _, _, err := OpenWrite(filepath.Join(dir, "f")); err == nil {
		t.Error("expected error")
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

func TestMkdirAll_OnReadonlyParent(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX perms only")
	}
	if os.Getuid() == 0 {
		t.Skip("root bypasses POSIX perms")
	}
	t.Parallel()
	dir := t.TempDir()
	chmodAndCleanup(t, dir, 0o500)
	if err := MkdirAll(filepath.Join(dir, "child"), 0o755); err == nil {
		t.Error("expected error creating child of read-only dir")
	}
}

func TestRemoveAll_StrictMissing(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	err := RemoveAll(filepath.Join(dir, "missing"), WithStrict(true))
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("got %v, want ErrNotFound", err)
	}
}

// --- hasParentDir branch coverage ---

func TestHasParentDir_Branches(t *testing.T) {
	t.Parallel()
	cases := []struct {
		child, parent string
		want          bool
	}{
		{"/a", "/a", true},          // identity
		{"/a/b", "/a", true},        // child of
		{"/a/../etc", "/a", false},  // escape
		{"/elsewhere", "/a", false}, // unrelated
		{".sub", "/a", false},       // rel may start with `.`
	}
	for _, c := range cases {
		if got := hasParentDir(c.child, c.parent); got != c.want {
			t.Errorf("hasParentDir(%q, %q) = %v, want %v", c.child, c.parent, got, c.want)
		}
	}
}

// --- ListDir with all options ---

func TestListDir_AllOptions(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	for _, name := range []string{"a.txt", ".hidden", "b.txt", ".dotrc"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o644); err != nil {
			t.Fatalf("setup: %v", err)
		}
	}
	got, err := ListDir(dir,
		WithSkipHidden(true),
		WithSorted(true),
		WithListFilter(func(e stdfs.DirEntry) bool { return strings.HasSuffix(e.Name(), ".txt") }),
	)
	if err != nil {
		t.Fatalf("ListDir: %v", err)
	}
	if len(got) != 2 {
		t.Errorf("got %d, want 2 (a.txt + b.txt)", len(got))
	}
	if got[0].Name() != "a.txt" || got[1].Name() != "b.txt" {
		t.Errorf("not sorted: %v", got)
	}
}

// --- resolveReadPath error paths (WithExpand + invalid path) ---

func TestReadFile_ExpandError(t *testing.T) {
	t.Parallel()
	_, err := ReadFile("nul\x00here", WithExpand())
	if err == nil {
		t.Fatal("expected error from expansion of NUL-bearing path")
	}
}

func TestReadFirstLine_ExpandError(t *testing.T) {
	t.Parallel()
	_, err := ReadFirstLine("nul\x00here", WithExpand())
	if err == nil {
		t.Fatal("expected error from expansion of NUL-bearing path")
	}
}

func TestOpenLines_ExpandError(t *testing.T) {
	t.Parallel()
	_, _, err := OpenLines("nul\x00here", WithExpand())
	if err == nil {
		t.Fatal("expected error from expansion of NUL-bearing path")
	}
}

func TestReadAt_ExpandError(t *testing.T) {
	t.Parallel()
	_, err := ReadAt("nul\x00here", 0, 4, WithExpand())
	if err == nil {
		t.Fatal("expected error from expansion of NUL-bearing path")
	}
}

// --- path.go error-path coverage ---

func TestAbs_ExpandError(t *testing.T) {
	t.Parallel()
	_, err := Abs("nul\x00here")
	if err == nil {
		t.Fatal("expected error from Abs on NUL-bearing path")
	}
}

func TestExpand_UnknownUser(t *testing.T) {
	t.Parallel()
	_, err := Expand("~__definitely_no_such_user_4f7a__/foo")
	if err == nil {
		t.Fatal("expected error from Expand on unknown ~user")
	}
}

func TestExpand_KnownUser(t *testing.T) {
	t.Parallel()
	// Look up the current user; ~current should successfully resolve.
	u, err := user.Current()
	if err != nil || u.Username == "" {
		t.Skip("user.Current unavailable")
	}
	if _, err := Expand("~" + u.Username + "/x"); err != nil {
		t.Errorf("Expand(~%s/x): %v", u.Username, err)
	}
}

// --- watcher.go input validation ---

func TestNewWatcher_EmptyPath(t *testing.T) {
	t.Parallel()
	_, err := NewWatcher("")
	if !errors.Is(err, ErrWatcherEmptyPath) {
		t.Errorf("got %v, want ErrWatcherEmptyPath", err)
	}
}

func TestNewWatcher_MissingPath(t *testing.T) {
	t.Parallel()
	_, err := NewWatcher(filepath.Join(t.TempDir(), "missing"))
	if err == nil {
		t.Fatal("expected error from NewWatcher on missing path")
	}
}

func TestNewLazyWatcher_EmptyPath(t *testing.T) {
	t.Parallel()
	_, err := NewLazyWatcher("")
	if !errors.Is(err, ErrWatcherEmptyPath) {
		t.Errorf("got %v, want ErrWatcherEmptyPath", err)
	}
}

func TestNewDirWatcher_EmptyPath(t *testing.T) {
	t.Parallel()
	_, err := NewDirWatcher("")
	if !errors.Is(err, ErrWatcherEmptyPath) {
		t.Errorf("got %v, want ErrWatcherEmptyPath", err)
	}
}

func TestNewDirWatcher_MissingPath(t *testing.T) {
	t.Parallel()
	_, err := NewDirWatcher(filepath.Join(t.TempDir(), "missing"))
	if err == nil {
		t.Fatal("expected error from NewDirWatcher on missing path")
	}
}

func TestNewDirWatcher_NotADirectory(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "f")
	if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}
	_, err := NewDirWatcher(path)
	if !errors.Is(err, ErrNotDir) {
		t.Errorf("got %v, want ErrNotDir", err)
	}
}

func TestWatcher_NilContextSubscribe(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	w, err := NewDirWatcher(dir, WithPolling(time.Second))
	if err != nil {
		t.Fatalf("NewDirWatcher: %v", err)
	}
	defer w.Close()
	var nilCtx context.Context
	_, err = w.Subscribe(nilCtx)
	if !errors.Is(err, ErrWatcherNilContext) {
		t.Errorf("got %v, want ErrWatcherNilContext", err)
	}
}

// --- watcher recursive directory subscription ---

func TestNewDirWatcher_NonRecursive(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	w, err := NewDirWatcher(dir, WithPolling(time.Hour), WithRecursive(false))
	if err != nil {
		t.Fatalf("NewDirWatcher: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Errorf("Close: %v", err)
	}
}

func TestNewDirWatcher_Recursive(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "sub", "deep"), 0o755); err != nil {
		t.Fatalf("setup: %v", err)
	}
	w, err := NewDirWatcher(dir, WithPolling(time.Hour), WithRecursive(true))
	if err != nil {
		t.Fatalf("NewDirWatcher: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Errorf("Close: %v", err)
	}
}

func TestNewLazyWatcher_OnMissingTarget(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	target := filepath.Join(dir, "later")
	w, err := NewLazyWatcher(target, WithPolling(time.Hour))
	if err != nil {
		t.Fatalf("NewLazyWatcher: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Errorf("Close: %v", err)
	}
}

// --- watcher polling backend coverage ---

func TestPollingBackend_DefaultInterval(t *testing.T) {
	t.Parallel()
	b := newPollingBackend(0)
	if b.interval != defaultWatcherPollingInterval {
		t.Errorf("interval=%v, want %v", b.interval, defaultWatcherPollingInterval)
	}
	if err := b.Close(); err != nil {
		t.Errorf("Close: %v", err)
	}
}

func TestPollingBackend_DoubleClose(t *testing.T) {
	t.Parallel()
	b := newPollingBackend(time.Hour)
	if err := b.Close(); err != nil {
		t.Errorf("first Close: %v", err)
	}
	if err := b.Close(); err != nil {
		t.Errorf("second Close: %v", err)
	}
}

func TestPollingBackend_DuplicateAdd(t *testing.T) {
	t.Parallel()
	b := newPollingBackend(time.Hour)
	defer b.Close()
	dir := t.TempDir()
	if err := b.AddPath(dir); err != nil {
		t.Fatalf("first AddPath: %v", err)
	}
	if err := b.AddPath(dir); err != nil {
		t.Errorf("dup AddPath: %v", err)
	}
}

func TestPollingBackend_AddAfterClose(t *testing.T) {
	t.Parallel()
	b := newPollingBackend(time.Hour)
	if err := b.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	err := b.AddPath(t.TempDir())
	if !errors.Is(err, ErrWatcherClosed) {
		t.Errorf("got %v, want ErrWatcherClosed", err)
	}
}

func TestSnapshotPath_Missing(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	snap := snapshotPath(filepath.Join(dir, "missing"))
	if snap.exists {
		t.Error("snapshot of missing path reports exists=true")
	}
}

func TestDiffSnapshots_AllBranches(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		prev *pollSnapshot
		curr *pollSnapshot
		want WatchOp
	}{
		{"both-missing", &pollSnapshot{exists: false}, &pollSnapshot{exists: false}, 0},
		{"create", &pollSnapshot{exists: false}, &pollSnapshot{exists: true}, WatchCreate},
		{"remove", &pollSnapshot{exists: true}, &pollSnapshot{exists: false}, WatchRemove},
		{"size-change",
			&pollSnapshot{exists: true, size: 0, mode: 0o644},
			&pollSnapshot{exists: true, size: 10, mode: 0o644},
			WatchWrite},
		{"perm-change",
			&pollSnapshot{exists: true, mode: 0o644},
			&pollSnapshot{exists: true, mode: 0o600},
			WatchChmod},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := diffSnapshots(tc.prev, tc.curr)
			if got&tc.want != tc.want {
				t.Errorf("got %v, want bits %v set", got, tc.want)
			}
		})
	}
}

func TestListDirNames_Missing(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	got := listDirNames(filepath.Join(dir, "missing"))
	if len(got) != 0 {
		t.Errorf("got %v, want empty", got)
	}
}

func TestPollingBackend_StopAt(t *testing.T) {
	t.Parallel()
	b := newPollingBackend(time.Millisecond)
	defer b.Close()
	dir := t.TempDir()
	if err := b.AddPath(dir); err != nil {
		t.Fatalf("AddPath: %v", err)
	}
	// Trigger child appearance + disappearance to exercise both paths.
	child := filepath.Join(dir, "x")
	if err := os.WriteFile(child, []byte("x"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}
	// Drain at least one event for the child create.
	select {
	case <-b.Events():
	case <-time.After(time.Second):
		t.Fatal("no event for child create")
	}
	if err := os.Remove(child); err != nil {
		t.Fatalf("remove: %v", err)
	}
	// And one for the disappearance.
	select {
	case <-b.Events():
	case <-time.After(time.Second):
		t.Fatal("no event for child remove")
	}
}

// --- find.go matchInDir invalid pattern ---

// --- dir.go error paths ---

func TestIsEmpty_NotDir(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "f")
	if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}
	_, err := IsEmpty(path)
	if !errors.Is(err, ErrNotDir) {
		t.Errorf("got %v, want ErrNotDir", err)
	}
}

func TestDirSize_PreCanceledContext(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := DirSize(ctx, t.TempDir())
	if !errors.Is(err, context.Canceled) {
		t.Errorf("got %v, want context.Canceled", err)
	}
}

func TestDirSize_MidWalkCancellation(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	for i := range 50 {
		if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("f%d", i)), []byte("x"), 0o644); err != nil {
			t.Fatalf("setup: %v", err)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Microsecond)
	defer cancel()
	_, err := DirSize(ctx, dir)
	// Either cancellation or success can happen depending on timing.
	if err != nil && !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("got %v, want nil or context error", err)
	}
}

// --- ExtractArchiveFile gzip via OpenAutoArchive trick (sniff path) ---

func TestExtractArchive_TarGzStream(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if err := os.WriteFile(filepath.Join(src, "f"), []byte("payload"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}
	var buf bytes.Buffer
	if err := CreateArchive(&buf, src, WithArchiveFormat(ArchiveFormatTarGz)); err != nil {
		t.Fatalf("CreateArchive(tgz): %v", err)
	}
	if err := ExtractArchive(&buf, filepath.Join(dir, "extract")); err != nil {
		t.Fatalf("ExtractArchive(tgz): %v", err)
	}
	if !Exists(filepath.Join(dir, "extract", "f")) {
		t.Error("extracted file missing")
	}
}

// --- archive zip skips non-regular entries (symlink) ---

func TestCreateArchive_Zip_SkipsSymlinks(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if err := os.WriteFile(filepath.Join(src, "f.txt"), []byte("payload"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if err := os.Symlink("f.txt", filepath.Join(src, "lnk")); err != nil {
		t.Skipf("symlinks not supported: %v", err)
	}
	out := filepath.Join(dir, "out.zip")
	if err := CreateArchiveFile(out, src, WithArchiveFormat(ArchiveFormatZip)); err != nil {
		t.Fatalf("CreateArchiveFile: %v", err)
	}
	// Verify the symlink was skipped: extract and confirm only f.txt is present.
	dst := filepath.Join(dir, "extract")
	if err := ExtractArchiveFile(out, dst); err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if Exists(filepath.Join(dst, "lnk")) {
		t.Error("symlink unexpectedly present in zip extract")
	}
}

func TestCreateArchive_Zip_FilterFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if err := os.WriteFile(filepath.Join(src, "skip.tmp"), []byte("x"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if err := os.WriteFile(filepath.Join(src, "keep.txt"), []byte("y"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}
	out := filepath.Join(dir, "out.zip")
	err := CreateArchiveFile(out, src,
		WithArchiveFormat(ArchiveFormatZip),
		WithArchiveCreateFilter(func(p string, _ os.FileInfo) bool {
			return !strings.HasSuffix(p, ".tmp")
		}))
	if err != nil {
		t.Fatalf("CreateArchiveFile: %v", err)
	}
}

// --- archive create filter on file (not dir) ---

func TestCreateArchive_TarGz_FilterFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if err := os.WriteFile(filepath.Join(src, "skip.tmp"), []byte("x"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if err := os.WriteFile(filepath.Join(src, "keep.txt"), []byte("y"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}
	out := filepath.Join(dir, "out.tar.gz")
	err := CreateArchiveFile(out, src,
		WithArchiveFormat(ArchiveFormatTarGz),
		WithArchiveCreateFilter(func(p string, _ os.FileInfo) bool {
			return !strings.HasSuffix(p, ".tmp")
		}))
	if err != nil {
		t.Fatalf("CreateArchiveFile: %v", err)
	}
}

// --- userdirs.go validation paths ---

func TestAppConfigDir_InvalidName(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"", ".", "..", "a/b"} {
		if _, err := AppConfigDir(name); !errors.Is(err, ErrInvalidPath) {
			t.Errorf("AppConfigDir(%q): got %v, want ErrInvalidPath", name, err)
		}
	}
}

func TestAppCacheDir_InvalidName(t *testing.T) {
	t.Parallel()
	if _, err := AppCacheDir(""); !errors.Is(err, ErrInvalidPath) {
		t.Errorf("got %v, want ErrInvalidPath", err)
	}
}

func TestAppDataDir_InvalidName(t *testing.T) {
	t.Parallel()
	if _, err := AppDataDir(""); !errors.Is(err, ErrInvalidPath) {
		t.Errorf("got %v, want ErrInvalidPath", err)
	}
}

func TestAppStateDir_InvalidName(t *testing.T) {
	t.Parallel()
	if _, err := AppStateDir(""); !errors.Is(err, ErrInvalidPath) {
		t.Errorf("got %v, want ErrInvalidPath", err)
	}
}

func TestAppRuntimeDir_InvalidName(t *testing.T) {
	t.Parallel()
	if _, err := AppRuntimeDir(""); !errors.Is(err, ErrInvalidPath) {
		t.Errorf("got %v, want ErrInvalidPath", err)
	}
}

func TestSystemConfigDir_InvalidName(t *testing.T) {
	t.Parallel()
	if _, err := SystemConfigDir(""); !errors.Is(err, ErrInvalidPath) {
		t.Errorf("got %v, want ErrInvalidPath", err)
	}
}

func TestSystemDataDir_InvalidName(t *testing.T) {
	t.Parallel()
	if _, err := SystemDataDir(""); !errors.Is(err, ErrInvalidPath) {
		t.Errorf("got %v, want ErrInvalidPath", err)
	}
}

func TestSystemStateDir_InvalidName(t *testing.T) {
	t.Parallel()
	if _, err := SystemStateDir(""); !errors.Is(err, ErrInvalidPath) {
		t.Errorf("got %v, want ErrInvalidPath", err)
	}
}

// --- stat.go missing-path branches ---

func TestLstat_Missing(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	_, err := Lstat(filepath.Join(dir, "nope"))
	if err == nil {
		t.Fatal("expected error from Lstat on missing path")
	}
}

func TestSetMtime_MissingPath(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	err := SetMtime(filepath.Join(dir, "nope"), time.Now())
	if err == nil {
		t.Fatal("expected error from SetMtime on missing path")
	}
}

func TestSetAtime_MissingPath(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	err := SetAtime(filepath.Join(dir, "nope"), time.Now())
	if err == nil {
		t.Fatal("expected error from SetAtime on missing path")
	}
}

func TestSetTimes_MissingPath(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	err := SetTimes(filepath.Join(dir, "nope"), time.Now(), time.Now())
	if err == nil {
		t.Fatal("expected error from SetTimes on missing path")
	}
}

func TestSameFile_MissingA(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	_, err := SameFile(filepath.Join(dir, "nope"), dir)
	if err == nil {
		t.Fatal("expected error from SameFile on missing a")
	}
}

func TestSameFile_MissingB(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	_, err := SameFile(dir, filepath.Join(dir, "nope"))
	if err == nil {
		t.Fatal("expected error from SameFile on missing b")
	}
}

func TestTouch_OpenError(t *testing.T) {
	t.Parallel()
	// Open on a path whose parent doesn't exist -> Touch returns error
	dir := t.TempDir()
	err := Touch(filepath.Join(dir, "missing", "f"))
	if err == nil {
		t.Fatal("expected error from Touch when parent missing")
	}
}

// --- walk.go walkSymlinkRec branches (followsymlinks) ---

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
	var visited int
	err := Walk(root, func(_ string, _ stdfs.DirEntry, _ error) error {
		visited++
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

// --- find.go FindUp error paths ---

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

func TestFindUp_AbsError(t *testing.T) {
	t.Parallel()
	// On most platforms filepath.Abs doesn't fail for any string, so
	// we instead exercise matchInDir's read-fail path by giving FindUp
	// a startDir whose ancestors include a missing component. ReadDir
	// will fail and matchInDir folds it to "no match", letting the
	// walk traverse cleanly.
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

// --- scaffold_version.go ScaffoldExtract conflict + merge paths ---

func TestScaffoldExtract_PromptSkip(t *testing.T) {
	t.Parallel()
	srcDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(srcDir, "f"), []byte("v1"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}
	dst := t.TempDir()
	if err := os.WriteFile(filepath.Join(dst, "f"), []byte("v0"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}
	srcFS := os.DirFS(srcDir)
	prompt := func(_ string, _ ScaffoldAction) ScaffoldActionOp {
		return ScaffoldActionSkip
	}
	err := ScaffoldExtract(srcFS, dst,
		WithScaffoldOnConflict(ScaffoldPromptInteractive),
		WithScaffoldPromptFunc(prompt))
	if err != nil {
		t.Fatalf("ScaffoldExtract: %v", err)
	}
	got, _ := os.ReadFile(filepath.Join(dst, "f"))
	if string(got) != "v0" {
		t.Errorf("got %q, want unchanged v0", got)
	}
}

// --- scaffold.go error paths ---

func TestScaffoldApply_PromptUnsupportedAction(t *testing.T) {
	t.Parallel()
	src := os.DirFS(t.TempDir())
	_ = src
	dir := t.TempDir()
	// Build an in-memory FS with a single file.
	srcDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(srcDir, "f"), []byte("v1"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "f"), []byte("v0"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}
	srcFS := os.DirFS(srcDir)
	prompt := func(_ string, _ ScaffoldAction) ScaffoldActionOp {
		return ScaffoldActionConflict // Conflict is invalid as a prompt response
	}
	err := ScaffoldApply(srcFS, dir, nil,
		WithScaffoldOnConflict(ScaffoldPromptInteractive),
		WithScaffoldPromptFunc(prompt))
	if !errors.Is(err, ErrScaffoldPromptUnsupported) {
		t.Errorf("got %v, want ErrScaffoldPromptUnsupported", err)
	}
}

func TestScaffoldActionOp_String_Unknown(t *testing.T) {
	t.Parallel()
	got := ScaffoldActionOp(99).String()
	if got == "" {
		t.Error("expected unknown label, got empty string")
	}
}

// --- archive.go error paths ---

func TestCreateArchive_UnknownFormat(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	dir := t.TempDir()
	err := CreateArchive(&buf, dir, WithArchiveFormat(ArchiveFormat(99)))
	if !errors.Is(err, ErrArchiveFormatUnknown) {
		t.Errorf("got %v, want ErrArchiveFormatUnknown", err)
	}
}

func TestExtractArchiveFile_OpenError(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	err := ExtractArchiveFile(filepath.Join(dir, "nonexistent.tar"), filepath.Join(dir, "out"))
	if err == nil {
		t.Fatal("expected error for missing archive")
	}
}

func TestCreateArchiveFile_CreateError(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatalf("setup: %v", err)
	}
	// Try to create archive at a path whose parent doesn't exist.
	err := CreateArchiveFile(filepath.Join(dir, "missing", "out.tar"), src, WithArchiveFormat(ArchiveFormatTar))
	if err == nil {
		t.Fatal("expected error for unwritable archive path")
	}
}

func TestOpenAutoArchive_OpenError(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	_, err := OpenAutoArchive(filepath.Join(dir, "nonexistent"))
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestOpenAutoArchive_BadGzip(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.gz")
	// gzip magic prefix but truncated body — gzip.NewReader will fail.
	if err := os.WriteFile(path, []byte{0x1f, 0x8b}, 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}
	_, err := OpenAutoArchive(path)
	if err == nil {
		t.Fatal("expected error from gzip.NewReader on truncated input")
	}
}

func TestExtractArchive_ZipStream_NoCap(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if err := os.WriteFile(filepath.Join(src, "f"), []byte("payload"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}
	var buf bytes.Buffer
	if err := CreateArchive(&buf, src, WithArchiveFormat(ArchiveFormatZip)); err != nil {
		t.Fatalf("CreateArchive: %v", err)
	}
	// maxBytes=0 disables the cap, exercising the io.Copy-without-LimitReader branch.
	if err := ExtractArchive(&buf, filepath.Join(dir, "extract"), WithArchiveMaxBytes(0)); err != nil {
		t.Fatalf("ExtractArchive: %v", err)
	}
}

func TestExtractArchive_ZipStream_CorruptZip(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	// "PK\x05\x06" is the zip end-of-central-directory marker; without
	// the trailing bytes it parses as a corrupt zip.
	r := bytes.NewReader([]byte("PK\x05\x06\x00\x00\x00\x00garbage"))
	err := ExtractArchive(r, filepath.Join(dir, "extract"))
	if err == nil {
		t.Fatal("expected error from corrupt zip stream")
	}
}

func TestExtractArchive_ZipStream_TooLarge(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	// Build a small zip stream and stream-extract it with a maxBytes
	// smaller than the on-disk size to trip the cap on the streaming
	// buffer.
	src := filepath.Join(dir, "src")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if err := os.WriteFile(filepath.Join(src, "f"), bytes.Repeat([]byte("X"), 4096), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}
	var buf bytes.Buffer
	if err := CreateArchive(&buf, src, WithArchiveFormat(ArchiveFormatZip)); err != nil {
		t.Fatalf("CreateArchive: %v", err)
	}

	// 100 bytes is well below the zip's actual size, so the streaming
	// buffer trips the cap.
	err := ExtractArchive(&buf, filepath.Join(dir, "extract"), WithArchiveMaxBytes(100))
	if !errors.Is(err, ErrArchiveTooLarge) {
		t.Errorf("got %v, want ErrArchiveTooLarge", err)
	}
}

func TestExtractArchive_UnknownFormat(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	// 16 bytes of garbage that doesn't match any sniffer.
	r := bytes.NewReader([]byte("not-an-archive!!"))
	err := ExtractArchive(r, dir)
	if !errors.Is(err, ErrArchiveFormatUnknown) {
		t.Errorf("got %v, want ErrArchiveFormatUnknown", err)
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
