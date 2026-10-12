// Package fs is a filesystem helpers library for CLI tools. It covers
// atomic writes, bounded reads, path resolution, cross-platform
// user-directory lookup, directory walking, file watching, archive
// creation and extraction, scaffolding, disk-info queries, advisory file
// locking, directory-backed caching, log rotation, tail-follow with
// rotation handling, versioned backups, transactional plan/apply,
// gitignore-aware walks, and the many small operations a CLI tool
// repeatedly needs to get right.
//
// The package sits on top of stdlib's [os], [io/fs], and
// [path/filepath], turning common CLI operations into one-line calls
// with safe defaults and useful errors. It is not a virtual
// filesystem abstraction; every operation touches the real
// filesystem. For tests, use [github.com/go-rotini/fs/fstest]:
// fstest.MockFS for an in-memory read-only [io/fs.FS], or
// fstest.NewTestHarness for a sandbox in [testing.T.TempDir].
//
// # Dependencies
//
// All production code lives in this one package. It imports only the
// standard library, except that one Windows-only file
// (mmap_windows.go) imports [golang.org/x/sys/windows] for the
// CreateFileMapping, MapViewOfFile and UnmapViewOfFile wrappers used
// by [Mmap]. No x/sys types appear in the public API. Other
// platform-specific calls (flock and LockFileEx for locks, mmap on
// POSIX) go through stdlib's [syscall] package.
//
// The package does not import [testing]. The test helpers (TestHarness,
// MockFS, WithTempEnv, TempFileT, TempDirT) live in the sub-package
// [github.com/go-rotini/fs/fstest], so production binaries do not pull
// in [testing] and its global flag registration.
//
// # API conventions
//
// Nearly every operation that touches a path returns errors as a
// [*PathError] holding the operation name, the path, and the
// underlying cause. [errors.Is] matches both this package's sentinels
// (e.g., [ErrNotFound]) and the equivalent stdlib sentinels (e.g.,
// [io/fs.ErrNotExist]) on the same error. [CopyDir] and
// [RemoveContents] aggregate per-entry errors into a [*MultiError]
// whose Unwrap returns every component, so [errors.Is] works across
// the aggregate.
//
// The defaults are safe: writes are atomic via temp+rename; reads are
// bounded ([WithMaxSize], default 100 MiB); removal is idempotent
// ([Remove], [RemoveAll], [RemoveContents]) unless [WithStrict];
// archive extraction confines every entry through [MustBeChildOf]
// (zip-slip / tar-slip defense).
//
// # Pitfalls
//
// Issues the package documents because they are easy to miss:
//
//   - Atomic writes require the temp file to live in the destination's
//     parent directory. [WriteFile] handles this; hand-rolled
//     temp+rename code must too. Cross-directory rename is sometimes
//     a copy + delete, not atomic.
//   - TOCTOU (time-of-check to time-of-use) races are real. An
//     Exists(path) && ReadFile(path) sequence is racy. The package's
//     predicates exist for ergonomics, not security;
//     security-critical code should use [OpenNoFollow] or [OpenAt] and
//     read-and-handle-error.
//   - [Exists] returns false on permission errors. Callers who need
//     to distinguish use [Stat] directly. Do not write
//     `if !Exists { create }` in privileged contexts.
//   - The watcher watches a file's parent directory (with basename
//     filtering) so editor atomic-save patterns are observed
//     correctly. Subscribing to a non-existent path requires
//     [NewLazyWatcher].
//   - [WithMaxSize] defends against /dev/zero-class reads. Default
//     100 MiB; callers reading attacker-controlled paths must keep
//     it set.
//   - [SanitizeFilename] inserts `_` before the extension when the
//     stem matches a Windows reserved name: CON.txt becomes
//     CON_.txt. Suffixing the whole filename (CON.txt_) leaves
//     Windows treating the file as the CON device.
//   - Archive extraction (zip/tar) must use [ExtractArchive]; every
//     entry passes through [MustBeChildOf] before any filesystem
//     write. Hand-rolled extraction code is the classic zip-slip
//     vulnerability.
//   - macOS [path/filepath.EvalSymlinks] resolves /var to /private/var
//     and similar symlinks. Tests that compare paths from
//     [testing.T.TempDir] against /var/folders/... should resolve via
//     [path/filepath.EvalSymlinks] on both sides before comparing.
//   - On Windows, symlink creation typically requires Administrator
//     or Developer Mode; without it the symlink helpers return the
//     OS error.
//   - Watching is polling-only: every [Watcher] stats its paths at the
//     [WithPolling] interval, 1 second by default. Pass a finer
//     interval when latency matters.
//   - The watcher's debouncer adds 75 ms of trailing-edge latency by
//     default ([WithDebounce]). Tests that need immediate event
//     visibility should pass WithDebounce(0).
//   - The watcher compares file size and modification time from
//     [os.Lstat]. On filesystems that round mtime to whole seconds
//     (FAT, network-attached SMB), two writes within one second that
//     leave the size unchanged can be missed.
//   - [Hash], [HashCompare], and [HashWriter] expose MD5 and SHA-1
//     for non-security uses (legacy compat, content-addressed
//     caches); they are not secure for integrity defense against
//     attackers.
//   - [ParseBytes] uses strict SI semantics: 1KB == 1000, matching
//     kubectl / docker / kafka. IEC binary units (1KiB == 1024) keep
//     their canonical meaning. For the legacy disk-vendor idiom
//     where bare KB means 1024, use [ParseBytesIEC]. Some other
//     libraries treat KB as 1024, so choose deliberately.
package fs
