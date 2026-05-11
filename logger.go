package fs

import (
	"io"
	"log/slog"
	"sync"
	"sync/atomic"
)

// pkgLogger is the package-level logger consulted by [Apply],
// [*Cache], [*Rotator], and the new Phase 28–35 subsystems for
// debug / trace output. Defaults to an io.Discard sink so the
// package is silent unless a caller swaps in a real logger via
// [SetLogger].
//
// Stored in an [atomic.Pointer] so [SetLogger] and concurrent
// reads via [logger] don't race. Initialization is lazy (no init
// function) so packages importing fs but never calling logger()
// pay nothing.
var (
	pkgLogger    atomic.Pointer[slog.Logger]
	pkgLoggerSet sync.Once
)

// discardLogger constructs the default no-op logger.
func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// SetLogger swaps the package-level [slog.Logger] used by Apply /
// Cache / Rotator / other phase-28+ subsystems. Pass nil to restore
// the default discard logger. Safe to call from any goroutine; the
// swap is atomic.
//
// Sensitive data note: package debug records include caller-supplied
// paths — log filenames (Rotator), cache keys' hashed-path locations
// (Cache), and per-op target paths (Apply, including ones that name
// secret files like ssh keys). Install a logger only after deciding
// whether your handler is allowed to see those paths; route to an
// audit sink or redact via a custom [slog.Handler] when callers will
// be untrusted.
//
// The watcher takes a per-instance logger via its [WithLogger]
// option (which predates this hook) and is not affected.
func SetLogger(l *slog.Logger) {
	if l == nil {
		pkgLogger.Store(discardLogger())
		return
	}
	pkgLogger.Store(l)
}

// logger returns the currently-installed package logger. Always
// non-nil — the first call lazy-initializes the discard sink.
func logger() *slog.Logger {
	if l := pkgLogger.Load(); l != nil {
		return l
	}
	pkgLoggerSet.Do(func() {
		if pkgLogger.Load() == nil {
			pkgLogger.Store(discardLogger())
		}
	})
	return pkgLogger.Load()
}
