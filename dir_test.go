package fs

import (
	"context"
	"errors"
	"fmt"
	stdfs "io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// --- Mkdir / MkdirAll ---

func TestMkdir_New(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	target := filepath.Join(dir, "sub")
	if err := Mkdir(target, 0o755); err != nil {
		t.Fatalf("Mkdir: %v", err)
	}
	if !IsDir(target) {
		t.Error("dir not created")
	}
}

func TestMkdir_AlreadyExists(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	target := filepath.Join(dir, "sub")
	if err := os.Mkdir(target, 0o755); err != nil {
		t.Fatalf("setup: %v", err)
	}
	err := Mkdir(target, 0o755)
	if !errors.Is(err, ErrAlreadyExists) {
		t.Errorf("got %v, want ErrAlreadyExists", err)
	}
}

func TestMkdirAll_Chain(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	deep := filepath.Join(dir, "a", "b", "c")
	if err := MkdirAll(deep, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if !IsDir(deep) {
		t.Error("deep dir not created")
	}
}

func TestMkdirAll_AlreadyExistsNoError(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := MkdirAll(dir, 0o755); err != nil {
		t.Errorf("MkdirAll on existing dir = %v, want nil", err)
	}
}

func TestMkdirAll_EnforcePerm(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX perms not enforced as bits on Windows")
	}
	t.Parallel()

	old := syscallUmask(0)
	defer syscallUmask(old)

	dir := t.TempDir()
	deep := filepath.Join(dir, "alpha", "beta", "gamma")

	if err := MkdirAll(deep, 0o751, WithEnforcePerm(true)); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	for _, p := range []string{
		filepath.Join(dir, "alpha"),
		filepath.Join(dir, "alpha", "beta"),
		deep,
	} {
		info, err := os.Stat(p)
		if err != nil {
			t.Fatalf("Stat %s: %v", p, err)
		}
		if got := info.Mode().Perm(); got != 0o751 {
			t.Errorf("%s: perm %o, want 0751", p, got)
		}
	}
}

func TestMkdirAll_EnforcePermSkipsPreexisting(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX perms not enforced as bits on Windows")
	}
	t.Parallel()

	dir := t.TempDir()
	pre := filepath.Join(dir, "pre")
	if err := os.Mkdir(pre, 0o700); err != nil {
		t.Fatalf("setup: %v", err)
	}

	deep := filepath.Join(pre, "child")
	if err := MkdirAll(deep, 0o755, WithEnforcePerm(true)); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	info, err := os.Stat(pre)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if got := info.Mode().Perm(); got != 0o700 {
		t.Errorf("pre-existing dir perm = %o, want 0700 (untouched)", got)
	}
}

// --- ListDir ---

func TestListDir_Basic(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	for _, name := range []string{"a.txt", "b.txt", "c.txt"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o644); err != nil {
			t.Fatalf("WriteFile %s: %v", name, err)
		}
	}
	entries, err := ListDir(dir)
	if err != nil {
		t.Fatalf("ListDir: %v", err)
	}
	if len(entries) != 3 {
		t.Errorf("got %d entries, want 3", len(entries))
	}
}

func TestListDir_Sorted(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	for _, name := range []string{"c", "a", "b"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o644); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}
	}
	entries, err := ListDir(dir, WithSorted(true))
	if err != nil {
		t.Fatalf("ListDir: %v", err)
	}
	got := make([]string, len(entries))
	for i, e := range entries {
		got[i] = e.Name()
	}
	want := []string{"a", "b", "c"}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("entries[%d] = %s, want %s (full: %v)", i, got[i], want[i], got)
		}
	}
}

func TestListDir_SkipHidden(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	for _, name := range []string{"visible", ".hidden", ".dotrc", "also_visible"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o644); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}
	}
	entries, err := ListDir(dir, WithSkipHidden(true))
	if err != nil {
		t.Fatalf("ListDir: %v", err)
	}
	if len(entries) != 2 {
		t.Errorf("got %d, want 2 entries", len(entries))
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".") {
			t.Errorf("hidden entry slipped through: %s", e.Name())
		}
	}
}

func TestListDir_Filter(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "subdir"), 0o755); err != nil {
		t.Fatalf("Mkdir: %v", err)
	}
	for _, name := range []string{"a.go", "b.go", "c.txt", "d.md"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o644); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}
	}
	entries, err := ListDir(dir, WithListFilter(func(e stdfs.DirEntry) bool {
		return !e.IsDir() && strings.HasSuffix(e.Name(), ".go")
	}))
	if err != nil {
		t.Fatalf("ListDir: %v", err)
	}
	if len(entries) != 2 {
		t.Errorf("got %d, want 2", len(entries))
	}
}

func TestListDir_Missing(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	_, err := ListDir(filepath.Join(dir, "missing"))
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("got %v, want ErrNotFound", err)
	}
}

func TestListDir_Wide(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	const n = 1500
	for i := range n {
		if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("f%04d", i)), []byte("x"), 0o644); err != nil {
			t.Fatalf("WriteFile %d: %v", i, err)
		}
	}
	entries, err := ListDir(dir, WithSorted(true))
	if err != nil {
		t.Fatalf("ListDir: %v", err)
	}
	if len(entries) != n {
		t.Errorf("got %d, want %d", len(entries), n)
	}
	for i := range n {
		want := fmt.Sprintf("f%04d", i)
		if entries[i].Name() != want {
			t.Errorf("entries[%d] = %s, want %s", i, entries[i].Name(), want)
			break
		}
	}
}

// --- IsEmpty ---

func TestIsEmpty_True(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	empty, err := IsEmpty(dir)
	if err != nil {
		t.Fatalf("IsEmpty: %v", err)
	}
	if !empty {
		t.Error("empty dir reported non-empty")
	}
}

func TestIsEmpty_False(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "f"), []byte("x"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	empty, err := IsEmpty(dir)
	if err != nil {
		t.Fatalf("IsEmpty: %v", err)
	}
	if empty {
		t.Error("non-empty dir reported empty")
	}
}

func TestIsEmpty_Missing(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	_, err := IsEmpty(filepath.Join(dir, "missing"))
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("got %v, want ErrNotFound", err)
	}
}

func TestIsEmpty_OnFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "f")
	if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	_, err := IsEmpty(path)
	if !errors.Is(err, ErrNotDir) {
		t.Errorf("got %v, want ErrNotDir", err)
	}
}

// --- DirSize ---

func TestDirSize_SumsRegularFiles(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a"), []byte("aaaa"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	sub := filepath.Join(dir, "sub")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatalf("Mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(sub, "b"), []byte("bbbbbbb"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	got, err := DirSize(context.Background(), dir)
	if err != nil {
		t.Fatalf("DirSize: %v", err)
	}
	if got != 11 {
		t.Errorf("DirSize = %d, want 11", got)
	}
}

func TestDirSize_Empty(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	got, err := DirSize(context.Background(), dir)
	if err != nil {
		t.Fatalf("DirSize: %v", err)
	}
	if got != 0 {
		t.Errorf("DirSize = %d, want 0", got)
	}
}

func TestDirSize_ContextCanceled(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	for i := range 50 {
		if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("f%d", i)), []byte("x"), 0o644); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := DirSize(ctx, dir)
	if !errors.Is(err, context.Canceled) {
		t.Errorf("got %v, want context.Canceled", err)
	}
}

func TestDirSize_DeadlineExceeded(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	for i := range 200 {
		if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("f%03d", i)), []byte("xx"), 0o644); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Nanosecond)
	defer cancel()
	time.Sleep(2 * time.Millisecond)
	_, err := DirSize(ctx, dir)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("got %v, want context.DeadlineExceeded", err)
	}
}

// --- Fault-injection: IsEmpty / ListDir / MkdirAll ---
//
// These tests swap package-level OS hooks (see fault_hooks.go) to
// exercise defensive error branches that real I/O can't easily
// provoke. None call t.Parallel — the hooks are package-global.

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

func TestFault_MkdirAll_EnforcePerm_ChmodError(t *testing.T) {
	h := newFaultyHooks(t)
	dir := t.TempDir()
	h.failChmodAlways()
	err := MkdirAll(filepath.Join(dir, "a", "b", "c"), 0o755, WithEnforcePerm(true))
	if !errors.Is(err, errInjected) {
		t.Errorf("got %v, want errInjected", err)
	}
}

// --- DirSize / IsEmpty / MkdirAll error paths ---

func TestDirSize_MissingPath(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	_, err := DirSize(context.Background(), filepath.Join(dir, "missing"))
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("got %v, want ErrNotFound", err)
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

func TestMkdirAll_OnReadonlyParent(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX perms only")
	}
	if os.Getuid() == 0 {
		t.Skip("root bypasses POSIX perms")
	}
	t.Parallel()
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatalf("Chmod %s: %v", dir, err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })
	if err := MkdirAll(filepath.Join(dir, "child"), 0o755); err == nil {
		t.Error("expected error creating child of read-only dir")
	}
}

// --- ListDir filter + sort + skipHidden combination ---

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
