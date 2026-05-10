//go:build windows

package fs

import (
	"path/filepath"
	"strings"
)

const opLongPath = "longpath"

// MaxPath is Windows' default per-path limit (`MAX_PATH`); paths
// longer than this require the `\\?\` prefix on most Win32 APIs.
const maxPath = 260

// LongPath returns path prefixed with `\\?\` (or `\\?\UNC\` for UNC
// paths) when len(path) >= MAX_PATH and the prefix isn't already
// present. The path is absolutized first since the long-path prefix
// requires absolute paths. Paths that already have a long-path
// prefix or that fit within MAX_PATH are returned unchanged.
func LongPath(path string) (string, error) {
	if len(path) < maxPath {
		return path, nil
	}
	if strings.HasPrefix(path, `\\?\`) {
		return path, nil
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", wrapPathError(opLongPath, path, err)
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
