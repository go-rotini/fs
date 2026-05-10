//go:build windows

package fs

import (
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
