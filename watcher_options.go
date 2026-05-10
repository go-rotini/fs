package fs

import (
	"log/slog"
	"time"
)

// WatcherOption configures a [*Watcher].
type WatcherOption func(*watcherOptions)

type watcherOptions struct {
	debounce        time.Duration
	logger          *slog.Logger
	recursive       bool
	bufferSize      int
	pollingInterval time.Duration // 0 = use platform-native backend
	forcePolling    bool          // explicit WithPolling toggle
}

const (
	defaultWatcherDebounce        = 75 * time.Millisecond
	defaultWatcherBufferSize      = 1
	defaultWatcherPollingInterval = 1 * time.Second
)

func newWatcherOptions(opts []WatcherOption) watcherOptions {
	cfg := watcherOptions{
		debounce:        defaultWatcherDebounce,
		recursive:       true, // honored only by NewDirWatcher
		bufferSize:      defaultWatcherBufferSize,
		pollingInterval: 0,
	}
	for _, o := range opts {
		o(&cfg)
	}
	return cfg
}

// WithDebounce overrides the trailing-edge debounce period applied
// to events before fan-out to subscribers. Default 75ms; zero
// disables debouncing entirely (every backend event becomes one
// subscriber event).
func WithDebounce(d time.Duration) WatcherOption {
	return func(o *watcherOptions) { o.debounce = d }
}

// WithLogger directs non-fatal diagnostic messages from a [*Watcher]
// to l. Default is a discard logger.
func WithLogger(l *slog.Logger) WatcherOption {
	return func(o *watcherOptions) { o.logger = l }
}

// WithRecursive controls whether [NewDirWatcher] watches subdirectories.
// Default true; pass false for a single-directory (non-recursive) watch.
func WithRecursive(b bool) WatcherOption {
	return func(o *watcherOptions) { o.recursive = b }
}

// WithBufferSize sizes each subscription's buffered channel. Default 1.
// A larger buffer reduces drop frequency on slow subscribers but
// increases memory per subscription.
func WithBufferSize(n int) WatcherOption {
	return func(o *watcherOptions) {
		if n > 0 {
			o.bufferSize = n
		}
	}
}

// WithPolling overrides the polling interval. In v0.1 every watcher
// uses polling regardless; this option is the only way to tune the
// stat-loop cadence (default 1 second).
//
// Once the platform-native backends (inotify / kqueue /
// ReadDirectoryChangesW) land in a follow-up release, WithPolling
// will force the polling backend in preference to the native one —
// useful for filesystems where the native APIs are known to be
// unreliable (NFS, FUSE, exotic mounts) or in tests that want
// deterministic, syscall-free behavior.
//
// interval <= 0 selects the default 1s.
func WithPolling(interval time.Duration) WatcherOption {
	return func(o *watcherOptions) {
		o.forcePolling = true
		if interval > 0 {
			o.pollingInterval = interval
		} else {
			o.pollingInterval = defaultWatcherPollingInterval
		}
	}
}
