//go:build !windows

package fs

import (
	"errors"
	"os"
	"syscall"
	"unsafe"
)

// isBrokenPipeErr reports whether err is the POSIX EPIPE returned
// when writing to a pipe whose read end has closed.
func isBrokenPipeErr(err error) bool {
	return errors.Is(err, syscall.EPIPE)
}

// isTerminal queries termios via TIOCGETA/TCGETS. A successful
// query indicates a TTY; ENOTTY (or any other error) indicates
// non-terminal. The exact ioctl number differs between Linux/freebsd
// and darwin; the syscall package's TIOCGETA / TCGETS constants
// provide the right value per platform via build-tagged shims.
func isTerminal(f *os.File) bool {
	var t syscall.Termios
	_, _, errno := syscall.Syscall6(
		syscall.SYS_IOCTL,
		f.Fd(),
		uintptr(ioctlGetTermios),
		uintptr(unsafe.Pointer(&t)),
		0, 0, 0,
	)
	return errno == 0
}
