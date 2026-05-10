package fs_test

import (
	"context"
	"fmt"
	"log"
	"path/filepath"
	"time"

	"github.com/go-rotini/fs"
)

// NewWatcher monitors a file for changes via the platform's
// notification mechanism. The watcher follows the file's parent
// directory and filters by basename, so editor atomic-save patterns
// (write-temp + rename) are observed correctly.
//
// This example has no Output block because event delivery is
// asynchronous — pkg.go.dev still renders the body as runnable
// reference code.
func ExampleNewWatcher() {
	tmp, cleanup, _ := fs.TempDir("", "fs-example-*")
	defer cleanup()

	path := filepath.Join(tmp, "config.yaml")
	if err := fs.WriteFile(path, []byte("port: 8080\n")); err != nil {
		log.Fatal(err)
	}

	// In v0.1 the polling backend is used regardless of platform;
	// pass WithPolling(d) to tune the cadence. Disable debouncing
	// for prompt visibility in tests.
	w, err := fs.NewWatcher(path,
		fs.WithPolling(50*time.Millisecond),
		fs.WithDebounce(0))
	if err != nil {
		log.Fatal(err)
	}
	defer w.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()
	events, err := w.Subscribe(ctx)
	if err != nil {
		log.Fatal(err)
	}

	// Mutate the file from another goroutine; the watcher delivers a
	// write event to the subscription channel.
	go func() {
		time.Sleep(100 * time.Millisecond)
		_ = fs.WriteFile(path, []byte("port: 9090\n"))
	}()

	for ev := range events {
		if ev.Op.Has(fs.WatchWrite) {
			fmt.Println("config changed; reloading")
			return
		}
	}
}
