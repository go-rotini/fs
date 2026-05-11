package fs_test

import (
	"fmt"
	"log"
	"path/filepath"
	"sort"
	"testing/fstest"

	"github.com/go-rotini/fs"
)

// ScaffoldApply walks an embedded template tree, renders every path
// and file contents through text/template with vars, and writes the
// rendered tree under dst. Default conflict policy is
// SkipExisting; a re-run on a previously-applied scaffold is a
// no-op (idempotent).
//
// In real code the source io/fs.FS is typically an [embed.FS]; the
// example uses [testing/fstest.MapFS] to keep it self-contained.
func ExampleScaffoldApply() {
	tmp, cleanup, _ := fs.TempDir("", "fs-example-*")
	defer cleanup()

	src := fstest.MapFS{
		"README.md":             {Data: []byte("# {{.Name}}\n\nA project for {{.Owner}}.\n")},
		"src/{{.Name}}/main.go": {Data: []byte("package {{.Name}}\n")},
	}
	vars := struct{ Name, Owner string }{"myapp", "alice"}

	if err := fs.ScaffoldApply(src, tmp, vars); err != nil {
		log.Fatal(err)
	}

	// Print the rendered tree.
	var got []string
	for _, p := range []string{"README.md", "src/myapp/main.go"} {
		data, err := fs.ReadFile(filepath.Join(tmp, p))
		if err != nil {
			log.Fatal(err)
		}
		got = append(got, p+": "+string(data))
	}
	sort.Strings(got)
	for _, line := range got {
		fmt.Print(line)
	}
	// Output:
	// README.md: # myapp
	//
	// A project for alice.
	// src/myapp/main.go: package myapp
}
