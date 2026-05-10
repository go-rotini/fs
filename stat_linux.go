//go:build linux

package fs

import (
	"os"
	"syscall"
	"time"
)

// Atime returns path's access time.
func Atime(path string) (time.Time, error) {
	st, err := rawStat(path, opAtime)
	if err != nil {
		return time.Time{}, err
	}
	return time.Unix(st.Atim.Unix()), nil
}

// Ctime returns path's status-change time.
func Ctime(path string) (time.Time, error) {
	st, err := rawStat(path, opCtime)
	if err != nil {
		return time.Time{}, err
	}
	return time.Unix(st.Ctim.Unix()), nil
}

// BTime returns [ErrNotSupported] on Linux. Birth time is reported
// by the statx(2) syscall introduced in kernel 4.11+, but stdlib's
// syscall package does not expose statx. Callers needing birth time
// on Linux can fall back to a third-party syscall wrapper.
func BTime(path string) (time.Time, error) {
	return time.Time{}, wrapPathError(opBtime, path, ErrNotSupported)
}

// Owner returns path's POSIX uid and gid.
func Owner(path string) (uid, gid int, err error) {
	st, err := rawStat(path, opOwner)
	if err != nil {
		return -1, -1, err
	}
	return int(st.Uid), int(st.Gid), nil
}

// SameDevice reports whether a and b live on the same filesystem.
func SameDevice(a, b string) (bool, error) {
	stA, err := rawStat(a, opSameDev)
	if err != nil {
		return false, err
	}
	stB, err := rawStat(b, opSameDev)
	if err != nil {
		return false, err
	}
	return stA.Dev == stB.Dev, nil
}

func rawStat(path, op string) (*syscall.Stat_t, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, wrapPathError(op, path, err)
	}
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return nil, wrapPathError(op, path, ErrNotSupported)
	}
	return st, nil
}
