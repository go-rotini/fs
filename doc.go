// Package fs is a CLI-focused filesystem helpers library: atomic writes,
// safe reads, path resolution, cross-platform user-directory lookup,
// directory walking, file watching, archive extraction, scaffolding,
// disk-info, and the dozens of small operations a CLI tool repeatedly
// needs to get right.
//
// The package is a layer on top of stdlib's [os], [io/fs], and
// [path/filepath] that turns the most common CLI operations into
// one-line calls with safe defaults and useful errors. It is NOT a
// virtual filesystem abstraction (see afero / go-billy); every
// operation talks to the real filesystem. For tests against a fixture
// without disk I/O, use the helpers in
// [github.com/go-rotini/fs/fstest]: `fstest.MockFS` (read paths) or
// `fstest.NewTestHarness` (write paths in a [testing.T.TempDir]).
//
// # Zero third-party dependencies
//
// The fs package is a single flat root for production code — watcher,
// lock, archive, scaffold, and disk-info helpers all live in the same
// package. The whole production surface has zero non-stdlib runtime
// imports AND does not import [testing]. Platform-native APIs
// (inotify on Linux, kqueue on macOS/BSD, ReadDirectoryChangesW on
// Windows; flock / LockFileEx for the post-v0.1 lock helpers) are
// accessed directly through stdlib's [syscall] package.
//
// The one sub-package is [github.com/go-rotini/fs/fstest], which
// holds the test helpers (`TestHarness`, `MockFS`, `WithTempEnv`,
// `TempFileT`, `TempDirT`). Keeping those out of the main package is
// what lets production binaries avoid pulling stdlib's [testing]
// package — and its global flag registration — into their import
// graph.
//
// # API conventions
//
// Every operation that touches a path returns a [*PathError] wrapping
// the operation name, the path, and the underlying cause; [errors.Is]
// matches both this package's sentinels (e.g., [ErrNotFound]) and the
// equivalent stdlib sentinels (e.g., [io/fs.ErrNotExist]) on the same
// error. Bulk operations aggregate errors into a [*MultiError] whose
// Unwrap returns every component, so [errors.Is] works across the
// aggregate.
//
// Defaults are safe-by-default: writes are atomic via temp+rename;
// reads are bounded ([WithMaxSize], default 100 MiB); removal is
// idempotent ([Remove], [RemoveAll], [RemoveContents]) unless
// [WithStrict]; archive extraction confines every entry through
// [MustBeChildOf] (zip-slip / tar-slip defense).
//
// # Pitfalls
//
// These are the issues the package documents in case you've never run
// into them — every one is a lesson learned the hard way:
//
//   - Atomic writes require the temp file to live in the destination's
//     parent directory. The package's [WriteFile] handles this; if you
//     write your own temp+rename you must do the same — cross-directory
//     rename is sometimes a copy + delete, not atomic.
//   - "TOCTOU" (time-of-check to time-of-use) races are real. A
//     `Exists(path) && ReadFile(path)` sequence is racy. The package's
//     predicates exist for ergonomics, not security; security-critical
//     code should use [OpenNoFollow] / [OpenAt] and read-and-handle-error.
//   - [Exists] returns false on permission errors. This is deliberate —
//     callers who need to distinguish use [Stat] directly. Document
//     loudly; do not write `if !Exists { create }` in privileged
//     contexts.
//   - The watcher watches a file's PARENT directory (with basename
//     filtering) so editor atomic-save patterns are observed correctly.
//     Subscribing to a non-existent path requires [NewLazyWatcher].
//   - [WithMaxSize] defends against /dev/zero-class reads. Default 100
//     MiB; callers reading attacker-controlled paths MUST keep it set.
//   - [SanitizeFilename] inserts `_` BEFORE the extension when the stem
//     matches a Windows reserved name: `CON.txt` becomes `CON_.txt`.
//     Suffixing the whole filename (`CON.txt_`) leaves Windows still
//     treating the file as the CON device.
//   - Archive extraction (zip/tar) MUST use the package's
//     [ExtractArchive] — every entry passes through [MustBeChildOf]
//     before any filesystem write. Hand-rolled extraction code is the
//     classic zip-slip vulnerability.
//   - macOS [path/filepath.EvalSymlinks] resolves /var to /private/var
//     and similar symlinks. Tests that compare paths from
//     [testing.T.TempDir] against `/var/folders/...` should resolve via
//     [path/filepath.EvalSymlinks] on both sides before comparing.
//   - On Windows, symlink creation typically requires Administrator or
//     Developer Mode. The package's symlink-using helpers return a
//     clear error when the privilege isn't held.
//   - The watcher's debouncer adds 75 ms of trailing-edge latency by
//     default ([WithDebounce]). Tests that need immediate event
//     visibility should pass `WithDebounce(0)`.
//   - The polling backend reads file mtimes from [os.Lstat]; on
//     filesystems that round mtime to second-resolution (FAT,
//     network-attached SMB), back-to-back writes within one second can
//     be missed. Use the platform-native backend (when fully wired) or
//     [WithPolling]-with-a-finer-interval where this matters.
//   - [Hash], [HashCompare], and [HashWriter] expose MD5 and SHA-1 for
//     non-security uses (legacy compat, content-addressed caches);
//     they are NOT secure for integrity defense against attackers.
//
// # Stability
//
// The public API is stable starting at v0.1.0. New features may arrive
// in minor releases; breaking changes are reserved for major version
// bumps. The post-v0.1 roadmap includes lock helpers, caching helpers,
// tail-follow, log rotation, versioned backups, transactional
// plan/apply, and a .gitignore parser — all in the fs package, not as
// sub-packages.
package fs
