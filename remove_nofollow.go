package fs

import (
	"os"
)

const opRemoveAllNoFollow = "removeallnofollow"

// RemoveAllNoFollow recursively removes path without following
// symlinks: a symlink, including path itself, is removed as a link
// and its target is left alone. It is [os.RemoveAll] with errors
// returned as a [*PathError], so on Unix it removes entries relative
// to already-open directories, and an entry swapped for a symlink
// while it runs is not followed either.
//
// Missing paths are not errors (idempotent removal). Other failures,
// such as a permission error, abort the removal and are returned.
func RemoveAllNoFollow(path string) error {
	if err := os.RemoveAll(path); err != nil {
		return wrapPathError(opRemoveAllNoFollow, path, err)
	}
	return nil
}
