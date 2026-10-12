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
