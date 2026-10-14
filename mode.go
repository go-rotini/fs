package fs

import "os"

// Common file-mode presets. These are typed [os.FileMode] values so
// they pass directly to [os.Chmod], [os.OpenFile], etc.
const (
	// Mode0644 is the default for new regular files (owner rw, group/other r).
	Mode0644 os.FileMode = 0o644

	// Mode0640 restricts read to owner + group.
	Mode0640 os.FileMode = 0o640

	// Mode0600 is owner-only read/write; appropriate for secret-bearing files.
	Mode0600 os.FileMode = 0o600

	// Mode0755 is the default for new directories (owner rwx, group/other rx).
	Mode0755 os.FileMode = 0o755

	// Mode0750 restricts directory access to owner + group.
	Mode0750 os.FileMode = 0o750

	// Mode0700 is owner-only directory access; appropriate for state dirs
	// containing secrets.
	Mode0700 os.FileMode = 0o700
)

const (
	opChmod            = "chmod"
	opEnsurePerm       = "ensureperm"
	opWarnInsecurePerm = "warninsecureperm"
)

// Chmod wraps [os.Chmod], returning errors as a [*PathError]. On
// POSIX the permission bits plus setuid, setgid and sticky apply; on
// Windows only the owner-write bit matters (it toggles read-only).
func Chmod(path string, mode os.FileMode) error {
	if err := os.Chmod(path, mode); err != nil {
		return wrapPathError(opChmod, path, err)
	}
	return nil
}

// EnsurePerm chmods path to mode if the current permission bits
// don't already match; otherwise it does nothing. Useful in idempotent
// setup routines. Symlinks are followed.
//
// Only the permission bits (`os.ModePerm`) of mode are used. When a
// chmod is needed, any setuid, setgid or sticky bit on path is
// cleared.
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

// WarnInsecurePerm reports whether path's mode permits more access
// than expected. Returns insecure=true when actual has any
// permission bit set that isn't in expected; e.g., expected=0o600,
// actual=0o644 surfaces the surplus group/other read bits as
// insecure=true. Permission bits LESS permissive than expected are
// not flagged (a 0o400 file is fine when expected=0o600).
//
// Callers decide what to do; warn the user, refuse to load the
// file, or repair via [Chmod] / [EnsurePerm]. Pairs with
// `WriteFile(..., WithPerm(fs.Mode0600))` for end-to-end
// secret-handling discipline.
//
// On Windows where POSIX permission bits don't apply, the result
// reflects the limited bits Go's [os.FileMode] surfaces; callers
// should treat it as advisory.
func WarnInsecurePerm(path string, expected os.FileMode) (insecure bool, actual os.FileMode, err error) {
	info, statErr := os.Stat(path)
	if statErr != nil {
		return false, 0, wrapPathError(opWarnInsecurePerm, path, statErr)
	}
	actual = info.Mode().Perm()
	want := expected.Perm()
	insecure = (actual &^ want) != 0
	return insecure, actual, nil
}
