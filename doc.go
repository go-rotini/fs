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
// All production code lives in this one package. It imports the
// standard library and golang.org/x/sys: [golang.org/x/sys/windows]
// for the mapping calls behind [Mmap] on Windows, and
// golang.org/x/sys/unix on POSIX systems for open(2)/openat(2) with
// O_NOFOLLOW and O_CLOEXEC, flock(2), fchownat(2), termios queries
// and the statfs family. No x/sys types appear in the public API.
//
// # Platforms
//
// Linux, macOS, Windows, FreeBSD, NetBSD, OpenBSD, DragonFly BSD,
// Solaris and illumos, and AIX are supported. On Solaris, illumos and
// AIX, [ProcessStartTime] returns [ErrNotSupported]; on AIX, [MountPoint] and
// [FilesystemType] return [ErrNotSupported], and locks use fcntl(2)
// record locks, which belong to the process rather than the open
// file: two locks on one path from the same process (two goroutines,
// or two [Apply] or [Resume] calls on one journal) both succeed
// instead of waiting, and closing any descriptor of the locked file,
// even one a read of it opened, releases the lock. The package also
// builds for js/wasm, wasip1 and Plan 9, where locking, memory
// mapping, [OpenNoFollow], [OpenAt], disk queries and
// [ProcessStartTime] return [ErrNotSupported] and the access
// predicates judge from permission bits. What takes a lock fails the
// same way there: [Apply], [Resume], [Rollback], [PIDLock] and
// writes with [WithLocked].
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
//     [os.Lstat], and, except on Windows, the file's device and inode,
//     so a file replaced by another with the same size and mtime is
//     still reported. On filesystems that round mtime to whole seconds
//     (FAT, network-attached SMB), two in-place writes within one
//     second that leave the size unchanged can be missed, as can such
//     a replacement on Windows.
//   - A subscription's buffer holds one event by default; events that
//     don't fit are dropped and the subscriber is sent
//     [WatchOverflow]. Directory watches usually want a larger
//     [WithBufferSize].
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
