package fs

import (
	"context"
	"errors"
	"io"
	stdfs "io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const opWatcher = "watcher"

// Sentinel errors for the watcher. They are returned wrapped in a
// [*PathError].
var (
	// ErrWatcherEmptyPath is returned by [NewWatcher] / [NewLazyWatcher]
	// / [NewDirWatcher] when path is empty.
	ErrWatcherEmptyPath = errors.New("fs: watcher: path must not be empty")

	// ErrWatcherNilContext is returned by [Watcher.Subscribe] when
	// ctx is nil.
	ErrWatcherNilContext = errors.New("fs: watcher: nil context")

	// ErrWatcherClosed is returned by [Watcher.Subscribe] when the
	// watcher has already been closed.
	ErrWatcherClosed = errors.New("fs: watcher: closed")
)

// Watcher monitors one path (file or directory) for changes. The
// watcher watches the parent directory and filters by basename, so
// editor atomic-save patterns (write-temp + rename) are detected
// correctly.
//
// Every Watcher polls: it calls [os.Lstat] on its watched paths at the
// configured interval (default 1 second; see [WithPolling]). There is
// no inotify, kqueue or ReadDirectoryChangesW backend.
//
// Construct via [NewWatcher] (existing path), [NewLazyWatcher]
// (path may not exist yet), or [NewDirWatcher] (recursive directory
// watching). Each [Watcher.Subscribe] call returns an independent
// buffered channel (see [WithBufferSize]); events a slow subscriber
// has no room for are dropped, and it is then sent [WatchOverflow].
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

	// lost is set when the backend or debouncer dropped an event, which
	// every subscriber must hear about.
	lost atomic.Bool

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
	lost   bool // events were dropped; WatchOverflow is owed (guarded by Watcher.mu)
}

// overflowRetry is how often a [WatchOverflow] that found no room is
// offered again.
const overflowRetry = 50 * time.Millisecond

// NewWatcher constructs a [*Watcher] for an existing path, made
// absolute. Watching the parent directory with basename filtering
// means atomic-rename saves are observed.
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
// exist yet; its parent directory must exist. The watcher reports
// [WatchCreate] when the path appears.
func NewLazyWatcher(path string, opts ...WatcherOption) (*Watcher, error) {
	if path == "" {
		return nil, wrapPathError(opWatcher, "", ErrWatcherEmptyPath)
	}
	return newWatcher(path, false, true, opts)
}

// NewDirWatcher watches a directory and, by default, every
// subdirectory beneath it, including ones created after it is called;
// pass [WithRecursive](false) to watch only the top level. It reports
// entries appearing ([WatchCreate]) and disappearing ([WatchRemove])
// in watched directories, writes and permission changes to the files
// in them ([WatchWrite], [WatchChmod]), and changes to the directories
// themselves. When a new subdirectory appears, its contents are
// reported as created too. Each poll stats every entry of every
// watched directory, so cost grows with the size of the tree.
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

	w.debouncer.onDrop = w.markLost
	backend := w.newPolling()
	w.backend = backend

	if err := w.registerInitial(lazy); err != nil {
		_ = backend.Close()
		w.debouncer.Close()
		return nil, wrapPathError(opWatcher, path, err)
	}

	go w.worker()
	return w, nil
}

// newPolling returns the polling backend; a directory watcher's also
// reports writes to the files in watched directories.
func (w *Watcher) newPolling() *pollingBackend {
	b := newPollingBackend(w.cfg.pollingInterval)
	b.mu.Lock()
	b.onDrop = w.markLost
	b.mu.Unlock()
	if w.isDir {
		b.reportChildWrites()
	}
	return b
}

// markLost records that an event was dropped before fan-out, so every
// subscriber is sent [WatchOverflow].
func (w *Watcher) markLost() {
	w.lost.Store(true)
}

// registerInitial adds the appropriate paths to the backend.
//
// For file watchers (not isDir), we watch the parent directory and
// filter by basename; this catches atomic-rename saves where the
// new inode replaces the old one.
//
// For directory watchers, we add the directory itself; recursive
// watches add every existing subdirectory at construction time.
//
// Lazy watchers tolerate a missing target; the parent directory
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
// values until ctx is canceled or [Watcher.Close] is called, at which
// point the channel is closed. Each Subscribe creates an independent
// subscription; events a slow subscriber has no room for are dropped
// rather than blocking the watcher, and the subscriber is then sent
// [WatchOverflow].
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

	// pump to debouncer so the main loop can
	// handle workerStop cleanly.
	pumpDone := make(chan struct{})
	go func() {
		defer close(pumpDone)
		for ev := range rawCh {
			if !w.shouldEmit(ev.path) {
				continue
			}
			w.debouncer.In(ev)
			w.followSubdirectory(ev)
		}
	}()
	defer func() { <-pumpDone }()

	for {
		var retry <-chan time.Time
		if w.overflowOwed() {
			retry = time.After(overflowRetry)
		}
		select {
		case <-w.workerStop:
			return
		case ev, ok := <-deboCh:
			if !ok {
				return
			}
			w.fanOut(&WatchEvent{Path: ev.path, Op: ev.op, Time: time.Now()})
		case <-retry:
			w.fanOut(nil)
		}
	}
}

// overflowOwed reports whether some subscriber is still owed a
// [WatchOverflow].
func (w *Watcher) overflowOwed() bool {
	if w.lost.Load() {
		return true
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	for _, s := range w.subs {
		if s.lost {
			return true
		}
	}
	return false
}

// followSubdirectory keeps a recursive directory watch in step with
// the tree: a directory that appears beneath the target is watched,
// with everything already inside it reported as created, and one that
// disappears is no longer watched once the backend has reported what
// was inside it removed.
func (w *Watcher) followSubdirectory(ev rawWatchEvent) {
	if !w.isDir || !w.recursive || ev.path == w.target {
		return
	}
	if ev.op&WatchRemove != 0 && ev.op&WatchCreate == 0 {
		_ = w.backend.RemoveWhenGone(ev.path) //nolint:errcheck // only fails to resolve an absolute path, which ev.path already is
		return
	}
	if ev.op&WatchCreate == 0 {
		return
	}
	if info, err := os.Lstat(ev.path); err != nil || !info.IsDir() {
		return
	}
	// The directory is registered before it is read, so an entry
	// created in between is either listed here or reported by the
	// next poll.
	_ = filepath.WalkDir(ev.path, func(path string, d stdfs.DirEntry, err error) error { //nolint:errcheck // the callback never returns an error
		if err != nil {
			return nil //nolint:nilerr // tolerate inaccessible entries and keep walking
		}
		if d.IsDir() {
			if aerr := w.backend.AddPath(path); aerr != nil {
				w.logger.Debug("watcher: AddPath failed", "path", path, "err", aerr)
			}
		}
		if path != ev.path {
			w.debouncer.In(rawWatchEvent{path: path, op: WatchCreate})
		}
		return nil
	})
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

// hasParentDir reports whether child lives under parent. A child
// equal to parent counts as inside; a child that resolves to ".." or
// a sibling does not. Filenames that incidentally start with two
// dots (e.g., "..foo") are NOT treated as escapes; only a "..\n"
// segment terminated by the path separator (or standalone "..") is.
func hasParentDir(child, parent string) bool {
	rel, err := filepath.Rel(parent, child)
	if err != nil {
		return false
	}
	if rel == "." {
		return true
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return false
	}
	return true
}

// fanOut sends ev (nil to only settle owed overflows) to every
// subscriber. A subscriber that missed events first gets a
// [WatchOverflow]; until that is delivered, its later events are
// dropped too, since it must rescan anyway.
func (w *Watcher) fanOut(ev *WatchEvent) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.lost.Swap(false) {
		for _, s := range w.subs {
			s.lost = true
		}
	}
	for _, s := range w.subs {
		if s.lost {
			select {
			case s.ch <- WatchEvent{Path: w.target, Op: WatchOverflow, Time: time.Now()}:
				s.lost = false
			default:
			}
			if s.lost {
				continue
			}
		}
		if ev == nil {
			continue
		}
		select {
		case s.ch <- *ev:
		case <-s.ctx.Done():
			// Subscription canceled mid-fanout; skip.
		default:
			// Slow subscriber; drop and owe it an overflow.
			s.lost = true
			w.logger.Debug("watcher: dropping event for slow subscriber", "path", ev.Path)
		}
	}
}

// Close stops the watcher and closes every subscription channel.
// Idempotent. It currently always returns nil.
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
		close(s.ch)
	}
	return nil
}
