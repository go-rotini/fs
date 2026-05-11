package fs

import "errors"

// watcherBackend is the contract every kernel-API or polling
// implementation must satisfy. Backends are constructed by
// [selectWatcherBackend] and owned exclusively by one [*Watcher].
type watcherBackend interface {
	// AddPath registers a directory or file for monitoring. The
	// backend reports any subsequent change as a [rawWatchEvent] on
	// Events().
	AddPath(path string) error

	// RemovePath unregisters a previously-added path. Removing a
	// path that wasn't registered is a no-op (no error).
	RemovePath(path string) error

	// Events returns the channel of raw events the backend emits.
	// The channel closes when Close() is called.
	Events() <-chan rawWatchEvent

	// Close releases any resources the backend acquired (kernel fds,
	// goroutines, ticker timers, etc.). Idempotent.
	Close() error
}

// rawWatchEvent is the backend to cross-platform-shell event shape.
// It carries no Time; the shell stamps Time at fan-out.
type rawWatchEvent struct {
	path string
	op   WatchOp
}

// errWatcherUnsupportedBackend is returned by per-platform native
// constructors when the running OS / arch lacks a usable
// implementation. The cross-platform shell catches this and falls
// back to the polling backend.
var errWatcherUnsupportedBackend = errors.New("watcher: native backend unavailable")
