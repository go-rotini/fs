//go:build windows

package fs

import (
	"errors"
	"path/filepath"
	"testing"
)

func TestWindows_ConfigDir_FromAppData(t *testing.T) {
	t.Setenv("APPDATA", `C:\Users\test\AppData\Roaming`)
	got, err := ConfigDir()
	if err != nil {
		t.Fatalf("ConfigDir: %v", err)
	}
	if got != `C:\Users\test\AppData\Roaming` {
		t.Errorf("got %s", got)
	}
}

func TestWindows_ConfigDir_Unset(t *testing.T) {
	t.Setenv("APPDATA", "")
	_, err := ConfigDir()
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("got %v, want ErrNotFound", err)
	}
}

func TestWindows_CacheDir_FromLocalAppData(t *testing.T) {
	t.Setenv("LOCALAPPDATA", `C:\Users\test\AppData\Local`)
	got, err := CacheDir()
	if err != nil {
		t.Fatalf("CacheDir: %v", err)
	}
	if got != `C:\Users\test\AppData\Local` {
		t.Errorf("got %s", got)
	}
}

func TestWindows_RuntimeDir(t *testing.T) {
	t.Setenv("LOCALAPPDATA", `C:\Users\test\AppData\Local`)
	got, err := RuntimeDir()
	if err != nil {
		t.Fatalf("RuntimeDir: %v", err)
	}
	want := filepath.Join(`C:\Users\test\AppData\Local`, "Temp")
	if got != want {
		t.Errorf("got %s, want %s", got, want)
	}
}

func TestWindows_SystemConfigDir(t *testing.T) {
	t.Setenv("PROGRAMDATA", `C:\ProgramData`)
	got, err := SystemConfigDir("myapp")
	if err != nil {
		t.Fatalf("SystemConfigDir: %v", err)
	}
	want := filepath.Join(`C:\ProgramData`, "myapp")
	if got != want {
		t.Errorf("got %s, want %s", got, want)
	}
}

func TestWindows_SystemConfigDir_Unset(t *testing.T) {
	t.Setenv("PROGRAMDATA", "")
	_, err := SystemConfigDir("myapp")
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("got %v, want ErrNotFound", err)
	}
}
