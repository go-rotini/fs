package fs_test

import (
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/go-rotini/fs"
)

// FindUp walks parent directories looking for a file with the given
// glob pattern. Useful for tools that need a project-relative config
// file no matter where they're invoked from.
func ExampleFindUp() {
	tmp, cleanup, _ := fs.TempDir("", "fs-example-*")
	defer cleanup()

	leaf := filepath.Join(tmp, "src", "pkg", "internal")
	_ = os.MkdirAll(leaf, 0o755)
	_ = fs.WriteFile(filepath.Join(tmp, ".env"), nil)

	got, ok, err := fs.FindUp(".env", leaf)
	if err != nil {
		log.Fatal(err)
	}
	rel, _ := filepath.Rel(tmp, got)
	fmt.Printf("found=%v rel=%s\n", ok, filepath.ToSlash(rel))
	// Output: found=true rel=.env
}

// ProjectRoot returns the first ancestor of startDir that contains
// one of the project markers (default: .git, go.mod, package.json,
// Cargo.toml). Errors with [fs.ErrNotFound] when nothing matches.
func ExampleProjectRoot() {
	tmp, cleanup, _ := fs.TempDir("", "fs-example-*")
	defer cleanup()

	_ = os.MkdirAll(filepath.Join(tmp, ".git"), 0o755)
	deep := filepath.Join(tmp, "src", "pkg", "internal")
	_ = os.MkdirAll(deep, 0o755)

	root, err := fs.ProjectRoot(deep)
	if err != nil {
		log.Fatal(err)
	}
	rel, _ := filepath.Rel(tmp, root)
	fmt.Println("rel:", filepath.ToSlash(rel))
	// Output: rel: .
}
