//go:build !windows

package fs

import "os"

// syncDir fsync's the directory at path so a subsequent power loss
// after a rename inside it sees the updated entry, not the previous
// one. Required by POSIX best practice for atomic-write durability.
//
// Returned errors surface raw; the caller wraps with *PathError.
func syncDir(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err //nolint:wrapcheck // wrapped by caller via *PathError
	}
	defer func() { _ = f.Close() }()
	return fileSync(f)
}
