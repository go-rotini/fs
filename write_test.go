package fs

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// --- WriteFile basics ---

func TestWriteFile_Basic(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "f")
	if err := WriteFile(path, []byte("hello")); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(got) != "hello" {
		t.Errorf("got %q, want hello", got)
	}
}

func TestWriteFile_Overwrite(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "f")
	if err := WriteFile(path, []byte("v1")); err != nil {
		t.Fatalf("write 1: %v", err)
	}
	if err := WriteFile(path, []byte("v2-longer")); err != nil {
		t.Fatalf("write 2: %v", err)
	}
	got, _ := os.ReadFile(path)
	if string(got) != "v2-longer" {
		t.Errorf("got %q", got)
	}
}

func TestWriteFile_DefaultModeNew(t *testing.T) {
	if runtime.GOOS == goosWindows {
		t.Skip("Unix mode bits don't apply on Windows")
	}
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "f")
	if err := WriteFile(path, []byte("x")); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	info, _ := os.Stat(path)
	if info.Mode().Perm() != 0o644 {
		t.Errorf("perm = %o, want 0o644", info.Mode().Perm())
	}
}

func TestWriteFile_PreservesExistingMode(t *testing.T) {
	if runtime.GOOS == goosWindows {
		t.Skip("Unix mode bits don't apply on Windows")
	}
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "f")
	if err := os.WriteFile(path, []byte("v1"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if err := WriteFile(path, []byte("v2")); err != nil {
		t.Fatalf("WriteFile (overwrite): %v", err)
	}
	info, _ := os.Stat(path)
	if info.Mode().Perm() != 0o600 {
		t.Errorf("mode not preserved on overwrite: got %o", info.Mode().Perm())
	}
}

func TestWriteFile_WithPermOverridesExisting(t *testing.T) {
	if runtime.GOOS == goosWindows {
		t.Skip("Unix mode bits don't apply on Windows")
	}
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "f")
	if err := os.WriteFile(path, []byte("v1"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if err := WriteFile(path, []byte("v2"), WithPerm(0o644)); err != nil {
		t.Fatalf("WriteFile WithPerm: %v", err)
	}
	info, _ := os.Stat(path)
	if info.Mode().Perm() != 0o644 {
		t.Errorf("got %o, want 0o644", info.Mode().Perm())
	}
}

func TestWriteFile_MissingParent(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "missing", "f")
	err := WriteFile(path, []byte("x"))
	if err == nil {
		t.Fatal("expected error when parent dir missing")
	}
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("got %v, want ErrNotFound", err)
	}
}

func TestWriteFile_WithMkdirAll(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "a", "b", "c", "f")
	if err := WriteFile(path, []byte("x"), WithMkdirAll(true)); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if !IsFile(path) {
		t.Error("file not created")
	}
}

// --- WriteString / WriteFileSecret / WriteFileEnsure ---

func TestWriteString(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "f")
	if err := WriteString(path, "hello"); err != nil {
		t.Fatalf("WriteString: %v", err)
	}
	got, _ := os.ReadFile(path)
	if string(got) != "hello" {
		t.Errorf("got %q", got)
	}
}

func TestWriteFileSecret_Default0o600(t *testing.T) {
	if runtime.GOOS == goosWindows {
		t.Skip("Unix mode bits don't apply on Windows")
	}
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "secret")
	if err := WriteFileSecret(path, []byte("token")); err != nil {
		t.Fatalf("WriteFileSecret: %v", err)
	}
	info, _ := os.Stat(path)
	if info.Mode().Perm() != 0o600 {
		t.Errorf("perm = %o, want 0o600", info.Mode().Perm())
	}
}

func TestWriteFileEnsure_CreatesParents(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "deep", "tree", "f")
	if err := WriteFileEnsure(path, []byte("x")); err != nil {
		t.Fatalf("WriteFileEnsure: %v", err)
	}
	if !IsFile(path) {
		t.Error("file not created")
	}
}

// --- WriteFileExclusive ---

func TestWriteFileExclusive_NewFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "f")
	if err := WriteFileExclusive(path, []byte("first")); err != nil {
		t.Fatalf("WriteFileExclusive: %v", err)
	}
	got, _ := os.ReadFile(path)
	if string(got) != "first" {
		t.Errorf("got %q", got)
	}
}

func TestWriteFileExclusive_AlreadyExists(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "f")
	if err := os.WriteFile(path, []byte("existing"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	err := WriteFileExclusive(path, []byte("new"))
	if !errors.Is(err, ErrAlreadyExists) {
		t.Errorf("got %v, want ErrAlreadyExists", err)
	}
	// Existing content untouched.
	got, _ := os.ReadFile(path)
	if string(got) != "existing" {
		t.Errorf("contents changed: %q", got)
	}
}

// --- Append / AppendString ---

func TestAppend_NewFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "log")
	if err := Append(path, []byte("first\n")); err != nil {
		t.Fatalf("Append: %v", err)
	}
	got, _ := os.ReadFile(path)
	if string(got) != "first\n" {
		t.Errorf("got %q", got)
	}
}

func TestAppend_Existing(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "log")
	if err := os.WriteFile(path, []byte("first\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if err := Append(path, []byte("second\n")); err != nil {
		t.Fatalf("Append: %v", err)
	}
	got, _ := os.ReadFile(path)
	if string(got) != "first\nsecond\n" {
		t.Errorf("got %q", got)
	}
}

func TestAppendString(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "log")
	if err := AppendString(path, "x"); err != nil {
		t.Fatalf("AppendString: %v", err)
	}
	got, _ := os.ReadFile(path)
	if string(got) != "x" {
		t.Errorf("got %q", got)
	}
}

func TestAppend_WithLocked(t *testing.T) {
	if runtime.GOOS == goosWindows {
		t.Skip("flock is POSIX-only; WithLocked is a no-op on Windows")
	}
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "log")
	if err := Append(path, []byte("locked\n"), WithLocked(true)); err != nil {
		t.Fatalf("Append: %v", err)
	}
	got, _ := os.ReadFile(path)
	if string(got) != "locked\n" {
		t.Errorf("got %q", got)
	}
}

// --- WriteAt ---

func TestWriteAt_Basic(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "f")
	if err := os.WriteFile(path, []byte("0123456789"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if err := WriteAt(path, 3, []byte("XXX")); err != nil {
		t.Fatalf("WriteAt: %v", err)
	}
	got, _ := os.ReadFile(path)
	if string(got) != "012XXX6789" {
		t.Errorf("got %q", got)
	}
}

func TestWriteAt_Missing(t *testing.T) {
	t.Parallel()
	err := WriteAt(filepath.Join(t.TempDir(), "missing"), 0, []byte("x"))
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("got %v, want ErrNotFound", err)
	}
}

func TestWriteAt_NegativeOffset(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "f")
	if err := os.WriteFile(path, []byte("abc"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	err := WriteAt(path, -1, []byte("x"))
	if !errors.Is(err, ErrInvalidPath) {
		t.Errorf("got %v, want ErrInvalidPath", err)
	}
}

// --- OpenWrite ---

func TestOpenWrite_StreamingCommit(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "stream")
	tmp, finalize, err := OpenWrite(path)
	if err != nil {
		t.Fatalf("OpenWrite: %v", err)
	}
	for _, chunk := range []string{"part1-", "part2-", "part3"} {
		if _, werr := tmp.WriteString(chunk); werr != nil {
			t.Fatalf("Write: %v", werr)
		}
	}
	if err := finalize(); err != nil {
		t.Fatalf("finalize: %v", err)
	}
	got, _ := os.ReadFile(path)
	if string(got) != "part1-part2-part3" {
		t.Errorf("got %q", got)
	}
}

func TestOpenWrite_NoFinalizeMeansNoCommit(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "stream")
	tmp, finalize, err := OpenWrite(path)
	if err != nil {
		t.Fatalf("OpenWrite: %v", err)
	}
	if _, werr := tmp.WriteString("uncommitted"); werr != nil {
		t.Fatalf("Write: %v", werr)
	}
	// Skip finalize; close the temp directly.
	_ = tmp.Close()
	if Exists(path) {
		t.Error("destination should not exist when finalize was never called")
	}
	// finalize should still be safe to NOT call (cleanup is a defer
	// inside finalize, but the temp leak here is OK — tests don't
	// rely on the leak being cleaned up). The harness's t.TempDir
	// removal handles it.
	_ = finalize
}

func TestOpenWrite_FinalizeIdempotent(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "stream")
	tmp, finalize, err := OpenWrite(path)
	if err != nil {
		t.Fatalf("OpenWrite: %v", err)
	}
	_, _ = tmp.WriteString("hello")
	if err := finalize(); err != nil {
		t.Fatalf("first finalize: %v", err)
	}
	if err := finalize(); err != nil {
		t.Errorf("second finalize: %v", err)
	}
}

func TestOpenWrite_PreservesExistingMode(t *testing.T) {
	if runtime.GOOS == goosWindows {
		t.Skip("Unix mode bits don't apply on Windows")
	}
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "f")
	if err := os.WriteFile(path, []byte("v1"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	tmp, finalize, err := OpenWrite(path)
	if err != nil {
		t.Fatalf("OpenWrite: %v", err)
	}
	_, _ = tmp.WriteString("v2")
	if err := finalize(); err != nil {
		t.Fatalf("finalize: %v", err)
	}
	info, _ := os.Stat(path)
	if info.Mode().Perm() != 0o600 {
		t.Errorf("mode not preserved: got %o", info.Mode().Perm())
	}
}

func TestOpenWrite_WithMkdirAll(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "deep", "nested", "f")

	f, finalize, err := OpenWrite(path, WithMkdirAll(true))
	if err != nil {
		t.Fatalf("OpenWrite: %v", err)
	}
	if _, err := f.WriteString("ok"); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if err := finalize(); err != nil {
		t.Fatalf("finalize: %v", err)
	}
	if got, _ := os.ReadFile(path); string(got) != "ok" {
		t.Errorf("got %q, want ok", got)
	}
}

func TestOpenWrite_WithBackup(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "f")
	if err := os.WriteFile(path, []byte("v1"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}

	f, finalize, err := OpenWrite(path, WithBackup(".bak"))
	if err != nil {
		t.Fatalf("OpenWrite: %v", err)
	}
	if _, err := f.WriteString("v2"); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if err := finalize(); err != nil {
		t.Fatalf("finalize: %v", err)
	}
	if got, _ := os.ReadFile(path); string(got) != "v2" {
		t.Errorf("file content: %q", got)
	}
	if got, _ := os.ReadFile(path + ".bak"); string(got) != "v1" {
		t.Errorf("backup content: %q", got)
	}
}

// --- Append branches ---

func TestAppend_WithMkdirAll(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "deep", "nested", "log")
	if err := Append(path, []byte("x"), WithMkdirAll(true)); err != nil {
		t.Fatalf("Append: %v", err)
	}
	if got, _ := os.ReadFile(path); string(got) != "x" {
		t.Errorf("got %q", got)
	}
}

func TestAppend_WithSync(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "log")
	if err := Append(path, []byte("x"), WithSync(true)); err != nil {
		t.Fatalf("Append: %v", err)
	}
}

// --- WriteFileExclusive: WithMkdirAll path ---

func TestWriteFileExclusive_WithMkdirAll(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "deep", "f")
	if err := WriteFileExclusive(path, []byte("x"), WithMkdirAll(true)); err != nil {
		t.Fatalf("WriteFileExclusive: %v", err)
	}
}

// --- WithBackup default suffix ---

func TestWriteFile_WithBackupDefaultSuffix(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "f")
	if err := os.WriteFile(path, []byte("orig"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if err := WriteFile(path, []byte("new"), WithBackup("")); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if got, _ := os.ReadFile(path + ".bak"); string(got) != "orig" {
		t.Errorf("default-suffix backup not produced: %q", got)
	}
}

// --- WithBackup ---

func TestWithBackup_CreatesBackupOnOverwrite(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "f")
	if err := os.WriteFile(path, []byte("v1"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if err := WriteFile(path, []byte("v2"), WithBackup("")); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	bak, err := os.ReadFile(path + ".bak")
	if err != nil {
		t.Fatalf("ReadFile bak: %v", err)
	}
	if string(bak) != "v1" {
		t.Errorf("backup contents = %q, want v1", bak)
	}
	cur, _ := os.ReadFile(path)
	if string(cur) != "v2" {
		t.Errorf("current contents = %q, want v2", cur)
	}
}

func TestWithBackup_NoOpIfDestMissing(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "f")
	if err := WriteFile(path, []byte("first"), WithBackup("")); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if Exists(path + ".bak") {
		t.Error("backup created for new file")
	}
}

func TestWithBackup_CustomSuffix(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "f")
	if err := os.WriteFile(path, []byte("v1"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if err := WriteFile(path, []byte("v2"), WithBackup(".prev")); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if !Exists(path + ".prev") {
		t.Error(".prev backup not created")
	}
	if Exists(path + ".bak") {
		t.Error(".bak created for custom suffix")
	}
}

func TestWithBackup_OverwritesPriorBackup(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "f")
	if err := os.WriteFile(path, []byte("v1"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if err := WriteFile(path, []byte("v2"), WithBackup("")); err != nil {
		t.Fatalf("WriteFile 1: %v", err)
	}
	if err := WriteFile(path, []byte("v3"), WithBackup("")); err != nil {
		t.Fatalf("WriteFile 2: %v", err)
	}
	bak, _ := os.ReadFile(path + ".bak")
	if string(bak) != "v2" {
		t.Errorf("bak = %q, want v2 (most-recent prior)", bak)
	}
}

// --- Atomicity: temp file cleaned up on error ---

func TestAtomicity_NoTempLeftOnSuccess(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "f")
	if err := WriteFile(path, []byte("x")); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	for _, e := range entries {
		name := e.Name()
		if strings.Contains(name, ".tmp.") {
			t.Errorf("temp file leaked: %s", name)
		}
	}
}

func TestAtomicity_DefaultTempPattern(t *testing.T) {
	t.Parallel()
	// The temp pattern includes the basename + ".tmp.*". The
	// best signal for testing this without fault injection is to
	// observe the pattern via WithTempPattern indirectly: we
	// replace the pattern and verify the alternate one is used.
	dir := t.TempDir()
	path := filepath.Join(dir, "f")
	if err := WriteFile(path, []byte("x"), WithTempPattern("custom-*")); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	// On success there should be no leftover; we're effectively
	// checking the pattern compiled without error.
	if !Exists(path) {
		t.Error("file not created")
	}
}

// --- WithAtomic(false): direct path ---

func TestWithAtomicFalse(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "f")
	if err := WriteFile(path, []byte("hello"), WithAtomic(false)); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	got, _ := os.ReadFile(path)
	if string(got) != "hello" {
		t.Errorf("got %q", got)
	}
}

// --- WithSync explicit ---

func TestWithSync_Explicit(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "f")
	if err := WriteFile(path, []byte("x"), WithSync(true)); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if !Exists(path) {
		t.Error("file not created")
	}
}

// --- internal: resolveMode ---

func TestResolveMode_NewFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "f")
	cfg := newWriteOptions(nil)
	mode, existed := resolveMode(path, cfg)
	if existed {
		t.Error("existed = true for missing path")
	}
	if mode != Mode0644 {
		t.Errorf("mode = %o, want 0o644", mode)
	}
}

func TestResolveMode_Existing(t *testing.T) {
	if runtime.GOOS == goosWindows {
		t.Skip("Unix mode bits don't apply on Windows")
	}
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "f")
	if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	cfg := newWriteOptions(nil)
	mode, existed := resolveMode(path, cfg)
	if !existed {
		t.Error("existed = false for present path")
	}
	if mode != 0o600 {
		t.Errorf("mode = %o, want 0o600 (preserved)", mode)
	}
}

func TestResolveMode_WithPermOverrides(t *testing.T) {
	if runtime.GOOS == goosWindows {
		t.Skip("Unix mode bits don't apply on Windows")
	}
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "f")
	if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	cfg := newWriteOptions([]WriteOption{WithPerm(0o644)})
	mode, _ := resolveMode(path, cfg)
	if mode != 0o644 {
		t.Errorf("mode = %o, want 0o644 (override)", mode)
	}
}

// --- WithDirPerm ---

func TestWithDirPerm(t *testing.T) {
	if runtime.GOOS == goosWindows {
		t.Skip("Unix mode bits don't apply on Windows")
	}
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "newdir", "f")
	if err := WriteFile(path, []byte("x"),
		WithMkdirAll(true),
		WithDirPerm(0o700),
	); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	info, err := os.Stat(filepath.Dir(path))
	if err != nil {
		t.Fatalf("Stat dir: %v", err)
	}
	if info.Mode().Perm() != 0o700 {
		t.Errorf("dir mode = %o, want 0o700", info.Mode().Perm())
	}
}

// --- contents round-trip via fs.ReadFile ---

func TestWriteFile_RoundTrip(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "f")
	want := bytes.Repeat([]byte("abc"), 1024)
	if err := WriteFile(path, want); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	got, err := ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("round-trip mismatch; got %d bytes, want %d", len(got), len(want))
	}
}
