//go:build !windows

package fs

// LongPath is a no-op pass-through on POSIX systems — the kernel
// imposes per-component but no MAX_PATH-style aggregate limit, so
// no prefix is needed.
func LongPath(path string) (string, error) { return path, nil }
