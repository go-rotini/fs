package fs_test

import (
	"fmt"
	"path/filepath"

	"github.com/go-rotini/fs"
)

// WithLock is the idiomatic call: acquire, run, release. Release
// runs even if fn panics. Use it for any operation that needs to
// coordinate with peer processes against the same lockfile.
func ExampleWithLock() {
	dir, cleanup, _ := fs.TempDir("", "lock-example-*")
	defer func() { _ = cleanup() }()

	lock := filepath.Join(dir, "app.lock")
	err := fs.WithLock(lock, func() error {
		// Critical section: do work that requires mutual exclusion
		// against other processes holding the same lockfile.
		return nil
	})
	fmt.Println("err:", err == nil)
	// Output:
	// err: true
}

// TryLock is the non-blocking acquire — useful for "if no one else
// is running, do this work; otherwise skip" patterns common in
// cron-style commands.
func ExampleTryLock() {
	dir, cleanup, _ := fs.TempDir("", "lock-example-*")
	defer func() { _ = cleanup() }()
	lock := filepath.Join(dir, "task.lock")

	h, ok, err := fs.TryLock(lock)
	if err != nil {
		return
	}
	if !ok {
		fmt.Println("another instance is running; exiting")
		return
	}
	defer func() { _ = h.Release() }()
	fmt.Println("acquired")
	// Output:
	// acquired
}

// PIDLock records the calling process's PID inside the lockfile so
// peers can identify the holder by inspecting the file. Stale locks
// (where the recorded PID no longer exists) are reclaimed
// automatically and the wrapped error matches [fs.ErrStaleLock].
func ExamplePIDLock() {
	dir, cleanup, _ := fs.TempDir("", "lock-example-*")
	defer func() { _ = cleanup() }()
	lock := filepath.Join(dir, "daemon.pid")

	h, err := fs.PIDLock(lock)
	if err != nil {
		// On a clean acquire, err is nil. On a stale-lock reclaim,
		// err is wrapped with fs.ErrStaleLock; the handle is still
		// valid in that case.
		return
	}
	defer func() { _ = h.Release() }()
	fmt.Println("locked with PID:", h.PID() > 0)
	// Output:
	// locked with PID: true
}
