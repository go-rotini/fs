# go-rotini/fs

A pure-Go filesystem helpers package for CLIs: atomic writes, safe reads,
path resolution, cross-platform user-directory lookup, file watching,
directory walking, archive extraction, scaffolding, disk-info, and the
dozens of small operations a CLI repeatedly needs to get right.

This package is part of the [go-rotini](https://github.com/go-rotini)
family and follows the same architectural principles, API idioms, and
quality standards as `go-rotini/yaml`, `go-rotini/toml`,
`go-rotini/jsonc`, `go-rotini/jsonschema`, and `go-rotini/env`.

## Status

🚧 **v0.1.0 — feature-complete except for native watcher backends.**
Every Phase 1–35 feature is implemented and tested. The one rough
edge: the watcher's platform-native event delivery (inotify on Linux,
kqueue on macOS/BSD, `ReadDirectoryChangesW` on Windows) is scheduled
for a follow-up minor release. In the interim every watcher uses the
polling backend (`os.Lstat` in a loop, 1-second default interval).
The polling backend is correct on every platform but has the latency
profile and mtime-resolution limits you'd expect from polling — see
`doc.go`'s Pitfalls section. Callers who need sub-second event
delivery today should hold off; callers who reload a config file or
template tree on save are well served by the polling backend.

## Features

- One-line happy path for every common operation: atomic writes, safe
  reads, idempotent removal, ergonomic predicates.
- Cross-platform from day one: Linux / macOS / Windows in CI; FreeBSD
  builds and vets clean.
- **Zero non-stdlib runtime dependencies, no `testing` import** — the
  production surface (watcher, archive, scaffold, disk-info, lock,
  cache, tail-follow, log rotation, versioned backups, transactional
  plan/apply, gitignore, mmap, and the Tier-B helpers) lives in one
  flat root. Importing `github.com/go-rotini/fs` does **not** pull
  stdlib's `testing` package — or its global `-test.*` flag
  registration — into your binary. Test helpers (`TestHarness`,
  `MockFS`, `WithTempEnv`, `TempFileT`, `TempDirT`) live in
  [`github.com/go-rotini/fs/fstest`](https://pkg.go.dev/github.com/go-rotini/fs/fstest)
  so callers opt into the `testing` dependency only in their `_test.go`
  files. Platform-native syscall paths (flock / `LockFileEx` for locks;
  mmap / `MapViewOfFile` for memory maps; future inotify / kqueue /
  `ReadDirectoryChangesW` for native watcher backends) are reached
  directly through stdlib's `syscall` package — no third-party wrappers.
- **Safe-by-default**: atomic writes via temp+rename, bounded reads
  (default 100 MiB), idempotent removal, mode preservation on
  overwrite, archive extraction confined via `MustBeChildOf`.
- Atomic-rename-aware file watching with multi-subscriber broadcast
  and 75 ms trailing-edge debouncing. v0.1 ships the polling backend
  only (1 s default interval, configurable); platform-native
  inotify / kqueue / `ReadDirectoryChangesW` are scheduled for a
  follow-up release.
- Find-up project discovery (`FindUp`, `ProjectRoot`), XDG-aware user
  directories, hidden / pattern walk filtering, and path-safety
  primitives (`IsSubpath`, `MustBeChildOf`, `EvalSymlinksWithin`).
- TOCTOU-safe open primitives: `OpenNoFollow` (refuses final-component
  symlinks) and `OpenAt` (resolves through a held directory FD).

## Installation

```bash
go get github.com/go-rotini/fs
```

Requires Go 1.26 or later.

## Quick start

### Atomic write

```go
// Always atomic via temp+rename in dst's directory; mode preserved
// on overwrite; parent dir fsync'd on POSIX.
if err := fs.WriteFile("/etc/myapp/config.yaml", []byte("port: 8080\n")); err != nil {
    log.Fatal(err)
}

// Secrets: pass WithPerm(0o600) for owner-only.
if err := fs.WriteFile("/etc/myapp/token", []byte(token), fs.WithPerm(fs.Mode0600)); err != nil {
    log.Fatal(err)
}
```

### Read with bounded size

```go
// Default cap is 100 MiB; over-cap returns ErrFileTooLarge.
data, err := fs.ReadFile(path)
if errors.Is(err, fs.ErrFileTooLarge) {
    log.Printf("input too large; use streaming")
}

// Iterate lines without loading the whole file. The iterator's
// second value is non-nil only if the underlying scanner errors
// mid-stream (line too long, I/O failure, etc.); always check it.
seq, closeFn, err := fs.OpenLines(path)
if err != nil {
    log.Fatal(err)
}
defer closeFn()
for line, err := range seq {
    if err != nil {
        log.Fatal(err)
    }
    process(line)
}
```

### Find the project root

```go
// Walks parents looking for .git / go.mod / package.json / Cargo.toml.
root, err := fs.ProjectRoot(".")
if err != nil {
    log.Fatal(err)
}

// Find every .envrc up the chain:
all, err := fs.FindUpAll(".envrc", ".")
```

### Walk with skip filters

```go
err := fs.Walk(root, func(path string, d fs.DirEntry, err error) error {
    if err != nil {
        return err
    }
    fmt.Println(path)
    return nil
}, fs.WalkSkipNames([]string{".git", "node_modules", ".terraform"}))
```

### Watch a config file

```go
w, err := fs.NewWatcher("/etc/myapp/config.yaml", fs.WithDebounce(75*time.Millisecond))
if err != nil {
    log.Fatal(err)
}
defer w.Close()

events, err := w.Subscribe(ctx)
if err != nil {
    log.Fatal(err)
}
for ev := range events {
    if ev.Op.Has(fs.WatchWrite) {
        reload()
    }
}
```

The watcher watches the file's PARENT directory and filters by
basename, so editor atomic-save patterns (write-temp + rename) are
detected correctly.

In v0.1 every watcher is backed by polling (`os.Lstat` in a loop,
1-second default interval). Pass `fs.WithPolling(d)` to tighten the
interval at the cost of more stat syscalls; pass `fs.WithDebounce(0)`
in tests that need immediate event visibility. Platform-native
inotify / kqueue / `ReadDirectoryChangesW` are scheduled for the
next minor — when they land, the API stays the same and the polling
fallback remains available via `WithPolling`.

### Extract an archive safely

```go
// Auto-detects tar / tar.gz / zip from leading bytes.
// Every entry resolves through MustBeChildOf(dst, ...) — defends
// against zip-slip / tar-slip.
if err := fs.ExtractArchiveFile("release.tar.gz", "/opt/myapp"); err != nil {
    log.Fatal(err)
}
```

### Scaffold from an embedded template

```go
//go:embed templates/*
var templates embed.FS

vars := struct{ AppName, Owner string }{"myapp", "alice"}

// Templates render both filenames (`{{.AppName}}.go`) and contents.
// Default policy keeps existing files (idempotent re-run).
if err := fs.ScaffoldApply(templates, "./out", vars); err != nil {
    log.Fatal(err)
}
```

### User directories

```go
// Linux: $XDG_CONFIG_HOME/myapp or ~/.config/myapp
// macOS: ~/Library/Application Support/myapp
// Windows: %APPDATA%\myapp
appConfig, err := fs.AppConfigDir("myapp")
```

### Test-harness for caller tests

The test helpers live in [`github.com/go-rotini/fs/fstest`](https://pkg.go.dev/github.com/go-rotini/fs/fstest)
so importing the main `fs` package never pulls stdlib's `testing`
into your production binary.

```go
import (
    "github.com/go-rotini/fs"
    "github.com/go-rotini/fs/fstest"
)

func TestMyApp(t *testing.T) {
    h := fstest.NewTestHarness(t)
    h.WriteString("config.yaml", "port: 8080\n")
    h.Mkdir("data/cache")

    // Run code under test, then golden-file compare:
    got := h.Snapshot()
    if got != want {
        t.Errorf("layout drift:\n%s", got)
    }
}
```

## Documentation

Full API reference is published on
[pkg.go.dev](https://pkg.go.dev/github.com/go-rotini/fs) once the first
release is tagged. The package's `doc.go` carries a Pitfalls section
covering atomic-write traps, TOCTOU races, the `Exists`-vs-permission
asymmetry, watcher debounce latency, polling-mtime resolution, the
`SanitizeFilename` reserved-name handling, and more.

## Architecture

The production package is a flat directory of files; the only
sub-package is `fstest`, which holds the test helpers. Keeping the
test helpers out of the main package is what lets production binaries
avoid pulling stdlib's `testing` package (and its global flag
registration) into their import graph.

Feature areas use name-prefixing where ambiguity would otherwise arise
(`*Watcher` / `WatchEvent` / `WatchOp`; `ExtractArchive` /
`CreateArchive` / `ArchiveFormat`; `ScaffoldApply` / `ScaffoldPlan`;
`DiskUsage` / `DiskUsageOf`).

Per-feature option types are scoped (`ReadOption`, `WriteOption`,
`WalkOption`, `CopyOption`, `RemoveOption`, `WatcherOption`,
`ArchiveExtractOption`, `ScaffoldOption`) so IDE autocomplete points
to the right knobs.

## Roadmap & scope

Scheduled for a follow-up minor release:

- **Native watcher backends** — inotify (Linux), kqueue (macOS/BSD),
  and `ReadDirectoryChangesW` (Windows). The polling fallback covers
  correctness in the interim.
- **Tier-B helpers deliberately deferred to v0.2 or later** — umask
  helpers, xattrs (Linux/macOS/FreeBSD `getxattr`/`setxattr` and
  Windows alternate-data-streams), sparse/preallocate helpers
  (`Preallocate`, `SparseSeek`, `FileHoles`), reflink/copy-on-write
  (`CopyReflink` on btrfs/XFS/APFS/ReFS), bind-mount / overlay
  structured introspection on Linux, and `MoveToTrash` (freedesktop
  / `NSWorkspace` / `SHFileOperation`). Each requires dedicated
  cross-platform design work.

Deliberately out of scope (would violate the zero-non-stdlib-runtime-
deps mandate):

- **Compression codecs `.zst` / `.xz` / `.lz4`** — stdlib ships
  `compress/gzip` and `compress/bzip2` only; non-stdlib codecs are
  not shipped. `tar.gz` and `zip` cover the overwhelming majority of
  CLI archive needs.
- **POSIX ACLs** — `acl_get_file` / `acl_set_file` are not in Go's
  `syscall` package; surfacing them would require a third-party cgo
  binding or hand-rolling each platform's ABI.
- **`O_DIRECT` helper** — block-aligned I/O is niche enough for CLI
  tooling that callers needing it can pass `syscall.O_DIRECT` to
  `OpenNoFollow` directly.

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md).

## Code of Conduct

See [CODE_OF_CONDUCT.md](CODE_OF_CONDUCT.md).

## Security

To report a vulnerability, see [SECURITY.md](SECURITY.md).

## License

MIT — see [LICENSE](LICENSE).

