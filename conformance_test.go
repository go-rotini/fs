package fs

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

// Conformance tests exercise filesystem-semantic guarantees the
// package promises. Each test name starts with `TestConformance`
// so `make test-conformance` (which uses `-run TestConformance`)
// can target them.

// TestConformanceAtomicWrite verifies the atomic-rename guarantee:
// readers concurrent with a [WriteFile] overwrite always observe
// either the OLD contents in full or the NEW contents in full —
// never a partial / interleaved view.
func TestConformanceAtomicWrite(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "config")

	old := []byte(strings.Repeat("OLD-", 4096))  // 16 KiB
	newC := []byte(strings.Repeat("NEW-", 4096)) // 16 KiB
	if err := os.WriteFile(path, old, 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}

	stop := make(chan struct{})
	var bad atomic.Int32
	var wg sync.WaitGroup

	// Start a flock of readers.
	for range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				data, err := os.ReadFile(path)
				if err != nil {
					// During rename the path is briefly unavailable on
					// some filesystems; skip transient ENOENT.
					continue
				}
				switch string(data) {
				case string(old), string(newC):
					// Acceptable.
				default:
					bad.Add(1)
				}
			}
		}()
	}

	// Run several overwrites.
	for range 32 {
		if err := WriteFile(path, newC); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}
		if err := WriteFile(path, old); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}
	}
	close(stop)
	wg.Wait()

	if got := bad.Load(); got > 0 {
		t.Errorf("readers observed %d torn writes; want 0 (atomic-write guarantee)", got)
	}
}

// TestConformanceSymlinkLoopDetection verifies that EvalSymlinks
// surfaces a → b → a as [ErrSymlinkLoop] within a bounded number
// of hops — not by hanging or recursing forever.
func TestConformanceSymlinkLoopDetection(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation typically requires elevation on Windows")
	}
	t.Parallel()
	dir := t.TempDir()
	a := filepath.Join(dir, "a")
	b := filepath.Join(dir, "b")
	if err := os.Symlink(b, a); err != nil {
		t.Fatalf("Symlink a → b: %v", err)
	}
	if err := os.Symlink(a, b); err != nil {
		t.Fatalf("Symlink b → a: %v", err)
	}

	_, err := EvalSymlinks(a)
	if !errors.Is(err, ErrSymlinkLoop) {
		t.Errorf("got %v, want ErrSymlinkLoop", err)
	}
}

// TestConformancePosixCaseSensitive verifies that on case-sensitive
// filesystems, foo and FOO are distinct paths under [EqualPath]
// and via Stat. Skipped on macOS (default APFS is case-insensitive)
// and Windows.
func TestConformancePosixCaseSensitive(t *testing.T) {
	if runtime.GOOS != "linux" && runtime.GOOS != "freebsd" {
		t.Skip("conformance applies only on case-sensitive filesystems")
	}
	t.Parallel()
	dir := t.TempDir()

	lower := filepath.Join(dir, "foo.txt")
	upper := filepath.Join(dir, "FOO.txt")
	if err := os.WriteFile(lower, []byte("lower"), 0o644); err != nil {
		t.Fatalf("WriteFile lower: %v", err)
	}
	if err := os.WriteFile(upper, []byte("UPPER"), 0o644); err != nil {
		t.Fatalf("WriteFile upper: %v", err)
	}

	got1, _ := os.ReadFile(lower)
	got2, _ := os.ReadFile(upper)
	if string(got1) == string(got2) {
		t.Errorf("case-sensitive fs returned same content for foo.txt and FOO.txt")
	}

	if EqualPath(lower, upper) {
		t.Error("EqualPath should distinguish case on case-sensitive fs")
	}
}

// TestConformanceZipSlipDefense verifies the archive surface
// refuses extracting an entry whose resolved path escapes dst.
// (Coverage duplicate of Phase 23 Zip-slip test, listed here as a
// conformance check so it surfaces in conformance-only runs.)
func TestConformanceZipSlipDefense(t *testing.T) {
	t.Parallel()
	dst := t.TempDir()
	tarBytes := makeMaliciousTar(t)

	err := ExtractArchive(bytes.NewReader(tarBytes), dst)
	if !errors.Is(err, ErrEscapesRoot) {
		t.Errorf("got %v, want ErrEscapesRoot", err)
	}
	if Exists(filepath.Join(filepath.Dir(dst), "escape.txt")) {
		t.Error("zip-slip-style entry escaped the dst boundary")
	}
}

// TestConformanceTOCTOUOpenNoFollow verifies [OpenNoFollow] refuses
// a path whose final component is a symlink — defending against
// the link-replace race where an attacker swaps a regular file for
// a symlink between Stat and Open.
func TestConformanceTOCTOUOpenNoFollow(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation typically requires elevation on Windows")
	}
	t.Parallel()
	dir := t.TempDir()
	target := filepath.Join(dir, "target")
	link := filepath.Join(dir, "link")
	if err := os.WriteFile(target, []byte("safe"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Fatalf("Symlink: %v", err)
	}

	_, err := OpenNoFollow(link, os.O_RDONLY, 0)
	if !errors.Is(err, ErrSymlinkLoop) {
		t.Errorf("got %v, want ErrSymlinkLoop (final-component symlink refused)", err)
	}
}
