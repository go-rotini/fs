package fs

import (
	"errors"

	"golang.org/x/sys/unix"
)

// isNoFollowErrno reports whether err is how open(2) refuses a symlink
// under O_NOFOLLOW: NetBSD returns EFTYPE.
func isNoFollowErrno(err error) bool {
	return errors.Is(err, unix.EFTYPE) || errors.Is(err, unix.ELOOP)
}
