//go:build !windows

package fs

// LongPath returns path unchanged on non-Windows systems, which have no
// MAX_PATH-style limit. On Windows it makes a path whose absolute form
// is 260 characters or more absolute and adds the `\\?\` long-path
// prefix.
func LongPath(path string) (string, error) { return path, nil }
