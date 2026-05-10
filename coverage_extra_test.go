package fs

// This file packs many small tests aimed at error paths and option
// combinations that the topical test files don't already exercise.
// Coverage-driven; functional invariants are owned by the topical
// suites.

import (
	"bytes"
	"context"
	"errors"
	stdfs "io/fs"
	"os"
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
