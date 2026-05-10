//go:build windows

package fs

import (
	"strings"
	"testing"
)

func TestLongPath_AddsPrefix(t *testing.T) {
	t.Parallel()
	long := `C:\` + strings.Repeat("a", 300)
	got, err := LongPath(long)
	if err != nil {
		t.Fatalf("LongPath: %v", err)
	}
	if !strings.HasPrefix(got, `\\?\`) {
		t.Errorf("expected \\?\\ prefix; got %q", got)
	}
}

func TestLongPath_AlreadyPrefixed(t *testing.T) {
	t.Parallel()
	in := `\\?\C:\` + strings.Repeat("a", 300)
	got, err := LongPath(in)
	if err != nil {
		t.Fatalf("LongPath: %v", err)
	}
	if got != in {
		t.Errorf("already-prefixed path was modified: %q", got)
	}
}

func TestLongPath_UNC(t *testing.T) {
	t.Parallel()
	in := `\\server\share\` + strings.Repeat("a", 300)
	got, err := LongPath(in)
	if err != nil {
		t.Fatalf("LongPath: %v", err)
	}
	if !strings.HasPrefix(got, `\\?\UNC\`) {
		t.Errorf("UNC long path missing \\?\\UNC\\ prefix: %q", got)
	}
}
