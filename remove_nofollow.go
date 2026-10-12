package fs

import (
	"errors"
	stdfs "io/fs"
	"os"
	"path/filepath"
)

const opRemoveAllNoFollow = "removeallnofollow"

// RemoveAllNoFollow recursively removes path, checking every entry
// with [os.Lstat] so that a symlink, including path itself, is
// removed as a link and its target is never descended into.
//
// The check and the removal are separate steps, so it does not
// protect against an entry being swapped for a symlink while it
// runs. [os.RemoveAll] also never follows symlinks.
//
// Missing paths are not errors (idempotent removal). Permission
// errors and other failures abort with the first error.
func RemoveAllNoFollow(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		if errors.Is(err, stdfs.ErrNotExist) {
			return nil
		}
		return wrapPathError(opRemoveAllNoFollow, path, err)
	}

	if info.Mode()&os.ModeSymlink != 0 {
		// Unlink the link itself, not its target.
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
