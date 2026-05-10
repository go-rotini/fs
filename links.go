package fs

import (
	"errors"
	stdfs "io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

const (
	opSymlink  = "symlink"
	opReadLink = "readlink"
	opHardlink = "hardlink"
)

// Symlink creates a symbolic link at linkPath pointing to target.
// Idempotent: if linkPath already exists as a symlink with the same
// target, returns nil. If linkPath exists but points elsewhere or is
// not a symlink, returns [ErrAlreadyExists].
//
// The target is stored verbatim in the link — it is not validated,
// resolved, or required to exist (a "dangling" symlink is allowed,
// matching [os.Symlink]).
func Symlink(target, linkPath string) error {
	if existing, err := os.Readlink(linkPath); err == nil {
		if existing == target {
			return nil
		}
		return wrapPathError(opSymlink, linkPath, ErrAlreadyExists)
	} else if _, lerr := os.Lstat(linkPath); lerr == nil {
		// linkPath exists but isn't a symlink.
		return wrapPathError(opSymlink, linkPath, ErrAlreadyExists)
	}

	if err := os.Symlink(target, linkPath); err != nil {
		return wrapPathError(opSymlink, linkPath, err)
	}
	return nil
}

// ReadLink returns the target stored in the symlink at linkPath.
// Wraps [os.Readlink].
func ReadLink(linkPath string) (string, error) {
	target, err := os.Readlink(linkPath)
	if err != nil {
		return "", wrapPathError(opReadLink, linkPath, err)
	}
	return target, nil
}

// EvalSymlinks resolves all symlinks in path and returns the
// canonical absolute path. Wraps [filepath.EvalSymlinks]. A symlink
// loop or excessive hop count returns [ErrSymlinkLoop].
//
// For constrained resolution that won't escape a parent directory,
// use [EvalSymlinksWithin].
func EvalSymlinks(path string) (string, error) {
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		// stdlib uses "too many links" message for loop detection.
		// Map to the package sentinel.
		if isSymlinkLoop(err) {
			return "", wrapPathError(opEvalSymlinks, path, ErrSymlinkLoop)
		}
		return "", wrapPathError(opEvalSymlinks, path, err)
	}
	return resolved, nil
}

// isSymlinkLoop reports whether err looks like a symlink-loop error
// from filepath.EvalSymlinks. POSIX surfaces ELOOP; filepath itself
// has an internal hop-counter that returns the bare
// `errors.New("EvalSymlinks: too many links")` sentinel — string-
// matched here.
func isSymlinkLoop(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, syscall.ELOOP) {
		return true
	}
	// filepath's internal sentinel is unexported; match by message.
	if strings.Contains(err.Error(), "too many links") {
		return true
	}
	// Some platforms wrap ELOOP inside a *fs.PathError without
	// errors.Is matching directly. Walk the chain.
	var pe *stdfs.PathError
	if errors.As(err, &pe) && pe.Err != nil {
		if errors.Is(pe.Err, syscall.ELOOP) {
			return true
		}
	}
	return false
}

// Hardlink creates a hard link at linkPath pointing to target.
// Idempotent: if linkPath already refers to the same inode as
// target, returns nil. If linkPath exists but is a different file,
// returns [ErrAlreadyExists]. Cross-device links surface as
// [ErrCrossDevice].
//
// target must exist and be a regular file (most filesystems forbid
// hard-linking directories).
func Hardlink(target, linkPath string) error {
	if same, err := SameFile(target, linkPath); err == nil && same {
		return nil
	}

	if _, lerr := os.Lstat(linkPath); lerr == nil {
		return wrapPathError(opHardlink, linkPath, ErrAlreadyExists)
	}

	if err := os.Link(target, linkPath); err != nil {
		if errors.Is(err, syscall.EXDEV) {
			return wrapPathError(opHardlink, linkPath, ErrCrossDevice)
		}
		return wrapPathError(opHardlink, linkPath, err)
	}
	return nil
}
