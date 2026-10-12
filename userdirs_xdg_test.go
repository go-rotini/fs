//go:build linux || freebsd || netbsd || openbsd

package fs

import (
	"errors"
	"path/filepath"
	"testing"
)

// XDG-based tests use t.Setenv (incompatible with t.Parallel).

func TestXDG_ConfigDir_Override(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "/tmp/cfg")
	got, err := ConfigDir()
	if err != nil {
		t.Fatalf("ConfigDir: %v", err)
	}
	if got != "/tmp/cfg" {
		t.Errorf("got %s, want /tmp/cfg", got)
	}
}

func TestXDG_ConfigDir_Fallback(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("HOME", "/tmp/h")
	got, err := ConfigDir()
	if err != nil {
		t.Fatalf("ConfigDir: %v", err)
	}
	want := filepath.Join("/tmp/h", ".config")
	if got != want {
		t.Errorf("got %s, want %s", got, want)
	}
}

func TestXDG_CacheDir_Override(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", "/tmp/cache")
	got, _ := CacheDir()
	if got != "/tmp/cache" {
		t.Errorf("got %s, want /tmp/cache", got)
	}
}

func TestXDG_DataDir_Fallback(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", "")
	t.Setenv("HOME", "/tmp/h")
	got, _ := DataDir()
	want := filepath.Join("/tmp/h", ".local", "share")
	if got != want {
		t.Errorf("got %s, want %s", got, want)
	}
}

func TestXDG_StateDir_Fallback(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", "")
	t.Setenv("HOME", "/tmp/h")
	got, _ := StateDir()
	want := filepath.Join("/tmp/h", ".local", "state")
	if got != want {
		t.Errorf("got %s, want %s", got, want)
	}
}

func TestXDG_RuntimeDir_NoFallback(t *testing.T) {
	t.Setenv("XDG_RUNTIME_DIR", "")
	_, err := RuntimeDir()
	if !errors.Is(err, ErrNotSupported) {
		t.Errorf("got %v, want ErrNotSupported", err)
	}
}

func TestXDG_RuntimeDir_Set(t *testing.T) {
	t.Setenv("XDG_RUNTIME_DIR", "/run/user/1000")
	got, err := RuntimeDir()
	if err != nil {
		t.Fatalf("RuntimeDir: %v", err)
	}
	if got != "/run/user/1000" {
		t.Errorf("got %s, want /run/user/1000", got)
	}
}

func TestXDG_SystemConfigDir(t *testing.T) {
	t.Parallel()
	got, err := SystemConfigDir("myapp")
	if err != nil {
		t.Fatalf("SystemConfigDir: %v", err)
	}
	if got != "/etc/myapp" {
		t.Errorf("got %s, want /etc/myapp", got)
	}
}

func TestXDG_SystemDataDir(t *testing.T) {
	t.Parallel()
	got, err := SystemDataDir("myapp")
	if err != nil {
		t.Fatalf("SystemDataDir: %v", err)
	}
	if got != "/var/lib/myapp" {
		t.Errorf("got %s, want /var/lib/myapp", got)
	}
}

func TestXDG_RelativeValuesIgnored(t *testing.T) {
	t.Setenv("HOME", "/tmp/h")
	for _, tc := range []struct {
		env  string
		fn   func() (string, error)
		want string
	}{
		{"XDG_CONFIG_HOME", ConfigDir, filepath.Join("/tmp/h", ".config")},
		{"XDG_CACHE_HOME", CacheDir, filepath.Join("/tmp/h", ".cache")},
		{"XDG_DATA_HOME", DataDir, filepath.Join("/tmp/h", ".local", "share")},
		{"XDG_STATE_HOME", StateDir, filepath.Join("/tmp/h", ".local", "state")},
	} {
		t.Setenv(tc.env, "relative/dir")
		got, err := tc.fn()
		if err != nil || got != tc.want {
			t.Errorf("%s=relative/dir: got %q, %v; want %q", tc.env, got, err, tc.want)
		}
	}
}

func TestXDG_RuntimeDir_RelativeIsUnset(t *testing.T) {
	t.Setenv("XDG_RUNTIME_DIR", "run/user/1000")
	if _, err := RuntimeDir(); !errors.Is(err, ErrNotSupported) {
		t.Errorf("got %v, want ErrNotSupported", err)
	}
}
