package fstest

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/go-rotini/fs"
)

func TestTempFileT_AutoCleanup(t *testing.T) {
	t.Parallel()
	var nameSnapshot string

	t.Run("inner", func(t *testing.T) {
		f := TempFileT(t, "rotini-tft-*")
		nameSnapshot = f.Name()
		if !fs.Exists(nameSnapshot) {
			t.Fatal("file should exist while subtest is alive")
		}
	})

	// After inner subtest completes, t.Cleanup ran.
	if fs.Exists(nameSnapshot) {
		t.Errorf("file %s should have been auto-cleaned after subtest", nameSnapshot)
	}
}

func TestTempDirT_AutoCleanup(t *testing.T) {
	t.Parallel()
	var dirSnapshot string

	t.Run("inner", func(t *testing.T) {
		dir := TempDirT(t, "rotini-tdt-*")
		dirSnapshot = dir
		if err := os.WriteFile(filepath.Join(dir, "f"), []byte("x"), 0o644); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}
		if !fs.IsDir(dirSnapshot) {
			t.Fatal("dir should exist while subtest is alive")
		}
	})

	if fs.Exists(dirSnapshot) {
		t.Errorf("dir %s should have been auto-cleaned after subtest", dirSnapshot)
	}
}
