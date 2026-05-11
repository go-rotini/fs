package fs

import (
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	stdfs "io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	opRotate       = "rotate"
	opRotateOpen   = "rotate.open"
	opRotateWrite  = "rotate.write"
	opRotateClose  = "rotate.close"
	opRotatePrune  = "rotate.prune"
	opRotateCompr  = "rotate.compress"
	opRotateRename = "rotate.rename"

	// rotateTimestampLayout suffixes rotated files. Sortable lex
	// (also chronologically) so retention pruning can sort by name.
	rotateTimestampLayout = "20060102-150405.000"

	// rotateGzipExt is appended to a rotated filename when
	// [WithRotateCompress] is enabled.
	rotateGzipExt = ".gz"

	// rotateFilePerm matches WriteFile's default for new log files.
	rotateFilePerm os.FileMode = 0o644
)

// ErrRotatorClosed is returned by [Rotator.Write] after [Rotator.Close]
// has been called.
var ErrRotatorClosed = errors.New("fs: rotator: closed")

// Rotator is an [io.WriteCloser] that rotates its backing file when
// a size or age threshold is exceeded. Rotation renames the current
// file with a sortable timestamp suffix (and optionally gzips it),
// then reopens the original path fresh.
//
// Rotator is safe for concurrent Write calls. Within a single
// process, writes are serialized via an internal mutex; across
// processes the backing file is opened with O_APPEND so writes don't
// interleave at byte granularity. Multiple processes pointing at the
// same path will each manage their own rotations, which can interact
// unpredictably — coordinate rotations via a single writer when this
// matters.
type Rotator struct {
	path string
	cfg  rotatorConfig

	mu        sync.Mutex
	f         *os.File
	curBytes  int64
	openedAt  time.Time
	closed    bool
}

// rotatorConfig holds the per-Rotator options. Zero value means "no
// size cap, no age cap, no retention, no compression, real wall
// clock".
type rotatorConfig struct {
	maxBytes int64
	maxAge   time.Duration
	keep     int
	compress bool
	nowFn    func() time.Time
}

// RotatorOption configures [NewRotator].
type RotatorOption func(*rotatorConfig)

// WithRotateMaxBytes rotates the file when its size would exceed n.
// The check runs before each Write; a single oversized Write is
// accepted in full and triggers rotation immediately afterward.
// Zero (the default) disables size-based rotation.
func WithRotateMaxBytes(n int64) RotatorOption {
	return func(c *rotatorConfig) {
		c.maxBytes = n
	}
}

// WithRotateMaxAge rotates the file when the time since the current
// file was opened exceeds d. Useful for daily-log patterns; the age
// check runs before each Write. Zero (the default) disables
// age-based rotation.
func WithRotateMaxAge(d time.Duration) RotatorOption {
	return func(c *rotatorConfig) {
		c.maxAge = d
	}
}

// WithRotateKeep retains the n most recent rotated files; older
// ones are removed after every rotation. Zero (the default) keeps
// every rotated file. The current (live) file is not counted.
func WithRotateKeep(n int) RotatorOption {
	return func(c *rotatorConfig) {
		c.keep = n
	}
}

// WithRotateCompress gzip-compresses rotated files after the move.
// Uses [compress/gzip] from stdlib so there's no third-party
// dependency. Compression runs synchronously after the rename;
// callers needing non-blocking compression should rotate in a
// background goroutine.
func WithRotateCompress() RotatorOption {
	return func(c *rotatorConfig) {
		c.compress = true
	}
}

// WithRotateClock overrides the wall clock used for age accounting
// and rotated-filename timestamps. Useful only in tests.
func WithRotateClock(now func() time.Time) RotatorOption {
	return func(c *rotatorConfig) {
		c.nowFn = now
	}
}

// NewRotator opens path (creating it if absent) and returns a
// [*Rotator]. Subsequent [Rotator.Write] calls append to the file
// and trigger rotation when size or age thresholds are crossed.
func NewRotator(path string, opts ...RotatorOption) (*Rotator, error) {
	if path == "" {
		return nil, wrapPathError(opRotateOpen, path, ErrInvalidPath)
	}

	cfg := rotatorConfig{nowFn: time.Now}
	for _, opt := range opts {
		opt(&cfg)
	}
	if cfg.nowFn == nil {
		cfg.nowFn = time.Now
	}

	if err := MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}

	f, info, err := openRotatedFile(path)
	if err != nil {
		return nil, err
	}
	return &Rotator{
		path:     path,
		cfg:      cfg,
		f:        f,
		curBytes: info.Size(),
		openedAt: cfg.nowFn(),
	}, nil
}

// Write appends p to the backing file. Before the append, the current
// size and age are checked against the configured thresholds; if
// either is exceeded, the file is rotated and a fresh one is opened.
// Returns the number of bytes written, which equals len(p) on
// success.
func (r *Rotator) Write(p []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.closed {
		return 0, wrapPathError(opRotateWrite, r.path, ErrRotatorClosed)
	}

	if r.shouldRotateLocked(int64(len(p))) {
		if err := r.rotateLocked(); err != nil {
			return 0, err
		}
	}

	n, err := r.f.Write(p)
	r.curBytes += int64(n)
	if err != nil {
		return n, wrapPathError(opRotateWrite, r.path, err)
	}
	return n, nil
}

// Close releases the underlying file handle. Subsequent Write calls
// return [ErrRotatorClosed]. Close is idempotent; the first call's
// result is returned to subsequent callers.
func (r *Rotator) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.closed {
		return nil
	}
	r.closed = true
	if r.f == nil {
		return nil
	}
	err := r.f.Close()
	r.f = nil
	if err != nil {
		return wrapPathError(opRotateClose, r.path, err)
	}
	return nil
}

// Rotate forces an immediate rotation regardless of size or age.
// Useful for SIGHUP-style "rotate now" semantics. Subsequent writes
// land in a fresh file at the original path.
func (r *Rotator) Rotate() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.closed {
		return wrapPathError(opRotate, r.path, ErrRotatorClosed)
	}
	return r.rotateLocked()
}

// shouldRotateLocked reports whether the next Write of pendingBytes
// would push the rotator past its configured size or age thresholds.
// Called with r.mu held.
func (r *Rotator) shouldRotateLocked(pendingBytes int64) bool {
	if r.curBytes == 0 {
		// Never rotate an empty file — an immediately-rotated empty
		// log produces a stream of zero-byte rotated artifacts.
		return false
	}
	if r.cfg.maxBytes > 0 && r.curBytes+pendingBytes > r.cfg.maxBytes {
		return true
	}
	if r.cfg.maxAge > 0 && r.cfg.nowFn().Sub(r.openedAt) >= r.cfg.maxAge {
		return true
	}
	return false
}

// rotateLocked closes the current file, renames it with a timestamp
// suffix, optionally compresses it, prunes old rotated files
// according to the keep policy, and opens a fresh file at the
// original path. Called with r.mu held.
func (r *Rotator) rotateLocked() error {
	if r.f != nil {
		if err := r.f.Close(); err != nil {
			return wrapPathError(opRotateClose, r.path, err)
		}
		r.f = nil
	}

	rotated := r.rotatedName()
	if err := os.Rename(r.path, rotated); err != nil {
		// If the file vanished between the size check and the rename
		// (another rotator pulled it), fall through to reopening at
		// path so the next write succeeds.
		if !errors.Is(err, stdfs.ErrNotExist) {
			return wrapPathError(opRotateRename, r.path, err)
		}
	} else if r.cfg.compress {
		if cerr := compressRotatedFile(rotated); cerr != nil {
			return cerr
		}
	}

	if r.cfg.keep > 0 {
		if perr := pruneRotated(r.path, r.cfg.keep); perr != nil {
			return perr
		}
	}

	f, info, err := openRotatedFile(r.path)
	if err != nil {
		return err
	}
	r.f = f
	r.curBytes = info.Size()
	r.openedAt = r.cfg.nowFn()
	return nil
}

// rotatedName returns the timestamped name for the current rotation.
// Format: <path>.<timestamp> — lex-sortable so rotated files appear
// in chronological order under `ls`.
func (r *Rotator) rotatedName() string {
	stamp := r.cfg.nowFn().UTC().Format(rotateTimestampLayout)
	return fmt.Sprintf("%s.%s", r.path, stamp)
}

// openRotatedFile opens path with O_APPEND so concurrent writers
// land at the end without interleaving inside a single Write call.
// Returns the open file plus its starting size so the Rotator can
// initialize curBytes accurately.
func openRotatedFile(path string) (*os.File, stdfs.FileInfo, error) {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_APPEND, rotateFilePerm)
	if err != nil {
		return nil, nil, wrapPathError(opRotateOpen, path, err)
	}
	info, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return nil, nil, wrapPathError(opRotateOpen, path, err)
	}
	return f, info, nil
}

// compressRotatedFile gzips rotated in place: writes <rotated>.gz
// and removes <rotated> on success. On any failure the original
// (uncompressed) rotated file is left intact for forensic recovery.
func compressRotatedFile(rotated string) error {
	src, err := os.Open(rotated)
	if err != nil {
		return wrapPathError(opRotateCompr, rotated, err)
	}
	defer func() { _ = src.Close() }()

	dst := rotated + rotateGzipExt
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, rotateFilePerm)
	if err != nil {
		return wrapPathError(opRotateCompr, dst, err)
	}
	gw := gzip.NewWriter(out)

	if _, copyErr := io.Copy(gw, src); copyErr != nil {
		_ = gw.Close()
		_ = out.Close()
		_ = os.Remove(dst)
		return wrapPathError(opRotateCompr, rotated, copyErr)
	}
	if err := gw.Close(); err != nil {
		_ = out.Close()
		_ = os.Remove(dst)
		return wrapPathError(opRotateCompr, dst, err)
	}
	if err := out.Close(); err != nil {
		_ = os.Remove(dst)
		return wrapPathError(opRotateCompr, dst, err)
	}

	if err := os.Remove(rotated); err != nil {
		return wrapPathError(opRotateCompr, rotated, err)
	}
	return nil
}

// pruneRotated enumerates rotated siblings of path (files matching
// "<basename>.<timestamp>" optionally with ".gz" suffix), sorts them
// chronologically (newest first), and removes everything beyond the
// keep-most-recent threshold.
func pruneRotated(path string, keep int) error {
	dir := filepath.Dir(path)
	base := filepath.Base(path)
	dirents, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, stdfs.ErrNotExist) {
			return nil
		}
		return wrapPathError(opRotatePrune, dir, err)
	}

	prefix := base + "."
	rotated := make([]string, 0, len(dirents))
	for _, e := range dirents {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if name == base || !strings.HasPrefix(name, prefix) {
			continue
		}
		// Reject the live file (no extra suffix) defensively.
		if name == base {
			continue
		}
		rotated = append(rotated, name)
	}
	if len(rotated) <= keep {
		return nil
	}

	// Sort lex-desc; the timestamp layout is lex-sortable, so this is
	// also chronological-desc. Newest at index 0.
	sort.Sort(sort.Reverse(sort.StringSlice(rotated)))

	for _, name := range rotated[keep:] {
		if rerr := os.Remove(filepath.Join(dir, name)); rerr != nil && !errors.Is(rerr, stdfs.ErrNotExist) {
			return wrapPathError(opRotatePrune, name, rerr)
		}
	}
	return nil
}
