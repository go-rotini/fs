# go-rotini/fs

A Go filesystem helpers package for CLIs: atomic writes, safe reads, path resolution, cross-platform user-directory lookup, file watching, archive extraction, scaffolding, advisory file locking, directory-backed caching, log rotation, tail-follow with rotation handling, versioned backups, transactional plan/apply, gitignore-aware walks, memory-mapped reads, and the dozens of small operations a CLI repeatedly needs to get right.

This package is used as the default filesystem package for [rotini](https://github.com/go-rotini/rotini).

## Features

- Atomic writes (temp file + `fsync` + rename + parent-dir `fsync`) via `WriteFile`, `WriteFileSecret`, `WriteFileExclusive`, `WriteString`, `Append`, with options `WithPerm`, `WithSync`, `WithBackup`, `WithMkdirAll`
- Bounded reads (default 100 MiB, overridable with `WithMaxSize`) via `ReadFile`, `ReadLines`, `ReadFirstLine`, `OpenLines`, `OpenChunked`, `ReadAt`
- Idempotent removal: `Remove`, `RemoveAll`, `RemoveContents`, plus symlink-safe `RemoveAllNoFollow`
- Path safety primitives: `IsSubpath`, `MustBeChildOf`, `EvalSymlinksWithin`, `SanitizeFilename`, `IsReservedName`, `LongPath`
- TOCTOU-safe open: `OpenNoFollow` (refuses final-component symlinks) and `OpenAt` (resolves through a held directory FD on POSIX)
- Cross-platform user and system directories (XDG on Linux/FreeBSD, Apple guidelines on macOS, `%APPDATA%` / `%LOCALAPPDATA%` / `%PROGRAMDATA%` on Windows): `Home`, `ConfigDir`, `CacheDir`, `DataDir`, `StateDir`, `RuntimeDir`, `ExecutableDir`, `BinaryPath`, plus `App*Dir(appName)` and `System*Dir(appName)` variants
- Path expansion: `Expand` handles `~`, `~user`, `$VAR`, `${VAR}` with opt-in strict mode
- Find-up project discovery: `FindUp`, `FindUpAll`, `ProjectRoot`, `FirstExisting`
- Directory walking: `Walk` with `WalkSkipHidden`, `WalkSkipNames`, `WalkSkipPatterns`, `WalkMaxDepth`, `WalkFollowSymlinks`, `WalkErrorHandler`, `WithWalkGitignore`; concurrent variant `WalkParallel(ctx, ...)`
- Glob: `Match`, `Glob`, `GlobAny` with path expansion before matching
- Atomic-rename-aware file watching: multi-subscriber broadcast, 75 ms trailing-edge debouncing, per-instance pluggable `slog.Logger`. v0.1 ships the polling backend; native inotify / kqueue / `ReadDirectoryChangesW` scheduled as a follow-up release
- Archive extraction and creation: tar / tar.gz / zip with path-confinement via `MustBeChildOf` (zip-slip / tar-slip defense), bounded via `WithArchiveMaxBytes` (default 10 GiB), entry-level filters
- Scaffolding from `io/fs.FS` (typically `embed.FS`): `ScaffoldApply` with `text/template`-rendered paths and contents, conflict policies, and a `ScaffoldExtract` first-run resource-extraction variant
- Advisory file locking: `Lock`, `LockShared`, `TryLock`, `LockTimeout`, `WithLock`, `IsLocked`, `PIDLock` with stale-lock reclamation and opt-in `WithPIDLockFingerprint` for recycled-PID defense; `syscall.Flock` on POSIX, `LockFileEx` on Windows
- Directory-backed cache: `*Cache` with TTL, mtime-based eviction, size-bounded LRU, content-versioned namespaces, `Get` / `GetWithError` / `Set` / `Delete` / `Purge` / `Stats` / `Entries`
- Tail-follow with rotation handling: `Tail(ctx, path) iter.Seq2[string, error]` detects rename-style rotation and in-place truncation via `os.SameFile`, reopens transparently (or opts into `ErrTailRotated` notifications)
- Log rotation: `*Rotator` (`io.WriteCloser`) with size and age triggers, retention count, optional gzip compression of rotated files
- Versioned backups: `WriteFileVersioned` / `ListVersions` / `RestoreVersion` with `WithVersionsKeep`, `WithVersionsMaxAge`, `WithVersionsPerm`, `WithVersionsMaxBytes`
- Transactional plan/apply: build a `*Plan` of `Create` / `Update` / `Delete` / `Rename` ops, preview with `Diff`, execute via `Apply` against an on-disk journal that supports `Resume` and `Rollback`
- Gitignore parser: `*Gitignore` with negation, anchoring, `**` recursive, and per-segment globs; integrates with `Walk` via `WithWalkGitignore`
- Memory-mapped reads: `Mmap` returns a `*Mapping` (`syscall.Mmap(PROT_READ, MAP_SHARED)` on POSIX, `CreateFileMapping` + `MapViewOfFile` on Windows)
- Content search: `FindByContent` / `FindByContentRegex` with binary-file skipping, configurable per-file size cap via `WithFindByContentMaxSize`
- Disk-info: `DiskUsage`, `DiskUsageOf`, `MountPoint`, `FilesystemType`, `IsNetworkFS`, `IsCaseInsensitiveFS`, `PreflightSpace`
- Hashing: streaming `Hash`, constant-time `HashCompare`, `HashWriter` for `io.MultiWriter` use; SHA-256 default, plus SHA-512 / SHA-1 / MD5 (the last two for non-security uses)
- Bytes formatting: strict-SI `ParseBytes` (`1KB == 1000`, matching kubectl / docker) plus IEC-binary `ParseBytesIEC` (`1KB == 1024`); `FormatBytes`
- Project-kind detection: `ProjectType` recognizes Go, Node, Rust, Python, Ruby, Java, .NET, PHP, Make, Docker via marker files; extensible via `RegisterProjectKind`
- Multi-root workspace discovery: `WorkspaceRoots` parses `go.work`, `package.json` workspaces (array + object forms), `pnpm-workspace.yaml`
- Stdio: `ReadStdin`, `OpenStdinLines`, `WriteStdout` / `WriteStderr` (translates `EPIPE` -> `ErrBrokenPipe`), `IsTerminal`
- Test helpers in `fs/fstest`: `NewTestHarness(t)`, `MockFS`, `WithTempEnv`, `TempFileT`, `TempDirT`. Importing `github.com/go-rotini/fs` does **not** pull stdlib's `testing` package into your binary
- Bi-directional sentinel matching: `errors.Is(err, fs.ErrNotFound)` matches both the package sentinel AND stdlib's `io/fs.ErrNotExist`; `*MultiError` with Go 1.20 `Unwrap() []error`
- Pluggable `slog.Logger` for `Apply` / `Cache` / `Rotator` debug output via package-level `SetLogger`
- DoS protection: bounded reads by default, archive extraction size cap, plan/apply journal size split, content-search size cap, gitignore + workspace parser size limits
- Zero non-stdlib runtime dependencies; cross-platform on Linux, macOS, Windows, FreeBSD

## Installation

```bash
go get github.com/go-rotini/fs
```

Requires Go 1.26 or later.

## Quick Start

```go
package main

import (
	"fmt"
	"log"

	"github.com/go-rotini/fs"
)

func main() {
	dir, cleanup, err := fs.TempDir("", "fs-quickstart-*")
	if err != nil {
		log.Fatal(err)
	}
	defer func() { _ = cleanup() }()

	cfg := dir + "/config.toml"

	// Atomic write: temp + fsync + rename + parent-dir fsync.
	if err := fs.WriteFile(cfg, []byte("[server]\nport = 8080\n")); err != nil {
		log.Fatal(err)
	}

	// Bounded read (default cap 100 MiB; override with WithMaxSize).
	data, err := fs.ReadFile(cfg)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(string(data))

	// Coordinate with peer processes via an advisory file lock.
	if err := fs.WithLock(dir+"/work.lock", func() error {
		// Critical section.
		return nil
	}); err != nil {
		log.Fatal(err)
	}

	// Discover the project root from any subdirectory.
	if root, err := fs.ProjectRoot(dir); err == nil {
		fmt.Println("project root:", root)
	}
}
```

For follow-style log reading with rotation handling, see [`ExampleTail`](https://pkg.go.dev/github.com/go-rotini/fs#example-Tail).

## Documentation

Full API reference is available on [pkg.go.dev](https://pkg.go.dev/github.com/go-rotini/fs).

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md) for guidelines on how to contribute to this project.

## Code of Conduct

This project follows a code of conduct to ensure a welcoming community. See [CODE_OF_CONDUCT.md](CODE_OF_CONDUCT.md).

## Security

To report a vulnerability, see [SECURITY.md](SECURITY.md).

## License

This project is licensed under the MIT License. See [LICENSE](LICENSE) for details.
