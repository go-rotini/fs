//go:build !windows && !plan9

package fs

import (
	"errors"
	"syscall"
)

// isNotEmptyErrno reports whether err is the OS's directory-not-empty error.
func isNotEmptyErrno(err error) bool {
	return errors.Is(err, syscall.ENOTEMPTY)
}

// isCrossDeviceErrno reports whether err is the OS's cross-filesystem rename or link error.
func isCrossDeviceErrno(err error) bool {
	return errors.Is(err, syscall.EXDEV)
}

// isLoopErrno reports whether err is ELOOP, too many symlinks.
func isLoopErrno(err error) bool {
	return errors.Is(err, syscall.ELOOP)
}
