//go:build windows

package fs

import (
	"os"
	"path/filepath"
	"unsafe"

	"golang.org/x/sys/windows"
)

// reparseTagNameSurrogate is the reparse-tag bit Windows sets on reparse points that stand
// for another file or directory, such as symlinks and directory junctions.
const reparseTagNameSurrogate = 0x20000000

// isLink reports whether path, whose Lstat result is info, is a link that a path lookup
// follows: a symlink, or a directory junction, which Lstat reports as irregular. Any other
// reparse point that redirects a lookup fails with [ErrEscapesRoot], since where it leads
// can't be read.
func isLink(path string, info os.FileInfo) (bool, error) {
	mode := info.Mode()
	if mode&os.ModeSymlink != 0 {
		return true, nil
	}
	if mode&os.ModeIrregular == 0 {
		return false, nil
	}
	tag, err := reparseTag(path)
	if err != nil {
		return false, err
	}
	switch {
	case tag == windows.IO_REPARSE_TAG_MOUNT_POINT, tag == windows.IO_REPARSE_TAG_SYMLINK:
		return true, nil
	case tag&reparseTagNameSurrogate != 0:
		return false, ErrEscapesRoot
	default:
		return false, nil
	}
}

// fileAttributeTagInfo is FILE_ATTRIBUTE_TAG_INFO.
type fileAttributeTagInfo struct {
	FileAttributes uint32
	ReparseTag     uint32
}

// reparseTag returns the reparse tag of path itself, or 0 when path is not a reparse point.
func reparseTag(path string) (uint32, error) {
	long, err := LongPath(path)
	if err != nil {
		return 0, err
	}
	p, err := windows.UTF16PtrFromString(long)
	if err != nil {
		return 0, err //nolint:wrapcheck // wrapped by the caller via *PathError
	}
	h, err := windows.CreateFile(p, windows.FILE_READ_ATTRIBUTES,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE, nil,
		windows.OPEN_EXISTING, windows.FILE_FLAG_OPEN_REPARSE_POINT|windows.FILE_FLAG_BACKUP_SEMANTICS, 0)
	if err != nil {
		return 0, err //nolint:wrapcheck // wrapped by the caller via *PathError
	}
	defer func() { _ = windows.CloseHandle(h) }() //nolint:errcheck // a failed close only leaks the handle
	var ti fileAttributeTagInfo
	if err := windows.GetFileInformationByHandleEx(h, windows.FileAttributeTagInfo,
		(*byte)(unsafe.Pointer(&ti)), uint32(unsafe.Sizeof(ti))); err != nil {
		return 0, err //nolint:wrapcheck // wrapped by the caller via *PathError
	}
	if ti.FileAttributes&windows.FILE_ATTRIBUTE_REPARSE_POINT == 0 {
		return 0, nil
	}
	return ti.ReparseTag, nil
}

// resolveJunctions follows the directory junctions left in the absolute path p, which
// [filepath.EvalSymlinks] does not resolve, and normalizes the result the way
// [filepath.EvalSymlinks] does (long names, actual case) so it compares with paths it
// returned. Another reparse point that redirects a lookup fails with [ErrEscapesRoot].
func resolveJunctions(p string) (string, error) {
	r, err := realPath(p, 0)
	if err != nil || r == p {
		return r, err
	}
	if norm, nerr := filepath.EvalSymlinks(r); nerr == nil {
		return norm, nil
	}
	return r, nil
}
