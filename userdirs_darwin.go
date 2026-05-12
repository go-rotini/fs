//go:build darwin

package fs

import (
	"os"
	"path/filepath"
)

func configDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err //nolint:wrapcheck // caller wraps via *PathError
	}
	return filepath.Join(home, "Library", "Application Support"), nil
}

func cacheDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err //nolint:wrapcheck // caller wraps via *PathError
	}
	return filepath.Join(home, "Library", "Caches"), nil
}

// On macOS, app data conventionally lives alongside config in
// Application Support. The dataDir / stateDir / configDir return the
// same root; per-app subdirectories distinguish purpose.
func dataDir() (string, error)  { return configDir() }
func stateDir() (string, error) { return configDir() }

func runtimeDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err //nolint:wrapcheck // caller wraps via *PathError
	}
	return filepath.Join(home, "Library", "Caches", "TemporaryItems"), nil
}

// systemAppSupportRoot is held as a constant so gocritic's
// filepathJoin check doesn't flag the structural leading slash.
const systemAppSupportRoot = "/Library/Application Support"

func systemConfigDir(app string) (string, error) {
	return filepath.Join(systemAppSupportRoot, app), nil
}

func systemDataDir(app string) (string, error)  { return systemConfigDir(app) }
func systemStateDir(app string) (string, error) { return systemConfigDir(app) }
