package fs_test

import (
	"fmt"
	"log"
	"path/filepath"

	"github.com/go-rotini/fs"
)

// WriteFile writes atomically: bytes go to a temp file in the
// destination's parent directory, then the temp is renamed over the
// destination. A reader concurrently observing the path sees either
// the old contents or the new contents, never a half-written file.
func ExampleWriteFile() {
	tmp, cleanup, _ := fs.TempDir("", "fs-example-*")
	defer cleanup()

	path := filepath.Join(tmp, "config.yaml")
	if err := fs.WriteFile(path, []byte("port: 8080\n")); err != nil {
		log.Fatal(err)
	}
	data, _ := fs.ReadFile(path)
	fmt.Print(string(data))
	// Output: port: 8080
}

// WriteFile honoring WithPerm gives the secrets-file idiom in one
// call: owner-only read/write, atomic on disk.
func ExampleWriteFile_secret() {
	tmp, cleanup, _ := fs.TempDir("", "fs-example-*")
	defer cleanup()

	path := filepath.Join(tmp, "token")
	if err := fs.WriteFile(path, []byte("hunter2"), fs.WithPerm(fs.Mode0600)); err != nil {
		log.Fatal(err)
	}
	// On a POSIX system, this file is now mode 0o600.
}

// WriteFile + WithMkdirAll creates missing parent directories
// before the write.
func ExampleWriteFile_ensure() {
	tmp, cleanup, _ := fs.TempDir("", "fs-example-*")
	defer cleanup()

	path := filepath.Join(tmp, "deep", "nested", "tree", "out.txt")
	if err := fs.WriteFile(path, []byte("hello"), fs.WithMkdirAll(true)); err != nil {
		log.Fatal(err)
	}
	fmt.Println(fs.Exists(path))
	// Output: true
}

// WriteFile honors WithBackup: the existing destination is renamed
// to "<path><suffix>" before the new content is committed.
func ExampleWriteFile_backup() {
	tmp, cleanup, _ := fs.TempDir("", "fs-example-*")
	defer cleanup()

	path := filepath.Join(tmp, "config.yaml")
	_ = fs.WriteFile(path, []byte("v1\n"))
	_ = fs.WriteFile(path, []byte("v2\n"), fs.WithBackup(".bak"))

	current, _ := fs.ReadFile(path)
	backup, _ := fs.ReadFile(path + ".bak")
	fmt.Printf("current=%q backup=%q", string(current), string(backup))
	// Output: current="v2\n" backup="v1\n"
}
