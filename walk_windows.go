//go:build windows

package fs

import (
	stdfs "io/fs"
	"os"
	"strings"
	"syscall"
)

// isHiddenEntry reports whether e is hidden on Windows. Both the
// dot-prefix convention and the FILE_ATTRIBUTE_HIDDEN bit are
// honored. A read failure on the path is treated as "not hidden" so
// the walk continues.
func isHiddenEntry(path string, e stdfs.DirEntry) bool {
	if strings.HasPrefix(e.Name(), ".") {
		return true
	}
	info, err := os.Lstat(path)
	if err != nil {
		return false
	}
	sys, ok := info.Sys().(*syscall.Win32FileAttributeData)
	if !ok {
		return false
	}
	return sys.FileAttributes&syscall.FILE_ATTRIBUTE_HIDDEN != 0
}
