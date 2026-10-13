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

// resolveLinks returns p with every symlink and directory junction resolved, as an
// absolute path. p must exist. Links are followed by [realPath], since
// [filepath.EvalSymlinks] fails on a path that passes through a junction; the result, which
// holds no links, is then passed through [filepath.EvalSymlinks] only to normalize it (long
// names, actual case), so paths resolved here compare with each other. Another reparse
// point that redirects a lookup fails with [ErrEscapesRoot].
func resolveLinks(p string) (string, error) {
	abs, err := filepath.Abs(p)
	if err != nil {
		return "", &PathError{Op: opAbs, Path: p, Cause: err}
	}
	resolved, err := realPath(abs, 0)
	if err != nil {
		return "", wrapPathError(opEvalSymlinks, p, err)
	}
	if norm, err := filepath.EvalSymlinks(resolved); err == nil {
		return norm, nil
	}
	if _, err := osStat(resolved); err != nil {
		return "", &PathError{Op: opEvalSymlinks, Path: p, Cause: err}
	}
	return resolved, nil
}
