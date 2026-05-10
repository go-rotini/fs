//go:build linux

package fs

// newNativeWatcherBackend on Linux is a placeholder that returns
// errWatcherUnsupportedBackend so the cross-platform shell falls
// through to the polling backend. A full inotify implementation
// (`syscall.InotifyInit1`, `InotifyAddWatch`, raw fd reads) is
// scheduled for a follow-up; the polling backend covers correctness
// in the interim and CI verifies behavioral parity via the same
// tests run under `WithPolling`.
func newNativeWatcherBackend(_ watcherOptions) (watcherBackend, error) {
	return nil, errWatcherUnsupportedBackend
}
