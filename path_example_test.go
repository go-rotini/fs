package fs_test

import (
	"fmt"

	"github.com/go-rotini/fs"
)

// Stem returns the basename of a path without its final extension.
// Leading-dot names (like .env or .gitignore) have no stem-vs-ext
// split.
func ExampleStem() {
	fmt.Println(fs.Stem("foo/bar.tar.gz"))
	fmt.Println(fs.Stem("README"))
	fmt.Println(fs.Stem(".env"))
	// Output:
	// bar.tar
	// README
	// .env
}

// SanitizeFilename strips characters that would be illegal in a
// filename on Windows or POSIX. When the cleaned stem matches a
// Windows reserved device name, an underscore is inserted before
// the extension; "CON.txt" becomes "CON_.txt", not "CON.txt_".
func ExampleSanitizeFilename() {
	fmt.Println(fs.SanitizeFilename("report: q4 / 2024.pdf"))
	fmt.Println(fs.SanitizeFilename("CON.txt"))
	fmt.Println(fs.SanitizeFilename("trailing-space . "))
	// Output:
	// report q4  2024.pdf
	// CON_.txt
	// trailing-space
}

// MustBeChildOf returns nil when child resolves inside parent, or
// [fs.ErrEscapesRoot] otherwise. Use to constrain user-supplied
// paths to a sandbox before any filesystem operation.
func ExampleMustBeChildOf() {
	fmt.Println(fs.MustBeChildOf("/var/lib/myapp", "/var/lib/myapp/data/cfg") == nil)
	fmt.Println(fs.MustBeChildOf("/var/lib/myapp", "/var/lib/myapp/../etc/passwd") == nil)
	// Output:
	// true
	// false
}

// Expand resolves leading ~ and embedded $VAR / ${VAR} references.
// Resolution is purely lexical; the filesystem is consulted only
// when a ~user is supplied. Unset variables expand to the empty
// string by default; pass [fs.WithStrictExpansion] to error
// instead. The example uses a literal path so the Output block is
// deterministic across environments.
func ExampleExpand() {
	// A path with no expansion markers passes through unchanged.
	got, _ := fs.Expand("/etc/myapp/config.yaml")
	fmt.Println(got)

	// An unset variable, in strict mode, errors instead of
	// silently expanding to "".
	_, err := fs.Expand("$ROTINI_FS_NEVER_SET_VAR/x", fs.WithStrictExpansion())
	fmt.Println(err != nil)
	// Output:
	// /etc/myapp/config.yaml
	// true
}
