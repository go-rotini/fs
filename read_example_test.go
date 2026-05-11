package fs_test

import (
	"fmt"
	"log"
	"path/filepath"

	"github.com/go-rotini/fs"
)

// ReadFile reads a whole file with a default 100 MiB cap. A file
// larger than the cap errors with [fs.ErrFileTooLarge] rather than
// allocating unbounded memory.
func ExampleReadFile() {
	tmp, cleanup, _ := fs.TempDir("", "fs-example-*")
	defer cleanup()

	path := filepath.Join(tmp, "in")
	_ = fs.WriteFile(path, []byte("hello world"))

	data, err := fs.ReadFile(path)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(string(data))
	// Output: hello world
}

// ReadLines reads a file into a string slice, one element per line.
// Trailing line endings (LF, CRLF, or bare CR) are stripped; a
// leading UTF-8 BOM is removed.
func ExampleReadLines() {
	tmp, cleanup, _ := fs.TempDir("", "fs-example-*")
	defer cleanup()

	path := filepath.Join(tmp, "log")
	_ = fs.WriteFile(path, []byte("first\nsecond\nthird\n"))

	lines, err := fs.ReadLines(path)
	if err != nil {
		log.Fatal(err)
	}
	for _, line := range lines {
		fmt.Println(line)
	}
	// Output:
	// first
	// second
	// third
}

// OpenLines returns an iterator. The second value is non-nil only
// if the underlying scanner errors mid-stream (line too long, I/O
// failure, etc.); always check it inside the loop.
func ExampleOpenLines() {
	tmp, cleanup, _ := fs.TempDir("", "fs-example-*")
	defer cleanup()

	path := filepath.Join(tmp, "log")
	_ = fs.WriteFile(path, []byte("one\ntwo\nthree\n"))

	seq, closeFn, err := fs.OpenLines(path)
	if err != nil {
		log.Fatal(err)
	}
	defer closeFn()

	for line, lerr := range seq {
		if lerr != nil {
			log.Fatal(lerr)
		}
		fmt.Println(line)
	}
	// Output:
	// one
	// two
	// three
}

// ReadFirstLine returns just the first line; useful for shebangs,
// version stamps, single-value config files.
func ExampleReadFirstLine() {
	tmp, cleanup, _ := fs.TempDir("", "fs-example-*")
	defer cleanup()

	path := filepath.Join(tmp, "VERSION")
	_ = fs.WriteFile(path, []byte("v1.2.3\nuntracked details\n"))

	v, err := fs.ReadFirstLine(path)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(v)
	// Output: v1.2.3
}
