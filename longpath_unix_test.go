//go:build !windows

package fs

import (
	"strings"
	"testing"
)

func TestLongPath_ShortPathPassThrough(t *testing.T) {
	t.Parallel()
	got, err := LongPath("/usr/local/bin")
	if err != nil {
		t.Fatalf("LongPath: %v", err)
	}
	if got != "/usr/local/bin" {
		t.Errorf("short path was modified: %s", got)
	}
}

func TestLongPath_LongPathUnchanged(t *testing.T) {
	t.Parallel()
	long := "/" + strings.Repeat("a", 300)
	got, err := LongPath(long)
	if err != nil {
		t.Fatalf("LongPath: %v", err)
	}
	// On POSIX, LongPath is a pass-through. The Windows-specific prefix
	// behavior is verified by longpath_windows_test.go.
	if got != long {
		t.Errorf("POSIX LongPath modified path: got %s", got)
	}
}
