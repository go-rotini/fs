//go:build aix

package fs

import (
	"errors"
	"io"
	"os"

	"golang.org/x/sys/unix"
)

// osFlock locks f with fcntl(2) record locks over the whole file,
// since AIX has no flock(2). Unlike flock, these locks belong to the
// process: a second lock from the same process succeeds instead of
// blocking, and closing any descriptor of the file releases them.
func osFlock(f *os.File, kind lockKind) error {
	lk := unix.Flock_t{Whence: io.SeekStart}
	cmd := unix.F_SETLKW
	switch kind {
	case lockKindExclusive:
		lk.Type = unix.F_WRLCK
	case lockKindExclusiveNoBlock:
		lk.Type = unix.F_WRLCK
		cmd = unix.F_SETLK
	case lockKindShared:
		lk.Type = unix.F_RDLCK
	default:
		return errInvalidLockKind
	}
	err := fcntlLockRetry(f, cmd, &lk)
	if errors.Is(err, unix.EAGAIN) || errors.Is(err, unix.EACCES) {
		return errLockBusy
	}
	return err
}

func fcntlLockRetry(f *os.File, cmd int, lk *unix.Flock_t) error {
	for {
		err := unix.FcntlFlock(f.Fd(), cmd, lk)
		if !errors.Is(err, unix.EINTR) {
			return err //nolint:wrapcheck // wrapped by caller via *PathError
		}
	}
}

func unlockFile(f *os.File) error {
	return fcntlLockRetry(f, unix.F_SETLK, &unix.Flock_t{Type: unix.F_UNLCK, Whence: io.SeekStart})
}

func lockFile(f *os.File) error {
	return osFlock(f, lockKindExclusive)
}
