# Changelog

All notable changes to `github.com/go-rotini/fs` are documented here.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [0.1.0] — 2026-05-10

Initial public release. Feature-complete for the v0.1 surface across
35 implementation phases. Zero non-stdlib runtime dependencies; cross-
platform on Linux, macOS, Windows, and FreeBSD.

### Added

**Core I/O & paths**

- Atomic write surface: `WriteFile`, `WriteFileSecret`, `WriteFileEnsure`,
  `WriteFileExclusive`, `WriteString`, `WriteAt`, `OpenWrite`, `Append`,
  with options `WithPerm`, `WithSync`, `WithBackup`, `WithMkdirAll`,
  `WithTempPattern`. Atomic via temp-file + fsync + rename + parent-dir
  fsync.
- Bounded read surface: `ReadFile`, `ReadFileMax`, `ReadLines`,
  `ReadFirstLine`, `OpenLines`, `OpenChunked`, `ReadAt`, with
  `WithMaxSize` (default 100 MiB) and BOM handling.
- Remove operations: `Remove`, `RemoveAll`, `RemoveContents` —
  idempotent by default, `WithStrict` to surface ErrNotFound.
- Path utilities: `Abs`, `Rel`, `Dir`, `Base`, `Ext`, `Stem`, `IsAbs`,
  `Expand` (`~` / `$VAR` / `${VAR}`), `EqualPath`, `ToSlash`,
  `FromSlash`, `IsSubpath`, `MustBeChildOf`, `EvalSymlinksWithin`.
- Predicates: `Exists`, `IsFile`, `IsDir`, `IsSymlink`, `IsExecutable`,
  `IsReadable`, `IsWritable`, `IsEmpty`, `SameFile`.
- Directory operations: `MkdirAll` (with `WithEnforcePerm`), `Mkdir`,
  `ListDir`, `DirSize`, `Cwd`, `Chdir`, `WithDir`.
- Copy / move: `CopyFile`, `CopyDir`, `Move`, `Rename`. `Move` falls
  back to copy+remove across devices; `Rename` is strict.
- Discovery: `FindUp`, `FindUpAll`, `ProjectRoot`, `FirstExisting`,
  `Find`, `FindByRegex`, `FindFunc`.
- Walk: `Walk` with `WalkSkipHidden`, `WalkSkipNames`, `WalkSkipPatterns`,
  `WalkMaxDepth`, `WalkFollowSymlinks`, `WalkErrorHandler`.
- Glob: `Match`, `Glob`, `GlobAny`.
- Temp helpers: `TempFile`, `TempDir`, `TempFileT`, `TempDirT`.

**Platform & system**

- User/system directories: `Home`, `ConfigDir`, `CacheDir`, `DataDir`,
  `StateDir`, `RuntimeDir`, `SystemTempDir`, `ExecutableDir`,
  `BinaryPath`, plus `App*Dir(appName)` / `System*Dir(appName)`
  variants. XDG on Linux/FreeBSD, Apple guidelines on macOS,
  `%APPDATA%` / `%LOCALAPPDATA%` / `%PROGRAMDATA%` on Windows.
- Hashing: `Hash`, `HashCompare` (constant-time), `HashWriter`
  (streaming), with `HashSHA256` (default), `HashSHA512`, `HashSHA1`,
  `HashMD5`.
- Bytes/mode helpers: `FormatBytes` (IEC), `ParseBytes` (strict SI by
  default — `KB=1000` matching kubectl/docker), `ParseBytesIEC`
  (legacy disk-vendor 1024-base), `Chmod`, `EnsurePerm`,
  `WarnInsecurePerm`.
- Stdio: `ReadStdin`, `OpenStdinLines`, `WriteStdout`, `WriteStderr`
  (translates `EPIPE` → `ErrBrokenPipe`), `IsTerminal`.
- Links & portability: `Symlink` (idempotent), `ReadLink`,
  `EvalSymlinks`, `Hardlink` (idempotent), `SanitizeFilename`,
  `IsReservedName`, `LongPath`.
- TOCTOU-safe open: `OpenNoFollow`, `OpenAt`.
- Locate-or-create + introspection: `EnsureFile`, `EnsureDir`, `Magic`,
  `ExtFormat`.

**Subsystems (Phases 21–27)**

- Watcher: `*Watcher`, `NewWatcher`, `NewLazyWatcher`, `NewDirWatcher`,
  `Subscribe`, `Close`, `WatchEvent`, `WatchOp` (`WatchCreate` /
  `WatchWrite` / `WatchRemove` / `WatchRename` / `WatchChmod`).
  Multi-subscriber broadcast with non-blocking fan-out; 75 ms
  trailing-edge debouncing. Polling backend wired end-to-end on every
  platform; native backends (inotify / kqueue / ReadDirectoryChangesW)
  are placeholders that gracefully fall back to polling, scheduled for
  a follow-up release.
- Disk-info: `DiskUsage`, `DiskUsageOf`, `MountPoint`, `FilesystemType`,
  `IsNetworkFS`, `IsCaseInsensitiveFS`, `PreflightSpace`,
  `ErrInsufficientSpace`. Platform-specific via `statfs` / `statvfs` /
  `GetDiskFreeSpaceExW` + `GetVolumeInformation`.
- Archive: `ArchiveFormat`, `ArchiveHeader`, `ExtractArchive`,
  `ExtractArchiveFile`, `CreateArchive`, `CreateArchiveFile`,
  `OpenAutoArchive`, with auto-format-detection and path-confinement
  via `MustBeChildOf` (zip-slip / tar-slip defense). Capped via
  `WithArchiveMaxBytes` (default 10 GiB).
- Scaffold: `ScaffoldApply`, `ScaffoldPlan`, `ScaffoldExtract`,
  `ScaffoldAction`, `ScaffoldActionOp`, `ScaffoldOnConflict`
  (`ScaffoldSkipExisting` / `ScaffoldOverwriteAll` /
  `ScaffoldPromptInteractive` / `ScaffoldMergeWithUserEdits`).
  Uses stdlib `text/template` with `missingkey=error`.
- Test harness sub-package `fs/fstest`: `NewTestHarness`, `MockFS`,
  `WithTempEnv`, `TempFileT`, `TempDirT`. Keeps `testing` out of
  production binaries.
- Verification: conformance, acceptance, and fuzz test suites with
  separate Makefile targets.

**v0.1 expanded (Phases 28–35)**

- Advisory file locking: `Lock`, `LockShared`, `TryLock`, `LockTimeout`,
  `WithLock`, `IsLocked`, `PIDLock`, `*LockHandle` (`Release`, `PID`),
  `ErrLockTimeout`, `ErrStaleLock`. Uses `syscall.Flock` on POSIX and
  `LockFileEx` via `kernel32.dll` on Windows.
- Directory-backed caching: `*Cache`, `NewCache`, `CacheStats`,
  `ErrCacheClosed`, options `WithCacheTTL`, `WithCacheMaxBytes`,
  `WithCacheVersion`, `WithCacheClock`. SHA-256 key sharding;
  mtime-based TTL; LRU eviction on Set when bytes exceed cap.
- Tail-follow with rotation: `Tail(ctx, path, opts...) iter.Seq2[string, error]`,
  `WithTailFromStart`, `WithTailPollInterval`, `WithTailBufferSize`.
  Detects rename-rotation via `os.SameFile` and in-place truncation;
  reopens at offset 0 in either case.
- Log rotation: `*Rotator` (`io.WriteCloser`), `NewRotator`, `Rotate`,
  options `WithRotateMaxBytes`, `WithRotateMaxAge`, `WithRotateKeep`,
  `WithRotateCompress`, `WithRotateClock`, `ErrRotatorClosed`.
  Rotated files get sortable UTC-timestamp suffix; gzip compression
  optional.
- Versioned backups: `WriteFileVersioned`, `ListVersions`,
  `RestoreVersion`, `VersionInfo`, options `WithVersionsKeep`,
  `WithVersionsMaxAge`, `WithVersionsPerm`, `WithVersionsClock`.
- Transactional plan/apply: `*Plan`, `NewPlan`, `Create`/`Update`/
  `Delete`/`Rename` builders, `Diff`, `Apply`, `Resume`, `Rollback`,
  `PlanOp`, `PlanAction` (`PlanActionCreate` / `PlanActionUpdate` /
  `PlanActionDelete` / `PlanActionRename`), `WithApplyNoMkdir`.
  On-disk journal supports resume and rollback.
- Gitignore parser: `*Gitignore`, `NewGitignore`, `LoadGitignore`,
  `Match`, plus `WithWalkGitignore` walk option. Handles negation
  (`!`), directory-only (`/`-trailing), anchoring (`/`-leading),
  recursive `**`, and per-segment glob metacharacters.
- Memory-mapped reads: `Mmap`, `*Mapping` (`Data`, `Len`, `Close`).
  `syscall.Mmap(PROT_READ, MAP_SHARED)` on POSIX;
  `CreateFileMapping` + `MapViewOfFile` on Windows.
- Content search: `FindByContent`, `FindByContentRegex`, `ContentMatch`.
  Line-oriented; binary files (NUL byte) skipped; 100 MiB per-file
  cap.
- Recursive `ChownRecursive` — POSIX-only via `os.Lchown`; Windows
  returns `ErrNotSupported`.
- Symlink-safe `RemoveAllNoFollow` — refuses to traverse symlinks
  during recursive removal.
- Concurrent walk: `WalkParallel`, `WalkParallelFunc` — worker-pool
  walker with first-error cancellation.
- Project-kind detection: `ProjectType`, `ProjectKind` (Go, Node,
  Rust, Python, Ruby, Java, .NET, PHP, Make, Docker).
- Multi-root workspace discovery: `WorkspaceRoots`, `WorkspaceRoot`
  — parses `go.work`, `package.json` workspaces (array + object
  forms), and `pnpm-workspace.yaml`.

**Errors model**

- `*PathError` wrapping every IO path with `Op` / `Path` / `Cause`.
- `*MultiError` aggregating bulk-operation errors with Go 1.20
  `Unwrap() []error`.
- Bi-directional sentinel matching: `errors.Is(err, fs.ErrNotFound)`
  matches both the package sentinel and stdlib's `io/fs.ErrNotExist`.
- Sentinels: `ErrNotFound`, `ErrAlreadyExists`, `ErrPermission`,
  `ErrNotDir`, `ErrIsDir`, `ErrCrossDevice`, `ErrFileTooLarge`,
  `ErrEmptyFile`, `ErrEscapesRoot`, `ErrSymlinkLoop`, `ErrInvalidPath`,
  `ErrHashMismatch`, `ErrNotEmpty`, `ErrShortRead`, `ErrBrokenPipe`,
  `ErrNotSupported`, `ErrInvalidByteSize`, `ErrInsufficientSpace`,
  `ErrArchiveTooLarge`, `ErrArchiveFormatUnknown`,
  `ErrScaffoldPromptRequired`, `ErrScaffoldMergeUnsupported`,
  `ErrScaffoldPromptUnsupported`, `ErrScaffoldUnresolvedConflict`,
  `ErrLockTimeout`, `ErrStaleLock`, `ErrCacheClosed`, `ErrRotatorClosed`.
- `FormatError(err, color...)` for CLI-friendly rendering.

### Documentation

- Package-level `doc.go` with a Pitfalls section covering atomic-write
  traps, TOCTOU races, watcher debounce latency, polling-mtime
  resolution, `SanitizeFilename` reserved-name handling, archive
  extraction confinement, macOS `/var` symlink resolution, MD5/SHA-1
  non-security usage, and the `ParseBytes` strict-SI default.
- Per-feature-area quickstart in `README.md`.
- 38 runnable `Example*` functions covering every major feature.

### Conventions

- Every IO operation returns `*PathError` with op/path/cause.
- Reads are bounded by default (100 MiB; override via `WithMaxSize`).
- Writes are atomic via temp+rename.
- Removes are idempotent by default.
- Cross-platform syscall code is split into `*_unix.go` / `*_darwin.go`
  / `*_linux.go` / `*_freebsd.go` / `*_windows.go` files with explicit
  `//go:build` tags.

### Known limitations

- Watcher's platform-native event delivery (inotify / kqueue /
  `ReadDirectoryChangesW`) is a planned follow-up. Every Watcher uses
  the polling fallback today.
- Tier-B helpers deliberately deferred: umask helpers, xattrs,
  sparse/preallocate helpers, reflink, `MoveToTrash`, bind-mount /
  overlay introspection.
- Out of scope (zero-non-stdlib-runtime-deps mandate): `.zst` / `.xz` /
  `.lz4` codecs, POSIX ACLs, `O_DIRECT` helper.

### Compatibility

- Minimum Go version: **1.26.2**.
- Cross-platform: Linux, macOS, Windows, FreeBSD.
- Runtime dependencies: **none** outside stdlib.

[Unreleased]: https://github.com/go-rotini/fs/compare/v0.1.0...HEAD
[0.1.0]: https://github.com/go-rotini/fs/releases/tag/v0.1.0
