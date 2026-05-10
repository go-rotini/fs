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
// Process-global state: cwd is shared across every goroutine, not
// goroutine-local. Concurrent callers race; this function is NOT
// safe to use from tests that call `t.Parallel()` — a parallel
// sibling test calling any cwd-relative API (including
// [filepath.Abs] on a relative path) will see the wrong directory
// for the duration of this call. Prefer [WithDir] for scoped
// changes, and confine all cwd-mutating code to serial tests.
//
// For code under test that needs a working directory, prefer
// taking an explicit `dir string` argument over reading
// process cwd.
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
// Process-global state: WithDir is NOT safe to use from tests that
// call `t.Parallel()`. cwd is shared across every goroutine, so a
// parallel sibling test calling [os.Getwd] or any cwd-relative API
// will observe the temporarily-changed directory mid-flight.
// Confine WithDir usage to tests that run serially, or refactor
// the code under test to take an explicit working-directory
// argument.
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
