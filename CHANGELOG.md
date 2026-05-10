# Changelog

All notable changes to this project will be documented in this file.

The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and the project uses [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [0.1.0] — 2026-05-10

Initial public release. The full v0.1.0 surface is summarized below by
feature area; see `.docs/FS_PACKAGE_REQUIREMENTS.md` for the
phase-by-phase implementation log.

### Added — Errors

- `*PathError{Op, Path, Cause}` envelope on every operation; `errors.Is`
  matches both this package's sentinels and the equivalent stdlib
  sentinels (`io/fs.ErrNotExist`, `ErrPermission`, `ErrExist`) on the
  same error.
- `*MultiError` for bulk operations; `Unwrap() []error` makes
  `errors.Is` work across the aggregate.
- Sentinels: `ErrNotFound`, `ErrAlreadyExists`, `ErrPermission`,
  `ErrNotDir`, `ErrIsDir`, `ErrCrossDevice`, `ErrFileTooLarge`,
  `ErrEmptyFile`, `ErrEscapesRoot`, `ErrSymlinkLoop`, `ErrInvalidPath`,
  `ErrHashMismatch`, `ErrNotEmpty`, `ErrShortRead`, `ErrBrokenPipe`,
  `ErrNotSupported`, `ErrInvalidByteSize`, `ErrInsufficientSpace`,
  `ErrArchiveTooLarge`, `ErrArchiveFormatUnknown`,
  `ErrScaffoldPromptRequired`, `ErrScaffoldMergeUnsupported`,
  `ErrScaffoldPromptUnsupported`, `ErrScaffoldUnresolvedConflict`,
  `ErrWatcherEmptyPath`, `ErrWatcherNilContext`, `ErrWatcherClosed`,
  `ErrWatcherUnsupportedOS`. `FormatError(err, color...)` for
  human-readable rendering.

### Added — Path

- `Expand`, `Abs`, `Clean`, `Join`, `JoinSlash`, `Dir`, `Base`, `Ext`,
  `Stem`, `Split`, `IsAbs`, `Rel`, `ToSlash`, `FromSlash`, `EqualPath`.
- Safety: `IsSubpath`, `MustBeChildOf`, `EvalSymlinksWithin`.
- `WithStrictExpansion` errors on unset `$VAR`.

### Added — Predicates and stat

- `Exists`, `IsFile`, `IsDir`, `IsSymlink`, `IsFIFO`, `IsSocket`,
  `IsCharDevice`, `IsBlockDevice` (cross-platform).
- `IsExecutable`, `IsReadable`, `IsWritable` (per-platform: POSIX uses
  `syscall.Access`; Windows uses PATHEXT and the read-only attribute).
- `Stat`, `Lstat`, `Mtime`, `Atime`, `Ctime`, `BTime` (`ErrNotSupported`
  on Linux without statx), `Owner` (`ErrNotSupported` on Windows),
  `SetMtime`, `SetAtime`, `SetTimes`, `Touch` (with `WithTimes`,
  `WithTouchPerm`), `SameFile`, `SameDevice`.

### Added — Read

- `ReadFile`, `ReadFileMax`, `ReadLines`, `ReadFirstLine`, `OpenLines`
  (`iter.Seq[string]`), `OpenChunked` (`iter.Seq2[[]byte, error]`),
  `ReadAt`.
- `WithMaxSize` (default 100 MiB), `WithExpand`, `WithAllowShort`.

### Added — Write (atomic by default)

- `WriteFile`, `WriteString`, `WriteFileSecret` (mode 0o600),
  `WriteFileEnsure` (auto-`MkdirAll`), `WriteFileExclusive` (`O_EXCL`),
  `WriteAt`, `OpenWrite` (streaming + idempotent finalize via
  `sync.Once`), `Append`, `AppendString` (with `WithLocked` for `flock`
  on POSIX, kernel-serialized `O_APPEND` on Windows).
- `WithPerm`, `WithDirPerm`, `WithMkdirAll`, `WithSync` (defaults: on
  for overwrites, off for new files), `WithAtomic`, `WithTempPattern`,
  `WithBackup(suffix)` (default `.bak`).

### Added — Stdio

- `ReadStdin` (uses `WithMaxSize`), `OpenStdinLines`, `WriteStdout`,
  `WriteStderr` (translate `syscall.EPIPE` to `ErrBrokenPipe`),
  `IsTerminal` (per-platform: termios ioctl on POSIX,
  `GetConsoleMode` on Windows).

### Added — Remove

- `Remove`, `RemoveAll`, `RemoveContents` (idempotent by default;
  `WithStrict` for stdlib-mirroring missing-target errors).
- `RemoveContents` aggregates per-entry failures into a `*MultiError`.

### Added — Copy / Move

- `CopyFile` (atomic via temp+rename, mode + mtime preserved by
  default, symlinks recreated as symlinks unless `WithFollowSymlinks`),
  `CopyDir` (recursive, multi-error aggregation, `WithFilter` prunes
  subtrees via `filepath.SkipDir`).
- `Move` (rename → copy+remove on EXDEV; cross-platform EXDEV detection
  including Windows `ERROR_NOT_SAME_DEVICE`).
- `Rename` (strict; surfaces EXDEV without falling back).

### Added — Directory

- `Mkdir`, `MkdirAll` (with `WithEnforcePerm` chmod'ing only the
  components THIS call created — pre-existing parents untouched),
  `ListDir` (`WithSkipHidden`, `WithSorted`, `WithListFilter`),
  `IsEmpty` (rejects non-dirs with `ErrNotDir`), `DirSize` (honors
  `context.Context`).
- `Cwd`, `Chdir`, `WithDir(path, fn)` — defer-based cwd restoration
  runs even when `fn` panics.

### Added — Discovery

- `FindUp`, `FindUpAll`, `ProjectRoot` (default markers `.git` /
  `go.mod` / `package.json` / `Cargo.toml`); `WithMaxAncestors`
  (default 32), `WithProjectMarkers`, `WithStopAt`.
- `ProjectRoot` is per-process memoized (`sync.Map` keyed on
  `abs(startDir) | sortedMarkers | stopAt | maxAncestors`).
- `FirstExisting([]string)`.
- `Find`, `FindByRegex`, `FindFunc` accept `WalkOption` and route
  through `Walk`.

### Added — Walk

- `Walk(root, fn, opts...)` honoring `filepath.SkipDir` and
  `filepath.SkipAll`. Default path uses `filepath.WalkDir`; symlink
  following uses a custom recursive walker with
  `filepath.EvalSymlinks` real-path tracking for loop detection.
- `WalkSkipHidden`, `WithSkipNames`, `WithSkipPatterns`, `WithMaxDepth`,
  `WalkFollowSymlinks`, `WithErrorHandler`. (Two `Walk*`-prefixed
  options resolve naming collisions with earlier `WithSkipHidden` /
  `WithFollowSymlinks`.)
- Hidden detection per-platform: POSIX = dot-prefix; Windows =
  dot-prefix OR `FILE_ATTRIBUTE_HIDDEN`.

### Added — Glob

- `Match` (wraps `filepath.Match`), `Glob` (Expand-then-`filepath.Glob`,
  honors `WithStrictExpansion`), `GlobAny` (deduplicated union,
  first-occurrence order across patterns).

### Added — Temp

- `TempFile`, `TempDir` (idempotent cleanup via `sync.Once`),
  `TempFileT(t)`, `TempDirT(t)` (cleanup auto-registered via
  `t.Cleanup`).

### Added — User and system directories

- `Home`, `ConfigDir`, `CacheDir`, `DataDir`, `StateDir`, `RuntimeDir`
  (XDG no-fallback on POSIX), `SystemTempDir`, `ExecutableDir`,
  `BinaryPath` (symlink-resolved + absolute).
- `App*Dir(appName)` and `System*Dir(appName)` variants for both
  per-user and system-wide dirs.
- `validateAppName` rejects empty / `.` / `..` / path-separator /
  NUL-byte names with `ErrInvalidPath`.

### Added — Hashing

- `HashAlgo` (`HashSHA256` default, `HashSHA512`, `HashSHA1`, `HashMD5`),
  `Hash`, `HashCompare` (constant-time via `crypto/subtle`),
  `HashWriter` (folds hashing into copies via `io.MultiWriter`).
- MD5 / SHA-1 exposed for non-security uses (legacy compat,
  content-addressed caches).

### Added — Mode helpers

- Mode preset constants `Mode0644`, `Mode0640`, `Mode0600`, `Mode0755`,
  `Mode0750`, `Mode0700`.
- `Chmod`, `EnsurePerm` (no-op when perms already match),
  `WarnInsecurePerm`.

### Added — Bytes formatting

- `FormatBytes` (IEC), `ParseBytes` (lenient: KB/MB/GB = 1024-based),
  `ParseBytesStrict` (true SI: KB = 1000).

### Added — Encoding

- `LineEnding` enum, `StripUTF8BOM`, `DetectLineEnding`,
  `NormalizeLineEndings`.

### Added — Links + sanitization

- `Symlink` (idempotent on same target; `ErrAlreadyExists` on
  conflict), `ReadLink`, `EvalSymlinks` (loop detection via substring
  match on filepath's "too many links" sentinel + `syscall.ELOOP`),
  `Hardlink` (idempotent via `SameFile`; EXDEV → `ErrCrossDevice`).
- `SanitizeFilename` (strips ASCII controls + Windows-illegal chars +
  trailing dots/spaces; reserved-stem rewrite inserts `_` BEFORE the
  extension so `CON.txt` becomes `CON_.txt` rather than the still-
  reserved `CON.txt_`).
- `IsReservedName`, `LongPath` (per-platform; POSIX pass-through;
  Windows prefixes with `\\?\` or `\\?\UNC\` only when ≥ MAX_PATH).

### Added — TOCTOU-safe open

- `OpenNoFollow` (POSIX `O_NOFOLLOW`; Windows
  `FILE_FLAG_OPEN_REPARSE_POINT` via `syscall.CreateFile`).
- `OpenAt` (Linux uses public `syscall.Openat`; darwin and freebsd
  call `syscall.Syscall6` with hardcoded `SYS_OPENAT` numbers — 463
  and 499 respectively — because their stdlib syscall packages don't
  surface the constant; Windows is `filepath.Join` + `os.OpenFile`
  fallback documented as not race-safe).

### Added — Locate-or-create + introspection

- `EnsureFile` (delegates to `WriteFileExclusive`; concurrent callers
  race-safely via `O_EXCL`), `EnsureDir` (accepts `MkdirOption`).
- `Magic(path, n)` (first n bytes; short-file no-padding,
  n ≤ 0 short-circuits without opening), `ExtFormat` (small canonical
  extension table).

### Added — Watcher

- `*Watcher` with `NewWatcher` / `NewLazyWatcher` / `NewDirWatcher`,
  `Subscribe(ctx)`, idempotent `Close`.
- `WatchEvent` / `WatchOp` (`WatchCreate` / `WatchWrite` /
  `WatchRemove` / `WatchRename` / `WatchChmod`).
- Polling backend wired through end-to-end. Platform-native backends
  (inotify / kqueue / `ReadDirectoryChangesW`) are placeholders that
  return `errWatcherUnsupportedBackend` so the cross-platform shell
  falls back to polling — full implementations scheduled as a
  follow-up; behavior is correct on every platform today.
- 75 ms trailing-edge debouncer (`WithDebounce`); multi-subscriber
  fan-out drops events on slow subscribers rather than stalling.
- File watchers register BOTH the parent directory and the target
  itself so atomic-rename saves are observable via mtime diff.

### Added — Disk-info

- `DiskUsage` struct, `DiskUsageOf`, `MountPoint`, `FilesystemType`,
  `IsNetworkFS` (covers nfs / cifs / smb / fuse and variants),
  `IsCaseInsensitiveFS` (probe-based), `PreflightSpace`.
- Per-platform: linux uses `syscall.Statfs` + `/proc/self/mountinfo`;
  darwin/freebsd use `syscall.Statfs` (Mntonname / Fstypename in
  `Statfs_t`); Windows uses `GetDiskFreeSpaceExW` /
  `GetVolumePathName` / `GetVolumeInformation` via lazy-loaded
  `kernel32.dll` procs.

### Added — Archive

- `ArchiveFormat` (Tar / TarGz / Zip / Unknown), `ArchiveHeader`,
  `ExtractArchive` / `ExtractArchiveFile` / `CreateArchive` /
  `CreateArchiveFile` / `OpenAutoArchive`.
- Auto-format detection via leading-byte sniff (gzip 1F 8B, zip 50 4B
  03 04, tar "ustar" at offset 257).
- **Path confinement**: every entry resolves through `MustBeChildOf`
  before any filesystem write — defends against zip-slip / tar-slip.
- Symlink targets in tar entries are validated against the dst
  boundary too.
- `WithPreserveMode`, `WithArchiveFilter` (extract),
  `WithArchiveCreateFilter` (create), `WithArchiveMaxBytes` (default
  10 GiB cap with mid-stream `ErrArchiveTooLarge`),
  `WithArchiveFormat`.

### Added — Scaffold

- `ScaffoldApply`, `ScaffoldPlan`, `ScaffoldExtract`, `ScaffoldAction`,
  `ScaffoldActionOp`, `ScaffoldOnConflict`.
- Templates use stdlib `text/template` with `Option("missingkey=error")`
  — typos abort rather than silently inserting `<no value>`.
- Path templates: a source named `src/{{.Name}}.go` becomes
  `src/myapp.go`.
- Conflict policies: `ScaffoldSkipExisting` (default — re-runs are
  no-ops), `ScaffoldOverwriteAll`, `ScaffoldPromptInteractive` (calls
  `WithScaffoldPromptFunc`), `ScaffoldMergeWithUserEdits` (post-v0.1).
- `ScaffoldExtract` is non-templated and tracks a SHA-256 marker file
  (`.scaffold-version` by default, override via
  `WithScaffoldVersionMarker`) — re-extracts only on hash change,
  preserving user edits when the source is unchanged.

### Added — Test harness

- `*TestHarness`, `NewTestHarness(t)`, `Path` (escape-safe — leading
  `/` stripped), `Read`, `Write`, `WriteString`, `Mkdir`, `Symlink`,
  `Remove`, `Snapshot` (deterministic golden-file output: sorted
  paths, `DIR  <p>` / `FILE <p> mode=<m> size=<s> <c>` /
  `LINK <p> -> <t>`).
- `MockFS(map[string]string)` returning `io/fs.FS` (wraps stdlib
  `testing/fstest.MapFS`).
- `WithTempEnv(t)` snapshots `os.Environ` and restores via
  `t.Cleanup`.

### Verification

- 469 tests pass on macOS across unit, conformance (`TestConformance*`),
  and acceptance (`TestAcceptance*`) suites; 6 fuzz targets seeded
  with representative inputs.
- Cross-builds clean on linux / windows / freebsd (and freebsd/386).
- `make lint` clean (golangci-lint with the configured ruleset);
  `make deps-check` enforces zero non-stdlib runtime imports;
  `govulncheck` clean.

### Known limitations

- Watcher's platform-native backends (inotify, kqueue,
  `ReadDirectoryChangesW`) return `errWatcherUnsupportedBackend`
  and gracefully fall back to polling. Full kernel-API
  implementations are scheduled for a follow-up release.
- Lock helpers (`Lock`, `LockShared`, `TryLock`, `LockTimeout`,
  `WithLock`, `IsLocked`, `PIDLock`, `*LockHandle`) are designed and
  documented but not yet implemented.

## [Unreleased]

### Planned (post-v0.1, all in the fs package — no sub-packages)

- Lock helpers: `flock` (POSIX) / `LockFileEx` (Windows) advisory
  file locking; PIDLock with stale-lock reclamation.
- Caching helpers: namespaced caches with TTL invalidation and
  LRU / size-bounded eviction; cache-key derivation built on `Hash`.
- Tail-follow with rotation handling.
- Log rotation (size + time, optional gzip).
- Versioned backups with retention.
- Transactional plan/apply with journaling and rollback.
- `.gitignore` parser (the v0.1 `WithSkipPatterns` covers the manual
  case).
- Watcher platform-native backends (inotify / kqueue /
  `ReadDirectoryChangesW`).
