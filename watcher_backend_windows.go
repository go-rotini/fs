//go:build windows

package fs

// newNativeWatcherBackend on Windows is a placeholder that returns
// errWatcherUnsupportedBackend so the cross-platform shell falls
// through to the polling backend. A full
// `ReadDirectoryChangesW`-via-`syscall.Syscall6` implementation is
// scheduled for a follow-up; the polling backend covers correctness
// in the interim.
func newNativeWatcherBackend(_ watcherOptions) (watcherBackend, error) {
	return nil, errWatcherUnsupportedBackend
}
