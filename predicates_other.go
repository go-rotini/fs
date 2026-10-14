//go:build !unix && !windows

package fs

import "os"

// Without access(2), these predicates judge from the permission bits
// alone.

// IsExecutable reports whether path is a regular file with an execute
// bit set.
func IsExecutable(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular() && info.Mode().Perm()&0o111 != 0
}

// IsReadable reports whether path can be opened for reading.
func IsReadable(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	_ = f.Close()
	return true
}

// IsWritable reports whether path has a write bit set.
func IsWritable(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().Perm()&0o222 != 0
}
