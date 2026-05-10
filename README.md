# go-rotini/fs

A pure-Go filesystem helpers package for CLIs: atomic writes, safe reads,
path resolution, cross-platform user-directory lookup, file watching,
directory walking, and the dozens of small operations a CLI repeatedly
needs to get right.

This package is part of the [go-rotini](https://github.com/go-rotini)
family and follows the same architectural principles, API idioms, and
quality standards as `go-rotini/yaml`, `go-rotini/toml`,
`go-rotini/jsonc`, `go-rotini/jsonschema`, and `go-rotini/env`.

## Status

🚧 **In active development.** The implementation plan and complete API
design live in `.docs/FS_PACKAGE_REQUIREMENTS.md`.

## Features

- One-line happy path for every common operation: atomic writes, safe
  reads, idempotent removal, ergonomic predicates.
- Cross-platform from day one: Linux / macOS / Windows in CI.
- Zero non-stdlib runtime dependencies in every package — including the
  watcher (built directly on `inotify` / `kqueue` / `ReadDirectoryChangesW`
  via stdlib `syscall`) and lock sub-package.
- Atomic-rename-aware file watching (`fs/watcher`).
- Find-up project discovery, XDG-aware user directories, gitignore-style
  walk filtering, and path-safety primitives (`IsSubpath`,
  `MustBeChildOf`).

## Installation

```bash
go get github.com/go-rotini/fs
```

Requires Go 1.26 or later.

## Documentation

Full API reference is published on
[pkg.go.dev](https://pkg.go.dev/github.com/go-rotini/fs) once the first
release is tagged.

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md).

## Code of Conduct

See [CODE_OF_CONDUCT.md](CODE_OF_CONDUCT.md).

## Security

To report a vulnerability, see [SECURITY.md](SECURITY.md).

## License

MIT — see [LICENSE](LICENSE).
