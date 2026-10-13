package fs

// watcherBackend is the contract a watch backend satisfies; the
// polling backend is the only one. A backend is owned exclusively by
// one [*Watcher].
type watcherBackend interface {
	// AddPath registers a directory or file for monitoring. The
	// backend reports any subsequent change as a [rawWatchEvent] on
	// Events().
	AddPath(path string) error

	// RemovePath unregisters a previously-added path and every path
	// registered beneath it. Removing a path that wasn't registered
	// is a no-op (no error).
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
