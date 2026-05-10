package fs

import (
	"errors"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// --- Cross-platform: Home / SystemTempDir / ExecutableDir / BinaryPath ---

func TestHome(t *testing.T) {
	t.Parallel()
	got, err := Home()
	if err != nil {
		t.Fatalf("Home: %v", err)
	}
	if got == "" {
		t.Error("Home returned empty")
	}
	if !filepath.IsAbs(got) {
		t.Errorf("Home not absolute: %s", got)
	}
}

func TestSystemTempDir(t *testing.T) {
	t.Parallel()
	got := SystemTempDir()
	if got == "" {
		t.Error("SystemTempDir returned empty")
	}
}

func TestExecutableDir(t *testing.T) {
	t.Parallel()
	got, err := ExecutableDir()
	if err != nil {
		t.Fatalf("ExecutableDir: %v", err)
	}
	if !filepath.IsAbs(got) {
		t.Errorf("not absolute: %s", got)
	}
	if !IsDir(got) {
		t.Errorf("not a directory: %s", got)
	}
}

func TestBinaryPath_AbsoluteAndResolved(t *testing.T) {
	t.Parallel()
	got, err := BinaryPath()
	if err != nil {
		t.Fatalf("BinaryPath: %v", err)
	}
	if !filepath.IsAbs(got) {
		t.Errorf("not absolute: %s", got)
	}
	if !Exists(got) {
		t.Errorf("does not exist: %s", got)
	}
	// Symlink-resolution invariant: applying EvalSymlinks again is a no-op.
	again, err := filepath.EvalSymlinks(got)
	if err != nil {
		t.Fatalf("EvalSymlinks: %v", err)
	}
	if again != got {
		t.Errorf("path was not symlink-resolved: %s -> %s", got, again)
	}
}

// --- AppName validation ---

func TestAppName_Valid(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"myapp", "my-app", "my.app", "my_app_v2"} {
		if err := validateAppName(name); err != nil {
			t.Errorf("validateAppName(%q) = %v, want nil", name, err)
		}
	}
}

func TestAppName_Invalid(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"", ".", "..", "a/b", "a\\b", "a\x00b"} {
		if err := validateAppName(name); !errors.Is(err, ErrInvalidPath) {
			t.Errorf("validateAppName(%q) = %v, want ErrInvalidPath", name, err)
		}
	}
}

func TestAppConfigDir_RejectsBadName(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"", ".", "..", "evil/../escape", "win\\bad"} {
		_, err := AppConfigDir(name)
		if !errors.Is(err, ErrInvalidPath) {
			t.Errorf("AppConfigDir(%q) = %v, want ErrInvalidPath", name, err)
		}
	}
}

func TestSystemConfigDir_RejectsBadName(t *testing.T) {
	t.Parallel()
	_, err := SystemConfigDir("a/b")
	if !errors.Is(err, ErrInvalidPath) {
		t.Errorf("got %v, want ErrInvalidPath", err)
	}
}

// --- AppConfigDir composes correctly ---

func TestAppConfigDir_AppendedToConfigDir(t *testing.T) {
	t.Parallel()
	base, berr := ConfigDir()
	got, gerr := AppConfigDir("myapp")
	if berr != nil || gerr != nil {
		t.Skipf("skip due to error (env): base=%v app=%v", berr, gerr)
	}
	if !strings.HasSuffix(got, filepath.Join(base[len(base)-1:], "myapp")) && filepath.Dir(got) != base {
		t.Errorf("AppConfigDir = %s; expected directory under %s", got, base)
	}
}

// --- Failure paths via cleared HOME ---

func TestUserDirs_HomeUnsetSurfacesError(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows uses APPDATA, not HOME")
	}
	t.Setenv("HOME", "")
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("XDG_CACHE_HOME", "")
	t.Setenv("XDG_DATA_HOME", "")
	t.Setenv("XDG_STATE_HOME", "")

	for _, fn := range []struct {
		name string
		call func() (string, error)
	}{
		{"ConfigDir", ConfigDir},
		{"CacheDir", CacheDir},
		{"DataDir", DataDir},
		{"StateDir", StateDir},
		{"Home", Home},
	} {
		if _, err := fn.call(); err == nil {
			t.Errorf("%s succeeded with HOME unset", fn.name)
		}
	}
}

// --- AppCacheDir / AppDataDir / AppStateDir / AppRuntimeDir ---

func TestAppCacheDir(t *testing.T) {
	t.Parallel()
	base, berr := CacheDir()
	got, gerr := AppCacheDir("myapp")
	if berr != nil || gerr != nil {
		t.Skipf("skip due to env: %v %v", berr, gerr)
	}
	if filepath.Dir(got) != base {
		t.Errorf("AppCacheDir = %s, want under %s", got, base)
	}
}

func TestAppDataDir(t *testing.T) {
	t.Parallel()
	base, berr := DataDir()
	got, gerr := AppDataDir("myapp")
	if berr != nil || gerr != nil {
		t.Skipf("skip due to env: %v %v", berr, gerr)
	}
	if filepath.Dir(got) != base {
		t.Errorf("AppDataDir = %s, want under %s", got, base)
	}
}

func TestAppStateDir(t *testing.T) {
	t.Parallel()
	base, berr := StateDir()
	got, gerr := AppStateDir("myapp")
	if berr != nil || gerr != nil {
		t.Skipf("skip due to env: %v %v", berr, gerr)
	}
	if filepath.Dir(got) != base {
		t.Errorf("AppStateDir = %s, want under %s", got, base)
	}
}

func TestAppRuntimeDir(t *testing.T) {
	t.Parallel()
	base, berr := RuntimeDir()
	got, gerr := AppRuntimeDir("myapp")
	if errors.Is(berr, ErrNotSupported) {
		// XDG_RUNTIME_DIR not set on this platform; skip.
		t.Skipf("RuntimeDir unavailable: %v", berr)
	}
	if berr != nil || gerr != nil {
		t.Skipf("skip due to env: %v %v", berr, gerr)
	}
	if filepath.Dir(got) != base {
		t.Errorf("AppRuntimeDir = %s, want under %s", got, base)
	}
}

func TestStateDir_NonEmpty(t *testing.T) {
	t.Parallel()
	got, err := StateDir()
	if err != nil {
		t.Fatalf("StateDir: %v", err)
	}
	if got == "" {
		t.Error("empty")
	}
}

// --- System*Dir ---

func TestSystemDataDir(t *testing.T) {
	t.Parallel()
	got, err := SystemDataDir("myapp")
	if err != nil {
		t.Fatalf("SystemDataDir: %v", err)
	}
	if !strings.HasSuffix(got, "myapp") {
		t.Errorf("got %s, want suffix myapp", got)
	}
}

func TestSystemStateDir(t *testing.T) {
	t.Parallel()
	got, err := SystemStateDir("myapp")
	if err != nil {
		t.Fatalf("SystemStateDir: %v", err)
	}
	if !strings.HasSuffix(got, "myapp") {
		t.Errorf("got %s, want suffix myapp", got)
	}
}

func TestSystemDataDir_RejectsBadName(t *testing.T) {
	t.Parallel()
	_, err := SystemDataDir("a/b")
	if !errors.Is(err, ErrInvalidPath) {
		t.Errorf("got %v, want ErrInvalidPath", err)
	}
}

func TestSystemStateDir_RejectsBadName(t *testing.T) {
	t.Parallel()
	_, err := SystemStateDir("a/b")
	if !errors.Is(err, ErrInvalidPath) {
		t.Errorf("got %v, want ErrInvalidPath", err)
	}
}
