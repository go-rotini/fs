package fs

import (
	"errors"
	stdfs "io/fs"
	"os"
)

// Exists reports whether path is statable. Returns false on permission
// errors as well as missing paths; callers who need the distinction
// should use [Stat] directly.
func Exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// IsFile reports whether path exists and is a regular file. Symlinks
// to regular files are followed.
func IsFile(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return info.Mode().IsRegular()
}

// IsDir reports whether path exists and is a directory. Symlinks to
// directories are followed.
func IsDir(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return info.IsDir()
}

// IsSymlink reports whether path exists and is itself a symlink (not
// a target of one). Uses [os.Lstat].
func IsSymlink(path string) bool {
	info, err := os.Lstat(path)
	if err != nil {
		return false
	}
	return info.Mode()&stdfs.ModeSymlink != 0
}

// IsFIFO reports whether path exists and is a named pipe (FIFO).
// Returns false on Windows where the file type doesn't apply.
func IsFIFO(path string) bool {
	info, err := os.Lstat(path)
	if err != nil {
		return false
	}
	return info.Mode()&stdfs.ModeNamedPipe != 0
}

// IsSocket reports whether path exists and is a Unix-domain socket.
// Returns false on Windows where the file type doesn't apply.
func IsSocket(path string) bool {
	info, err := os.Lstat(path)
	if err != nil {
		return false
	}
	return info.Mode()&stdfs.ModeSocket != 0
}

// IsCharDevice reports whether path exists and is a character-special
// file. Returns false on Windows where the file type doesn't apply.
func IsCharDevice(path string) bool {
	info, err := os.Lstat(path)
	if err != nil {
		return false
	}
	mode := info.Mode()
	return mode&stdfs.ModeDevice != 0 && mode&stdfs.ModeCharDevice != 0
}

// IsBlockDevice reports whether path exists and is a block-special
// file. Returns false on Windows where the file type doesn't apply.
func IsBlockDevice(path string) bool {
	info, err := os.Lstat(path)
	if err != nil {
		return false
	}
	mode := info.Mode()
	return mode&stdfs.ModeDevice != 0 && mode&stdfs.ModeCharDevice == 0
}

// existsPredicate is the helper consulted by IsReadable / IsWritable
// before the platform-specific access check. A path that doesn't
// statable cannot be readable / writable regardless of mode bits.
func existsPredicate(path string) bool {
	_, err := os.Stat(path)
	if err == nil {
		return true
	}
	// A permission error on Stat itself counts as "exists" for the
	// access-check path: the caller's IsReadable / IsWritable will
	// then attempt the platform-specific check, which may itself
	// return false.
	return errors.Is(err, stdfs.ErrPermission)
}
