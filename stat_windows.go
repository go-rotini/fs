//go:build windows

package fs

import (
	"os"
	"syscall"
	"time"
)

// Atime returns path's access time.
func Atime(path string) (time.Time, error) {
	d, err := winAttrs(path, opAtime)
	if err != nil {
		return time.Time{}, err
	}
	return time.Unix(0, d.LastAccessTime.Nanoseconds()), nil
}

// Ctime returns path's creation time on Windows, which has no POSIX
// status-change time; it is the same value as [BTime].
func Ctime(path string) (time.Time, error) {
	d, err := winAttrs(path, opCtime)
	if err != nil {
		return time.Time{}, err
	}
	return time.Unix(0, d.CreationTime.Nanoseconds()), nil
}

// BTime returns path's creation time on Windows.
func BTime(path string) (time.Time, error) {
	d, err := winAttrs(path, opBtime)
	if err != nil {
		return time.Time{}, err
	}
	return time.Unix(0, d.CreationTime.Nanoseconds()), nil
}

// Owner returns [ErrNotSupported] on Windows. The POSIX uid/gid
// concept does not map onto Windows ACLs.
func Owner(path string) (uid, gid int, err error) {
	return -1, -1, wrapPathError(opOwner, path, ErrNotSupported)
}

// SameDevice reports whether a and b live on the same volume by
// comparing the volume serial numbers from BY_HANDLE_FILE_INFORMATION.
func SameDevice(a, b string) (bool, error) {
	idA, err := winVolumeSerial(a)
	if err != nil {
		return false, wrapPathError(opSameDev, a, err)
	}
	idB, err := winVolumeSerial(b)
	if err != nil {
		return false, wrapPathError(opSameDev, b, err)
	}
	return idA == idB, nil
}

// winAttrs returns the Win32FileAttributeData for path or wraps the
// error with op.
func winAttrs(path, op string) (*syscall.Win32FileAttributeData, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, wrapPathError(op, path, err)
	}
	d, ok := info.Sys().(*syscall.Win32FileAttributeData)
	if !ok {
		return nil, wrapPathError(op, path, ErrNotSupported)
	}
	return d, nil
}

// winVolumeSerial returns the volume serial number containing path.
func winVolumeSerial(path string) (uint32, error) {
	long, err := LongPath(path)
	if err != nil {
		return 0, err
	}
	pathPtr, err := syscall.UTF16PtrFromString(long)
	if err != nil {
		return 0, err //nolint:wrapcheck // wrapped by caller via *PathError
	}
	h, err := syscall.CreateFile(
		pathPtr,
		0, // no access required for volume info
		syscall.FILE_SHARE_READ|syscall.FILE_SHARE_WRITE|syscall.FILE_SHARE_DELETE,
		nil,
		syscall.OPEN_EXISTING,
		syscall.FILE_FLAG_BACKUP_SEMANTICS, // allows opening directories
		0,
	)
	if err != nil {
		return 0, err //nolint:wrapcheck // wrapped by caller via *PathError
	}
	defer func() { _ = syscall.CloseHandle(h) }() //nolint:errcheck // a failed close only leaks the handle

	var info syscall.ByHandleFileInformation
	if err := syscall.GetFileInformationByHandle(h, &info); err != nil {
		return 0, err //nolint:wrapcheck // wrapped by caller via *PathError
	}
	return info.VolumeSerialNumber, nil
}
