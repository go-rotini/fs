package fs

import (
	"errors"
	stdfs "io/fs"
	"os"
	"path/filepath"
)

const opRemoveAllNoFollow = "removeallnofollow"

// RemoveAllNoFollow recursively removes path, refusing to traverse
// symlinks. Unlike [os.RemoveAll], which follows symlinks during
// its descent, this variant uses [os.Lstat] at every step so a
// symlinked directory under path is unlinked (the link itself) but
// its target subtree is left untouched.
//
// Use this for security-sensitive cleanup where an attacker may
// have placed a symlink under the target path expecting RemoveAll
// to wipe out a victim directory elsewhere on the filesystem.
//
// Missing paths are not errors (idempotent removal). Permission
// errors and other syscall failures abort the walk with the first
// error encountered.
func RemoveAllNoFollow(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		if errors.Is(err, stdfs.ErrNotExist) {
			return nil
		}
		return wrapPathError(opRemoveAllNoFollow, path, err)
	}

	if info.Mode()&os.ModeSymlink != 0 {
		// path is itself a symlink — unlink the link, not the target.
		if rerr := os.Remove(path); rerr != nil && !errors.Is(rerr, stdfs.ErrNotExist) {
			return wrapPathError(opRemoveAllNoFollow, path, rerr)
		}
		return nil
	}

	if !info.IsDir() {
		if rerr := os.Remove(path); rerr != nil && !errors.Is(rerr, stdfs.ErrNotExist) {
			return wrapPathError(opRemoveAllNoFollow, path, rerr)
		}
		return nil
	}

	entries, err := os.ReadDir(path)
	if err != nil {
		return wrapPathError(opRemoveAllNoFollow, path, err)
	}
	for _, e := range entries {
		child := filepath.Join(path, e.Name())
		if rerr := RemoveAllNoFollow(child); rerr != nil {
			return rerr
		}
	}
	if rerr := os.Remove(path); rerr != nil && !errors.Is(rerr, stdfs.ErrNotExist) {
		return wrapPathError(opRemoveAllNoFollow, path, rerr)
	}
	return nil
}
