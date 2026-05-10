// Package fs is a CLI-focused filesystem helpers library: atomic writes,
// safe reads, path resolution, cross-platform user-directory lookup,
// directory walking, file watching, and the dozens of small operations a
// CLI tool repeatedly needs to get right.
//
// The package is a layer on top of stdlib's [os], [io/fs], and
// [path/filepath] that turns the most common CLI operations into one-line
// calls with safe defaults and useful errors. It is NOT a virtual
// filesystem abstraction (see `afero` / `go-billy`); every operation
// talks to the real filesystem.
//
// # Zero third-party dependencies
//
// Every package — root, watcher, lock, fstest — has zero non-stdlib
// runtime imports. Platform-native notification APIs (inotify / kqueue /
// ReadDirectoryChangesW) and locking primitives (flock / LockFileEx) are
// accessed directly through stdlib's `syscall` package.
package fs
