//go:build !windows

package fs

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"
)

func openNoFollow(path string, flag int, perm os.FileMode) (*os.File, error) {
	fd, err := syscall.Open(path, flag|syscall.O_NOFOLLOW, uint32(perm.Perm()))
	if err != nil {
		if errors.Is(err, syscall.ELOOP) {
			return nil, wrapPathError(opOpenNoFollow, path, ErrSymlinkLoop)
		}
		return nil, wrapPathError(opOpenNoFollow, path, err)
	}
	return os.NewFile(uintptr(fd), filepath.Base(path)), nil
}

// openAt resolves name through dir's underlying file descriptor via
// the platform's openat(2) syscall. Per-platform `openAtSyscall`
// shims handle the divergence: linux exposes [syscall.Openat]
// directly; darwin/freebsd require [syscall.Syscall6] with a
// hardcoded syscall number because their stdlib syscall packages
// don't surface SYS_OPENAT.
func openAt(dir *os.File, name string, flag int, perm os.FileMode) (*os.File, error) {
	if dir == nil {
		return nil, wrapPathError(opOpenAt, name, ErrInvalidPath)
	}
	fd, err := openAtSyscall(int(dir.Fd()), name, flag, uint32(perm.Perm()))
	if err != nil {
		return nil, wrapPathError(opOpenAt, name, err)
	}
	return os.NewFile(uintptr(fd), name), nil
}
