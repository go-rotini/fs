//go:build unix

package fs

import (
	"errors"
	"os"
	"syscall"

	"golang.org/x/sys/unix"
)

// isBrokenPipeErr reports whether err is the POSIX EPIPE returned
// when writing to a pipe whose read end has closed.
func isBrokenPipeErr(err error) bool {
	return errors.Is(err, syscall.EPIPE)
}

// isTerminal queries termios (TIOCGETA on darwin and the BSDs, TCGETS
// elsewhere). A successful query indicates a TTY; ENOTTY (or any other
// error) indicates non-terminal.
func isTerminal(f *os.File) bool {
	_, err := unix.IoctlGetTermios(int(f.Fd()), ioctlGetTermios)
	return err == nil
}
