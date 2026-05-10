//go:build darwin

package fs

import (
	"syscall"
	"unsafe"
)

// sysOpenAt is the openat(2) syscall number on darwin (xnu / macOS).
// The stdlib's syscall package generates this from
// bsd/kern/syscalls.master but does not export the constant; the
// value is sourced from Apple's syscall header.
const sysOpenAt = 463

func openAtSyscall(dirfd int, path string, flags int, mode uint32) (int, error) {
	p, err := syscall.BytePtrFromString(path)
	if err != nil {
		return 0, err //nolint:wrapcheck // outer openAt wraps via *PathError
	}
	fd, _, errno := syscall.Syscall6(
		sysOpenAt,
		uintptr(dirfd),
		uintptr(unsafe.Pointer(p)),
		uintptr(flags),
		uintptr(mode),
		0, 0,
	)
	if errno != 0 {
		return 0, errno
	}
	return int(fd), nil
}
