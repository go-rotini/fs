package fs

import (
	"os"
	"syscall"
	"time"
)

// Atime returns path's access time.
func Atime(path string) (time.Time, error) {
	d, err := rawDir(path, opAtime)
	if err != nil {
		return time.Time{}, err
	}
	return time.Unix(int64(d.Atime), 0), nil
}

// Ctime returns [ErrNotSupported]: Plan 9 records no status-change
// time.
func Ctime(path string) (time.Time, error) {
	return time.Time{}, wrapPathError(opCtime, path, ErrNotSupported)
}

// BTime returns [ErrNotSupported]: Plan 9 records no birth time.
func BTime(path string) (time.Time, error) {
	return time.Time{}, wrapPathError(opBtime, path, ErrNotSupported)
}

// Owner returns [ErrNotSupported]: Plan 9 owners are names, not
// numeric ids.
func Owner(path string) (uid, gid int, err error) {
	return -1, -1, wrapPathError(opOwner, path, ErrNotSupported)
}

// SameDevice reports whether a and b are served by the same device.
func SameDevice(a, b string) (bool, error) {
	da, err := rawDir(a, opSameDev)
	if err != nil {
		return false, err
	}
	db, err := rawDir(b, opSameDev)
	if err != nil {
		return false, err
	}
	return da.Type == db.Type && da.Dev == db.Dev, nil
}

func rawDir(path, op string) (*syscall.Dir, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, wrapPathError(op, path, err)
	}
	d, ok := info.Sys().(*syscall.Dir)
	if !ok {
		return nil, wrapPathError(op, path, ErrNotSupported)
	}
	return d, nil
}
