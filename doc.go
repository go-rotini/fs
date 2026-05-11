// Package fs is a filesystem helpers library for CLI tools. It covers
// atomic writes, safe reads, path resolution, cross-platform user-
// directory lookup, directory walking, file watching, archive
// extraction, scaffolding, disk-info queries, advisory file locking,
// directory-backed caching, log rotation, tail-follow with rotation
// handling, versioned backups, transactional plan/apply, gitignore-
// aware walks, and the dozens of small operations a CLI tool
// repeatedly needs to get right.
//
// The package sits on top of stdlib's [os], [io/fs], and
// [path/filepath], turning common CLI operations into one-line calls
// with safe defaults and useful errors. It is not a virtual
// filesystem abstraction (see afero / go-billy); every operation
// touches the real filesystem. For tests against a fixture without
// disk I/O, use [github.com/go-rotini/fs/fstest]: fstest.MockFS for
// read paths or fstest.NewTestHarness for write paths in a
// [testing.T.TempDir].
//
// # Zero third-party dependencies
//
// The fs package is a single flat root for production code: watcher,
// lock, cache, rotator, plan, archive, scaffold, gitignore, mmap, and
// disk-info helpers all live in the same package. The production
// surface has zero non-stdlib runtime imports except for one
// narrowly-scoped exception (see below) and does not import [testing].
// Platform-native syscalls (flock and LockFileEx for the lock
// helpers; mmap and MapViewOfFile for the memory-map helpers; the
// native watcher backends are a planned follow-up, see Stability) go
// through stdlib's [syscall] package or the documented
// [golang.org/x/sys] exception.
//
// # The x/sys exception
//
// [golang.org/x/sys/windows] is imported by exactly one Windows-only
// file (mmap_windows.go) for typed Handle, CreateFileMapping,
// MapViewOfFile, and UnmapViewOfFile wrappers around kernel32.dll.
// The package is Go-team-maintained, MIT-licensed, and has zero non-
// stdlib transitive deps. The exception is scoped to platform-syscall
// files; no public API surface leaks x/sys types. The same exception
// applies to future syscall surfaces where x/sys carries a wrapper
// materially safer than hand-rolling syscall.Syscall6 against a DLL
// proc.
//
// The one sub-package is [github.com/go-rotini/fs/fstest], which
// holds the test helpers (TestHarness, MockFS, WithTempEnv,
// TempFileT, TempDirT). Keeping those out of the main package lets
// production binaries avoid pulling stdlib's [testing] package, and
// its global flag registration, into their import graph.
//
// # API conventions
//
// Every operation that touches a path returns a [*PathError]
// wrapping the operation name, the path, and the underlying cause.
// [errors.Is] matches both this package's sentinels (e.g.,
// [ErrNotFound]) and the equivalent stdlib sentinels (e.g.,
// [io/fs.ErrNotExist]) on the same error. Bulk operations aggregate
// errors into a [*MultiError] whose Unwrap returns every component,
// so [errors.Is] works across the aggregate.
//
// Defaults are safe by default: writes are atomic via temp+rename;
// reads are bounded ([WithMaxSize], default 100 MiB); removal is
// idempotent ([Remove], [RemoveAll], [RemoveContents]) unless
// [WithStrict]; archive extraction confines every entry through
// [MustBeChildOf] (zip-slip / tar-slip defense).
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
//     predicates exist for ergonomics, not security; security-
//     critical code should use [OpenNoFollow] or [OpenAt] and
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
//     or Developer Mode. The package's symlink-using helpers return
//     a clear error when the privilege isn't held.
//   - v0.1 ships polling-only watching. The platform-native backends
//     (inotify on Linux, kqueue on macOS/BSD, ReadDirectoryChangesW
//     on Windows) are not yet wired up; every [Watcher] uses the
//     polling backend regardless of [WithPolling]. Default interval
//     is 1 second; pass [WithPolling] with a finer interval when
//     latency matters. The API will not change when native backends
//     land.
//   - The watcher's debouncer adds 75 ms of trailing-edge latency by
//     default ([WithDebounce]). Tests that need immediate event
//     visibility should pass WithDebounce(0).
//   - The polling backend reads file mtimes from [os.Lstat]; on
//     filesystems that round mtime to second resolution (FAT,
//     network-attached SMB), back-to-back writes within one second
//     can be missed. Until native backends land, [WithPolling] with
//     a finer interval is the only mitigation.
//   - [Hash], [HashCompare], and [HashWriter] expose MD5 and SHA-1
//     for non-security uses (legacy compat, content-addressed
//     caches); they are not secure for integrity defense against
//     attackers.
//   - [ParseBytes] uses strict SI semantics: 1KB == 1000, matching
//     kubectl / docker / kafka. IEC binary units (1KiB == 1024) keep
//     their canonical meaning. For the legacy disk-vendor idiom
//     where bare KB means 1024, use [ParseBytesIEC]. This is the
//     opposite of what some older Go libraries do, so callers must
//     choose deliberately.
//
// # Stability
//
// The public API is stable starting at v0.1.0. New features may
// arrive in minor releases; breaking changes are reserved for major
// version bumps. Items still on the roadmap for a later minor
// release: the platform-native watcher backends (inotify, kqueue,
// ReadDirectoryChangesW; today every Watcher uses the polling
// fallback) and the Tier-C items recorded in the package's design
// doc (umask helpers, xattrs, sparse/preallocate, reflink,
// MoveToTrash, bind-mount introspection). All future additions land
// in the fs package itself; the only sub-package is fstest for test
// helpers.
package fs
