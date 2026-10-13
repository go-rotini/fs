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
	paths    map[string]*pollSnapshot            // absolute path -> last stat
	dirChild map[string]map[string]*pollSnapshot // for watched directories: child name -> last stat
	// statChildren makes each tick stat the children of watched directories, so writes to
	// files inside them are reported. Without it children are only listed, which reports
	// them appearing and disappearing.
	statChildren bool
	// onDrop, when set, is called for every event dropped because out
	// is full.
	onDrop func()
	out    chan rawWatchEvent
	stop   chan struct{}
	done   chan struct{}
	closed bool
}

type pollSnapshot struct {
	exists  bool
	isDir   bool
	size    int64
	modTime time.Time
	mode    os.FileMode
	id      fileID
	hasID   bool
}

func newPollSnapshot(info os.FileInfo) *pollSnapshot {
	id, hasID := fileIdentity(info)
	return &pollSnapshot{
		exists:  true,
		isDir:   info.IsDir(),
		size:    info.Size(),
		modTime: info.ModTime(),
		mode:    info.Mode(),
		id:      id,
		hasID:   hasID,
	}
}

func newPollingBackend(interval time.Duration) *pollingBackend {
	if interval <= 0 {
		interval = defaultWatcherPollingInterval
	}
	b := &pollingBackend{
		interval: interval,
		paths:    make(map[string]*pollSnapshot),
		dirChild: make(map[string]map[string]*pollSnapshot),
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
		b.dirChild[abs] = listDirChildren(abs, b.statChildren)
	}
	return nil
}

// reportChildWrites makes the backend report writes and permission changes to the files
// inside every watched directory, at the cost of one stat per child per tick. Call it before
// the first AddPath.
func (b *pollingBackend) reportChildWrites() {
	b.mu.Lock()
	b.statChildren = true
	b.mu.Unlock()
}

func (b *pollingBackend) RemovePath(path string) error {
	abs, err := filepath.Abs(path)
	if err != nil {
		return err //nolint:wrapcheck // outer Watcher wraps via *PathError
	}
	abs = filepath.Clean(abs)

	b.mu.Lock()
	defer b.mu.Unlock()
	for p := range b.paths {
		if p == abs || hasParentDir(p, abs) {
			delete(b.paths, p)
			delete(b.dirChild, p)
		}
	}
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
	// Child snapshots are replaced, never mutated, so a shallow copy
	// of each child map is enough.
	pathSnap := make(map[string]*pollSnapshot, len(b.paths))
	maps.Copy(pathSnap, b.paths)
	dirSnap := make(map[string]map[string]*pollSnapshot, len(b.dirChild))
	for p, children := range b.dirChild {
		dirSnap[p] = maps.Clone(children)
	}
	statChildren := b.statChildren
	b.mu.Unlock()

	for path, prev := range pathSnap {
		curr := snapshotPath(path)
		op := diffSnapshots(prev, curr)
		if op != 0 {
			b.emit(rawWatchEvent{path: path, op: op})
		}

		// Track child appearance, disappearance and (with
		// statChildren) writes for watched directories.
		prevChildren, isDir := dirSnap[path]
		var currChildren map[string]*pollSnapshot
		if isDir {
			currChildren = map[string]*pollSnapshot{}
			if curr.exists && curr.isDir {
				currChildren = listDirChildren(path, statChildren)
			}
			b.diffChildren(path, prevChildren, currChildren, pathSnap)
		}

		b.mu.Lock()
		// Update only paths still registered: RemovePath may have run
		// since the watch set was copied.
		if _, still := b.paths[path]; still && !b.closed {
			b.paths[path] = curr
			if isDir {
				b.dirChild[path] = currChildren
			}
		}
		b.mu.Unlock()
	}
}

// diffChildren emits create and remove events for dir's children, and write or chmod events
// for child files when both snapshots carry stat data. Children registered as paths of their
// own report their changes themselves and are skipped here.
func (b *pollingBackend) diffChildren(dir string, prev, curr, registered map[string]*pollSnapshot) {
	for name, cs := range curr {
		child := filepath.Join(dir, name)
		ps, had := prev[name]
		if !had {
			b.emit(rawWatchEvent{path: child, op: WatchCreate})
			continue
		}
		if ps == nil || cs == nil || ps.isDir || cs.isDir {
			continue
		}
		if _, own := registered[child]; own {
			continue
		}
		if op := diffSnapshots(ps, cs); op != 0 {
			b.emit(rawWatchEvent{path: child, op: op})
		}
	}
	for name := range prev {
		if _, still := curr[name]; !still {
			b.emit(rawWatchEvent{path: filepath.Join(dir, name), op: WatchRemove})
		}
	}
}

func (b *pollingBackend) emit(ev rawWatchEvent) {
	b.mu.Lock()
	closed, onDrop := b.closed, b.onDrop
	b.mu.Unlock()
	if closed {
		return
	}
	select {
	case b.out <- ev:
	default:
		// Drop on overflow; consumer is too slow.
		if onDrop != nil {
			onDrop()
		}
	}
}

// snapshotPath stats path and returns a snapshot. A missing path is
// represented as exists=false rather than an error.
func snapshotPath(path string) *pollSnapshot {
	info, err := os.Lstat(path)
	if err != nil {
		return &pollSnapshot{exists: false}
	}
	return newPollSnapshot(info)
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
	// A different file renamed over the path with the same size and
	// modification time (cp -p, rsync -t, tar x) is still a write.
	if prev.hasID && curr.hasID && prev.id != curr.id {
		op |= WatchWrite
	}
	if prev.mode.Perm() != curr.mode.Perm() {
		op |= WatchChmod
	}
	return op
}

// listDirChildren returns dir's children by basename. With stat, each
// maps to its snapshot; without, to nil. On error it returns an empty
// map (the caller treats this as "no children visible").
func listDirChildren(dir string, stat bool) map[string]*pollSnapshot {
	out := map[string]*pollSnapshot{}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return out
	}
	for _, e := range entries {
		if !stat {
			out[e.Name()] = nil
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue // removed since the listing; the next tick reports it
		}
		out[e.Name()] = newPollSnapshot(info)
	}
	return out
}
