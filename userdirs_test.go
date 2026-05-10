package fs

import (
	"errors"
	"path/filepath"
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
