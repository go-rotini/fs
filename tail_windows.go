//go:build windows

package fs

import (
	"os"
	"syscall"
)

// openSharedRead opens path read-only with FILE_SHARE_DELETE in
// addition to the usual read/write sharing. Go's os.Open omits
// FILE_SHARE_DELETE, which makes a logrotate-style rename (or unlink)
// of the file fail with a sharing violation while Tail holds the
// handle; granting it lets rename-based rotation work on Windows the
// way it does on POSIX.
func openSharedRead(path string) (*os.File, error) {
	p, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return nil, err //nolint:wrapcheck // caller wraps via *PathError
	}
	h, err := syscall.CreateFile(
		p,
		syscall.GENERIC_READ,
		syscall.FILE_SHARE_READ|syscall.FILE_SHARE_WRITE|syscall.FILE_SHARE_DELETE,
		nil,
		syscall.OPEN_EXISTING,
		syscall.FILE_ATTRIBUTE_NORMAL,
		0,
	)
	if err != nil {
		return nil, err //nolint:wrapcheck // caller wraps via *PathError
	}
	return os.NewFile(uintptr(h), path), nil
}
