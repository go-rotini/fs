//go:build !windows

package fs

import "os"

// isLink reports whether path, whose Lstat result is info, is a link that a path lookup
// follows.
func isLink(_ string, info os.FileInfo) (bool, error) {
	return info.Mode()&os.ModeSymlink != 0, nil
}

// resolveJunctions returns p unchanged: only Windows has links that [filepath.EvalSymlinks]
// leaves unresolved.
func resolveJunctions(p string) (string, error) { return p, nil }
