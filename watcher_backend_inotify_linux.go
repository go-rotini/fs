//go:build linux

package fs

// newNativeWatcherBackend on Linux returns errWatcherUnsupportedBackend:
// there is no inotify backend, so the watcher uses polling.
func newNativeWatcherBackend(_ watcherOptions) (watcherBackend, error) {
	return nil, errWatcherUnsupportedBackend
}
