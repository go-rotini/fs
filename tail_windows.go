//go:build windows

package fs

import (
	"errors"
	"os"
	"syscall"

	"golang.org/x/sys/windows"
)

// openSharedRead opens path read-only with FILE_SHARE_DELETE in
// addition to the usual read/write sharing. Go's os.Open omits
// FILE_SHARE_DELETE, which makes a logrotate-style rename (or unlink)
// of the file fail with a sharing violation while Tail holds the
// handle; granting it lets rename-based rotation work on Windows the
// way it does on POSIX.
func openSharedRead(path string) (*os.File, error) {
	long, err := LongPath(path)
	if err != nil {
		return nil, err
	}
	p, err := syscall.UTF16PtrFromString(long)
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

// openSharedAppend opens path for appending, creating it with perm, as
// os.OpenFile(path, O_WRONLY|O_CREATE|O_APPEND, perm) does, but also
// grants FILE_SHARE_DELETE. Without it, another Rotator's rename of the
// file while this one holds it fails with a sharing violation, and
// neither rotates.
func openSharedAppend(path string, perm os.FileMode) (*os.File, error) {
	long, err := LongPath(path)
	if err != nil {
		return nil, err
	}
	p, err := windows.UTF16PtrFromString(long)
	if err != nil {
		return nil, err //nolint:wrapcheck // caller wraps via *PathError
	}
	// The access rights os.OpenFile asks for with O_APPEND: everything
	// GENERIC_WRITE grants except FILE_WRITE_DATA, so every write lands
	// at the end of the file.
	access := uint32(windows.FILE_APPEND_DATA | windows.FILE_WRITE_ATTRIBUTES | windows.FILE_WRITE_EA |
		windows.STANDARD_RIGHTS_WRITE | windows.SYNCHRONIZE)
	attrs := uint32(windows.FILE_ATTRIBUTE_NORMAL)
	if perm&0o200 == 0 {
		attrs = windows.FILE_ATTRIBUTE_READONLY
	}
	h, err := windows.CreateFile(p, access,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE, nil,
		windows.OPEN_ALWAYS, attrs, 0)
	if err != nil {
		// As os.OpenFile reports it: a directory refuses write access.
		if errors.Is(err, windows.ERROR_ACCESS_DENIED) {
			if fa, aerr := windows.GetFileAttributes(p); aerr == nil && fa&windows.FILE_ATTRIBUTE_DIRECTORY != 0 {
				return nil, syscall.EISDIR
			}
		}
		return nil, err //nolint:wrapcheck // caller wraps via *PathError
	}
	return os.NewFile(uintptr(h), path), nil
}
