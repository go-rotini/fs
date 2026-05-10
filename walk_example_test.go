package fs_test

import (
	"fmt"
	stdfs "io/fs"
	"log"
	"os"
	"path/filepath"
	"sort"

	"github.com/go-rotini/fs"
)

// Walk traverses a tree; the callback receives every entry. Use
// [fs.WalkSkipNames] to prune well-known directories (`.git`,
// `node_modules`, `.terraform`, etc.) — the skipped directory's
// entire subtree is omitted.
func ExampleWalk() {
	tmp, cleanup, _ := fs.TempDir("", "fs-example-*")
	defer cleanup()

	for _, p := range []string{
		"src/main.go",
		".git/HEAD",
		"node_modules/foo/index.js",
		"README.md",
	} {
		_ = os.MkdirAll(filepath.Dir(filepath.Join(tmp, p)), 0o755)
		_ = fs.WriteFile(filepath.Join(tmp, p), []byte{})
	}

	var visited []string
	err := fs.Walk(tmp, func(path string, _ stdfs.DirEntry, werr error) error {
		if werr != nil {
			return werr
		}
		rel, _ := filepath.Rel(tmp, path)
		visited = append(visited, filepath.ToSlash(rel))
		return nil
	}, fs.WalkSkipNames([]string{".git", "node_modules"}))
	if err != nil {
		log.Fatal(err)
	}
	sort.Strings(visited)
	for _, v := range visited {
		fmt.Println(v)
	}
	// Output:
	// .
	// README.md
	// src
	// src/main.go
}
