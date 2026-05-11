package fs_test

import (
	"fmt"

	"github.com/go-rotini/fs"
)

// Gitignore parses .gitignore-style patterns and matches paths
// against them. It handles negation, anchoring, directory-only
// patterns, `**` recursive wildcards, and the standard glob
// metacharacters. Pair it with WithWalkGitignore for source-tree
// walks that respect the project's ignore rules.
func ExampleNewGitignore() {
	g := fs.NewGitignore([]string{
		"*.log",
		"!important.log",
		"node_modules/",
	})

	fmt.Println("foo.log:", g.Match("foo.log", false))
	fmt.Println("important.log:", g.Match("important.log", false))
	fmt.Println("node_modules:", g.Match("node_modules", true))
	fmt.Println("src/main.go:", g.Match("src/main.go", false))
	// Output:
	// foo.log: true
	// important.log: false
	// node_modules: true
	// src/main.go: false
}
