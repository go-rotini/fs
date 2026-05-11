package fs

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestChownRecursive_WindowsReturnsNotSupported(t *testing.T) {
	t.Parallel()
	if runtime.GOOS != "windows" {
		t.Skip("Windows-only test")
	}
	err := ChownRecursive(t.TempDir(), 0, 0)
	if !errors.Is(err, ErrNotSupported) {
		t.Errorf("err = %v; want ErrNotSupported", err)
	}
}

func TestChownRecursive_PosixNoOpChange(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("POSIX-only test")
	}
	root := t.TempDir()
	mustMkdir(t, filepath.Join(root, "sub"))
	mustWrite(t, filepath.Join(root, "sub", "f.txt"), "x")

	// Pass uid=-1 gid=-1 which is "no change" on POSIX. Verifies the
	// walk runs over every entry without errors.
	if err := ChownRecursive(root, -1, -1); err != nil {
		// On systems where the test user can't Chown at all, expect
		// EPERM; treat as informational rather than a hard fail.
		if os.IsPermission(err) {
			t.Skipf("skipping: %v", err)
		}
		t.Fatalf("ChownRecursive: %v", err)
	}
}
