package fs_test

import (
	"fmt"
	"path/filepath"

	"github.com/go-rotini/fs"
)

// FindByContent walks a directory tree returning every line that
// contains the search string. Use FindByContentRegex for richer
// patterns.
func ExampleFindByContent() {
	dir, cleanup, _ := fs.TempDir("", "fbc-example-*")
	defer func() { _ = cleanup() }()
	_ = fs.WriteFile(filepath.Join(dir, "a.txt"), []byte("hello\nworld\n"))

	matches, _ := fs.FindByContent(dir, "world")
	fmt.Println("matches:", len(matches))
	// Output:
	// matches: 1
}
