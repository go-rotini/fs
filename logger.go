package fs

import (
	"io"
	"log/slog"
	"sync"
	"sync/atomic"
)

// pkgLogger is the package-level logger consulted by [Apply],
// [*Cache], [*Rotator], and other subsystems for debug output.
// Defaults to a discard sink so the package is silent unless a
// caller swaps in a real logger via [SetLogger].
//
// Stored in an [atomic.Pointer] so concurrent SetLogger and reads
// via logger() don't race. Initialized lazily; packages that import
// fs but never call logger() pay nothing.
var (
	pkgLogger    atomic.Pointer[slog.Logger]
	pkgLoggerSet sync.Once
)

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// SetLogger swaps the package-level [slog.Logger] used by Apply,
// Cache, Rotator, and other subsystems. Pass nil to restore the
// default discard logger. Safe to call from any goroutine.
//
// Note: package debug records include caller-supplied paths (log
// filenames, plan op paths including ones that may name secret
// files). Install a logger only after deciding whether the handler
// is allowed to see those paths; redact via a custom [slog.Handler]
// when callers may be untrusted.
//
// The watcher takes a per-instance logger via its [WithLogger]
// option and is not affected by this hook.
func SetLogger(l *slog.Logger) {
	if l == nil {
		pkgLogger.Store(discardLogger())
		return
	}
	pkgLogger.Store(l)
}

// logger returns the currently-installed package logger. Always
// non-nil; the first call lazy-initializes the discard sink.
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
