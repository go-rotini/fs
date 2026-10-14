package fs

import "os"

const (
	opOpenNoFollow = "opennofollow"
	opOpenAt       = "openat"
)

// OpenNoFollow opens path without following a symlink at the final
// component. If the final component is a symlink, the error wraps
// [ErrSymlinkLoop]. Defends against link-replace attacks where an
// attacker swaps the target between a stat and an open.
//
// On POSIX this is `O_NOFOLLOW`, which fails atomically. On Windows the
// path is opened as a reparse point and refused if it is one
// (symbolic links, junctions and mount points), and perm has no
// effect.
//
// Intermediate components are still resolved normally; if `/a/b`
// is a symlink and you open `/a/b/c`, the symlink at `b` is
// followed but the final component `c` is not.
//
// The flag and perm arguments mirror [os.OpenFile].
func OpenNoFollow(path string, flag int, perm os.FileMode) (*os.File, error) {
	return openNoFollow(path, flag, perm)
}

// OpenAt opens name relative to dir. On POSIX it uses openat(2), which
// resolves name through dir's open file descriptor rather than
// re-walking dir's path, defending against an attacker swapping a
// directory for a symlink between calls. It does not confine name:
// an absolute name ignores dir, and `..` or symlinks in name are
// followed. On Windows it falls back to [filepath.Join] and
// [os.OpenFile], which is not race-safe.
//
// A nil dir returns an error wrapping [ErrInvalidPath]. The flag and
// perm arguments mirror [os.OpenFile].
func OpenAt(dir *os.File, name string, flag int, perm os.FileMode) (*os.File, error) {
	return openAt(dir, name, flag, perm)
}
