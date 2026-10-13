//go:build !unix && !windows

package fs

import "os"

// openNoFollow returns [ErrNotSupported]: this platform has no
// O_NOFOLLOW.
func openNoFollow(path string, _ int, _ os.FileMode) (*os.File, error) {
	return nil, wrapPathError(opOpenNoFollow, path, ErrNotSupported)
}

// openAt returns [ErrNotSupported]: this platform has no openat(2).
func openAt(dir *os.File, name string, _ int, _ os.FileMode) (*os.File, error) {
	if dir == nil {
		return nil, wrapPathError(opOpenAt, name, ErrInvalidPath)
	}
	return nil, wrapPathError(opOpenAt, name, ErrNotSupported)
}
