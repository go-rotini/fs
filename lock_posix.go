//go:build unix

package fs

import (
	"errors"
	"os"
	"syscall"
)

// platformPIDAlive is the POSIX variant of [pidAlive]. Signal 0
// returns success iff the target exists; EPERM means "exists but I
// can't signal it" (still alive).
func platformPIDAlive(pid int) bool {
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	err = proc.Signal(syscall.Signal(0))
	if err == nil {
		return true
	}
	return errors.Is(err, syscall.EPERM)
}
