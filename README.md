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

✅ **v0.1.0 surface complete.** Every Phase-1-through-26 feature is
implemented and tested. The watcher's platform-native backends (inotify
/ kqueue / ReadDirectoryChangesW) are scheduled as a follow-up; the
polling fallback is fully wired and gives correct behavior on every
platform today.

## Features

- One-line happy path for every common operation: atomic writes, safe
  reads, idempotent removal, ergonomic predicates.
- Cross-platform from day one: Linux / macOS / Windows in CI; FreeBSD
  builds and vets clean.
- **Zero non-stdlib runtime dependencies, no `testing` import** — the
  production surface (watcher, archive, scaffold, disk-info, post-v0.1
  lock helpers) lives in one flat root. Importing `github.com/go-rotini/fs`
  does **not** pull stdlib's `testing` package — or its global
  `-test.*` flag registration — into your binary. Test helpers
  (`TestHarness`, `MockFS`, `WithTempEnv`, `TempFileT`, `TempDirT`)
  live in [`github.com/go-rotini/fs/fstest`](https://pkg.go.dev/github.com/go-rotini/fs/fstest)
  so callers opt into the `testing` dependency only in their `_test.go`
  files. Platform-native APIs (inotify / kqueue / `ReadDirectoryChangesW`;
  flock / `LockFileEx`) are accessed directly through stdlib's `syscall`
  package.
- **Safe-by-default**: atomic writes via temp+rename, bounded reads
  (default 100 MiB), idempotent removal, mode preservation on
  overwrite, archive extraction confined via `MustBeChildOf`.
- Atomic-rename-aware file watching with multi-subscriber broadcast
  and 75 ms trailing-edge debouncing.
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

// Secrets get 0o600.
if err := fs.WriteFileSecret("/etc/myapp/token", []byte(token)); err != nil {
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
}, fs.WithSkipNames([]string{".git", "node_modules", ".terraform"}))
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

## Known gaps (post-v0.1 roadmap)

The following ship in the fs package post-v0.1 (not as sub-packages):
file locking via `flock`/`LockFileEx`, namespaced caches with TTL/LRU,
tail-follow with rotation handling, log rotation, versioned backups,
transactional plan/apply, `.gitignore` parser. The watcher's
platform-native backends (inotify / kqueue / `ReadDirectoryChangesW`)
are also scheduled as follow-ups; the polling fallback covers
correctness in the interim.

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md).

## Code of Conduct

See [CODE_OF_CONDUCT.md](CODE_OF_CONDUCT.md).

## Security

To report a vulnerability, see [SECURITY.md](SECURITY.md).

## License

MIT — see [LICENSE](LICENSE).

