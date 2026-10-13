//go:build !windows

package fs

import (
	"os"
	"path/filepath"
)

// isLink reports whether path, whose Lstat result is info, is a link that a path lookup
// follows.
func isLink(_ string, info os.FileInfo) (bool, error) {
	return info.Mode()&os.ModeSymlink != 0, nil
}

// resolveLinks returns p with every symlink resolved, as an absolute path. p must exist.
func resolveLinks(p string) (string, error) {
	resolved, err := filepath.EvalSymlinks(p)
	if err != nil {
		return "", &PathError{Op: opEvalSymlinks, Path: p, Cause: err}
	}
	abs, err := filepath.Abs(resolved)
	if err != nil {
		return "", &PathError{Op: opAbs, Path: resolved, Cause: err}
	}
	return abs, nil
}
