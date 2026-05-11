//go:build !windows

package fs

import "syscall"

// IsExecutable reports whether path exists and is executable for the
// calling process. Uses access(2) with X_OK so the answer reflects
// effective uid/gid and ACLs, not just the mode bits.
func IsExecutable(path string) bool {
	if !existsPredicate(path) {
		return false
	}
	// On POSIX, only regular files are meaningfully "executable";
	// directories return EACCES with X_OK semantics that don't match
	// the predicate's intent. Filter to regular files first.
	if !IsFile(path) {
		return false
	}
	return syscall.Access(path, xOK) == nil
}

// IsReadable reports whether the calling process can read path.
func IsReadable(path string) bool {
	if !existsPredicate(path) {
		return false
	}
	return syscall.Access(path, rOK) == nil
}

// IsWritable reports whether the calling process can write path. For
// directories this means new entries can be created inside.
func IsWritable(path string) bool {
	if !existsPredicate(path) {
		return false
	}
	return syscall.Access(path, wOK) == nil
}

// access(2) mode bits. stdlib's syscall package doesn't expose these
// as named constants on every supported OS, so the package defines
// them locally; the values match POSIX X/OK / R_OK / W_OK.
const (
	xOK = 0x1 // executable
	wOK = 0x2 // writable
	rOK = 0x4 // readable
)
