package fs

import (
	"errors"
	stdfs "io/fs"
	"os"
	"sync"
)

const (
	opTempFile = "tempfile"
	opTempDir  = "tempdir"
)

// TempFile creates a temp file in dir using pattern (see
// [os.CreateTemp]). dir defaults to [os.TempDir] when empty. Returns
// the open file, a cleanup function, and any error.
//
// The cleanup closes the file (best-effort; may already be closed by
// the caller) and removes it. Cleanup is idempotent; repeated calls
// run the work exactly once and return the same error from the first
// invocation; subsequent calls return nil.
func TempFile(dir, pattern string) (*os.File, func() error, error) {
	if dir == "" {
		dir = os.TempDir()
	}
	f, err := os.CreateTemp(dir, pattern)
	if err != nil {
		return nil, nil, wrapPathError(opTempFile, dir, err)
	}

	name := f.Name()
	var (
		once   sync.Once
		runErr error
	)
	cleanup := func() error {
		once.Do(func() {
			_ = f.Close()
			if rerr := os.Remove(name); rerr != nil && !errors.Is(rerr, stdfs.ErrNotExist) {
				runErr = wrapPathError(opTempFile, name, rerr)
			}
		})
		return runErr
	}
	return f, cleanup, nil
}

// TempDir creates a temp directory in dir using pattern (see
// [os.MkdirTemp]). dir defaults to [os.TempDir] when empty. Returns
// the directory path, a cleanup function, and any error.
//
// The cleanup is [os.RemoveAll]. Idempotent; repeated calls run the
// work exactly once.
func TempDir(dir, pattern string) (string, func() error, error) {
	if dir == "" {
		dir = os.TempDir()
	}
	name, err := os.MkdirTemp(dir, pattern)
	if err != nil {
		return "", nil, wrapPathError(opTempDir, dir, err)
	}

	var (
		once   sync.Once
		runErr error
	)
	cleanup := func() error {
		once.Do(func() {
			if rerr := os.RemoveAll(name); rerr != nil {
				runErr = wrapPathError(opTempDir, name, rerr)
			}
		})
		return runErr
	}
	return name, cleanup, nil
}

// TempFileT and TempDirT; `t.Cleanup`-registering convenience
// wrappers; live in [github.com/go-rotini/fs/fstest]. They cannot
// live here without pulling [testing] into every consumer's
// production binary.
