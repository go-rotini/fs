//go:build !windows

package fs

import "os"

// openSharedRead opens path read-only. On POSIX a plain open already
// permits rename and unlink of the open file, so this is just os.Open.
func openSharedRead(path string) (*os.File, error) {
	return os.Open(path) //nolint:wrapcheck // caller wraps via *PathError
}
