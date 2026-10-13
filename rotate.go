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
	"strconv"
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

	// rotateRetryDelay is how long Write waits to try a threshold
	// rotation again after one fails.
	rotateRetryDelay = time.Second
)

// ErrRotatorClosed is returned by [Rotator.Write] after [Rotator.Close]
// has been called.
var ErrRotatorClosed = errors.New("fs: rotator: closed")

// Rotator is an [io.WriteCloser] that rotates its backing file when
// a size or age threshold is exceeded. Rotation renames the current
// file to "<path>.<UTC timestamp>" (layout 20060102-150405.000, plus
// ".gz" with [WithRotateCompress]) and reopens path fresh. When that
// name is taken (two rotations in the same millisecond), a sequence
// number is added ("_0001", "_0002", ...), so names still sort in
// rotation order and no rotated file is overwritten. An empty file is
// never rotated automatically.
//
// Rotator is safe for concurrent Write calls. Within a process,
// writes are serialized via an internal mutex. Across processes, the
// backing file is opened with O_APPEND so writes don't interleave
// inside a single call. Before each Write the Rotator checks that
// path still names the file it holds; if another process rotated it,
// the Rotator reopens path first, so its writes land in the live file
// and the size check sees that file's size.
//
// Age-based rotation remains process-local: each Rotator tracks the
// time it opened the current file. Coordinate manual [Rotator.Rotate]
// calls if cross-process age coordination matters.
type Rotator struct {
	path string
	cfg  rotatorConfig

	mu       sync.Mutex
	f        *os.File
	held     stdfs.FileInfo // f's identity, from when it was opened
	curBytes int64
	openedAt time.Time
	retryAt  time.Time // after a failed threshold rotation, when Write tries again
	closed   bool
}

type rotatorConfig struct {
	maxBytes int64
	maxAge   time.Duration
	keep     int
	compress bool
	perm     os.FileMode
	nowFn    func() time.Time
}

// WithRotatePerm sets the mode for the files the Rotator creates: the
// live file and compressed rotated files. Default 0o644. Use 0o600 for
// logs that may hold secrets.
func WithRotatePerm(mode os.FileMode) RotatorOption {
	return func(c *rotatorConfig) {
		c.perm = mode.Perm()
	}
}

// RotatorOption configures [NewRotator].
type RotatorOption func(*rotatorConfig)

// WithRotateMaxBytes rotates the file before a Write that would take
// its size past n. A single Write larger than n still goes whole into
// the fresh file. Zero disables size-based rotation.
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
// The live file is not counted, but every other file in its directory
// named "<base>.<suffix>" is, so keep unrelated files out of that
// name space.
func WithRotateKeep(n int) RotatorOption {
	return func(c *rotatorConfig) {
		c.keep = n
	}
}

// WithRotateCompress gzips each rotated file after the rename. It runs
// synchronously inside the Write or [Rotator.Rotate] that triggered
// the rotation.
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

// NewRotator opens path for appending, creating it (mode 0o644, see
// [WithRotatePerm]) and any missing parent directories (mode 0o755)
// if absent. Subsequent
// [Rotator.Write] calls append to the file and trigger rotation when
// size or age thresholds are crossed. An empty path returns an error
// wrapping [ErrInvalidPath].
func NewRotator(path string, opts ...RotatorOption) (*Rotator, error) {
	if path == "" {
		return nil, wrapPathError(opRotateOpen, path, ErrInvalidPath)
	}

	cfg := rotatorConfig{nowFn: time.Now, perm: rotateFilePerm}
	for _, opt := range opts {
		opt(&cfg)
	}
	if cfg.nowFn == nil {
		cfg.nowFn = time.Now
	}

	if err := MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}

	f, info, err := openRotatedFile(path, cfg.perm)
	if err != nil {
		return nil, err
	}
	return &Rotator{
		path:     path,
		cfg:      cfg,
		f:        f,
		held:     info,
		curBytes: info.Size(),
		openedAt: cfg.nowFn(),
	}, nil
}

// Write appends p to the backing file. Before the append, the
// current size and age are checked against the configured
// thresholds; if either is exceeded the file is rotated and a fresh
// one is opened first. Returns len(p) on success.
//
// If that rotation fails, p is still appended to the file at path and
// Write returns len(p) with the rotation error. Writes in the next
// second append without trying the rotation; the first Write after
// that tries it again.
func (r *Rotator) Write(p []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.closed {
		return 0, wrapPathError(opRotateWrite, r.path, ErrRotatorClosed)
	}

	var rotErr error
	if r.shouldRotateLocked(int64(len(p))) && !r.cfg.nowFn().Before(r.retryAt) {
		if rotErr = r.rotateLocked(); rotErr != nil {
			r.retryAt = r.cfg.nowFn().Add(rotateRetryDelay)
		}
	}
	if r.f == nil {
		if err := r.reopenLocked(); err != nil {
			return 0, errors.Join(rotErr, err)
		}
	}

	n, err := r.f.Write(p)
	r.curBytes += int64(n)
	if err != nil {
		return n, errors.Join(rotErr, wrapPathError(opRotateWrite, r.path, err))
	}
	return n, rotErr
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
// Useful for SIGHUP-style "rotate now" semantics. If the rotation
// fails, the Rotator keeps writing to the file at path.
func (r *Rotator) Rotate() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.closed {
		return wrapPathError(opRotate, r.path, ErrRotatorClosed)
	}
	return r.rotateLocked()
}

// followPathLocked makes sure the held file is still the one at path.
// If another process rotated or removed it, path is reopened. It
// returns the live file's size, or the local counter if it can't be
// determined.
func (r *Rotator) followPathLocked() int64 {
	if r.f == nil {
		return r.curBytes
	}
	// A file's identity never changes, so the one recorded at open
	// stands in for an fstat: when path still names the held file, its
	// stat is the held file's stat.
	if cur, serr := os.Stat(r.path); serr == nil && os.SameFile(r.held, cur) {
		return cur.Size()
	}
	if err := r.reopenLocked(); err != nil {
		if held, serr := r.f.Stat(); serr == nil {
			return held.Size()
		}
		return r.curBytes
	}
	return r.curBytes
}

// reopenLocked opens path and makes it the held file, closing the
// previous one.
func (r *Rotator) reopenLocked() error {
	f, info, err := openRotatedFile(r.path, r.cfg.perm)
	if err != nil {
		return err
	}
	if r.f != nil {
		_ = r.f.Close()
	}
	r.f = f
	r.held = info
	r.curBytes = info.Size()
	r.openedAt = r.cfg.nowFn()
	return nil
}

// shouldRotateLocked reports whether the next Write would push past
// a configured threshold, after following a rotation made by another
// process.
func (r *Rotator) shouldRotateLocked(pendingBytes int64) bool {
	size := r.followPathLocked()
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

// rotateLocked closes the held file, renames it aside, compresses and
// prunes, then opens path fresh. path is reopened on every failure
// too, so a failed rotation leaves the Rotator writing to the file at
// path.
func (r *Rotator) rotateLocked() (err error) {
	held, openedAt := r.held, r.openedAt
	defer func() {
		if err != nil && r.f == nil && r.reopenLocked() == nil && os.SameFile(held, r.held) {
			r.openedAt = openedAt // not rotated: the file keeps its age
		}
	}()
	if r.f != nil {
		cerr := r.f.Close()
		r.f = nil
		if cerr != nil {
			return wrapPathError(opRotateClose, r.path, cerr)
		}
	}

	rotated := r.rotatedName()
	if err := osRename(r.path, rotated); err != nil {
		// Tolerate the case where another rotator already pulled the
		// file out from under us; fall through to reopening at path.
		if !errors.Is(err, stdfs.ErrNotExist) {
			return wrapPathError(opRotateRename, r.path, err)
		}
	} else {
		logger().Debug("fs.rotate: rotated", "from", r.path, "to", rotated)
		if r.cfg.compress {
			if cerr := compressRotatedFile(rotated, r.cfg.perm); cerr != nil {
				return cerr
			}
		}
	}

	if r.cfg.keep > 0 {
		if perr := pruneRotated(r.path, r.cfg.keep); perr != nil {
			return perr
		}
	}

	return r.reopenLocked()
}

// rotatedName returns "<path>.<stamp>", or, when a rotated file with
// that stamp exists (with or without ".gz"), "<path>.<stamp>_NNNN"
// numbered one above the highest present. "_" sorts after "." so a
// numbered name sorts after the plain one, with or without ".gz", and
// numbering past the highest keeps the order after pruning.
func (r *Rotator) rotatedName() string {
	base := fmt.Sprintf("%s.%s", r.path, r.cfg.nowFn().UTC().Format(rotateTimestampLayout))
	baseName := filepath.Base(base)
	ents, err := os.ReadDir(filepath.Dir(base))
	if err != nil {
		return base
	}
	next := 0
	for _, e := range ents {
		name := strings.TrimSuffix(e.Name(), rotateGzipExt)
		switch {
		case name == baseName:
			next = max(next, 1)
		case strings.HasPrefix(name, baseName+"_"):
			if n, aerr := strconv.Atoi(name[len(baseName)+1:]); aerr == nil && n >= next {
				next = n + 1
			}
		}
	}
	if next == 0 {
		return base
	}
	return fmt.Sprintf("%s_%04d", base, next)
}

func openRotatedFile(path string, perm os.FileMode) (*os.File, stdfs.FileInfo, error) {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_APPEND, perm)
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
func compressRotatedFile(rotated string, perm os.FileMode) error {
	src, err := os.Open(rotated)
	if err != nil {
		return wrapPathError(opRotateCompr, rotated, err)
	}

	dst := rotated + rotateGzipExt
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, perm)
	if err != nil {
		_ = src.Close()
		return wrapPathError(opRotateCompr, dst, err)
	}
	gw := gzip.NewWriter(out)

	_, copyErr := io.Copy(gw, src)
	// Close the source as soon as the copy is done: Windows refuses to
	// remove a file that still has an open handle, and the remove below
	// is the whole point of the rotation.
	_ = src.Close()
	if copyErr != nil {
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
