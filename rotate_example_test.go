package fs_test

import (
	"fmt"
	"path/filepath"

	"github.com/go-rotini/fs"
)

// NewRotator returns an io.WriteCloser that transparently rotates
// the backing file when size or age thresholds are exceeded. The
// keep policy bounds disk usage; gzip compression is optional.
func ExampleNewRotator() {
	dir, cleanup, _ := fs.TempDir("", "rotate-example-*")
	defer func() { _ = cleanup() }()
	logfile := filepath.Join(dir, "app.log")

	r, err := fs.NewRotator(logfile,
		fs.WithRotateMaxBytes(10),
		fs.WithRotateKeep(3),
		fs.WithRotateCompress(true),
	)
	if err != nil {
		return
	}
	defer func() { _ = r.Close() }()

	_, _ = r.Write([]byte("first-line\n"))
	_, _ = r.Write([]byte("second-line\n"))
	fmt.Println("done")
	// Output:
	// done
}
