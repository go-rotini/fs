//go:build unix

package fs

import (
	"errors"
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"
)

// openNoFollow opens path with O_NOFOLLOW. Like [os.OpenFile] it sets
// O_CLOEXEC, so the descriptor isn't inherited by child processes, and
// retries on EINTR.
func openNoFollow(path string, flag int, perm os.FileMode) (*os.File, error) {
	fd, err := retryEINTR(func() (int, error) {
		return unix.Open(path, flag|unix.O_NOFOLLOW|unix.O_CLOEXEC, uint32(perm.Perm()))
	})
	if err != nil {
		if isNoFollowErrno(err) {
			return nil, wrapPathError(opOpenNoFollow, path, ErrSymlinkLoop)
		}
		return nil, wrapPathError(opOpenNoFollow, path, err)
	}
	return os.NewFile(uintptr(fd), filepath.Base(path)), nil
}

// openAt resolves name through dir's file descriptor with openat(2),
// adding O_CLOEXEC and retrying on EINTR.
func openAt(dir *os.File, name string, flag int, perm os.FileMode) (*os.File, error) {
	if dir == nil {
		return nil, wrapPathError(opOpenAt, name, ErrInvalidPath)
	}
	fd, err := retryEINTR(func() (int, error) {
		return unix.Openat(int(dir.Fd()), name, flag|unix.O_CLOEXEC, uint32(perm.Perm()))
	})
	if err != nil {
		return nil, wrapPathError(opOpenAt, name, err)
	}
	return os.NewFile(uintptr(fd), name), nil
}

func retryEINTR(open func() (int, error)) (int, error) {
	for {
		fd, err := open()
		if !errors.Is(err, unix.EINTR) {
			return fd, err
		}
	}
}
