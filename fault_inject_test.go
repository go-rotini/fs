package fs

import (
	"archive/tar"
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
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
	origMkdirAll    func(string, os.FileMode) error
	origSymlink     func(string, string) error
	origLink        func(string, string) error
	origReadlink    func(string) (string, error)
	origRemove      func(string) error
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
		origMkdirAll:    osMkdirAll,
		origSymlink:     osSymlink,
		origLink:        osLink,
		origReadlink:    osReadlink,
		origRemove:      osRemove,
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
	osMkdirAll = h.origMkdirAll
	osSymlink = h.origSymlink
	osLink = h.origLink
	osReadlink = h.origReadlink
	osRemove = h.origRemove
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

func (h *faultyHooks) failMkdirAllAlways() {
	osMkdirAll = func(string, os.FileMode) error { return errInjected }
}

func (h *faultyHooks) failSymlinkAlways() {
	osSymlink = func(string, string) error { return errInjected }
}

func (h *faultyHooks) failLinkAlways() {
	osLink = func(string, string) error { return errInjected }
}

func (h *faultyHooks) failReadlinkAlways() {
	osReadlink = func(string) (string, error) { return "", errInjected }
}

func (h *faultyHooks) failRemoveAlways() {
	osRemove = func(string) error { return errInjected }
}

func (h *faultyHooks) failOpenFileAlways() {
	osOpenFile = func(string, int, os.FileMode) (*os.File, error) { return nil, errInjected }
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

// --- CopyDir fault paths ---

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

// --- ExtractArchive root-dir Mkdir fault ---

func TestFault_ExtractArchive_RootMkdirError(t *testing.T) {
	h := newFaultyHooks(t)
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if err := os.WriteFile(filepath.Join(src, "f"), []byte("x"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}
	var buf bytes.Buffer
	if err := CreateArchive(&buf, src, WithArchiveFormat(ArchiveFormatTar)); err != nil {
		t.Fatalf("CreateArchive: %v", err)
	}
	h.failMkdirAllAlways()
	err := ExtractArchive(&buf, filepath.Join(dir, "extract"))
	if !errors.Is(err, errInjected) {
		t.Errorf("got %v, want errInjected", err)
	}
}

// --- ExtractTar fileClose fault (after successful copy) ---

func TestFault_ExtractTar_FinalCloseError(t *testing.T) {
	h := newFaultyHooks(t)
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if err := os.WriteFile(filepath.Join(src, "f"), []byte("xy"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}
	archive := filepath.Join(dir, "archive.tar")
	if err := CreateArchiveFile(archive, src, WithArchiveFormat(ArchiveFormatTar)); err != nil {
		t.Fatalf("create: %v", err)
	}
	h.failCloseAlways()
	err := ExtractArchiveFile(archive, filepath.Join(dir, "extract"))
	if !errors.Is(err, errInjected) {
		t.Errorf("got %v, want errInjected", err)
	}
}

// --- IsEmpty / ListDir fault paths ---

func TestFault_IsEmpty_OpenError(t *testing.T) {
	h := newFaultyHooks(t)
	dir := t.TempDir()
	h.failOpenAlways()
	_, err := IsEmpty(dir)
	if !errors.Is(err, errInjected) {
		t.Errorf("got %v, want errInjected", err)
	}
}

func TestFault_ListDir_OpenError(t *testing.T) {
	h := newFaultyHooks(t)
	dir := t.TempDir()
	h.failOpenAlways()
	_, err := ListDir(dir)
	if !errors.Is(err, errInjected) {
		t.Errorf("got %v, want errInjected", err)
	}
}

// --- ScaffoldExtract marker-write fault ---

func TestFault_ScaffoldExtract_MarkerWriteError(t *testing.T) {
	h := newFaultyHooks(t)
	srcDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(srcDir, "f"), []byte("v1"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}
	dst := t.TempDir()
	srcFS := os.DirFS(srcDir)

	// First three osChmod calls (one for the temp file content, one
	// for any remaining setup) succeed; last one (the marker write's
	// chmod) fails. Using a counter is more deterministic than
	// pattern-matching paths.
	calls := 0
	orig := osChmod
	osChmod = func(p string, m os.FileMode) error {
		calls++
		if calls == 1 {
			return orig(p, m)
		}
		return errInjected
	}
	_ = h

	err := ScaffoldExtract(srcFS, dst)
	if !errors.Is(err, errInjected) {
		t.Errorf("got %v, want errInjected", err)
	}
}

// --- Scaffold fault paths ---

func TestFault_ScaffoldApply_MkdirError(t *testing.T) {
	h := newFaultyHooks(t)
	srcDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(srcDir, "sub"), 0o755); err != nil {
		t.Fatalf("setup: %v", err)
	}
	dst := t.TempDir()
	srcFS := os.DirFS(srcDir)
	h.failMkdirAllAlways()
	err := ScaffoldApply(srcFS, filepath.Join(dst, "out"), nil)
	if !errors.Is(err, errInjected) {
		t.Errorf("got %v, want errInjected", err)
	}
}

func TestFault_ScaffoldExtract_MkdirError(t *testing.T) {
	h := newFaultyHooks(t)
	srcDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(srcDir, "sub"), 0o755); err != nil {
		t.Fatalf("setup: %v", err)
	}
	dst := t.TempDir()
	srcFS := os.DirFS(srcDir)
	h.failMkdirAllAlways()
	err := ScaffoldExtract(srcFS, filepath.Join(dst, "out"))
	if !errors.Is(err, errInjected) {
		t.Errorf("got %v, want errInjected", err)
	}
}

// --- openTempForWrite chmod failure ---

func TestFault_OpenTempForWrite_ChmodError(t *testing.T) {
	h := newFaultyHooks(t)
	dir := t.TempDir()
	h.failChmodAlways()
	err := WriteFile(filepath.Join(dir, "f"), []byte("x"))
	if !errors.Is(err, errInjected) {
		t.Errorf("got %v, want errInjected", err)
	}
}

// --- Append fault paths ---

func TestFault_Append_MkdirAllError(t *testing.T) {
	h := newFaultyHooks(t)
	dir := t.TempDir()
	h.failMkdirAllAlways()
	err := Append(filepath.Join(dir, "sub", "f"), []byte("x"), WithMkdirAll(true))
	if !errors.Is(err, errInjected) {
		t.Errorf("got %v, want errInjected", err)
	}
}

func TestFault_Append_OpenError(t *testing.T) {
	h := newFaultyHooks(t)
	dir := t.TempDir()
	h.failOpenFileAlways()
	err := Append(filepath.Join(dir, "f"), []byte("x"))
	if !errors.Is(err, errInjected) {
		t.Errorf("got %v, want errInjected", err)
	}
}

func TestFault_WriteAt_OpenError(t *testing.T) {
	h := newFaultyHooks(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "f")
	if err := os.WriteFile(path, []byte("orig"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}
	h.failOpenFileAlways()
	err := WriteAt(path, 0, []byte("x"))
	if !errors.Is(err, errInjected) {
		t.Errorf("got %v, want errInjected", err)
	}
}

// --- Move fault paths (post-EXDEV branches) ---

func TestFault_Move_LstatErrorAfterEXDEV(t *testing.T) {
	h := newFaultyHooks(t)
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	if err := os.WriteFile(src, []byte("x"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}

	calls := 0
	osRename = func(string, string) error {
		calls++
		return exdevError()
	}
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

	// EXDEV on outer; CopyDir succeeds; then RemoveAll fails. RemoveAll
	// uses the stdlib path directly (not hooked), but failing osRemove
	// will not cover this since RemoveAll calls os.Remove not osRemove.
	// Use chmod-readonly trick: make the source dir readonly so its
	// children can't be removed.
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

// --- Touch fault paths ---

func TestFault_Touch_CloseError(t *testing.T) {
	h := newFaultyHooks(t)
	dir := t.TempDir()
	h.failCloseAlways()
	err := Touch(filepath.Join(dir, "newfile"))
	if !errors.Is(err, errInjected) {
		t.Errorf("got %v, want errInjected", err)
	}
}

func TestFault_Touch_ChtimesError(t *testing.T) {
	h := newFaultyHooks(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "f")
	if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}
	h.failChtimesAlways()
	err := Touch(path)
	if !errors.Is(err, errInjected) {
		t.Errorf("got %v, want errInjected", err)
	}
}

// --- SetMtime / SetAtime / SetTimes osChtimes fault ---

func TestFault_SetMtime_ChtimesError(t *testing.T) {
	h := newFaultyHooks(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "f")
	if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}
	h.failChtimesAlways()
	err := SetMtime(path, time.Now())
	if !errors.Is(err, errInjected) {
		t.Errorf("got %v, want errInjected", err)
	}
}

func TestFault_SetAtime_ChtimesError(t *testing.T) {
	h := newFaultyHooks(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "f")
	if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}
	h.failChtimesAlways()
	err := SetAtime(path, time.Now())
	if !errors.Is(err, errInjected) {
		t.Errorf("got %v, want errInjected", err)
	}
}

func TestFault_SetTimes_ChtimesError(t *testing.T) {
	h := newFaultyHooks(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "f")
	if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}
	h.failChtimesAlways()
	err := SetTimes(path, time.Now(), time.Now())
	if !errors.Is(err, errInjected) {
		t.Errorf("got %v, want errInjected", err)
	}
}

// --- MkdirAll(WithEnforcePerm) + osChmod fault ---

func TestFault_MkdirAll_EnforcePerm_ChmodError(t *testing.T) {
	h := newFaultyHooks(t)
	dir := t.TempDir()
	h.failChmodAlways()
	err := MkdirAll(filepath.Join(dir, "a", "b", "c"), 0o755, WithEnforcePerm(true))
	if !errors.Is(err, errInjected) {
		t.Errorf("got %v, want errInjected", err)
	}
}

// --- WithMkdirAll + osMkdirAll fault ---

func TestFault_WriteFile_MkdirAllError(t *testing.T) {
	h := newFaultyHooks(t)
	dir := t.TempDir()
	h.failMkdirAllAlways()
	err := WriteFile(filepath.Join(dir, "sub", "f"), []byte("x"), WithMkdirAll(true))
	if !errors.Is(err, errInjected) {
		t.Errorf("got %v, want errInjected", err)
	}
}

func TestFault_WriteFileExclusive_MkdirAllError(t *testing.T) {
	h := newFaultyHooks(t)
	dir := t.TempDir()
	h.failMkdirAllAlways()
	err := WriteFileExclusive(filepath.Join(dir, "sub", "f"), []byte("x"), WithMkdirAll(true))
	if !errors.Is(err, errInjected) {
		t.Errorf("got %v, want errInjected", err)
	}
}

func TestFault_OpenWrite_MkdirAllError(t *testing.T) {
	h := newFaultyHooks(t)
	dir := t.TempDir()
	h.failMkdirAllAlways()
	_, _, err := OpenWrite(filepath.Join(dir, "sub", "f"), WithMkdirAll(true))
	if !errors.Is(err, errInjected) {
		t.Errorf("got %v, want errInjected", err)
	}
}

// --- archive create with symlink: Readlink fault ---

func TestFault_CreateArchive_Tar_ReadlinkError(t *testing.T) {
	h := newFaultyHooks(t)
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if err := os.Symlink("target", filepath.Join(src, "lnk")); err != nil {
		t.Skipf("symlinks not supported: %v", err)
	}
	h.failReadlinkAlways()

	out := filepath.Join(dir, "out.tar")
	err := CreateArchiveFile(out, src, WithArchiveFormat(ArchiveFormatTar))
	if !errors.Is(err, errInjected) {
		t.Errorf("got %v, want errInjected", err)
	}
}

// --- CopyDir per-entry MkdirAll fault (subdir only) ---

func TestFault_CopyDir_SubdirMkdirError(t *testing.T) {
	h := newFaultyHooks(t)
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	dst := filepath.Join(dir, "dst")
	if err := os.MkdirAll(filepath.Join(src, "sub"), 0o755); err != nil {
		t.Fatalf("setup: %v", err)
	}

	calls := 0
	orig := osMkdirAll
	osMkdirAll = func(p string, m os.FileMode) error {
		calls++
		// First MkdirAll succeeds (creates dst); second fails (the
		// per-entry subdir create inside the walk callback).
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

// --- CopyDir per-entry copy faults (file + symlink) ---

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

// --- copySymlink fault paths ---

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

// --- archive extract fault paths ---

func TestFault_ExtractTar_MkdirError(t *testing.T) {
	h := newFaultyHooks(t)
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	if err := os.MkdirAll(filepath.Join(src, "sub"), 0o755); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if err := os.WriteFile(filepath.Join(src, "sub", "f"), []byte("xy"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}
	archive := filepath.Join(dir, "archive.tar")
	if err := CreateArchiveFile(archive, src, WithArchiveFormat(ArchiveFormatTar)); err != nil {
		t.Fatalf("create: %v", err)
	}

	h.failMkdirAllAlways()
	err := ExtractArchiveFile(archive, filepath.Join(dir, "extract"))
	if !errors.Is(err, errInjected) {
		t.Errorf("got %v, want errInjected", err)
	}
}

func TestFault_ExtractTar_OpenFileError(t *testing.T) {
	h := newFaultyHooks(t)
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if err := os.WriteFile(filepath.Join(src, "f"), []byte("xy"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}
	archive := filepath.Join(dir, "archive.tar")
	if err := CreateArchiveFile(archive, src, WithArchiveFormat(ArchiveFormatTar)); err != nil {
		t.Fatalf("create: %v", err)
	}

	h.failOpenFileAlways()
	err := ExtractArchiveFile(archive, filepath.Join(dir, "extract"))
	if !errors.Is(err, errInjected) {
		t.Errorf("got %v, want errInjected", err)
	}
}

func TestFault_ExtractZip_MkdirError(t *testing.T) {
	h := newFaultyHooks(t)
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	if err := os.MkdirAll(filepath.Join(src, "sub"), 0o755); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if err := os.WriteFile(filepath.Join(src, "sub", "f"), []byte("xy"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}
	archive := filepath.Join(dir, "archive.zip")
	if err := CreateArchiveFile(archive, src, WithArchiveFormat(ArchiveFormatZip)); err != nil {
		t.Fatalf("create: %v", err)
	}

	h.failMkdirAllAlways()
	err := ExtractArchiveFile(archive, filepath.Join(dir, "extract"))
	if !errors.Is(err, errInjected) {
		t.Errorf("got %v, want errInjected", err)
	}
}

func TestFault_ExtractZip_OpenFileError(t *testing.T) {
	h := newFaultyHooks(t)
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if err := os.WriteFile(filepath.Join(src, "f"), []byte("xy"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}
	archive := filepath.Join(dir, "archive.zip")
	if err := CreateArchiveFile(archive, src, WithArchiveFormat(ArchiveFormatZip)); err != nil {
		t.Fatalf("create: %v", err)
	}

	h.failOpenFileAlways()
	err := ExtractArchiveFile(archive, filepath.Join(dir, "extract"))
	if !errors.Is(err, errInjected) {
		t.Errorf("got %v, want errInjected", err)
	}
}

func TestFault_ExtractTar_HardlinkError(t *testing.T) {
	h := newFaultyHooks(t)
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatalf("setup: %v", err)
	}
	target := filepath.Join(src, "real")
	if err := os.WriteFile(target, []byte("xy"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if err := os.Link(target, filepath.Join(src, "hard")); err != nil {
		t.Skipf("hardlinks not supported: %v", err)
	}

	// Build a tar that has a hardlink entry. The standard CreateArchive
	// doesn't emit hardlink entries, so build one manually.
	archive := filepath.Join(dir, "archive.tar")
	tarFile, err := os.Create(archive)
	if err != nil {
		t.Fatalf("create tar: %v", err)
	}
	tw := tar.NewWriter(tarFile)
	if err := tw.WriteHeader(&tar.Header{Name: "real", Mode: 0o644, Size: 2, Typeflag: tar.TypeReg}); err != nil {
		t.Fatalf("WriteHeader real: %v", err)
	}
	if _, err := tw.Write([]byte("xy")); err != nil {
		t.Fatalf("Write real: %v", err)
	}
	if err := tw.WriteHeader(&tar.Header{Name: "hard", Mode: 0o644, Linkname: "real", Typeflag: tar.TypeLink}); err != nil {
		t.Fatalf("WriteHeader hard: %v", err)
	}
	if err := tw.Close(); err != nil {
		t.Fatalf("tw close: %v", err)
	}
	if err := tarFile.Close(); err != nil {
		t.Fatalf("file close: %v", err)
	}

	h.failLinkAlways()
	err = ExtractArchiveFile(archive, filepath.Join(dir, "extract"))
	if !errors.Is(err, errInjected) {
		t.Errorf("got %v, want errInjected", err)
	}
}

func TestFault_ExtractTar_SymlinkError(t *testing.T) {
	h := newFaultyHooks(t)
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if err := os.Symlink("target", filepath.Join(src, "lnk")); err != nil {
		t.Skipf("symlinks not supported: %v", err)
	}
	archive := filepath.Join(dir, "archive.tar")
	if err := CreateArchiveFile(archive, src, WithArchiveFormat(ArchiveFormatTar)); err != nil {
		t.Fatalf("create: %v", err)
	}

	h.failSymlinkAlways()
	err := ExtractArchiveFile(archive, filepath.Join(dir, "extract"))
	if !errors.Is(err, errInjected) {
		t.Errorf("got %v, want errInjected", err)
	}
}
