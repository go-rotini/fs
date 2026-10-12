//go:build darwin || freebsd || netbsd || openbsd || dragonfly

package fs

// newNativeWatcherBackend on darwin and the BSDs returns
// errWatcherUnsupportedBackend: there is no kqueue backend, so the
// watcher uses polling.
func newNativeWatcherBackend(_ watcherOptions) (watcherBackend, error) {
	return nil, errWatcherUnsupportedBackend
}
