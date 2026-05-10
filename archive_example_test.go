package fs_test

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"

	"github.com/go-rotini/fs"
)

// ExtractArchiveFile auto-detects the format (tar / tar.gz / zip)
// from the file's leading bytes. Every entry passes through
// [fs.MustBeChildOf] before any filesystem write — extraction
// outside dst is impossible by construction (zip-slip / tar-slip
// defense).
func ExampleExtractArchiveFile() {
	tmp, cleanup, _ := fs.TempDir("", "fs-example-*")
	defer cleanup()

	// Build a tiny tar.gz to extract.
	src := filepath.Join(tmp, "src")
	_ = os.MkdirAll(src, 0o755)
	_ = fs.WriteFile(filepath.Join(src, "README.md"), []byte("# example\n"))
	_ = fs.WriteFile(filepath.Join(src, "main.go"), []byte("package main\n"))
	archive := filepath.Join(tmp, "release.tar.gz")
	if err := fs.CreateArchiveFile(archive, src, fs.WithArchiveFormat(fs.ArchiveFormatTarGz)); err != nil {
		log.Fatal(err)
	}

	// Extract elsewhere.
	dst := filepath.Join(tmp, "out")
	if err := fs.ExtractArchiveFile(archive, dst); err != nil {
		log.Fatal(err)
	}

	// List what landed.
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
	// README.md
	// main.go
}

// CreateArchiveFile writes a tar / tar.gz / zip archive from a
// directory tree. Pass [fs.WithArchiveFormat] to choose the
// container; [fs.WithArchiveCreateFilter] to skip entries.
func ExampleCreateArchiveFile() {
	tmp, cleanup, _ := fs.TempDir("", "fs-example-*")
	defer cleanup()

	src := filepath.Join(tmp, "tree")
	_ = os.MkdirAll(src, 0o755)
	_ = fs.WriteFile(filepath.Join(src, "keep.txt"), []byte("ok"))
	_ = fs.WriteFile(filepath.Join(src, "skip.tmp"), []byte("scratch"))

	out := filepath.Join(tmp, "out.zip")
	err := fs.CreateArchiveFile(out, src,
		fs.WithArchiveFormat(fs.ArchiveFormatZip),
		fs.WithArchiveCreateFilter(func(p string, _ os.FileInfo) bool {
			return filepath.Ext(p) != ".tmp"
		}))
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(fs.Exists(out))
	// Output: true
}
