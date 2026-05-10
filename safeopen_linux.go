//go:build linux

package fs

import "syscall"

func openAtSyscall(dirfd int, path string, flags int, mode uint32) (int, error) {
	return syscall.Openat(dirfd, path, flags, mode) //nolint:wrapcheck // outer openAt wraps via *PathError
}
