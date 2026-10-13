//go:build windows

package fs

import (
	"path/filepath"
	"strings"
)

const opLongPath = "longpath"

// maxPath is Windows' default per-path limit (`MAX_PATH`); paths
// longer than this require the `\\?\` prefix on most Win32 APIs.
const maxPath = 260

// LongPath returns path as an absolute path prefixed with `\\?\` (or
// `\\?\UNC\` for UNC paths) when its absolute form, which for a
// relative path includes the working directory, is MAX_PATH or more
// characters long. Paths that already have the prefix, and paths
// shorter than MAX_PATH whose absolute form fits too, are returned
// unchanged; a longer path whose absolute form fits is returned in that
// absolute form.
func LongPath(path string) (string, error) {
	if strings.HasPrefix(path, `\\?\`) {
		return path, nil
	}
	if len(path) < maxPath && filepath.IsAbs(path) {
		return path, nil
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", wrapPathError(opLongPath, path, err)
	}
	if len(abs) < maxPath {
		if len(path) >= maxPath {
			return abs, nil
		}
		return path, nil
	}
	if strings.HasPrefix(abs, `\\?\`) {
		return abs, nil
	}
	if strings.HasPrefix(abs, `\\`) {
		// UNC: \\server\share\... -> \\?\UNC\server\share\...
		return `\\?\UNC\` + abs[2:], nil
	}
	return `\\?\` + abs, nil
}
