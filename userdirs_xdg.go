//go:build linux || freebsd

package fs

import (
	"fmt"
	"os"
	"path/filepath"
)

func configDir() (string, error) {
	if v := os.Getenv("XDG_CONFIG_HOME"); v != "" {
		return v, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err //nolint:wrapcheck // caller wraps via *PathError
	}
	return filepath.Join(home, ".config"), nil
}

func cacheDir() (string, error) {
	if v := os.Getenv("XDG_CACHE_HOME"); v != "" {
		return v, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err //nolint:wrapcheck // caller wraps via *PathError
	}
	return filepath.Join(home, ".cache"), nil
}

func dataDir() (string, error) {
	if v := os.Getenv("XDG_DATA_HOME"); v != "" {
		return v, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err //nolint:wrapcheck // caller wraps via *PathError
	}
	return filepath.Join(home, ".local", "share"), nil
}

func stateDir() (string, error) {
	if v := os.Getenv("XDG_STATE_HOME"); v != "" {
		return v, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err //nolint:wrapcheck // caller wraps via *PathError
	}
	return filepath.Join(home, ".local", "state"), nil
}

// runtimeDir returns $XDG_RUNTIME_DIR. The XDG spec gives no
// fallback (the runtime dir is meant to be created by the system at
// login with the user's uid/gid); without it, the package errors
// rather than guessing a path that won't have the right ownership.
func runtimeDir() (string, error) {
	v := os.Getenv("XDG_RUNTIME_DIR")
	if v == "" {
		return "", fmt.Errorf("%w: XDG_RUNTIME_DIR not set", ErrNotSupported)
	}
	return v, nil
}

// Absolute system-config and system-data roots. Held as constants so
// gocritic's filepathJoin check (which flags path separators inside
// string literals passed to filepath.Join) doesn't fire on the
// structural leading slash, which can't be expressed any other way.
const (
	systemEtcRoot    = "/etc"
	systemVarLibRoot = "/var/lib"
)

func systemConfigDir(app string) (string, error) {
	return filepath.Join(systemEtcRoot, app), nil
}

func systemDataDir(app string) (string, error) {
	return filepath.Join(systemVarLibRoot, app), nil
}

func systemStateDir(app string) (string, error) {
	return filepath.Join(systemVarLibRoot, app), nil
}
