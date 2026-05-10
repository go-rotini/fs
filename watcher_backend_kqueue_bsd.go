//go:build darwin || freebsd || netbsd || openbsd || dragonfly

package fs

// newNativeWatcherBackend on darwin / freebsd / netbsd / openbsd /
// dragonfly is a placeholder that returns
// errWatcherUnsupportedBackend so the cross-platform shell falls
// through to the polling backend. A full kqueue implementation
// (`syscall.Kqueue`, `Kevent`) is scheduled for a follow-up; the
// polling backend covers correctness in the interim.
func newNativeWatcherBackend(_ watcherOptions) (watcherBackend, error) {
	return nil, errWatcherUnsupportedBackend
}
