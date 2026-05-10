//go:build !windows

package fs

import (
	stdfs "io/fs"
	"strings"
)

// isHiddenEntry reports whether e represents a POSIX hidden entry —
// dot-prefix on the basename. The path argument is unused on POSIX
// (only Windows needs to read the FILE_ATTRIBUTE_HIDDEN bit).
func isHiddenEntry(_ string, e stdfs.DirEntry) bool {
	return strings.HasPrefix(e.Name(), ".")
}
