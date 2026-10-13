//go:build unix && !aix

package fs

import (
	"errors"
	"os"

	"golang.org/x/sys/unix"
)

// osFlock applies the requested lock kind to f via flock(2).
// Returns [errLockBusy] when a non-blocking acquire fails because the
// lock is held; returns the raw syscall error otherwise.
//
// POSIX advisory locking via flock(2) is per-open-file-description on
// Linux/BSD/macOS (the lock is associated with the descriptor that
// called flock, not the process or the inode). Closing the descriptor
// releases the lock. Lock kind compatibility:
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
		how = unix.LOCK_EX
	case lockKindExclusiveNoBlock:
		how = unix.LOCK_EX | unix.LOCK_NB
	case lockKindShared:
		how = unix.LOCK_SH
	default:
		return errInvalidLockKind
	}
	err := flockRetry(f, how)
	if err == nil {
		return nil
	}
	if errors.Is(err, unix.EWOULDBLOCK) {
		return errLockBusy
	}
	return err
}

// flockRetry calls flock(2), retrying when a signal interrupts a
// blocking wait.
func flockRetry(f *os.File, how int) error {
	for {
		err := unix.Flock(int(f.Fd()), how)
		if !errors.Is(err, unix.EINTR) {
			return err //nolint:wrapcheck // wrapped by caller via *PathError
		}
	}
}

// unlockFile releases the advisory lock on f. Closing f also releases
// the lock; this explicit unlock exists so [LockHandle.Release] can
// surface the syscall's error separately from the close's.
func unlockFile(f *os.File) error {
	return flockRetry(f, unix.LOCK_UN)
}

// lockFile acquires an exclusive advisory lock on f. Used by Append
// under [WithLocked]. The lock is released when f is closed; the
// caller is responsible for that.
func lockFile(f *os.File) error {
	return flockRetry(f, unix.LOCK_EX)
}
