package fs

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// Fault-injection tests exercise the defensive error branches in
// the write / copy / archive helpers — the ones that fire only on
// disk-full, hardware fault, in-flight unlink, and similar
// conditions that real tests can't easily provoke.
//
// All tests in this file MUST run serially. The injection helper
// swaps a package-level hook variable; running the tests in
// parallel would race against each other and against any other
// tests that happen to call the swapped helper.

// faultyHooks captures the current values of every fault-prone hook
// and restores them via t.Cleanup.
type faultyHooks struct {
	t               *testing.T
	origOpenFile    func(string, int, os.FileMode) (*os.File, error)
	origOpen        func(string) (*os.File, error)
	origRename      func(string, string) error
	origChmod       func(string, os.FileMode) error
	origChtimes     func(string, time.Time, time.Time) error
	origStat        func(string) (os.FileInfo, error)
	origLstat       func(string) (os.FileInfo, error)
	origFileSync    func(*os.File) error
	origFileClose   func(*os.File) error
	origFileWrite   func(*os.File, []byte) (int, error)
	origFileWriteAt func(*os.File, []byte, int64) (int, error)
}

// newFaultyHooks snapshots the current hook values and registers
// a t.Cleanup that restores them after the test. Returns a
// receiver whose methods install fault-injecting variants for
// individual hooks.
func newFaultyHooks(t *testing.T) *faultyHooks {
	t.Helper()
	h := &faultyHooks{
		t:               t,
		origOpenFile:    osOpenFile,
		origOpen:        osOpen,
		origRename:      osRename,
		origChmod:       osChmod,
		origChtimes:     osChtimes,
		origStat:        osStat,
		origLstat:       osLstat,
		origFileSync:    fileSync,
		origFileClose:   fileClose,
		origFileWrite:   fileWrite,
		origFileWriteAt: fileWriteAt,
	}
	t.Cleanup(h.restore)
	return h
}

func (h *faultyHooks) restore() {
	osOpenFile = h.origOpenFile
	osOpen = h.origOpen
	osRename = h.origRename
	osChmod = h.origChmod
	osChtimes = h.origChtimes
	osStat = h.origStat
	osLstat = h.origLstat
	fileSync = h.origFileSync
	fileClose = h.origFileClose
	fileWrite = h.origFileWrite
	fileWriteAt = h.origFileWriteAt
}

var errInjected = errors.New("fault injected")

// failSyncAlways replaces fileSync with a function that always
// returns errInjected.
func (h *faultyHooks) failSyncAlways() {
	fileSync = func(*os.File) error { return errInjected }
}

// failCloseAlways replaces fileClose with a function that always
// returns errInjected.
func (h *faultyHooks) failCloseAlways() {
	// Still close the underlying fd so subsequent open calls have
	// available descriptors; report the injected error to the
	// caller.
	orig := h.origFileClose
	fileClose = func(f *os.File) error {
		_ = orig(f)
		return errInjected
	}
}

// failWriteAlways replaces fileWrite with a function that returns
// errInjected on every call.
func (h *faultyHooks) failWriteAlways() {
	fileWrite = func(*os.File, []byte) (int, error) { return 0, errInjected }
}

func (h *faultyHooks) failWriteAtAlways() {
	fileWriteAt = func(*os.File, []byte, int64) (int, error) { return 0, errInjected }
}

// failRenameAlways replaces osRename with a function that always
// returns errInjected.
func (h *faultyHooks) failRenameAlways() {
	osRename = func(string, string) error { return errInjected }
}

// failChmodAlways replaces osChmod with a function that always
// returns errInjected.
func (h *faultyHooks) failChmodAlways() {
	osChmod = func(string, os.FileMode) error { return errInjected }
}

// failChtimesAlways replaces osChtimes with a function that always
// returns errInjected.
func (h *faultyHooks) failChtimesAlways() {
	osChtimes = func(string, time.Time, time.Time) error { return errInjected }
}

func (h *faultyHooks) failStatAlways() {
	osStat = func(string) (os.FileInfo, error) { return nil, errInjected }
}

func (h *faultyHooks) failLstatAlways() {
	osLstat = func(string) (os.FileInfo, error) { return nil, errInjected }
}

func (h *faultyHooks) failOpenAlways() {
	osOpen = func(string) (*os.File, error) { return nil, errInjected }
}

// --- Write family ---

func TestFault_WriteFile_SyncError(t *testing.T) {
	h := newFaultyHooks(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "f")
	if err := os.WriteFile(path, []byte("orig"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}
	h.failSyncAlways()
	// WriteFile over an existing file syncs by default.
	err := WriteFile(path, []byte("new"))
	if !errors.Is(err, errInjected) {
		t.Errorf("got %v, want errInjected", err)
	}
}

func TestFault_WriteFile_CloseError(t *testing.T) {
	h := newFaultyHooks(t)
	dir := t.TempDir()
	h.failCloseAlways()
	err := WriteFile(filepath.Join(dir, "f"), []byte("x"))
	if !errors.Is(err, errInjected) {
		t.Errorf("got %v, want errInjected", err)
	}
}

func TestFault_WriteFile_WriteError(t *testing.T) {
	h := newFaultyHooks(t)
	dir := t.TempDir()
	h.failWriteAlways()
	err := WriteFile(filepath.Join(dir, "f"), []byte("x"))
	if !errors.Is(err, errInjected) {
		t.Errorf("got %v, want errInjected", err)
	}
}

func TestFault_WriteFile_RenameError(t *testing.T) {
	h := newFaultyHooks(t)
	dir := t.TempDir()
	h.failRenameAlways()
	err := WriteFile(filepath.Join(dir, "f"), []byte("x"))
	if !errors.Is(err, errInjected) {
		t.Errorf("got %v, want errInjected", err)
	}
}

func TestFault_WriteFile_BackupRenameError(t *testing.T) {
	h := newFaultyHooks(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "f")
	if err := os.WriteFile(path, []byte("v1"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}
	// Wrap osRename so the FIRST call (the backup rename) fails;
	// subsequent calls behave normally. Without injecting per-call,
	// any rename failure surfaces here, which is what we want.
	calls := 0
	orig := osRename
	osRename = func(src, dst string) error {
		calls++
		if calls == 1 {
			return errInjected
		}
		return orig(src, dst)
	}
	defer func() { _ = h }() // hook restore via t.Cleanup

	err := WriteFile(path, []byte("v2"), WithBackup(".bak"))
	if !errors.Is(err, errInjected) {
		t.Errorf("got %v, want errInjected", err)
	}
}

func TestFault_WriteFile_AtomicFalseSyncError(t *testing.T) {
	h := newFaultyHooks(t)
	dir := t.TempDir()
	h.failSyncAlways()
	err := WriteFile(filepath.Join(dir, "f"), []byte("x"), WithAtomic(false), WithSync(true))
	if !errors.Is(err, errInjected) {
		t.Errorf("got %v, want errInjected", err)
	}
}

func TestFault_WriteFile_AtomicFalseCloseError(t *testing.T) {
	h := newFaultyHooks(t)
	dir := t.TempDir()
	h.failCloseAlways()
	err := WriteFile(filepath.Join(dir, "f"), []byte("x"), WithAtomic(false))
	if !errors.Is(err, errInjected) {
		t.Errorf("got %v, want errInjected", err)
	}
}

func TestFault_WriteFile_AtomicFalseWriteError(t *testing.T) {
	h := newFaultyHooks(t)
	dir := t.TempDir()
	h.failWriteAlways()
	err := WriteFile(filepath.Join(dir, "f"), []byte("x"), WithAtomic(false))
	if !errors.Is(err, errInjected) {
		t.Errorf("got %v, want errInjected", err)
	}
}

func TestFault_WriteFileExclusive_SyncError(t *testing.T) {
	h := newFaultyHooks(t)
	dir := t.TempDir()
	h.failSyncAlways()
	err := WriteFileExclusive(filepath.Join(dir, "f"), []byte("x"), WithSync(true))
	if !errors.Is(err, errInjected) {
		t.Errorf("got %v, want errInjected", err)
	}
}

func TestFault_WriteFileExclusive_CloseError(t *testing.T) {
	h := newFaultyHooks(t)
	dir := t.TempDir()
	h.failCloseAlways()
	err := WriteFileExclusive(filepath.Join(dir, "f"), []byte("x"))
	if !errors.Is(err, errInjected) {
		t.Errorf("got %v, want errInjected", err)
	}
}

func TestFault_WriteFileExclusive_WriteError(t *testing.T) {
	h := newFaultyHooks(t)
	dir := t.TempDir()
	h.failWriteAlways()
	err := WriteFileExclusive(filepath.Join(dir, "f"), []byte("x"))
	if !errors.Is(err, errInjected) {
		t.Errorf("got %v, want errInjected", err)
	}
}

func TestFault_Append_WriteError(t *testing.T) {
	h := newFaultyHooks(t)
	dir := t.TempDir()
	h.failWriteAlways()
	err := Append(filepath.Join(dir, "f"), []byte("x"))
	if !errors.Is(err, errInjected) {
		t.Errorf("got %v, want errInjected", err)
	}
}

func TestFault_Append_SyncError(t *testing.T) {
	h := newFaultyHooks(t)
	dir := t.TempDir()
	h.failSyncAlways()
	err := Append(filepath.Join(dir, "f"), []byte("x"), WithSync(true))
	if !errors.Is(err, errInjected) {
		t.Errorf("got %v, want errInjected", err)
	}
}

func TestFault_WriteAt_WriteError(t *testing.T) {
	h := newFaultyHooks(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "f")
	if err := os.WriteFile(path, []byte("xxxxx"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}
	h.failWriteAtAlways()
	err := WriteAt(path, 0, []byte("y"))
	if !errors.Is(err, errInjected) {
		t.Errorf("got %v, want errInjected", err)
	}
}

func TestFault_WriteAt_SyncError(t *testing.T) {
	h := newFaultyHooks(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "f")
	if err := os.WriteFile(path, []byte("xxxxx"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}
	h.failSyncAlways()
	err := WriteAt(path, 0, []byte("y"), WithSync(true))
	if !errors.Is(err, errInjected) {
		t.Errorf("got %v, want errInjected", err)
	}
}

// --- OpenWrite finalize paths ---

func TestFault_OpenWrite_FinalizeSyncError(t *testing.T) {
	h := newFaultyHooks(t)
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
	h.failSyncAlways()
	if err := finalize(); !errors.Is(err, errInjected) {
		t.Errorf("got %v, want errInjected", err)
	}
}

func TestFault_OpenWrite_FinalizeCloseError(t *testing.T) {
	h := newFaultyHooks(t)
	dir := t.TempDir()
	f, finalize, err := OpenWrite(filepath.Join(dir, "f"))
	if err != nil {
		t.Fatalf("OpenWrite: %v", err)
	}
	if _, err := f.WriteString("v"); err != nil {
		t.Fatalf("Write: %v", err)
	}
	h.failCloseAlways()
	if err := finalize(); !errors.Is(err, errInjected) {
		t.Errorf("got %v, want errInjected", err)
	}
}

func TestFault_OpenWrite_FinalizeRenameError(t *testing.T) {
	h := newFaultyHooks(t)
	dir := t.TempDir()
	f, finalize, err := OpenWrite(filepath.Join(dir, "f"))
	if err != nil {
		t.Fatalf("OpenWrite: %v", err)
	}
	if _, err := f.WriteString("v"); err != nil {
		t.Fatalf("Write: %v", err)
	}
	h.failRenameAlways()
	if err := finalize(); !errors.Is(err, errInjected) {
		t.Errorf("got %v, want errInjected", err)
	}
}

func TestFault_OpenWrite_FinalizeBackupRenameError(t *testing.T) {
	h := newFaultyHooks(t)
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
	h.failRenameAlways()
	if err := finalize(); !errors.Is(err, errInjected) {
		t.Errorf("got %v, want errInjected", err)
	}
}

// --- Copy family ---

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

// --- io.Copy mid-stream failure via faulty source reader ---

// faultyReader returns errInjected after readUpTo bytes.
type faultyReader struct {
	rem int
}

func (r *faultyReader) Read(p []byte) (int, error) {
	if r.rem <= 0 {
		return 0, errInjected
	}
	n := min(len(p), r.rem)
	for i := range n {
		p[i] = 'X'
	}
	r.rem -= n
	return n, nil
}

// Sanity: faultyReader does what we expect.
func TestFault_FaultyReader(t *testing.T) {
	r := &faultyReader{rem: 4}
	buf := make([]byte, 16)
	n, err := io.ReadFull(r, buf)
	if n != 4 {
		t.Errorf("read %d, want 4", n)
	}
	if !errors.Is(err, errInjected) && !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Errorf("got %v, want errInjected or unexpected EOF", err)
	}
}

// --- Move EXDEV fallback via injected rename ---

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

// --- Mid-stream read failures via fileRead hook ---

func TestFault_CopyFile_ReadError(t *testing.T) {
	h := newFaultyHooks(t)
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	if err := os.WriteFile(src, []byte("payload"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}
	orig := fileRead
	t.Cleanup(func() { fileRead = orig })
	fileRead = func(*os.File, []byte) (int, error) { return 0, errInjected }
	_ = h

	err := CopyFile(src, filepath.Join(dir, "dst"))
	if !errors.Is(err, errInjected) {
		t.Errorf("got %v, want errInjected", err)
	}
}

func TestFault_CreateArchive_Tar_ReadError(t *testing.T) {
	h := newFaultyHooks(t)
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if err := os.WriteFile(filepath.Join(src, "f"), []byte("xy"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}
	orig := fileRead
	t.Cleanup(func() { fileRead = orig })
	fileRead = func(*os.File, []byte) (int, error) { return 0, errInjected }
	_ = h

	out := filepath.Join(dir, "out.tar")
	err := CreateArchiveFile(out, src, WithArchiveFormat(ArchiveFormatTar))
	if !errors.Is(err, errInjected) {
		t.Errorf("got %v, want errInjected", err)
	}
}

func TestFault_CreateArchive_Zip_ReadError(t *testing.T) {
	h := newFaultyHooks(t)
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if err := os.WriteFile(filepath.Join(src, "f"), []byte("xy"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}
	orig := fileRead
	t.Cleanup(func() { fileRead = orig })
	fileRead = func(*os.File, []byte) (int, error) { return 0, errInjected }
	_ = h

	out := filepath.Join(dir, "out.zip")
	err := CreateArchiveFile(out, src, WithArchiveFormat(ArchiveFormatZip))
	if !errors.Is(err, errInjected) {
		t.Errorf("got %v, want errInjected", err)
	}
}

func TestFault_CreateArchive_Tar_OpenError(t *testing.T) {
	h := newFaultyHooks(t)
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if err := os.WriteFile(filepath.Join(src, "f"), []byte("xy"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}
	h.failOpenAlways()

	out := filepath.Join(dir, "out.tar")
	err := CreateArchiveFile(out, src, WithArchiveFormat(ArchiveFormatTar))
	if !errors.Is(err, errInjected) {
		t.Errorf("got %v, want errInjected", err)
	}
}

func TestFault_CreateArchive_Zip_OpenError(t *testing.T) {
	h := newFaultyHooks(t)
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if err := os.WriteFile(filepath.Join(src, "f"), []byte("xy"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}
	h.failOpenAlways()

	out := filepath.Join(dir, "out.zip")
	err := CreateArchiveFile(out, src, WithArchiveFormat(ArchiveFormatZip))
	if !errors.Is(err, errInjected) {
		t.Errorf("got %v, want errInjected", err)
	}
}

func TestFault_Move_CrossDeviceFallback(t *testing.T) {
	h := newFaultyHooks(t)
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	dst := filepath.Join(dir, "dst")
	if err := os.WriteFile(src, []byte("payload"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}

	// First osRename returns syscall.EXDEV; subsequent calls behave
	// normally (so the CopyFile fallback's own internal rename can
	// commit the destination).
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
