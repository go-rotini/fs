package fs_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/go-rotini/fs"
)

// Tail follows path indefinitely, yielding each appended line. Use a
// context to bound the lifetime; when ctx is cancelled the iterator
// returns cleanly. Rotation is handled automatically; when the file
// is renamed and a fresh one appears at path, the iterator picks up
// the new file from offset 0.
func ExampleTail() {
	dir, cleanup, _ := fs.TempDir("", "tail-example-*")
	defer func() { _ = cleanup() }()
	logfile := filepath.Join(dir, "app.log")
	_ = os.WriteFile(logfile, nil, 0o644)

	// Append a line on a short delay so the example demonstrates the
	// follow-as-it-grows pattern. Bound the whole example with a
	// short timeout so it terminates deterministically.
	go func() {
		time.Sleep(50 * time.Millisecond)
		f, err := os.OpenFile(logfile, os.O_APPEND|os.O_WRONLY, 0)
		if err != nil {
			return
		}
		defer func() { _ = f.Close() }()
		_, _ = f.WriteString("hello\n")
	}()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	for line, err := range fs.Tail(ctx, logfile, fs.WithTailPollInterval(10*time.Millisecond)) {
		if err != nil {
			return
		}
		fmt.Println(line)
		break
	}
	// Output:
	// hello
}
