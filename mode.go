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
