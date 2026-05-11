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

	// rotateTimestampLayout is lex-sortable (and chronologically so),
	// allowing retention pruning to sort by name.
	rotateTimestampLayout = "20060102-150405.000"

	rotateGzipExt              = ".gz"
	rotateFilePerm os.FileMode = 0o644
)

// ErrRotatorClosed is returned by [Rotator.Write] after [Rotator.Close]
// has been called.
var ErrRotatorClosed = errors.New("fs: rotator: closed")

// Rotator is an [io.WriteCloser] that rotates its backing file when
// a size or age threshold is exceeded. Rotation renames the current
// file with a sortable timestamp suffix (optionally gzipping it) and
// reopens the original path fresh.
//
// Rotator is safe for concurrent Write calls. Within a process,
// writes are serialized via an internal mutex. Across processes, the
// backing file is opened with O_APPEND so writes don't interleave
// inside a single call, and the size check on each Write consults
// the actual on-disk file size, so two processes sharing the same
// path cooperate on the byte threshold.
//
// Age-based rotation remains process-local: each Rotator tracks the
// time it opened the current file. Coordinate manual [Rotator.Rotate]
// calls if cross-process age coordination matters.
type Rotator struct {
	path string
	cfg  rotatorConfig

	mu       sync.Mutex
	f        *os.File
	curBytes int64
	openedAt time.Time
	closed   bool
}

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
// The check runs before each Write; an oversized Write is accepted
// in full and triggers rotation immediately afterward. Zero disables
// size-based rotation.
func WithRotateMaxBytes(n int64) RotatorOption {
	return func(c *rotatorConfig) {
		c.maxBytes = n
	}
}

// WithRotateMaxAge rotates the file when the time since the current
// file was opened exceeds d. Zero disables age-based rotation.
func WithRotateMaxAge(d time.Duration) RotatorOption {
	return func(c *rotatorConfig) {
		c.maxAge = d
	}
}

// WithRotateKeep retains the n most recent rotated files; older ones
// are removed after every rotation. Zero keeps every rotated file.
// The live file is not counted.
func WithRotateKeep(n int) RotatorOption {
	return func(c *rotatorConfig) {
		c.keep = n
	}
}

// WithRotateCompress controls gzip compression of rotated files
// after the move. Compression runs synchronously after the rename;
// callers needing non-blocking compression should rotate in a
// background goroutine.
func WithRotateCompress(b bool) RotatorOption {
	return func(c *rotatorConfig) {
		c.compress = b
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

// Write appends p to the backing file. Before the append, the
// current size and age are checked against the configured
// thresholds; if either is exceeded the file is rotated and a fresh
// one is opened. Returns len(p) on success.
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
// return [ErrRotatorClosed]. Idempotent.
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
// Useful for SIGHUP-style "rotate now" semantics.
func (r *Rotator) Rotate() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.closed {
		return wrapPathError(opRotate, r.path, ErrRotatorClosed)
	}
	return r.rotateLocked()
}

// shouldRotateLocked reports whether the next Write would push past
// a configured threshold. The size check consults the actual on-disk
// file size via Stat so cross-process writers cooperate; if Stat
// fails, the local counter is used as a fallback.
func (r *Rotator) shouldRotateLocked(pendingBytes int64) bool {
	size := r.curBytes
	if r.f != nil {
		if info, err := r.f.Stat(); err == nil {
			size = info.Size()
		}
	}
	if size == 0 {
		// An empty file would otherwise rotate into an endless stream
		// of zero-byte artifacts.
		return false
	}
	if r.cfg.maxBytes > 0 && size+pendingBytes > r.cfg.maxBytes {
		return true
	}
	if r.cfg.maxAge > 0 && r.cfg.nowFn().Sub(r.openedAt) >= r.cfg.maxAge {
		return true
	}
	return false
}

func (r *Rotator) rotateLocked() error {
	if r.f != nil {
		if err := r.f.Close(); err != nil {
			return wrapPathError(opRotateClose, r.path, err)
		}
		r.f = nil
	}

	rotated := r.rotatedName()
	if err := os.Rename(r.path, rotated); err != nil {
		// Tolerate the case where another rotator already pulled the
		// file out from under us; fall through to reopening at path.
		if !errors.Is(err, stdfs.ErrNotExist) {
			return wrapPathError(opRotateRename, r.path, err)
		}
	} else {
		logger().Debug("fs.rotate: rotated", "from", r.path, "to", rotated)
		if r.cfg.compress {
			if cerr := compressRotatedFile(rotated); cerr != nil {
				return cerr
			}
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

func (r *Rotator) rotatedName() string {
	stamp := r.cfg.nowFn().UTC().Format(rotateTimestampLayout)
	return fmt.Sprintf("%s.%s", r.path, stamp)
}

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

// compressRotatedFile gzips rotated to <rotated>.gz and removes the
// original on success. On any failure the uncompressed file is left
// intact for recovery.
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

// pruneRotated enumerates rotated siblings of path, sorts them
// newest-first, and removes everything beyond the keep-most-recent
// threshold. The timestamp layout is lex-sortable, so name-sort and
// chronological-sort coincide.
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
		rotated = append(rotated, name)
	}
	if len(rotated) <= keep {
		return nil
	}

	sort.Sort(sort.Reverse(sort.StringSlice(rotated)))

	for _, name := range rotated[keep:] {
		if rerr := os.Remove(filepath.Join(dir, name)); rerr != nil && !errors.Is(rerr, stdfs.ErrNotExist) {
			return wrapPathError(opRotatePrune, name, rerr)
		}
	}
	return nil
}
