//go:build !windows

package fs

import "os"

// openSharedRead opens path read-only. On POSIX a plain open already
// permits rename and unlink of the open file, so this is just os.Open.
func openSharedRead(path string) (*os.File, error) {
	return os.Open(path) //nolint:wrapcheck // caller wraps via *PathError
}

// openSharedAppend opens path for appending, creating it with perm. On
// POSIX a plain open already permits rename of the open file, so this
// is just os.OpenFile.
func openSharedAppend(path string, perm os.FileMode) (*os.File, error) {
	return os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_APPEND, perm) //nolint:wrapcheck // caller wraps via *PathError
}
