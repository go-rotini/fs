package fs

import "os"

const (
	opCwd     = "cwd"
	opChdir   = "chdir"
	opWithDir = "withdir"
)

// Cwd returns the process's current working directory. Wraps
// [os.Getwd] with the package's error envelope.
func Cwd() (string, error) {
	wd, err := os.Getwd()
	if err != nil {
		return "", wrapPathError(opCwd, "", err)
	}
	return wd, nil
}

// Chdir changes the process's current working directory.
//
// The working directory is process-global, shared by every
// goroutine: do not use Chdir from tests that call t.Parallel, since
// any cwd-relative call in a parallel test (including [filepath.Abs]
// on a relative path) sees the change. Prefer [WithDir] for scoped
// changes, or pass an explicit directory to the code under test.
func Chdir(path string) error {
	if err := os.Chdir(path); err != nil {
		return wrapPathError(opChdir, path, err)
	}
	return nil
}

// WithDir runs fn with the process's current working directory set to
// path, then restores the original cwd. Restoration runs even if fn
// panics (the panic is re-raised after restore).
//
// If fn returns an error, that error is returned. If fn returns nil
// but the post-fn restore fails, the restore error is returned.
//
// Like [Chdir], WithDir changes process-global state and is not safe
// in tests that call t.Parallel.
func WithDir(path string, fn func() error) (err error) {
	orig, gerr := os.Getwd()
	if gerr != nil {
		return wrapPathError(opWithDir, path, gerr)
	}
	if cerr := os.Chdir(path); cerr != nil {
		return wrapPathError(opWithDir, path, cerr)
	}
	defer func() {
		if rerr := os.Chdir(orig); rerr != nil && err == nil {
			err = wrapPathError(opWithDir, orig, rerr)
		}
	}()
	return fn()
}
