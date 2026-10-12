package fs

import (
	"bytes"
	"os"
	"path/filepath"
	"sync"
)

// WriteOption configures [WriteFile], [WriteString], [WriteIfChanged],
// [WriteFileExclusive], [EnsureFile], [Append], [AppendString],
// [WriteAt] and [OpenWrite]. Each function honors only the options
// that apply to it.
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

// WithPerm sets the file's permission bits. Without it, new files get
// 0o644 and an atomic overwrite keeps the existing file's permissions.
// Atomic writes apply the mode exactly; for other writes it only
// applies when the file is created, and the process umask applies.
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

// WithMkdirAll creates missing parent directories before writing
// (mode from [WithDirPerm]). Default false.
func WithMkdirAll(b bool) WriteOption {
	return func(o *writeOptions) { o.mkdirAll = b }
}

// WithSync controls fsync. For atomic writes ([WriteFile] and
// [OpenWrite]) the temp file is synced before the rename and, on
// POSIX, the parent directory after it; this is on by default when
// overwriting an existing file and off for a new one, and WithSync
// overrides either way. Other writes sync the file only when
// WithSync(true) is given.
func WithSync(b bool) WriteOption {
	return func(o *writeOptions) {
		o.sync = b
		o.hasSync = true
	}
}

// WithAtomic toggles the temp-file + rename strategy of [WriteFile].
// Default true. Disable for very large files where doubled disk usage
// is prohibitive, at the cost of writes that can leave torn content
// visible to concurrent readers. [WithBackup] and [WithTempPattern]
// apply only to atomic writes.
func WithAtomic(b bool) WriteOption {
	return func(o *writeOptions) { o.atomic = b }
}

// WithLocked makes [Append] and [AppendString] hold an exclusive
// advisory flock(2) on the file while writing. It is a no-op on
// Windows, where appends are already serialized by the OS.
func WithLocked(b bool) WriteOption {
	return func(o *writeOptions) { o.locked = b }
}

// WithTempPattern overrides the default temp-file naming pattern
// used by atomic writes. Pattern is an [os.CreateTemp] template
// (`*` is replaced by the random suffix). Default
// `<basename>.tmp.*`.
func WithTempPattern(pattern string) WriteOption {
	return func(o *writeOptions) { o.tempPattern = pattern }
}

// WithBackup makes an atomic write first rename the existing file at
// the destination to `<dest><suffix>`, replacing any earlier backup.
// An empty suffix means ".bak". Nothing is backed up when the
// destination does not exist.
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

// resolveWritePath returns path unchanged: writes do not expand ~ or
// $VAR, so callers that need it call [Expand] first.
func resolveWritePath(path string, _ writeOptions) string { return path }

// WriteFile writes data to path atomically (write-temp + rename).
// The temp file lives in the destination's directory. Default mode
// 0o644 for new files; existing files keep their mode unless
// [WithPerm] is set. Errors if the parent directory doesn't exist;
// pass [WithMkdirAll] to create it. See [WithSync] for durability and
// [WithAtomic] to write in place instead.
//
// For files containing secrets (tokens, keys), pass
// `WithPerm(fs.Mode0600)` so the result is owner-only.
func WriteFile(path string, data []byte, opts ...WriteOption) error {
	cfg := newWriteOptions(opts)
	return doWriteFile(path, data, cfg)
}

// WriteString is shorthand for WriteFile(path, []byte(s), opts...).
func WriteString(path, s string, opts ...WriteOption) error {
	return WriteFile(path, []byte(s), opts...)
}

// WriteIfChanged writes data to path only when it differs from the file
// already there, reporting whether a write happened. A missing or unreadable
// destination counts as changed. Skipping identical writes leaves the file's
// mtime untouched, so file watchers and mtime-keyed build caches don't re-fire
// on a no-op regeneration. Options match [WriteFile].
func WriteIfChanged(path string, data []byte, opts ...WriteOption) (bool, error) {
	if existing, err := os.ReadFile(path); err == nil && bytes.Equal(existing, data) {
		return false, nil
	}
	if err := WriteFile(path, data, opts...); err != nil {
		return false, err
	}
	return true, nil
}

// WriteFileExclusive writes data to path with O_CREATE|O_EXCL
// semantics: if path already exists the error wraps
// [ErrAlreadyExists]. Useful for "create only if not present" setups.
//
// The file is written in place, not via temp+rename, so concurrent
// readers can see partial content; if the write fails the file is
// removed.
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
// defaults on for overwrites and off for new files; an explicit
// WithSync overrides either.
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

// Append appends data to path. It is not atomic with respect to other
// writers; pass [WithLocked] for an advisory-lock-protected append.
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

// WriteAt writes data to path starting at offset, in place. path must
// exist. A negative offset returns an error wrapping [ErrInvalidPath].
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
// temp file, in path's directory, that the caller writes through; the
// second is a finalize function that closes it and atomically renames
// it over path. Until finalize is called the destination is
// unchanged. The [WriteFile] options for mode, sync and backup apply.
//
// If finalize fails, the temp file is removed. To abandon the write
// without calling finalize, close the file and remove its Name.
//
// Finalize runs once; later calls return the first call's result.
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
