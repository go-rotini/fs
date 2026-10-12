//go:build windows

package fs

// newNativeWatcherBackend on Windows returns
// errWatcherUnsupportedBackend: there is no ReadDirectoryChangesW
// backend, so the watcher uses polling.
func newNativeWatcherBackend(_ watcherOptions) (watcherBackend, error) {
	return nil, errWatcherUnsupportedBackend
}
