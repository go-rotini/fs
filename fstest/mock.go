package fstest

import (
	stdfs "io/fs"
	"testing/fstest"
)

// MockFS returns a read-only [io/fs.FS] built from a map of
// slash-separated paths to file contents; parent directories are
// implied. Use it to feed functions that take an [io/fs.FS], such as
// [fs.ScaffoldApply] and [fs.ScaffoldExtract], without touching disk.
// The functions that take OS paths, such as [fs.Walk], cannot read it.
//
// It is a [testing/fstest.MapFS].
func MockFS(layout map[string]string) stdfs.FS {
	files := make(fstest.MapFS, len(layout))
	for path, content := range layout {
		files[path] = &fstest.MapFile{Data: []byte(content)}
	}
	return files
}
