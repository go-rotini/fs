//go:build unix

package fs

import (
	"errors"
	"os"
	"path/filepath"
	"slices"

	"golang.org/x/sys/unix"
)

func chownRecursive(root string, uid, gid int) error {
	if err := unix.Fchownat(unix.AT_FDCWD, root, uid, gid, unix.AT_SYMLINK_NOFOLLOW); err != nil {
		return wrapPathError(opChown, root, err)
	}
	fd, err := retryEINTR(func() (int, error) {
		return unix.Open(root, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	})
	if err != nil {
		if isNoFollowErrno(err) || errors.Is(err, unix.ENOTDIR) {
			return nil // root is a file or a symlink: done
		}
		return wrapPathError(opChown, root, err)
	}
	return chownDirFD(fd, root, uid, gid)
}

// chownDirFD changes the ownership of every entry in the directory
// open as fd (named path, for errors), recursing through descriptors.
// It closes fd.
func chownDirFD(fd int, path string, uid, gid int) error {
	defer unix.Close(fd)
	names, err := readDirNames(fd)
	if err != nil {
		return wrapPathError(opChown, path, err)
	}
	for _, name := range names {
		child := filepath.Join(path, name)
		if err := unix.Fchownat(fd, name, uid, gid, unix.AT_SYMLINK_NOFOLLOW); err != nil {
			return wrapPathError(opChown, child, err)
		}
		cfd, err := retryEINTR(func() (int, error) {
			return unix.Openat(fd, name, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
		})
		if err != nil {
			if isNoFollowErrno(err) || errors.Is(err, unix.ENOTDIR) {
				continue // not a directory, or a symlink: nothing below it
			}
			return wrapPathError(opChown, child, err)
		}
		if err := chownDirFD(cfd, child, uid, gid); err != nil {
			return err
		}
	}
	return nil
}

// readDirNames lists the directory open as fd, sorted, without
// consuming fd.
func readDirNames(fd int) ([]string, error) {
	dfd, err := unix.Dup(fd)
	if err != nil {
		return nil, err //nolint:wrapcheck // wrapped by caller
	}
	d := os.NewFile(uintptr(dfd), "dir")
	defer d.Close()
	names, err := d.Readdirnames(-1)
	if err != nil {
		return nil, err //nolint:wrapcheck // wrapped by caller
	}
	slices.Sort(names)
	return names, nil
}
