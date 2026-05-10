//go:build windows

package fs

import (
	"os"
	"path/filepath"
	"syscall"
)

// openNoFollow opens path with FILE_FLAG_OPEN_REPARSE_POINT so a
// reparse point (symlink, junction) at the final component is
// returned as the reparse point itself rather than transparently
// followed. The Go flag bits are translated to Win32 access /
// disposition via the same mapping the stdlib uses. perm is
// accepted for cross-platform signature symmetry but Win32
// CreateFile uses NTFS ACLs rather than POSIX mode bits — perm has
// no effect.
func openNoFollow(path string, flag int, _ os.FileMode) (*os.File, error) {
	pathp, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return nil, wrapPathError(opOpenNoFollow, path, err)
	}

	var access uint32
	switch {
	case flag&os.O_WRONLY != 0:
		access = syscall.GENERIC_WRITE
	case flag&os.O_RDWR != 0:
		access = syscall.GENERIC_READ | syscall.GENERIC_WRITE
	default:
		access = syscall.GENERIC_READ
	}
	if flag&os.O_APPEND != 0 {
		access &^= syscall.GENERIC_WRITE
		access |= syscall.FILE_APPEND_DATA
	}

	var disposition uint32
	switch {
	case flag&(os.O_CREATE|os.O_EXCL) == os.O_CREATE|os.O_EXCL:
		disposition = syscall.CREATE_NEW
	case flag&(os.O_CREATE|os.O_TRUNC) == os.O_CREATE|os.O_TRUNC:
		disposition = syscall.CREATE_ALWAYS
	case flag&os.O_CREATE != 0:
		disposition = syscall.OPEN_ALWAYS
	case flag&os.O_TRUNC != 0:
		disposition = syscall.TRUNCATE_EXISTING
	default:
		disposition = syscall.OPEN_EXISTING
	}

	share := uint32(syscall.FILE_SHARE_READ | syscall.FILE_SHARE_WRITE | syscall.FILE_SHARE_DELETE)
	attrs := uint32(syscall.FILE_ATTRIBUTE_NORMAL | syscall.FILE_FLAG_OPEN_REPARSE_POINT)

	handle, err := syscall.CreateFile(pathp, access, share, nil, disposition, attrs, 0)
	if err != nil {
		return nil, wrapPathError(opOpenNoFollow, path, err)
	}
	return os.NewFile(uintptr(handle), filepath.Base(path)), nil
}

// openAt is a Join + OpenFile fallback. Documented as not race-safe
// on Windows; callers needing TOCTOU resistance must use other
// hardening.
func openAt(dir *os.File, name string, flag int, perm os.FileMode) (*os.File, error) {
	if dir == nil {
		return nil, wrapPathError(opOpenAt, name, ErrInvalidPath)
	}
	full := filepath.Join(dir.Name(), name)
	f, err := os.OpenFile(full, flag, perm)
	if err != nil {
		return nil, wrapPathError(opOpenAt, full, err)
	}
	return f, nil
}
