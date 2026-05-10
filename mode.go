package fs

import "os"

// Common file-mode presets. These are typed [os.FileMode] values so
// they pass directly to [os.Chmod], [os.OpenFile], etc.
const (
	// Mode0644 is the default for new regular files (owner rw, group/other r).
	Mode0644 os.FileMode = 0o644

	// Mode0640 restricts read to owner + group.
	Mode0640 os.FileMode = 0o640

	// Mode0600 is owner-only read/write — appropriate for secret-bearing files.
	Mode0600 os.FileMode = 0o600

	// Mode0755 is the default for new directories (owner rwx, group/other rx).
	Mode0755 os.FileMode = 0o755

	// Mode0750 restricts directory access to owner + group.
	Mode0750 os.FileMode = 0o750

	// Mode0700 is owner-only directory access — appropriate for state dirs
	// containing secrets.
	Mode0700 os.FileMode = 0o700
)

const (
	opChmod      = "chmod"
	opEnsurePerm = "ensureperm"
)

// Chmod wraps [os.Chmod] with the package's error envelope. The mode
// argument is interpreted by the OS — only the permission bits
// (`os.ModePerm`) are honored on POSIX, and on Windows only the
// read-only bit changes meaningfully.
func Chmod(path string, mode os.FileMode) error {
	if err := os.Chmod(path, mode); err != nil {
		return wrapPathError(opChmod, path, err)
	}
	return nil
}

// EnsurePerm chmods path to mode if the current permission bits
// don't already match. A path that already has the desired mode is
// a no-op (no syscall, no error). Useful in idempotent setup
// routines.
//
// Only the permission bits (`os.ModePerm`) are compared; type bits
// (directory, symlink, etc.) on the existing inode are ignored.
func EnsurePerm(path string, mode os.FileMode) error {
	info, err := os.Stat(path)
	if err != nil {
		return wrapPathError(opEnsurePerm, path, err)
	}
	want := mode & os.ModePerm
	if info.Mode().Perm() == want {
		return nil
	}
	if err := os.Chmod(path, want); err != nil {
		return wrapPathError(opEnsurePerm, path, err)
	}
	return nil
}
