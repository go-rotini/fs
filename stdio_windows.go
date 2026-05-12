//go:build windows

package fs

import (
	"errors"
	"os"
	"syscall"
)

// isTerminal queries the console mode for f's underlying handle.
// Non-console handles (pipes, redirected files) fail with
// ERROR_INVALID_HANDLE; the predicate returns false.
func isTerminal(f *os.File) bool {
	var mode uint32
	err := syscall.GetConsoleMode(syscall.Handle(f.Fd()), &mode)
	return err == nil
}

// winErrorNoData is ERROR_NO_DATA ("The pipe is being closed"),
// returned when writing to an anonymous pipe whose read end has
// closed. Go's syscall package exposes ERROR_BROKEN_PIPE (the
// named-pipe case) but not this one, so it's named here.
const winErrorNoData = syscall.Errno(232)

// isBrokenPipeErr reports whether err is one of the Win32 errors
// returned when writing to a pipe whose read end has closed. EPIPE
// is included for parity with POSIX in case a higher layer maps it.
func isBrokenPipeErr(err error) bool {
	return errors.Is(err, syscall.EPIPE) ||
		errors.Is(err, syscall.ERROR_BROKEN_PIPE) ||
		errors.Is(err, winErrorNoData)
}
