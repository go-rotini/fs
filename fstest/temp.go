package fstest

import (
	"os"
	"testing"

	"github.com/go-rotini/fs"
)

// TempFileT is [fs.TempFile] with cleanup auto-registered via
// [testing.T.Cleanup]. Tests get the open file directly and never
// have to manage cleanup themselves.
func TempFileT(t *testing.T, pattern string) *os.File {
	t.Helper()
	f, cleanup, err := fs.TempFile("", pattern)
	if err != nil {
		t.Fatalf("TempFileT: %v", err)
	}
	t.Cleanup(func() {
		// Best-effort cleanup at test teardown; the cleanup func
		// itself is idempotent and reports any remove error from the
		// first invocation.
		_ = cleanup() //nolint:errcheck // see comment above
	})
	return f
}

// TempDirT is [fs.TempDir] with cleanup auto-registered via
// [testing.T.Cleanup]. Tests get the directory path directly.
func TempDirT(t *testing.T, pattern string) string {
	t.Helper()
	dir, cleanup, err := fs.TempDir("", pattern)
	if err != nil {
		t.Fatalf("TempDirT: %v", err)
	}
	t.Cleanup(func() {
		_ = cleanup() //nolint:errcheck // see comment above
	})
	return dir
}
