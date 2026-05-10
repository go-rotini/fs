//go:build windows

package fs

import (
	"os"
	"path/filepath"
	"syscall"
)

// openNoFollow opens path while refusing a reparse point at the
// final component. The implementation passes FILE_FLAG_OPEN_REPARSE_POINT
// so CreateFile returns a handle to the reparse point itself rather
// than transparently following it, then queries the handle's
// attributes and refuses any FILE_ATTRIBUTE_REPARSE_POINT (covers
// symbolic links, junctions, and mount points — all forms of
// "link-like" reparse Windows surfaces).
//
// On a match, the handle is closed and [ErrSymlinkLoop] is
// returned, matching the POSIX O_NOFOLLOW semantics documented on
// [OpenNoFollow]. The Go flag bits are translated to Win32 access /
// disposition via the same mapping the stdlib uses. perm is
// accepted for cross-platform signature symmetry but Win32
// CreateFile uses NTFS ACLs rather than POSIX mode bits — perm has
// no effect.
//
// Note: this is not race-free. Between CreateFile succeeding and
// GetFileInformationByHandle running, the file COULD in principle be
// swapped — but the handle we hold pins the *original* inode so the
// attribute we read is the attribute of the file we opened, not the
// post-swap file. The remaining race is purely "the attacker swaps
// a real file in for a symlink between the open and the attribute
// query", in which case we'd refuse a now-real file; that's a false
// positive, not a security hole.
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

	// Refuse reparse points (symlinks, junctions, mount points).
	var info syscall.ByHandleFileInformation
	if ierr := syscall.GetFileInformationByHandle(handle, &info); ierr != nil {
		_ = syscall.CloseHandle(handle)
		return nil, wrapPathError(opOpenNoFollow, path, ierr)
	}
	if info.FileAttributes&syscall.FILE_ATTRIBUTE_REPARSE_POINT != 0 {
		_ = syscall.CloseHandle(handle)
		return nil, wrapPathError(opOpenNoFollow, path, ErrSymlinkLoop)
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
