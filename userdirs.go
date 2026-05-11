package fs

import (
	"os"
	"path/filepath"
	"strings"
)

const (
	opHome            = "home"
	opConfigDir       = "configdir"
	opCacheDir        = "cachedir"
	opDataDir         = "datadir"
	opStateDir        = "statedir"
	opRuntimeDir      = "runtimedir"
	opAppConfigDir    = "appconfigdir"
	opAppCacheDir     = "appcachedir"
	opAppDataDir      = "appdatadir"
	opAppStateDir     = "appstatedir"
	opAppRuntimeDir   = "appruntimedir"
	opSystemConfigDir = "systemconfigdir"
	opSystemDataDir   = "systemdatadir"
	opSystemStateDir  = "systemstatedir"
	opExecutableDir   = "executabledir"
	opBinaryPath      = "binarypath"
)

// Home returns the current user's home directory. Wraps
// [os.UserHomeDir].
func Home() (string, error) {
	h, err := os.UserHomeDir()
	if err != nil {
		return "", wrapPathError(opHome, "", err)
	}
	return h, nil
}

// ConfigDir returns the user's per-user config directory.
//
//	Linux/BSD: $XDG_CONFIG_HOME or ~/.config
//	macOS:     ~/Library/Application Support
//	Windows:   %APPDATA%
func ConfigDir() (string, error) {
	p, err := configDir()
	if err != nil {
		return "", wrapPathError(opConfigDir, "", err)
	}
	return p, nil
}

// CacheDir returns the user's per-user cache directory.
//
//	Linux/BSD: $XDG_CACHE_HOME or ~/.cache
//	macOS:     ~/Library/Caches
//	Windows:   %LOCALAPPDATA%
func CacheDir() (string, error) {
	p, err := cacheDir()
	if err != nil {
		return "", wrapPathError(opCacheDir, "", err)
	}
	return p, nil
}

// DataDir returns the user's per-user data directory.
//
//	Linux/BSD: $XDG_DATA_HOME or ~/.local/share
//	macOS:     ~/Library/Application Support
//	Windows:   %APPDATA%
func DataDir() (string, error) {
	p, err := dataDir()
	if err != nil {
		return "", wrapPathError(opDataDir, "", err)
	}
	return p, nil
}

// StateDir returns the user's per-user state directory.
//
//	Linux/BSD: $XDG_STATE_HOME or ~/.local/state
//	macOS:     ~/Library/Application Support
//	Windows:   %LOCALAPPDATA%
func StateDir() (string, error) {
	p, err := stateDir()
	if err != nil {
		return "", wrapPathError(opStateDir, "", err)
	}
	return p, nil
}

// RuntimeDir returns the user's runtime directory.
//
//	Linux/BSD: $XDG_RUNTIME_DIR (no fallback per XDG spec; errors with
//	           [ErrNotSupported] when unset).
//	macOS:     ~/Library/Caches/TemporaryItems
//	Windows:   %LOCALAPPDATA%\Temp
func RuntimeDir() (string, error) {
	p, err := runtimeDir()
	if err != nil {
		return "", wrapPathError(opRuntimeDir, "", err)
	}
	return p, nil
}

// SystemTempDir returns the system temp directory ([os.TempDir]).
func SystemTempDir() string { return os.TempDir() }

// ExecutableDir returns the directory containing the running
// executable. Symlinks in the path are NOT resolved; use
// [BinaryPath] for the symlink-resolved binary path.
func ExecutableDir() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", wrapPathError(opExecutableDir, "", err)
	}
	return filepath.Dir(exe), nil
}

// BinaryPath returns the absolute, symlink-resolved path of the
// running executable. Wraps [os.Executable] + [filepath.EvalSymlinks].
func BinaryPath() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", wrapPathError(opBinaryPath, "", err)
	}
	resolved, err := filepath.EvalSymlinks(exe)
	if err != nil {
		return "", wrapPathError(opBinaryPath, exe, err)
	}
	abs, err := filepath.Abs(resolved)
	if err != nil {
		return "", wrapPathError(opBinaryPath, resolved, err)
	}
	return abs, nil
}

// AppConfigDir returns [ConfigDir]/<appName>. appName must not
// contain a path separator (`/` or `\`), be empty, or be `.` / `..`.
// Invalid appName errors with [ErrInvalidPath].
func AppConfigDir(appName string) (string, error) {
	return appDir(opAppConfigDir, appName, ConfigDir)
}

// AppCacheDir returns [CacheDir]/<appName>. See [AppConfigDir] for
// appName validation.
func AppCacheDir(appName string) (string, error) {
	return appDir(opAppCacheDir, appName, CacheDir)
}

// AppDataDir returns [DataDir]/<appName>.
func AppDataDir(appName string) (string, error) {
	return appDir(opAppDataDir, appName, DataDir)
}

// AppStateDir returns [StateDir]/<appName>.
func AppStateDir(appName string) (string, error) {
	return appDir(opAppStateDir, appName, StateDir)
}

// AppRuntimeDir returns [RuntimeDir]/<appName>.
func AppRuntimeDir(appName string) (string, error) {
	return appDir(opAppRuntimeDir, appName, RuntimeDir)
}

func appDir(op, appName string, base func() (string, error)) (string, error) {
	if err := validateAppName(appName); err != nil {
		return "", wrapPathError(op, appName, err)
	}
	b, err := base()
	if err != nil {
		return "", err
	}
	return filepath.Join(b, appName), nil
}

// SystemConfigDir returns the system-wide config directory for app:
//
//	Linux/BSD: /etc/<appName>
//	macOS:     /Library/Application Support/<appName>
//	Windows:   %PROGRAMDATA%\<appName>
//
// The calling tool is responsible for creating the directory with
// appropriate permissions and for handling the access-denied case
// when run unprivileged.
func SystemConfigDir(appName string) (string, error) {
	if err := validateAppName(appName); err != nil {
		return "", wrapPathError(opSystemConfigDir, appName, err)
	}
	p, err := systemConfigDir(appName)
	if err != nil {
		return "", wrapPathError(opSystemConfigDir, appName, err)
	}
	return p, nil
}

// SystemDataDir is the system-wide analog of [DataDir].
//
//	Linux/BSD: /var/lib/<appName>
//	macOS:     /Library/Application Support/<appName>
//	Windows:   %PROGRAMDATA%\<appName>
func SystemDataDir(appName string) (string, error) {
	if err := validateAppName(appName); err != nil {
		return "", wrapPathError(opSystemDataDir, appName, err)
	}
	p, err := systemDataDir(appName)
	if err != nil {
		return "", wrapPathError(opSystemDataDir, appName, err)
	}
	return p, nil
}

// SystemStateDir is the system-wide analog of [StateDir].
//
//	Linux/BSD: /var/lib/<appName>
//	macOS:     /Library/Application Support/<appName>
//	Windows:   %PROGRAMDATA%\<appName>
func SystemStateDir(appName string) (string, error) {
	if err := validateAppName(appName); err != nil {
		return "", wrapPathError(opSystemStateDir, appName, err)
	}
	p, err := systemStateDir(appName)
	if err != nil {
		return "", wrapPathError(opSystemStateDir, appName, err)
	}
	return p, nil
}

// validateAppName rejects empty names, names containing path
// separators, and the special directory references "." and "..".
// Cleared appNames keep the App*/System* helpers from accidentally
// reaching outside the intended parent.
func validateAppName(name string) error {
	if name == "" || name == "." || name == ".." {
		return ErrInvalidPath
	}
	if strings.ContainsAny(name, "/\\") {
		return ErrInvalidPath
	}
	if strings.ContainsRune(name, 0) {
		return ErrInvalidPath
	}
	return nil
}
