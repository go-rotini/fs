package fs

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

// --- Stat / Lstat ---

func TestStat_Existing(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	file := filepath.Join(dir, "f")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	info, err := Stat(file)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if info.Size() != 1 {
		t.Errorf("Size = %d, want 1", info.Size())
	}
}

func TestStat_Missing(t *testing.T) {
	t.Parallel()
	_, err := Stat(filepath.Join(t.TempDir(), "missing"))
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
}

func TestLstat_Symlink(t *testing.T) {
	if runtime.GOOS == goosWindows {
		t.Skip("Windows symlinks require admin/developer mode")
	}
	t.Parallel()
	dir := t.TempDir()
	target := filepath.Join(dir, "target")
	if err := os.WriteFile(target, []byte("x"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	link := filepath.Join(dir, "link")
	if err := os.Symlink(target, link); err != nil {
		t.Fatalf("Symlink: %v", err)
	}
	info, err := Lstat(link)
	if err != nil {
		t.Fatalf("Lstat: %v", err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		t.Error("Lstat returned non-symlink for a symlink")
	}
}

// --- Mtime / Atime / Ctime ---

func TestMtime(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	file := filepath.Join(dir, "f")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	got, err := Mtime(file)
	if err != nil {
		t.Fatalf("Mtime: %v", err)
	}
	if got.IsZero() {
		t.Error("Mtime is zero")
	}
}

func TestAtime(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	file := filepath.Join(dir, "f")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	got, err := Atime(file)
	if err != nil {
		t.Fatalf("Atime: %v", err)
	}
	if got.IsZero() {
		t.Error("Atime is zero")
	}
}

func TestCtime(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	file := filepath.Join(dir, "f")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	got, err := Ctime(file)
	if err != nil {
		t.Fatalf("Ctime: %v", err)
	}
	if got.IsZero() {
		t.Error("Ctime is zero")
	}
}

func TestBTime(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	file := filepath.Join(dir, "f")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	got, err := BTime(file)
	if runtime.GOOS == "linux" {
		if !errors.Is(err, ErrNotSupported) {
			t.Errorf("Linux BTime: err = %v, want ErrNotSupported", err)
		}
		return
	}
	if err != nil {
		t.Fatalf("BTime: %v", err)
	}
	if got.IsZero() {
		t.Error("BTime is zero on a platform that should expose it")
	}
}

// --- Owner ---

func TestOwner(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	file := filepath.Join(dir, "f")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	uid, gid, err := Owner(file)
	if runtime.GOOS == goosWindows {
		if !errors.Is(err, ErrNotSupported) {
			t.Errorf("Windows Owner: err = %v, want ErrNotSupported", err)
		}
		if uid != -1 || gid != -1 {
			t.Errorf("Windows Owner returned (%d, %d), want (-1, -1)", uid, gid)
		}
		return
	}
	if err != nil {
		t.Fatalf("Owner: %v", err)
	}
	if uid < 0 || gid < 0 {
		t.Errorf("got (%d, %d); want non-negative on POSIX", uid, gid)
	}
}

// --- Time setters ---

func TestSetMtime(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	file := filepath.Join(dir, "f")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	want := time.Date(2024, 6, 15, 12, 30, 45, 0, time.UTC)
	if err := SetMtime(file, want); err != nil {
		t.Fatalf("SetMtime: %v", err)
	}
	got, err := Mtime(file)
	if err != nil {
		t.Fatalf("Mtime: %v", err)
	}
	if !got.Equal(want) {
		t.Errorf("Mtime = %v, want %v", got, want)
	}
}

func TestSetAtime(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	file := filepath.Join(dir, "f")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	want := time.Date(2024, 6, 15, 12, 30, 45, 0, time.UTC)
	if err := SetAtime(file, want); err != nil {
		t.Fatalf("SetAtime: %v", err)
	}
	got, err := Atime(file)
	if err != nil {
		t.Fatalf("Atime: %v", err)
	}
	if !got.Equal(want) {
		t.Errorf("Atime = %v, want %v", got, want)
	}
}

func TestSetTimes(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	file := filepath.Join(dir, "f")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	at := time.Date(2024, 6, 15, 12, 30, 45, 0, time.UTC)
	mt := time.Date(2024, 7, 20, 10, 15, 30, 0, time.UTC)
	if err := SetTimes(file, at, mt); err != nil {
		t.Fatalf("SetTimes: %v", err)
	}
	gotA, _ := Atime(file)
	gotM, _ := Mtime(file)
	if !gotA.Equal(at) || !gotM.Equal(mt) {
		t.Errorf("got (%v, %v); want (%v, %v)", gotA, gotM, at, mt)
	}
}

func TestSetMtime_Missing(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := SetMtime(filepath.Join(dir, "missing"), time.Now()); err == nil {
		t.Error("expected error for missing path")
	}
}

func TestSetAtime_Missing(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := SetAtime(filepath.Join(dir, "missing"), time.Now()); err == nil {
		t.Error("expected error for missing path")
	}
}

func TestSetTimes_Missing(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := SetTimes(filepath.Join(dir, "missing"), time.Now(), time.Now()); err == nil {
		t.Error("expected error for missing path")
	}
}

// --- Touch ---

func TestTouch_CreatesMissing(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	file := filepath.Join(dir, "new")
	if err := Touch(file); err != nil {
		t.Fatalf("Touch: %v", err)
	}
	if !Exists(file) {
		t.Error("Touch did not create file")
	}
	info, err := Stat(file)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if info.Size() != 0 {
		t.Errorf("size = %d, want 0", info.Size())
	}
}

func TestTouch_UpdatesExisting(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	file := filepath.Join(dir, "f")
	if err := os.WriteFile(file, []byte("hello"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	old := time.Now().Add(-1 * time.Hour)
	if err := SetMtime(file, old); err != nil {
		t.Fatalf("SetMtime: %v", err)
	}
	if err := Touch(file); err != nil {
		t.Fatalf("Touch: %v", err)
	}
	got, _ := Mtime(file)
	if !got.After(old) {
		t.Errorf("Touch did not advance mtime: got %v, old %v", got, old)
	}
	// Touch must NOT truncate.
	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(data) != "hello" {
		t.Errorf("contents = %q; Touch should not truncate", string(data))
	}
}

func TestTouch_WithTimes(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	file := filepath.Join(dir, "f")
	at := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	mt := time.Date(2024, 2, 2, 0, 0, 0, 0, time.UTC)
	if err := Touch(file, WithTimes(at, mt)); err != nil {
		t.Fatalf("Touch: %v", err)
	}
	gotA, _ := Atime(file)
	gotM, _ := Mtime(file)
	if !gotA.Equal(at) || !gotM.Equal(mt) {
		t.Errorf("got (%v, %v); want (%v, %v)", gotA, gotM, at, mt)
	}
}

func TestTouch_WithPerm(t *testing.T) {
	if runtime.GOOS == goosWindows {
		t.Skip("Unix mode bits don't apply on Windows")
	}
	t.Parallel()
	dir := t.TempDir()
	file := filepath.Join(dir, "f")
	if err := Touch(file, WithTouchPerm(0o600)); err != nil {
		t.Fatalf("Touch: %v", err)
	}
	info, err := Stat(file)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("perm = %o, want 0o600", info.Mode().Perm())
	}
}

// --- SameFile / SameDevice ---

func TestSameFile_True(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	file := filepath.Join(dir, "f")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	ok, err := SameFile(file, file)
	if err != nil || !ok {
		t.Errorf("got (%v, %v); want (true, nil)", ok, err)
	}
}

func TestSameFile_Different(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	a := filepath.Join(dir, "a")
	b := filepath.Join(dir, "b")
	for _, p := range []string{a, b} {
		if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
			t.Fatalf("WriteFile %s: %v", p, err)
		}
	}
	ok, err := SameFile(a, b)
	if err != nil || ok {
		t.Errorf("got (%v, %v); want (false, nil)", ok, err)
	}
}

func TestSameDevice_SameDir(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	a := filepath.Join(dir, "a")
	b := filepath.Join(dir, "b")
	for _, p := range []string{a, b} {
		if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
			t.Fatalf("WriteFile %s: %v", p, err)
		}
	}
	ok, err := SameDevice(a, b)
	if err != nil {
		t.Fatalf("SameDevice: %v", err)
	}
	if !ok {
		t.Error("two files in the same temp dir should be on the same device")
	}
}

// --- Fault-injection: Touch / SetMtime / SetAtime / SetTimes ---
//
// These tests swap package-level OS hooks (see fault_hooks.go) to
// exercise defensive error branches that real I/O can't easily
// provoke. None call t.Parallel; the hooks are package-global.

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

// --- Touch / Lstat / Set*Time / SameFile error paths ---

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

func TestTouch_WithExplicitTimes(t *testing.T) {
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

func TestTouch_OpenError(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	err := Touch(filepath.Join(dir, "missing", "f"))
	if err == nil {
		t.Fatal("expected error from Touch when parent missing")
	}
}

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
