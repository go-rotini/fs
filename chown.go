package fs

import (
	stdfs "io/fs"
	"os"
	"runtime"
)

const opChown = "chown"

// ChownRecursive walks root and applies uid/gid to every entry,
// including root itself. Passing -1 for uid or gid leaves that value
// unchanged on POSIX (matching [os.Chown] semantics).
//
// On Windows returns [ErrNotSupported]; POSIX uid/gid does not map
// cleanly onto NTFS ACLs.
//
// Symlinks are not followed; the link itself has its ownership
// changed via [os.Lchown] so cross-mount targets are not affected.
func ChownRecursive(root string, uid, gid int) error {
	if runtime.GOOS == goosWindows {
		return wrapPathError(opChown, root, ErrNotSupported)
	}
	return Walk(root, func(path string, _ stdfs.DirEntry, werr error) error {
		if werr != nil {
			return werr
		}
		if cerr := os.Lchown(path, uid, gid); cerr != nil {
			return cerr //nolint:wrapcheck // outer Walk wraps via *PathError
		}
		return nil
	})
}
