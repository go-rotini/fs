//go:build darwin

package fs

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestDarwin_ConfigDir(t *testing.T) {
	t.Parallel()
	got, err := ConfigDir()
	if err != nil {
		t.Fatalf("ConfigDir: %v", err)
	}
	if !strings.HasSuffix(got, filepath.Join("Library", "Application Support")) {
		t.Errorf("ConfigDir = %s; want suffix Library/Application Support", got)
	}
}

func TestDarwin_CacheDir(t *testing.T) {
	t.Parallel()
	got, err := CacheDir()
	if err != nil {
		t.Fatalf("CacheDir: %v", err)
	}
	if !strings.HasSuffix(got, filepath.Join("Library", "Caches")) {
		t.Errorf("CacheDir = %s; want suffix Library/Caches", got)
	}
}

func TestDarwin_DataDirSameAsConfigDir(t *testing.T) {
	t.Parallel()
	c, _ := ConfigDir()
	d, _ := DataDir()
	if c != d {
		t.Errorf("ConfigDir=%s, DataDir=%s; macOS conventionally returns the same root", c, d)
	}
}

func TestDarwin_RuntimeDir(t *testing.T) {
	t.Parallel()
	got, err := RuntimeDir()
	if err != nil {
		t.Fatalf("RuntimeDir: %v", err)
	}
	if !strings.HasSuffix(got, filepath.Join("Library", "Caches", "TemporaryItems")) {
		t.Errorf("RuntimeDir = %s; want suffix Library/Caches/TemporaryItems", got)
	}
}

func TestDarwin_SystemConfigDir(t *testing.T) {
	t.Parallel()
	got, err := SystemConfigDir("rotini-test")
	if err != nil {
		t.Fatalf("SystemConfigDir: %v", err)
	}
	want := "/Library/Application Support/rotini-test"
	if got != want {
		t.Errorf("got %s, want %s", got, want)
	}
}
