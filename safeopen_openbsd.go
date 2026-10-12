//go:build openbsd

package fs

import "golang.org/x/sys/unix"

// openAtSyscall goes through x/sys/unix's libc wrapper: OpenBSD refuses system calls made
// other than through libc, so the raw syscall used on the other BSDs isn't available.
func openAtSyscall(dirfd int, path string, flags int, mode uint32) (int, error) {
	return unix.Openat(dirfd, path, flags, mode) //nolint:wrapcheck // outer openAt wraps via *PathError
}
