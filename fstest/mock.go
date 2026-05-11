package fstest

import (
	stdfs "io/fs"
	"testing/fstest"
)

// MockFS returns a read-only [io/fs.FS] over a path to contents map.
// Useful for testing read paths ([fs.Walk], [fs.Glob], [fs.Find],
// [fs.ReadFile] against the io/fs.FS surface) without touching
// disk.
//
// Wraps stdlib's [testing/fstest.MapFS]; entries are accessible via
// the same path-based methods as any other io/fs.FS.
func MockFS(layout map[string]string) stdfs.FS {
	files := make(fstest.MapFS, len(layout))
	for path, content := range layout {
		files[path] = &fstest.MapFile{Data: []byte(content)}
	}
	return files
}
