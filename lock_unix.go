//go:build !windows

package fs

import (
	"errors"
	"os"
	"syscall"
)

// osFlock applies the requested lock kind to f via [syscall.Flock].
// Returns [errLockBusy] when a non-blocking acquire fails because the
// lock is held; returns the raw syscall error otherwise.
//
// POSIX advisory locking via flock(2) is per-open-file-description on
// Linux/BSD/macOS (the lock is associated with the descriptor that
// called flock, not the process or the inode). Closing the descriptor
// releases the lock; a fork+exec child inherits the lock unless the
// parent uses FD_CLOEXEC. Lock kind compatibility:
//
//   - LOCK_EX (exclusive): one holder at a time.
//   - LOCK_SH (shared): multiple shared holders allowed; mutually
//     exclusive with LOCK_EX.
//   - LOCK_NB (non-blocking): OR'd with EX or SH; returns EWOULDBLOCK
//     when the lock is held by another holder.
func osFlock(f *os.File, kind lockKind) error {
	var how int
	switch kind {
	case lockKindExclusive:
		how = syscall.LOCK_EX
	case lockKindExclusiveNoBlock:
		how = syscall.LOCK_EX | syscall.LOCK_NB
	case lockKindShared:
		how = syscall.LOCK_SH
	default:
		return errInvalidLockKind
	}
	err := syscall.Flock(int(f.Fd()), how)
	if err == nil {
		return nil
	}
	if errors.Is(err, syscall.EWOULDBLOCK) {
		return errLockBusy
	}
	return err //nolint:wrapcheck // wrapped by caller via *PathError
}

// unlockFile releases the advisory lock on f. Closing f also releases
// the lock; this explicit unlock exists so [LockHandle.Release] can
// surface the syscall's error separately from the close's.
func unlockFile(f *os.File) error {
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_UN); err != nil {
		return err //nolint:wrapcheck // wrapped by caller via *PathError
	}
	return nil
}

// platformPIDAlive is the POSIX variant of [pidAlive]. Signal 0
// returns success iff the target exists; EPERM means "exists but I
// can't signal it" (still alive).
func platformPIDAlive(pid int) bool {
	return pidAliveSignal0(pid)
}
