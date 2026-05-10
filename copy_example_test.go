package fs_test

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"

	"github.com/go-rotini/fs"
)

// CopyFile copies one file atomically: bytes go to a temp file in
// the destination's parent, then the temp is renamed over the
// destination. Source mode (and, by default, mtime) are preserved.
func ExampleCopyFile() {
	tmp, cleanup, _ := fs.TempDir("", "fs-example-*")
	defer cleanup()

	src := filepath.Join(tmp, "src")
	dst := filepath.Join(tmp, "dst")
	_ = fs.WriteFile(src, []byte("payload"))

	if err := fs.CopyFile(src, dst); err != nil {
		log.Fatal(err)
	}
	got, _ := fs.ReadFile(dst)
	fmt.Println(string(got))
	// Output: payload
}

// CopyDir recursively copies a tree. Per-entry errors aggregate
// into a [*fs.MultiError]; the walk continues so a partial copy
// surfaces every problem entry rather than the first.
func ExampleCopyDir() {
	tmp, cleanup, _ := fs.TempDir("", "fs-example-*")
	defer cleanup()

	src := filepath.Join(tmp, "src")
	dst := filepath.Join(tmp, "dst")
	for _, p := range []string{"a.txt", "sub/b.txt", "sub/deep/c.txt"} {
		_ = os.MkdirAll(filepath.Dir(filepath.Join(src, p)), 0o755)
		_ = fs.WriteFile(filepath.Join(src, p), []byte(p))
	}
	if err := fs.CopyDir(src, dst); err != nil {
		log.Fatal(err)
	}

	var got []string
	_ = filepath.WalkDir(dst, func(path string, d os.DirEntry, _ error) error {
		if d.IsDir() {
			return nil
		}
		rel, _ := filepath.Rel(dst, path)
		got = append(got, filepath.ToSlash(rel))
		return nil
	})
	sort.Strings(got)
	for _, p := range got {
		fmt.Println(p)
	}
	// Output:
	// a.txt
	// sub/b.txt
	// sub/deep/c.txt
}
