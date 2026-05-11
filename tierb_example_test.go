package fs_test

import (
	"fmt"
	"path/filepath"

	"github.com/go-rotini/fs"
)

// Mmap maps a file read-only into memory. Use Data() to access the
// bytes and Close() to release both the mapping and the file.
func ExampleMmap() {
	dir, cleanup, _ := fs.TempDir("", "mmap-example-*")
	defer func() { _ = cleanup() }()
	path := filepath.Join(dir, "data.bin")
	_ = fs.WriteFile(path, []byte("hello"))

	m, err := fs.Mmap(path)
	if err != nil {
		return
	}
	defer func() { _ = m.Close() }()
	fmt.Println(string(m.Data()))
	// Output:
	// hello
}

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
