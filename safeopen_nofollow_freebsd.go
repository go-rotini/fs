package fs

import (
	"errors"

	"golang.org/x/sys/unix"
)

// isNoFollowErrno reports whether err is how open(2) refuses a symlink
// under O_NOFOLLOW: FreeBSD returns EMLINK.
func isNoFollowErrno(err error) bool {
	return errors.Is(err, unix.EMLINK) || errors.Is(err, unix.ELOOP)
}
