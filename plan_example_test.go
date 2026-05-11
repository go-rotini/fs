package fs_test

import (
	"fmt"
	"path/filepath"

	"github.com/go-rotini/fs"
)

// A Plan records intended filesystem mutations. Diff renders a
// preview for `--dry-run`; Apply executes the plan against the
// filesystem and writes a journal so an interrupted run can be
// resumed or rolled back.
func ExampleApply() {
	dir, cleanup, _ := fs.TempDir("", "plan-example-*")
	defer func() { _ = cleanup() }()
	target := filepath.Join(dir, "out", "hello.txt")
	jdir := filepath.Join(dir, "journal")

	p := fs.NewPlan().Create(target, []byte("hi"), 0o644)
	_ = p.Diff() // render preview for --dry-run (output omitted; path is temp)

	if err := fs.Apply(p, jdir); err != nil {
		return
	}
	fmt.Println("applied")
	// Output:
	// applied
}
