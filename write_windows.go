//go:build windows

package fs

import "os"

// syncDir is a no-op on Windows. Directory entries are flushed
// implicitly when the file system metadata journal is committed;
// there is no `fsync(directory)` primitive in the Windows API.
func syncDir(_ string) error {
	return nil
}

// lockFile is a no-op on Windows. Append-mode writes are already
// serialized by the OS at the file-handle level.
func lockFile(_ *os.File) error {
	return nil
}
