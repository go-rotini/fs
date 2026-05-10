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

func systemConfigDir(app string) (string, error) {
	//nolint:gocritic // POSIX absolute prefix; gocritic's filepathJoin rule is overzealous here
	return filepath.Join("/Library", "Application Support", app), nil
}

func systemDataDir(app string) (string, error)  { return systemConfigDir(app) }
func systemStateDir(app string) (string, error) { return systemConfigDir(app) }
