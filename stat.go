package fs

import (
	"os"
	"time"
)

// Operation labels for *PathError.Op on stat-layer errors.
const (
	opStat     = "stat"
	opLstat    = "lstat"
	opMtime    = "mtime"
	opAtime    = "atime"
	opCtime    = "ctime"
	opBtime    = "btime"
	opOwner    = "owner"
	opSetTimes = "settimes"
	opTouch    = "touch"
	opSameDev  = "samedevice"
)

// Stat wraps [os.Stat] with the package's error chain. The returned
// FileInfo is the stdlib type.
func Stat(path string) (os.FileInfo, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, wrapPathError(opStat, path, err)
	}
	return info, nil
}

// Lstat is the symlink-not-following variant of [Stat].
func Lstat(path string) (os.FileInfo, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, wrapPathError(opLstat, path, err)
	}
	return info, nil
}

// Mtime returns path's modification time.
func Mtime(path string) (time.Time, error) {
	info, err := os.Stat(path)
	if err != nil {
		return time.Time{}, wrapPathError(opMtime, path, err)
	}
	return info.ModTime(), nil
}

// SetMtime sets path's modification time. Atime is preserved.
func SetMtime(path string, t time.Time) error {
	atime, err := Atime(path)
	if err != nil {
		return err
	}
	if err := osChtimes(path, atime, t); err != nil {
		return wrapPathError(opSetTimes, path, err)
	}
	return nil
}

// SetAtime sets path's access time. Mtime is preserved.
func SetAtime(path string, t time.Time) error {
	mtime, err := Mtime(path)
	if err != nil {
		return err
	}
	if err := osChtimes(path, t, mtime); err != nil {
		return wrapPathError(opSetTimes, path, err)
	}
	return nil
}

// SetTimes sets both atime and mtime in one syscall.
func SetTimes(path string, atime, mtime time.Time) error {
	if err := osChtimes(path, atime, mtime); err != nil {
		return wrapPathError(opSetTimes, path, err)
	}
	return nil
}

// SameFile reports whether a and b refer to the same file (same
// dev+inode on POSIX, same volume serial + file index on Windows).
// Wraps [os.SameFile] with path-string ergonomics.
func SameFile(a, b string) (bool, error) {
	infoA, err := os.Stat(a)
	if err != nil {
		return false, wrapPathError(opStat, a, err)
	}
	infoB, err := os.Stat(b)
	if err != nil {
		return false, wrapPathError(opStat, b, err)
	}
	return os.SameFile(infoA, infoB), nil
}

// TouchOption configures [Touch].
type TouchOption func(*touchOptions)

type touchOptions struct {
	atime, mtime time.Time
	hasTimes     bool
	perm         os.FileMode
	hasPerm      bool
}

// WithTimes overrides Touch's "now" with explicit atime/mtime.
func WithTimes(atime, mtime time.Time) TouchOption {
	return func(o *touchOptions) {
		o.atime = atime
		o.mtime = mtime
		o.hasTimes = true
	}
}

// WithTouchPerm sets the mode for newly-created files. Existing files
// keep their mode.
func WithTouchPerm(mode os.FileMode) TouchOption {
	return func(o *touchOptions) {
		o.perm = mode
		o.hasPerm = true
	}
}

// Touch creates path with the given mode (default 0o644) if missing,
// or updates its mtime to "now" if it exists. [WithTimes] overrides
// the default "now" behavior.
func Touch(path string, opts ...TouchOption) error {
	cfg := touchOptions{perm: Mode0644}
	for _, opt := range opts {
		opt(&cfg)
	}

	atime := cfg.atime
	mtime := cfg.mtime
	if !cfg.hasTimes {
		atime = time.Now()
		mtime = atime
	}

	if !Exists(path) {
		f, err := osOpenFile(path, os.O_CREATE|os.O_WRONLY, cfg.perm)
		if err != nil {
			return wrapPathError(opTouch, path, err)
		}
		if cerr := fileClose(f); cerr != nil {
			return wrapPathError(opTouch, path, cerr)
		}
	}
	if err := osChtimes(path, atime, mtime); err != nil {
		return wrapPathError(opTouch, path, err)
	}
	return nil
}
