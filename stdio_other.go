//go:build !unix && !windows

package fs

import "os"

// isTerminal reports false: this platform has no terminal query.
func isTerminal(*os.File) bool { return false }
