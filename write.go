package fs

import (
	"os"
	"path/filepath"
	"sync"
)

// WriteOption configures write operations.
type WriteOption func(*writeOptions)

type writeOptions struct {
	perm        os.FileMode
	hasPerm     bool
	dirPerm     os.FileMode
	mkdirAll    bool
	sync        bool
	hasSync     bool
	atomic      bool
	locked      bool
	tempPattern string
	backup      string
	hasBackup   bool
}

func newWriteOptions(opts []WriteOption) writeOptions {
	cfg := writeOptions{
		perm:    Mode0644,
		dirPerm: Mode0755,
		atomic:  true, // default-on; explicit WithAtomic(false) opts out
	}
	for _, o := range opts {
		o(&cfg)
	}
	return cfg
}

// WithPerm sets the file mode for new files. Existing files preserve
// their current mode unless WithPerm is also set on the call.
func WithPerm(mode os.FileMode) WriteOption {
	return func(o *writeOptions) {
		o.perm = mode.Perm()
		o.hasPerm = true
	}
}

// WithDirPerm sets the mode for parent directories created via
// [WithMkdirAll]. Default 0o755.
func WithDirPerm(mode os.FileMode) WriteOption {
	return func(o *writeOptions) {
		o.dirPerm = mode.Perm()
	}
}

// WithMkdirAll creates missing parent directories before writing.
func WithMkdirAll(b bool) WriteOption {
	return func(o *writeOptions) { o.mkdirAll = b }
}

// WithSync forces an fsync on the temp file (and parent directory on
// POSIX) before/after the rename. Default-on for overwrites of
// existing files; default-off for new files. Setting it explicitly
// overrides the default in either direction.
func WithSync(b bool) WriteOption {
	return func(o *writeOptions) {
		o.sync = b
		o.hasSync = true
	}
}

// WithAtomic toggles the temp-file + rename strategy. Default true.
// Disable for very large files where doubled disk usage is
// prohibitive — at the cost of writes that can leave torn content
// visible to concurrent readers.
func WithAtomic(b bool) WriteOption {
	return func(o *writeOptions) { o.atomic = b }
}

// WithLocked acquires an advisory flock on the file before writing
// (POSIX only; no-op on Windows where O_APPEND is already serialized
// by the OS). Used by [Append] / [AppendString].
func WithLocked(b bool) WriteOption {
	return func(o *writeOptions) { o.locked = b }
}

// WithTempPattern overrides the default temp-file naming pattern
// used by atomic writes. Pattern is a [os.CreateTemp] template
// (`*` is replaced by the random suffix). Default
// `<basename>.tmp.*`.
func WithTempPattern(pattern string) WriteOption {
	return func(o *writeOptions) { o.tempPattern = pattern }
}

// WithBackup renames the existing destination to `<dest><suffix>`
// before the rename completes. Empty suffix defaults to ".bak".
// A pre-existing backup at the same path is overwritten.
func WithBackup(suffix string) WriteOption {
	return func(o *writeOptions) {
		if suffix == "" {
			suffix = ".bak"
		}
		o.backup = suffix
		o.hasBackup = true
	}
}

// Operation labels for *PathError.Op on write-layer errors.
const (
	opWrite     = "write"
	opAppend    = "append"
	opRename    = "rename"
	opOpenWrite = "openwrite"
)

// resolveWritePath returns path unchanged. The package's writeOptions
// type does not expose a tilde / $VAR expansion knob; callers that
// need expansion compose [Expand] themselves before calling the
// write API. Kept as a function so future expansion options have a
// natural insertion point.
func resolveWritePath(path string, _ writeOptions) string { return path }

// WriteFile writes data to path atomically (write-temp + rename).
// Default mode 0o644 for new files; existing files preserve their
// mode unless [WithPerm] is set. Errors if the parent directory
// doesn't exist (use [WriteFileEnsure] or [WithMkdirAll] to create
// it).
func WriteFile(path string, data []byte, opts ...WriteOption) error {
	cfg := newWriteOptions(opts)
	return doWriteFile(path, data, cfg)
}

// WriteString is shorthand for WriteFile(path, []byte(s), opts...).
func WriteString(path, s string, opts ...WriteOption) error {
	return WriteFile(path, []byte(s), opts...)
}

// WriteFileSecret is [WriteFile] with mode 0o600 (owner-only
// read/write). Use for files containing tokens, keys, or other
// sensitive data.
func WriteFileSecret(path string, data []byte, opts ...WriteOption) error {
	cfg := newWriteOptions(opts)
	if !cfg.hasPerm {
		cfg.perm = Mode0600
		cfg.hasPerm = true
	}
	return doWriteFile(path, data, cfg)
}

// WriteFileEnsure is [WriteFile] with [WithMkdirAll] true.
func WriteFileEnsure(path string, data []byte, opts ...WriteOption) error {
	cfg := newWriteOptions(opts)
	cfg.mkdirAll = true
	return doWriteFile(path, data, cfg)
}

// WriteFileExclusive writes data to path with O_CREATE|O_EXCL
// semantics: errors with [ErrAlreadyExists] if path already exists.
// Useful for "create only if not present" idempotent setups.
//
// Atomic-write internals are disabled (the temp+rename strategy
// would race against a concurrent creator); the file is opened
// directly with O_EXCL.
func WriteFileExclusive(path string, data []byte, opts ...WriteOption) error {
	cfg := newWriteOptions(opts)
	p := resolveWritePath(path, cfg)
	if cfg.mkdirAll {
		if err := osMkdirAll(filepath.Dir(p), cfg.dirPerm); err != nil {
			return wrapPathError(opMkdirAll, p, err)
		}
	}
	mode := cfg.perm
	if !cfg.hasPerm {
		mode = Mode0644
	}
	f, err := osOpenFile(p, os.O_CREATE|os.O_WRONLY|os.O_EXCL, mode)
	if err != nil {
		return wrapPathError(opWrite, p, err)
	}
	if _, werr := fileWrite(f, data); werr != nil {
		closeQuietly(f)
		_ = os.Remove(p)
		return wrapPathError(opWrite, p, werr)
	}
	if cfg.hasSync && cfg.sync {
		if serr := fileSync(f); serr != nil {
			closeQuietly(f)
			return wrapPathError(opWrite, p, serr)
		}
	}
	if cerr := fileClose(f); cerr != nil {
		return wrapPathError(opWrite, p, cerr)
	}
	return nil
}

// doWriteFile is the shared implementation for the atomic write
// family.
func doWriteFile(path string, data []byte, cfg writeOptions) error {
	p := resolveWritePath(path, cfg)
	if cfg.mkdirAll {
		if err := osMkdirAll(filepath.Dir(p), cfg.dirPerm); err != nil {
			return wrapPathError(opMkdirAll, p, err)
		}
	}

	if !cfg.atomic {
		return writeDirect(p, data, cfg)
	}
	return writeAtomic(p, data, cfg)
}

// writeDirect performs a non-atomic write: open, write, close.
// Caller has already opted out of atomicity via WithAtomic(false).
func writeDirect(path string, data []byte, cfg writeOptions) error {
	mode, _ := resolveMode(path, cfg)
	f, err := osOpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return wrapPathError(opWrite, path, err)
	}
	if _, werr := fileWrite(f, data); werr != nil {
		closeQuietly(f)
		return wrapPathError(opWrite, path, werr)
	}
	if cfg.hasSync && cfg.sync {
		if serr := fileSync(f); serr != nil {
			closeQuietly(f)
			return wrapPathError(opWrite, path, serr)
		}
	}
	if cerr := fileClose(f); cerr != nil {
		return wrapPathError(opWrite, path, cerr)
	}
	return nil
}

// writeAtomic performs the temp-file + rename atomic write. Sync
// behavior follows the spec: default-on for overwrites, default-off
// for new files; explicit WithSync overrides either direction.
func writeAtomic(path string, data []byte, cfg writeOptions) error {
	mode, existed := resolveMode(path, cfg)
	doSync := existed
	if cfg.hasSync {
		doSync = cfg.sync
	}

	tmp, err := openTempForWrite(path, mode, cfg.tempPattern)
	if err != nil {
		return wrapPathError(opWrite, path, err)
	}
	tmpName := tmp.Name()
	cleanup := func() { _ = os.Remove(tmpName) }
	committed := false
	defer func() {
		if !committed {
			cleanup()
		}
	}()

	if _, werr := fileWrite(tmp, data); werr != nil {
		closeQuietly(tmp)
		return wrapPathError(opWrite, path, werr)
	}
	if doSync {
		if serr := fileSync(tmp); serr != nil {
			closeQuietly(tmp)
			return wrapPathError(opWrite, path, serr)
		}
	}
	if cerr := fileClose(tmp); cerr != nil {
		return wrapPathError(opWrite, path, cerr)
	}

	if cfg.hasBackup && existed {
		backup := path + cfg.backup
		_ = os.Remove(backup)
		if rerr := osRename(path, backup); rerr != nil {
			return wrapPathError(opRename, path, rerr)
		}
	}

	if rerr := osRename(tmpName, path); rerr != nil {
		return wrapPathError(opRename, path, rerr)
	}
	committed = true

	if doSync {
		if serr := syncDir(filepath.Dir(path)); serr != nil {
			return wrapPathError(opWrite, path, serr)
		}
	}
	return nil
}

// resolveMode picks the file mode for the write: explicit WithPerm
// wins; existing file's mode preserved on overwrite; default
// otherwise. The boolean reports whether the destination existed
// before the write.
func resolveMode(path string, cfg writeOptions) (os.FileMode, bool) {
	if cfg.hasPerm {
		// Explicit WithPerm.
		_, err := os.Stat(path)
		return cfg.perm, err == nil
	}
	info, err := os.Stat(path)
	if err == nil {
		return info.Mode().Perm(), true
	}
	return cfg.perm, false
}

// openTempForWrite creates a uniquely-named temp file in the
// destination's parent directory, with the requested mode. Pattern
// defaults to `<basename>.tmp.*` (where * is the random suffix).
//
// Returned errors surface raw; the caller wraps with *PathError.
func openTempForWrite(dest string, mode os.FileMode, pattern string) (*os.File, error) {
	dir := filepath.Dir(dest)
	if pattern == "" {
		pattern = filepath.Base(dest) + ".tmp.*"
	}
	tmp, err := os.CreateTemp(dir, pattern)
	if err != nil {
		return nil, err //nolint:wrapcheck // wrapped by caller via *PathError
	}
	if err := osChmod(tmp.Name(), mode); err != nil {
		closeQuietly(tmp)
		_ = os.Remove(tmp.Name())
		return nil, err
	}
	return tmp, nil
}

// Append appends data to path. NOT atomic with respect to other
// writers. Pass [WithLocked] for advisory-flock-protected append on
// POSIX; on Windows, O_APPEND is already serialized by the OS.
//
// Creates the file with [WithPerm] (default 0o644) if missing.
func Append(path string, data []byte, opts ...WriteOption) error {
	cfg := newWriteOptions(opts)
	p := resolveWritePath(path, cfg)
	if cfg.mkdirAll {
		if err := osMkdirAll(filepath.Dir(p), cfg.dirPerm); err != nil {
			return wrapPathError(opMkdirAll, p, err)
		}
	}
	mode := cfg.perm
	f, err := osOpenFile(p, os.O_CREATE|os.O_WRONLY|os.O_APPEND, mode)
	if err != nil {
		return wrapPathError(opAppend, p, err)
	}
	defer func() { closeQuietly(f) }()

	if cfg.locked {
		if lerr := lockFile(f); lerr != nil {
			return wrapPathError(opAppend, p, lerr)
		}
	}
	if _, werr := fileWrite(f, data); werr != nil {
		return wrapPathError(opAppend, p, werr)
	}
	if cfg.hasSync && cfg.sync {
		if serr := fileSync(f); serr != nil {
			return wrapPathError(opAppend, p, serr)
		}
	}
	return nil
}

// AppendString is shorthand for Append(path, []byte(s), opts...).
func AppendString(path, s string, opts ...WriteOption) error {
	return Append(path, []byte(s), opts...)
}

// WriteAt writes data to path starting at offset. Path must exist;
// the underlying file is opened with O_RDWR. NOT atomic by
// definition.
func WriteAt(path string, offset int64, data []byte, opts ...WriteOption) error {
	cfg := newWriteOptions(opts)
	p := resolveWritePath(path, cfg)
	if offset < 0 {
		return wrapPathError(opWrite, p, ErrInvalidPath)
	}
	f, err := osOpenFile(p, os.O_RDWR, 0)
	if err != nil {
		return wrapPathError(opWrite, p, err)
	}
	defer func() { closeQuietly(f) }()
	if _, werr := fileWriteAt(f, data, offset); werr != nil {
		return wrapPathError(opWrite, p, werr)
	}
	if cfg.hasSync && cfg.sync {
		if serr := fileSync(f); serr != nil {
			return wrapPathError(opWrite, p, serr)
		}
	}
	return nil
}

// OpenWrite opens path for streaming writes. The first return is the
// open file the caller writes through; the second is a finalize
// function that closes the file and atomically renames the
// underlying temp file over path. Until finalize is called the
// destination is unchanged.
//
// If finalize is not called (or returns an error), the temp file is
// removed on the second call so the destination is never affected.
//
// Finalize is idempotent: calling it twice returns the same result
// the first call did (success returns nil; failure returns the
// original error).
func OpenWrite(path string, opts ...WriteOption) (*os.File, func() error, error) {
	cfg := newWriteOptions(opts)
	p := resolveWritePath(path, cfg)
	if cfg.mkdirAll {
		if err := osMkdirAll(filepath.Dir(p), cfg.dirPerm); err != nil {
			return nil, nil, wrapPathError(opMkdirAll, p, err)
		}
	}

	mode, existed := resolveMode(p, cfg)
	doSync := existed
	if cfg.hasSync {
		doSync = cfg.sync
	}

	tmp, err := openTempForWrite(p, mode, cfg.tempPattern)
	if err != nil {
		return nil, nil, wrapPathError(opOpenWrite, p, err)
	}

	var (
		once   sync.Once
		result error
	)
	finalize := func() error {
		once.Do(func() {
			tmpName := tmp.Name()
			defer func() {
				// Cleanup on error path; rename-success path returns
				// before this defer fires.
				if result != nil {
					_ = os.Remove(tmpName)
				}
			}()
			if doSync {
				if serr := fileSync(tmp); serr != nil {
					closeQuietly(tmp)
					result = wrapPathError(opWrite, p, serr)
					return
				}
			}
			if cerr := fileClose(tmp); cerr != nil {
				result = wrapPathError(opWrite, p, cerr)
				return
			}
			if cfg.hasBackup && existed {
				backup := p + cfg.backup
				_ = os.Remove(backup)
				if rerr := osRename(p, backup); rerr != nil {
					result = wrapPathError(opRename, p, rerr)
					return
				}
			}
			if rerr := osRename(tmpName, p); rerr != nil {
				result = wrapPathError(opRename, p, rerr)
				return
			}
			if doSync {
				if serr := syncDir(filepath.Dir(p)); serr != nil {
					result = wrapPathError(opWrite, p, serr)
					return
				}
			}
		})
		return result
	}
	return tmp, finalize, nil
}
