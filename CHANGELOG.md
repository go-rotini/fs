# Changelog

All notable changes to this project will be documented in this file.

The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and the project uses [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added
- Phase 1: project scaffolding — config files, GitHub workflows, Makefile, package documentation skeleton, zero-runtime-dep `go.mod`, and the `make deps-check` lint step that enforces the package's defining constraint (no third-party imports in any runtime graph). `make lint`, `go-licenses check`, and `govulncheck` all pass against the empty package.
