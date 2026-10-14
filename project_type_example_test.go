package fs_test

import (
	"fmt"
	"path/filepath"

	"github.com/go-rotini/fs"
)

// ProjectType detects which language ecosystems own a project root
// based on canonical marker files.
func ExampleProjectType() {
	dir, cleanup, _ := fs.TempDir("", "pt-example-*")
	defer func() { _ = cleanup() }()
	_ = fs.WriteFile(filepath.Join(dir, "go.mod"), []byte("module x\n"))

	kinds, _ := fs.ProjectType(dir)
	fmt.Println(kinds)
	// Output:
	// [go]
}
