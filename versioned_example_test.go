package fs_test

import (
	"fmt"
	"path/filepath"

	"github.com/go-rotini/fs"
)

// WriteFileVersioned writes a new value atomically and saves the
// prior content as a timestamped backup. WithVersionsKeep bounds
// the number of retained backups; WithVersionsMaxAge bounds their
// age. ListVersions returns the surviving backups newest first.
func ExampleWriteFileVersioned() {
	dir, cleanup, _ := fs.TempDir("", "versioned-example-*")
	defer func() { _ = cleanup() }()
	cfg := filepath.Join(dir, "config.yaml")

	_, _ = fs.WriteFileVersioned(cfg, []byte("v1"))
	_, _ = fs.WriteFileVersioned(cfg, []byte("v2"), fs.WithVersionsKeep(3))
	_, _ = fs.WriteFileVersioned(cfg, []byte("v3"), fs.WithVersionsKeep(3))

	versions, _ := fs.ListVersions(cfg)
	fmt.Println("backups:", len(versions))
	// Output:
	// backups: 2
}
