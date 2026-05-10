//go:build !windows

package fs

import (
	"os"
	"syscall"
)

// syncDir fsync's the directory at path so a subsequent power loss
// after a rename inside it sees the updated entry, not the previous
// one. Required by POSIX best practice for atomic-write durability.
//
// Returned errors surface raw; the caller wraps with *PathError.
func syncDir(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err //nolint:wrapcheck // wrapped by caller via *PathError
	}
	defer func() { _ = f.Close() }()
	return f.Sync() //nolint:wrapcheck // wrapped by caller via *PathError
}

// lockFile acquires an exclusive advisory lock on f. Used by Append
// under [WithLocked]. The lock is released when f is closed; the
// caller is responsible for that.
func lockFile(f *os.File) error {
	return syscall.Flock(int(f.Fd()), syscall.LOCK_EX) //nolint:wrapcheck // wrapped by caller
}
