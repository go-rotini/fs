//go:build !(darwin || dragonfly || freebsd || netbsd || openbsd || linux || windows)

package fs

import "strconv"

// platformProcessStartTime returns [ErrNotSupported]: this platform
// has no supported way to read a process's start time.
func platformProcessStartTime(pid int) (string, error) {
	return "", wrapPathError(opProcStartTime, strconv.Itoa(pid), ErrNotSupported)
}
