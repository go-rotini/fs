package fs

import "os"

const (
	opOpenNoFollow = "opennofollow"
	opOpenAt       = "openat"
)

// OpenNoFollow opens path without following a symlink at the final
// component. If the final component IS a symlink, the call returns
// [ErrSymlinkLoop] (mapped from POSIX `ELOOP` or the equivalent
// Windows reparse-point error). Defends against link-replace
// attacks where an attacker swaps the target between a stat and an
// open.
//
// Intermediate components are still resolved normally — if `/a/b`
// is a symlink and you open `/a/b/c`, the symlink at `b` is
// followed but the final component `c` is not.
//
// The flag and perm arguments mirror [os.OpenFile].
func OpenNoFollow(path string, flag int, perm os.FileMode) (*os.File, error) {
	return openNoFollow(path, flag, perm)
}

// OpenAt opens name relative to dir. On POSIX it uses the
// `openat(2)` syscall, which resolves name through dir's underlying
// inode rather than re-walking the path — this defends against
// directory-replace races where an attacker swaps a directory for a
// symlink between calls.
//
// On Windows, where there is no native `openat` equivalent in the
// stable API surface, the implementation falls back to
// [filepath.Join] + [os.OpenFile]. The fallback is NOT race-safe;
// callers that need TOCTOU resistance on Windows must use other
// hardening (e.g., transactional NTFS, locked parent directories).
//
// dir must be non-nil. name is interpreted as a relative path
// component; absolute paths are ignored by the underlying syscall.
func OpenAt(dir *os.File, name string, flag int, perm os.FileMode) (*os.File, error) {
	return openAt(dir, name, flag, perm)
}
