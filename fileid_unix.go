//go:build unix || js || wasip1

package fs

import (
	"os"
	"syscall"
)

// fileID identifies a file by device and inode, for detecting that a
// path now names a different file, or that a directory was already
// visited, without another system call.
type fileID struct {
	dev, ino uint64
}

// fileIdentity returns info's device and inode. ok is false when info
// doesn't carry them.
func fileIdentity(info os.FileInfo) (id fileID, ok bool) {
	st, isStat := info.Sys().(*syscall.Stat_t)
	if !isStat || st == nil {
		return fileID{}, false
	}
	return fileID{dev: uint64(st.Dev), ino: uint64(st.Ino)}, true //nolint:unconvert // field types vary by platform
}
