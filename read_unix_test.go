//go:build !windows

package fs

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

// mkfifoForTest creates a FIFO at path; t.TempDir handles cleanup.
func mkfifoForTest(_ *testing.T, path string) error {
	return syscall.Mkfifo(path, 0o600)
}

// TestReadFile_FIFOEnforced exercises the LimitReader-at-cap path:
// FIFOs report no size via Stat, so ReadFile falls back to bounded
// streaming reads.
func TestReadFile_FIFOEnforced(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	fifo := filepath.Join(dir, "fifo")
	if err := mkfifoForTest(t, fifo); err != nil {
		t.Skipf("Mkfifo unavailable: %v", err)
	}

	go func() {
		f, err := os.OpenFile(fifo, os.O_WRONLY, 0o600)
		if err != nil {
			return
		}
		defer func() { _ = f.Close() }()
		_, _ = f.Write(bytes.Repeat([]byte("x"), 4096))
	}()

	_, err := ReadFile(fifo, WithMaxSize(64))
	if !errors.Is(err, ErrFileTooLarge) {
		t.Errorf("got %v, want ErrFileTooLarge", err)
	}
}
