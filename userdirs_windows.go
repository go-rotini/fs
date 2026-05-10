//go:build windows

package fs

import (
	"fmt"
	"os"
	"path/filepath"
)

// envOrError returns os.Getenv(name) or a wrapped ErrNotFound when
// the variable is unset.
func envOrError(name string) (string, error) {
	v := os.Getenv(name)
	if v == "" {
		return "", fmt.Errorf("%w: %s not set", ErrNotFound, name)
	}
	return v, nil
}

func configDir() (string, error) { return envOrError("APPDATA") }
func cacheDir() (string, error)  { return envOrError("LOCALAPPDATA") }
func dataDir() (string, error)   { return envOrError("APPDATA") }
func stateDir() (string, error)  { return envOrError("LOCALAPPDATA") }

func runtimeDir() (string, error) {
	base, err := envOrError("LOCALAPPDATA")
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "Temp"), nil
}

func systemConfigDir(app string) (string, error) {
	base, err := envOrError("PROGRAMDATA")
	if err != nil {
		return "", err
	}
	return filepath.Join(base, app), nil
}

func systemDataDir(app string) (string, error)  { return systemConfigDir(app) }
func systemStateDir(app string) (string, error) { return systemConfigDir(app) }
