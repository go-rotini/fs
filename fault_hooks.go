package fs

import "os"

// Package-internal hooks for OS calls whose error returns are
// defensive guards against rare conditions (disk full, hardware
// failure, in-flight unlink, etc.). Production code calls through
// these so tests can temporarily swap in fault-injecting variants
// to exercise the otherwise-unreachable error branches.
//
// Hooks are package-global by design — wiring an interface plus a
// receiver through every call site would clutter the API surface
// for the sake of test scaffolding. Tests that swap hooks must
// NOT use t.Parallel; the [injectFailure] helper enforces serial
// ordering via t.Cleanup.
//
// In production, none of these are ever reassigned — verified by
// the absence of any non-test exports in this file and by the
// var declarations being lowercase / unexported.
var (
	// File-open / mkdir / rename / chmod / chtimes / remove / stat
	// are direct stdlib functions; the package's writers and copy
	// helpers route through these vars.
	osOpenFile = os.OpenFile
	osOpen     = os.Open
	osRename   = os.Rename
	osChmod    = os.Chmod
	osChtimes  = os.Chtimes
	osStat     = os.Stat
	osLstat    = os.Lstat
	osMkdirAll = os.MkdirAll
	osSymlink  = os.Symlink
	osLink     = os.Link
	osReadlink = os.Readlink
	osRemove   = os.Remove

	// Method-value hooks for *os.File. The signatures match the
	// stdlib method values: e.g., (*os.File).Sync has signature
	// func(*os.File) error so fileSync(f) is equivalent to f.Sync().
	fileSync    = (*os.File).Sync
	fileClose   = (*os.File).Close
	fileWrite   = (*os.File).Write
	fileWriteAt = (*os.File).WriteAt
	fileRead    = (*os.File).Read
)

// hookedReader wraps *os.File so that io.Copy and similar callers
// route Read calls through the [fileRead] hook. This lets fault
// tests inject mid-stream read errors without having to swap an
// entire reader implementation through the codebase.
type hookedReader struct{ f *os.File }

func (r hookedReader) Read(p []byte) (int, error) { return fileRead(r.f, p) }

// closeQuietly invokes the [fileClose] hook and discards its error.
// Used on cleanup paths after a write/sync failure has already been
// recorded; surfacing the close error there would just mask the
// primary cause. Wrapping the discard in a helper keeps errcheck
// happy without scattering nolint directives.
func closeQuietly(f *os.File) { _ = fileClose(f) } //nolint:errcheck // best-effort cleanup; caller has already recorded the primary failure
