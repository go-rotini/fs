package fs

import (
	"context"
	"errors"
	"io"
	stdfs "io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const opWatcher = "watcher"

// Sentinel errors for the watcher surface. Wrapped via *PathError at
// the cross-platform shell entry points.
var (
	// ErrWatcherEmptyPath is returned by [NewWatcher] / [NewLazyWatcher]
	// / [NewDirWatcher] when path is empty.
	ErrWatcherEmptyPath = errors.New("fs: watcher: path must not be empty")

	// ErrWatcherNilContext is returned by [(*Watcher).Subscribe] when
	// ctx is nil.
	ErrWatcherNilContext = errors.New("fs: watcher: nil context")

	// ErrWatcherClosed is returned by [(*Watcher).Subscribe] when the
	// watcher has already been closed.
	ErrWatcherClosed = errors.New("fs: watcher: closed")

	// ErrWatcherUnsupportedOS is returned when no native backend is
	// available and polling cannot be used either. In practice the
	// package always succeeds with the polling fallback, so this
	// sentinel is reserved for future use.
	ErrWatcherUnsupportedOS = errors.New("fs: watcher: native backend unavailable on this platform — use WithPolling")
)

// Watcher monitors one path (file or directory) for changes. The
// watcher watches the parent directory and filters by basename, so
// editor atomic-save patterns (write-temp + rename) are detected
// correctly.
//
// In v0.1 every Watcher uses the polling backend regardless of
// platform — `os.Lstat` in a loop at the configured interval
// (default 1 second; override via [WithPolling]). The
// platform-native backends (inotify on Linux, kqueue on macOS/BSD,
// ReadDirectoryChangesW on Windows) are scheduled for a follow-up
// release; the API will not change when they land.
//
// Construct via [NewWatcher] (existing path), [NewLazyWatcher]
// (path may not exist yet), or [NewDirWatcher] (recursive directory
// watching). Each [(*Watcher).Subscribe] call returns an
// independent buffered channel; surplus events on a slow subscriber
// are dropped.
type Watcher struct {
	target    string // absolute, cleaned
	parentDir string // absolute, cleaned (filepath.Dir(target))
	basename  string // filepath.Base(target); "" when target is itself a directory being watched recursively
	isDir     bool   // true for NewDirWatcher
	recursive bool
	cfg       watcherOptions
	logger    *slog.Logger

	backend   watcherBackend
	debouncer *debouncer

	mu         sync.Mutex
	closed     bool
	subs       []*watcherSub
	workerStop chan struct{}
	workerDone chan struct{}
}

type watcherSub struct {
	ch     chan WatchEvent
	ctx    context.Context
	cancel context.CancelFunc
}

// NewWatcher constructs a [*Watcher] for an existing path. The path
// is resolved to an absolute path; parent-directory watching plus
// basename filtering means atomic-rename saves are observed
// correctly on every backend.
func NewWatcher(path string, opts ...WatcherOption) (*Watcher, error) {
	if path == "" {
		return nil, wrapPathError(opWatcher, "", ErrWatcherEmptyPath)
	}
	if _, err := os.Stat(path); err != nil {
		return nil, wrapPathError(opWatcher, path, err)
	}
	return newWatcher(path, false, false, opts)
}

// NewLazyWatcher constructs a [*Watcher] for a path that may not
// exist yet. The watcher fires `WatchCreate` on the path when it
// appears.
func NewLazyWatcher(path string, opts ...WatcherOption) (*Watcher, error) {
	if path == "" {
		return nil, wrapPathError(opWatcher, "", ErrWatcherEmptyPath)
	}
	return newWatcher(path, false, true, opts)
}

// NewDirWatcher watches an entire directory tree. Use
// [WithRecursive(false)] to limit to a single directory level.
func NewDirWatcher(path string, opts ...WatcherOption) (*Watcher, error) {
	if path == "" {
		return nil, wrapPathError(opWatcher, "", ErrWatcherEmptyPath)
	}
	info, err := os.Stat(path)
	if err != nil {
		return nil, wrapPathError(opWatcher, path, err)
	}
	if !info.IsDir() {
		return nil, wrapPathError(opWatcher, path, ErrNotDir)
	}
	return newWatcher(path, true, false, opts)
}

func newWatcher(path string, isDir, lazy bool, opts []WatcherOption) (*Watcher, error) {
	cfg := newWatcherOptions(opts)

	logger := cfg.logger
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}

	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, wrapPathError(opWatcher, path, err)
	}
	abs = filepath.Clean(abs)

	w := &Watcher{
		target:     abs,
		parentDir:  filepath.Dir(abs),
		basename:   filepath.Base(abs),
		isDir:      isDir,
		recursive:  cfg.recursive,
		cfg:        cfg,
		logger:     logger,
		debouncer:  newDebouncer(cfg.debounce),
		workerStop: make(chan struct{}),
		workerDone: make(chan struct{}),
	}
	if isDir {
		w.basename = "" // directory watch matches everything beneath target
	}

	backend, err := w.selectBackend()
	if err != nil {
		w.debouncer.Close()
		return nil, wrapPathError(opWatcher, path, err)
	}
	w.backend = backend

	if err := w.registerInitial(lazy); err != nil {
		_ = backend.Close()
		w.debouncer.Close()
		return nil, wrapPathError(opWatcher, path, err)
	}

	go w.worker()
	return w, nil
}

// selectBackend returns the native backend if available, or falls
// back to polling. WithPolling forces polling regardless of
// availability.
func (w *Watcher) selectBackend() (watcherBackend, error) {
	if w.cfg.forcePolling {
		return newPollingBackend(w.cfg.pollingInterval), nil
	}
	native, err := newNativeWatcherBackend(w.cfg)
	if err == nil {
		return native, nil
	}
	if !errors.Is(err, errWatcherUnsupportedBackend) {
		return nil, err
	}
	w.logger.Debug("watcher: native backend unavailable, falling back to polling", "err", err)
	interval := w.cfg.pollingInterval
	if interval <= 0 {
		interval = defaultWatcherPollingInterval
	}
	return newPollingBackend(interval), nil
}

// registerInitial adds the appropriate paths to the backend.
//
// For file watchers (not isDir), we watch the parent directory and
// filter by basename — this catches atomic-rename saves where the
// new inode replaces the old one.
//
// For directory watchers, we add the directory itself; recursive
// watches add every existing subdirectory at construction time.
//
// Lazy watchers tolerate a missing target — the parent directory
// must still exist.
func (w *Watcher) registerInitial(lazy bool) error {
	// All errors here are wrapped at the newWatcher call site via
	// wrapPathError(opWatcher, ...); the //nolint:wrapcheck markers
	// reflect that.
	if w.isDir {
		if err := w.backend.AddPath(w.target); err != nil {
			return err //nolint:wrapcheck // outer newWatcher wraps
		}
		if w.recursive {
			return w.addSubdirectoriesUnder(w.target)
		}
		return nil
	}

	// File watcher: watch the parent (so dir-child diffs catch
	// create/remove/rename) AND the file itself (so its own
	// mtime/size diff catches in-place writes). Lazy watchers
	// register the missing target; the polling backend's snapshot
	// for an absent path is exists=false, and the appearance
	// transition fires WatchCreate naturally.
	if !lazy {
		if _, err := os.Stat(w.target); err != nil {
			return err //nolint:wrapcheck // outer newWatcher wraps
		}
	}
	if _, err := os.Stat(w.parentDir); err != nil {
		return err //nolint:wrapcheck // outer newWatcher wraps
	}
	if err := w.backend.AddPath(w.parentDir); err != nil {
		return err //nolint:wrapcheck // outer newWatcher wraps
	}
	if err := w.backend.AddPath(w.target); err != nil {
		return err //nolint:wrapcheck // outer newWatcher wraps
	}
	return nil
}

func (w *Watcher) addSubdirectoriesUnder(root string) error {
	//nolint:wrapcheck // outer newWatcher wraps the WalkDir error chain
	return filepath.WalkDir(root, func(path string, d stdfs.DirEntry, err error) error {
		if err != nil {
			//nolint:nilerr // intentional: tolerate inaccessible subdirs and keep walking
			return nil
		}
		if !d.IsDir() || path == root {
			return nil
		}
		if aerr := w.backend.AddPath(path); aerr != nil {
			w.logger.Debug("watcher: AddPath failed", "path", path, "err", aerr)
		}
		return nil
	})
}

// Subscribe returns a buffered channel that receives [WatchEvent]
// values until ctx is canceled or [(*Watcher).Close] is called. Each
// Subscribe creates an independent subscription; surplus events on
// a slow subscriber are dropped rather than blocking the worker.
func (w *Watcher) Subscribe(ctx context.Context) (<-chan WatchEvent, error) {
	if ctx == nil {
		return nil, wrapPathError(opWatcher, w.target, ErrWatcherNilContext)
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return nil, wrapPathError(opWatcher, w.target, ErrWatcherClosed)
	}

	subCtx, cancel := context.WithCancel(ctx)
	sub := &watcherSub{
		ch:     make(chan WatchEvent, w.cfg.bufferSize),
		ctx:    subCtx,
		cancel: cancel,
	}
	w.subs = append(w.subs, sub)

	// Auto-remove when ctx fires.
	go func() {
		<-sub.ctx.Done()
		w.removeSub(sub)
	}()

	return sub.ch, nil
}

func (w *Watcher) removeSub(sub *watcherSub) {
	w.mu.Lock()
	defer w.mu.Unlock()
	for i, s := range w.subs {
		if s == sub {
			w.subs = append(w.subs[:i], w.subs[i+1:]...)
			close(s.ch)
			return
		}
	}
}

// worker pulls events from the backend through the debouncer and
// fans them out to every subscription. The worker exits when
// workerStop is closed (via Close) or when the backend's Events
// channel is closed.
func (w *Watcher) worker() {
	defer close(w.workerDone)
	rawCh := w.backend.Events()
	deboCh := w.debouncer.Out()

	// Pump backend → debouncer in a sub-goroutine so the main loop can
	// also handle workerStop cleanly.
	pumpDone := make(chan struct{})
	go func() {
		defer close(pumpDone)
		for ev := range rawCh {
			if w.shouldEmit(ev.path) {
				w.debouncer.In(ev)
			}
		}
	}()

	for {
		select {
		case <-w.workerStop:
			return
		case ev, ok := <-deboCh:
			if !ok {
				return
			}
			w.fanOut(WatchEvent{Path: ev.path, Op: ev.op, Time: time.Now()})
		}
	}
}

// shouldEmit applies the basename filter for file watchers. For
// directory watchers all events under target are forwarded.
func (w *Watcher) shouldEmit(path string) bool {
	if w.isDir {
		// Pass through any path that's the dir itself or beneath it.
		return path == w.target || hasParentDir(path, w.target)
	}
	if path == w.target {
		return true
	}
	return filepath.Base(path) == w.basename && filepath.Dir(path) == w.parentDir
}

// hasParentDir reports whether child lives under parent.
func hasParentDir(child, parent string) bool {
	rel, err := filepath.Rel(parent, child)
	if err != nil {
		return false
	}
	if rel == "." || rel == ".." {
		return rel == "."
	}
	return rel[0] != '.' || (len(rel) >= 2 && rel[1] != '.')
}

func (w *Watcher) fanOut(ev WatchEvent) {
	w.mu.Lock()
	subs := make([]*watcherSub, len(w.subs))
	copy(subs, w.subs)
	w.mu.Unlock()
	for _, s := range subs {
		select {
		case s.ch <- ev:
		case <-s.ctx.Done():
			// Subscription canceled mid-fanout; skip.
		default:
			// Slow subscriber; drop.
			w.logger.Debug("watcher: dropping event for slow subscriber", "path", ev.Path)
		}
	}
}

// Close releases all resources. Idempotent — repeated calls are
// no-ops.
func (w *Watcher) Close() error {
	w.mu.Lock()
	if w.closed {
		w.mu.Unlock()
		return nil
	}
	w.closed = true
	subs := w.subs
	w.subs = nil
	w.mu.Unlock()

	for _, s := range subs {
		s.cancel()
	}

	close(w.workerStop)
	if err := w.backend.Close(); err != nil {
		w.logger.Debug("watcher: backend Close error", "err", err)
	}
	w.debouncer.Close()
	<-w.workerDone

	for _, s := range subs {
		// Channels close when removeSub runs via ctx-cancel; if a sub
		// missed it (unlikely), drain to avoid leaks.
		select {
		case <-s.ch:
		default:
		}
	}
	return nil
}
