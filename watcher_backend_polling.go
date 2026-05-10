package fs

import (
	"maps"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// pollingBackend implements [watcherBackend] by stat'ing each
// registered path on a ticker and synthesizing events from
// snapshot diffs. Always available; selected as the fallback when
// no platform-native backend is usable, and explicitly via
// [WithPolling].
type pollingBackend struct {
	interval time.Duration

	mu       sync.Mutex
	paths    map[string]*pollSnapshot       // absolute path → last stat
	dirChild map[string]map[string]struct{} // for watched directories: tracked child names
	out      chan rawWatchEvent
	stop     chan struct{}
	done     chan struct{}
	closed   bool
}

type pollSnapshot struct {
	exists  bool
	isDir   bool
	size    int64
	modTime time.Time
	mode    os.FileMode
}

func newPollingBackend(interval time.Duration) *pollingBackend {
	if interval <= 0 {
		interval = defaultWatcherPollingInterval
	}
	b := &pollingBackend{
		interval: interval,
		paths:    make(map[string]*pollSnapshot),
		dirChild: make(map[string]map[string]struct{}),
		out:      make(chan rawWatchEvent, 64),
		stop:     make(chan struct{}),
		done:     make(chan struct{}),
	}
	go b.loop()
	return b
}

func (b *pollingBackend) AddPath(path string) error {
	abs, err := filepath.Abs(path)
	if err != nil {
		return err //nolint:wrapcheck // outer Watcher wraps via *PathError
	}
	abs = filepath.Clean(abs)

	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return ErrWatcherClosed
	}
	if _, ok := b.paths[abs]; ok {
		return nil // already registered
	}
	snap := snapshotPath(abs)
	b.paths[abs] = snap
	if snap.exists && snap.isDir {
		b.dirChild[abs] = listDirNames(abs)
	}
	return nil
}

func (b *pollingBackend) RemovePath(path string) error {
	abs, err := filepath.Abs(path)
	if err != nil {
		return err //nolint:wrapcheck // outer Watcher wraps via *PathError
	}
	abs = filepath.Clean(abs)

	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.paths, abs)
	delete(b.dirChild, abs)
	return nil
}

func (b *pollingBackend) Events() <-chan rawWatchEvent { return b.out }

func (b *pollingBackend) Close() error {
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		return nil
	}
	b.closed = true
	close(b.stop)
	b.mu.Unlock()

	<-b.done
	close(b.out)
	return nil
}

// loop is the single goroutine that ticks the interval and emits diffs.
func (b *pollingBackend) loop() {
	defer close(b.done)
	ticker := time.NewTicker(b.interval)
	defer ticker.Stop()

	for {
		select {
		case <-b.stop:
			return
		case <-ticker.C:
			b.tick()
		}
	}
}

func (b *pollingBackend) tick() {
	b.mu.Lock()
	// Snapshot the watch set so we don't hold the lock during emits.
	pathSnap := make(map[string]*pollSnapshot, len(b.paths))
	maps.Copy(pathSnap, b.paths)
	dirSnap := make(map[string]map[string]struct{}, len(b.dirChild))
	for p, children := range b.dirChild {
		clone := make(map[string]struct{}, len(children))
		maps.Copy(clone, children)
		dirSnap[p] = clone
	}
	b.mu.Unlock()

	for path, prev := range pathSnap {
		curr := snapshotPath(path)
		op := diffSnapshots(prev, curr)
		if op != 0 {
			b.emit(rawWatchEvent{path: path, op: op})
		}

		// Track child appearance/disappearance for watched directories.
		if prevChildren, isDir := dirSnap[path]; isDir {
			currChildren := map[string]struct{}{}
			if curr.exists && curr.isDir {
				currChildren = listDirNames(path)
			}
			for name := range currChildren {
				if _, had := prevChildren[name]; !had {
					b.emit(rawWatchEvent{path: filepath.Join(path, name), op: WatchCreate})
				}
			}
			for name := range prevChildren {
				if _, still := currChildren[name]; !still {
					b.emit(rawWatchEvent{path: filepath.Join(path, name), op: WatchRemove})
				}
			}
			b.mu.Lock()
			if !b.closed {
				if curr.exists && curr.isDir {
					b.dirChild[path] = currChildren
				}
			}
			b.mu.Unlock()
		}

		b.mu.Lock()
		if !b.closed {
			b.paths[path] = curr
		}
		b.mu.Unlock()
	}
}

func (b *pollingBackend) emit(ev rawWatchEvent) {
	b.mu.Lock()
	closed := b.closed
	b.mu.Unlock()
	if closed {
		return
	}
	select {
	case b.out <- ev:
	default:
		// Drop on overflow; consumer is too slow.
	}
}

// snapshotPath stats path and returns a snapshot. A missing path is
// represented as exists=false rather than an error.
func snapshotPath(path string) *pollSnapshot {
	info, err := os.Lstat(path)
	if err != nil {
		return &pollSnapshot{exists: false}
	}
	return &pollSnapshot{
		exists:  true,
		isDir:   info.IsDir(),
		size:    info.Size(),
		modTime: info.ModTime(),
		mode:    info.Mode(),
	}
}

// diffSnapshots returns a WatchOp bitmask describing how curr differs
// from prev. Returns 0 when nothing changed.
func diffSnapshots(prev, curr *pollSnapshot) WatchOp {
	switch {
	case !prev.exists && !curr.exists:
		return 0
	case !prev.exists && curr.exists:
		return WatchCreate
	case prev.exists && !curr.exists:
		return WatchRemove
	}
	var op WatchOp
	if prev.modTime != curr.modTime || prev.size != curr.size {
		op |= WatchWrite
	}
	if prev.mode.Perm() != curr.mode.Perm() {
		op |= WatchChmod
	}
	return op
}

// listDirNames returns the set of basenames in dir. On error returns
// an empty map (the caller treats this as "no children visible").
func listDirNames(dir string) map[string]struct{} {
	out := map[string]struct{}{}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return out
	}
	for _, e := range entries {
		out[e.Name()] = struct{}{}
	}
	return out
}
