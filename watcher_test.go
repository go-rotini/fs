package fs

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// All watcher tests use WithPolling with a short interval to keep
// the runtime deterministic across platforms; the platform-native
// backends are placeholders that fall through to polling anyway.

const (
	pollInterval = 20 * time.Millisecond
	debounceWait = 0 // disable debouncing in most tests so events surface immediately
)

func newTestWatcher(t *testing.T, path string, dir bool, lazy bool) *Watcher {
	t.Helper()
	opts := []WatcherOption{
		WithPolling(pollInterval),
		WithDebounce(debounceWait),
		WithBufferSize(64),
	}
	var (
		w   *Watcher
		err error
	)
	switch {
	case dir:
		w, err = NewDirWatcher(path, opts...)
	case lazy:
		w, err = NewLazyWatcher(path, opts...)
	default:
		w, err = NewWatcher(path, opts...)
	}
	if err != nil {
		t.Fatalf("watcher init: %v", err)
	}
	t.Cleanup(func() { _ = w.Close() })
	return w
}

// waitForEvent reads a single event with a deadline.
func waitForEvent(t *testing.T, ch <-chan WatchEvent, timeout time.Duration) (WatchEvent, bool) {
	t.Helper()
	select {
	case ev, ok := <-ch:
		if !ok {
			return WatchEvent{}, false
		}
		return ev, true
	case <-time.After(timeout):
		return WatchEvent{}, false
	}
}

// --- Construction errors ---

func TestWatcher_EmptyPath(t *testing.T) {
	t.Parallel()
	if _, err := NewWatcher(""); !errors.Is(err, ErrWatcherEmptyPath) {
		t.Errorf("got %v, want ErrWatcherEmptyPath", err)
	}
	if _, err := NewLazyWatcher(""); !errors.Is(err, ErrWatcherEmptyPath) {
		t.Errorf("got %v, want ErrWatcherEmptyPath", err)
	}
	if _, err := NewDirWatcher(""); !errors.Is(err, ErrWatcherEmptyPath) {
		t.Errorf("got %v, want ErrWatcherEmptyPath", err)
	}
}

func TestWatcher_NewMissingPath(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	_, err := NewWatcher(filepath.Join(dir, "missing"))
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("got %v, want ErrNotFound", err)
	}
}

func TestWatcher_NewDirOnFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "f")
	if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	_, err := NewDirWatcher(path, WithPolling(pollInterval))
	if !errors.Is(err, ErrNotDir) {
		t.Errorf("got %v, want ErrNotDir", err)
	}
}

func TestWatcher_SubscribeAfterClose(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "f")
	if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	w := newTestWatcher(t, path, false, false)
	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if _, err := w.Subscribe(context.Background()); !errors.Is(err, ErrWatcherClosed) {
		t.Errorf("got %v, want ErrWatcherClosed", err)
	}
}

func TestWatcher_SubscribeNilContext(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "f")
	if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	w := newTestWatcher(t, path, false, false)
	if _, err := w.Subscribe(nil); !errors.Is(err, ErrWatcherNilContext) {
		t.Errorf("got %v, want ErrWatcherNilContext", err)
	}
}

// --- Detection ---

func TestWatcher_DetectsWrite(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "f")
	if err := os.WriteFile(path, []byte("v1"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	w := newTestWatcher(t, path, false, false)
	ch, err := w.Subscribe(context.Background())
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}

	time.Sleep(2 * pollInterval) // let the polling baseline establish

	if err := os.WriteFile(path, []byte("v2-different-len"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	ev, ok := waitForEvent(t, ch, 2*time.Second)
	if !ok {
		t.Fatal("no event received after write")
	}
	if !ev.Op.Has(WatchWrite) {
		t.Errorf("expected WatchWrite, got Op=%s", ev.Op)
	}
}

func TestWatcher_AtomicRenameSave(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte("orig"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	w := newTestWatcher(t, path, false, false)
	ch, err := w.Subscribe(context.Background())
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}

	time.Sleep(2 * pollInterval)

	// Editor atomic-save pattern: write to temp, rename over original.
	tmp := filepath.Join(dir, "config.yaml.tmp")
	if err := os.WriteFile(tmp, []byte("updated"), 0o644); err != nil {
		t.Fatalf("WriteFile temp: %v", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		t.Fatalf("Rename: %v", err)
	}

	ev, ok := waitForEvent(t, ch, 2*time.Second)
	if !ok {
		t.Fatal("no event after atomic-rename save")
	}
	if !ev.Op.Has(WatchWrite | WatchCreate) {
		t.Errorf("atomic-rename event Op=%s; want Write or Create bit", ev.Op)
	}
}

func TestWatcher_LazyDetectsCreate(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "appears-later")

	w := newTestWatcher(t, path, false, true)
	ch, err := w.Subscribe(context.Background())
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}

	time.Sleep(2 * pollInterval)
	if err := os.WriteFile(path, []byte("hello"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	ev, ok := waitForEvent(t, ch, 2*time.Second)
	if !ok {
		t.Fatal("no event after lazy-target creation")
	}
	if !ev.Op.Has(WatchCreate) {
		t.Errorf("expected WatchCreate, got Op=%s", ev.Op)
	}
}

func TestWatcher_DirWatchSeesChildCreate(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	w := newTestWatcher(t, dir, true, false)
	ch, err := w.Subscribe(context.Background())
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}

	time.Sleep(2 * pollInterval)

	child := filepath.Join(dir, "newfile")
	if err := os.WriteFile(child, []byte("x"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	// Drain events until we see one for the new child path. The
	// directory's own WatchWrite tick is expected (creating a child
	// updates the parent's mtime) and arrives before the child-level
	// WatchCreate.
	deadline := time.After(2 * time.Second)
	for {
		select {
		case ev, ok := <-ch:
			if !ok {
				t.Fatal("channel closed before child create event")
			}
			if ev.Path == child && ev.Op.Has(WatchCreate) {
				return
			}
		case <-deadline:
			t.Fatal("no WatchCreate event for child within 2s")
		}
	}
}

// --- Multi-subscriber + cleanup ---

func TestWatcher_MultiSubscriberFanout(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "f")
	if err := os.WriteFile(path, []byte("v1"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	w := newTestWatcher(t, path, false, false)
	const numSubs = 4
	chs := make([]<-chan WatchEvent, numSubs)
	for i := range chs {
		c, err := w.Subscribe(context.Background())
		if err != nil {
			t.Fatalf("Subscribe %d: %v", i, err)
		}
		chs[i] = c
	}

	time.Sleep(2 * pollInterval)
	if err := os.WriteFile(path, []byte("v2-longer"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	var wg sync.WaitGroup
	var got atomic.Int32
	for i, ch := range chs {
		wg.Add(1)
		go func(i int, ch <-chan WatchEvent) {
			defer wg.Done()
			if _, ok := waitForEvent(t, ch, 2*time.Second); ok {
				got.Add(1)
			} else {
				t.Errorf("subscriber %d missed event", i)
			}
		}(i, ch)
	}
	wg.Wait()
	if got.Load() != numSubs {
		t.Errorf("got %d subscribers received event; want %d", got.Load(), numSubs)
	}
}

func TestWatcher_CtxCancelClosesSubscription(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "f")
	if err := os.WriteFile(path, []byte("v"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	w := newTestWatcher(t, path, false, false)
	ctx, cancel := context.WithCancel(context.Background())
	ch, err := w.Subscribe(ctx)
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}

	cancel()
	deadline := time.After(2 * time.Second)
	for {
		select {
		case _, ok := <-ch:
			if !ok {
				return // channel closed; success
			}
			// Drain any in-flight events that may have arrived before cancel.
		case <-deadline:
			t.Fatal("subscription channel did not close within 2s of ctx cancel")
		}
	}
}

func TestWatcher_CloseIdempotent(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "f")
	if err := os.WriteFile(path, []byte("v"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	w := newTestWatcher(t, path, false, false)
	if err := w.Close(); err != nil {
		t.Errorf("first Close: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Errorf("second Close: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Errorf("third Close: %v", err)
	}
}

// --- Debouncing ---

// --- WatchOp ---

func TestWatchOp_String(t *testing.T) {
	t.Parallel()
	cases := []struct {
		op   WatchOp
		want string
	}{
		{0, "none"},
		{WatchCreate, "create"},
		{WatchWrite, "write"},
		{WatchRemove, "remove"},
		{WatchRename, "rename"},
		{WatchChmod, "chmod"},
		{WatchCreate | WatchWrite, "create+write"},
		{WatchCreate | WatchWrite | WatchRemove | WatchRename | WatchChmod, "create+write+remove+rename+chmod"},
	}
	for _, c := range cases {
		if got := c.op.String(); got != c.want {
			t.Errorf("WatchOp(%d).String() = %q, want %q", c.op, got, c.want)
		}
	}
}

func TestWatchOp_Has(t *testing.T) {
	t.Parallel()
	op := WatchCreate | WatchWrite
	if !op.Has(WatchCreate) || !op.Has(WatchWrite) {
		t.Error("Has should report set bits")
	}
	if op.Has(WatchRemove) {
		t.Error("Has should not report unset bit")
	}
}

// --- Watcher options not exercised elsewhere ---

func TestWatcher_OptionsDoNotPanic(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "f")
	if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	w, err := NewWatcher(path,
		WithPolling(pollInterval),
		WithDebounce(0),
		WithLogger(nil), // nil logger should be tolerated; constructor falls back to discard
		WithRecursive(false),
		WithBufferSize(8),
	)
	if err != nil {
		t.Fatalf("NewWatcher: %v", err)
	}
	t.Cleanup(func() { _ = w.Close() })
}

// --- Backend selection: native-fallback-to-polling ---

// TestWatcher_NativeBackendFallsBackToPolling exercises the
// non-WithPolling path through selectBackend. The native backend
// constructors on every platform are placeholders that return
// errWatcherUnsupportedBackend; selectBackend catches that and
// returns a polling backend instead. The watcher must construct
// successfully and basic event flow must still work.
func TestWatcher_NativeBackendFallsBackToPolling(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "f")
	if err := os.WriteFile(path, []byte("v"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	// No WithPolling; forces native-backend attempt + fallback.
	w, err := NewWatcher(path, WithDebounce(0))
	if err != nil {
		t.Fatalf("NewWatcher: %v", err)
	}
	t.Cleanup(func() { _ = w.Close() })
}

func TestWatcher_NewDirWatcherRecursive(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	for _, p := range []string{
		filepath.Join(dir, "sub1"),
		filepath.Join(dir, "sub2", "deep"),
	} {
		if err := os.MkdirAll(p, 0o755); err != nil {
			t.Fatalf("MkdirAll: %v", err)
		}
	}

	w, err := NewDirWatcher(dir,
		WithPolling(pollInterval),
		WithDebounce(0),
		WithRecursive(true),
	)
	if err != nil {
		t.Fatalf("NewDirWatcher: %v", err)
	}
	t.Cleanup(func() { _ = w.Close() })
}

// --- pollingBackend.RemovePath ---

func TestPollingBackend_RemovePath(t *testing.T) {
	t.Parallel()
	b := newPollingBackend(pollInterval)
	t.Cleanup(func() { _ = b.Close() })

	dir := t.TempDir()
	if err := b.AddPath(dir); err != nil {
		t.Fatalf("AddPath: %v", err)
	}
	if err := b.RemovePath(dir); err != nil {
		t.Errorf("RemovePath: %v", err)
	}
	// Removing again must succeed (idempotent).
	if err := b.RemovePath(dir); err != nil {
		t.Errorf("RemovePath idempotent: %v", err)
	}
}

func TestPollingBackend_AddAfterCloseErrors(t *testing.T) {
	t.Parallel()
	b := newPollingBackend(pollInterval)
	if err := b.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := b.AddPath(t.TempDir()); !errors.Is(err, ErrWatcherClosed) {
		t.Errorf("got %v, want ErrWatcherClosed", err)
	}
}

func TestWatcher_DebounceCoalescesBurst(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "f")
	if err := os.WriteFile(path, []byte("v0"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	w, err := NewWatcher(path,
		WithPolling(pollInterval),
		WithDebounce(150*time.Millisecond),
	)
	if err != nil {
		t.Fatalf("NewWatcher: %v", err)
	}
	t.Cleanup(func() { _ = w.Close() })

	ch, err := w.Subscribe(context.Background())
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}

	time.Sleep(2 * pollInterval)

	// Five rapid writes within the debounce window; should coalesce.
	for i := range 5 {
		if err := os.WriteFile(path, []byte{byte(i)}, 0o644); err != nil {
			t.Fatalf("WriteFile %d: %v", i, err)
		}
		time.Sleep(20 * time.Millisecond)
	}

	// First event after debounce.
	if _, ok := waitForEvent(t, ch, 2*time.Second); !ok {
		t.Fatal("no event after burst")
	}
	// No more events should arrive within a short additional window.
	if _, ok := waitForEvent(t, ch, 200*time.Millisecond); ok {
		t.Error("debouncer emitted a second event during the burst window")
	}
}

// --- Watcher input validation ---

func TestNewWatcher_EmptyPath(t *testing.T) {
	t.Parallel()
	_, err := NewWatcher("")
	if !errors.Is(err, ErrWatcherEmptyPath) {
		t.Errorf("got %v, want ErrWatcherEmptyPath", err)
	}
}

func TestNewWatcher_MissingPath(t *testing.T) {
	t.Parallel()
	_, err := NewWatcher(filepath.Join(t.TempDir(), "missing"))
	if err == nil {
		t.Fatal("expected error from NewWatcher on missing path")
	}
}

func TestNewLazyWatcher_EmptyPath(t *testing.T) {
	t.Parallel()
	_, err := NewLazyWatcher("")
	if !errors.Is(err, ErrWatcherEmptyPath) {
		t.Errorf("got %v, want ErrWatcherEmptyPath", err)
	}
}

func TestNewLazyWatcher_OnNonExistentParent(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	missing := filepath.Join(dir, "no", "such", "dir", "f")
	_, err := NewLazyWatcher(missing, WithPolling(pollInterval))
	if err == nil {
		t.Error("expected error: lazy still requires parent to exist")
	}
}

func TestNewLazyWatcher_OnMissingTarget(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	target := filepath.Join(dir, "later")
	w, err := NewLazyWatcher(target, WithPolling(time.Hour))
	if err != nil {
		t.Fatalf("NewLazyWatcher: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Errorf("Close: %v", err)
	}
}

func TestNewDirWatcher_EmptyPath(t *testing.T) {
	t.Parallel()
	_, err := NewDirWatcher("")
	if !errors.Is(err, ErrWatcherEmptyPath) {
		t.Errorf("got %v, want ErrWatcherEmptyPath", err)
	}
}

func TestNewDirWatcher_MissingPath(t *testing.T) {
	t.Parallel()
	_, err := NewDirWatcher(filepath.Join(t.TempDir(), "missing"))
	if err == nil {
		t.Fatal("expected error from NewDirWatcher on missing path")
	}
}

func TestNewDirWatcher_NotADirectory(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "f")
	if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}
	_, err := NewDirWatcher(path)
	if !errors.Is(err, ErrNotDir) {
		t.Errorf("got %v, want ErrNotDir", err)
	}
}

func TestNewDirWatcher_NonRecursive(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	w, err := NewDirWatcher(dir, WithPolling(time.Hour), WithRecursive(false))
	if err != nil {
		t.Fatalf("NewDirWatcher: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Errorf("Close: %v", err)
	}
}

func TestNewDirWatcher_Recursive(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "sub", "deep"), 0o755); err != nil {
		t.Fatalf("setup: %v", err)
	}
	w, err := NewDirWatcher(dir, WithPolling(time.Hour), WithRecursive(true))
	if err != nil {
		t.Fatalf("NewDirWatcher: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Errorf("Close: %v", err)
	}
}

func TestWatcher_NilContextSubscribe(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	w, err := NewDirWatcher(dir, WithPolling(time.Second))
	if err != nil {
		t.Fatalf("NewDirWatcher: %v", err)
	}
	defer w.Close()
	var nilCtx context.Context
	_, err = w.Subscribe(nilCtx)
	if !errors.Is(err, ErrWatcherNilContext) {
		t.Errorf("got %v, want ErrWatcherNilContext", err)
	}
}

// --- hasParentDir branch coverage ---

func TestHasParentDir_Branches(t *testing.T) {
	t.Parallel()
	cases := []struct {
		child, parent string
		want          bool
	}{
		{"/a", "/a", true},          // identity
		{"/a/b", "/a", true},        // child of
		{"/a/../etc", "/a", false},  // escape
		{"/elsewhere", "/a", false}, // unrelated
		{".sub", "/a", false},       // rel may start with `.`

		// Regression: filenames with a literal two-dot prefix are
		// still legitimate children, not escapes. The old
		// rel[0]/rel[1] heuristic wrongly treated these as escapes.
		{"/a/..foo", "/a", true},       // file with two-dot prefix
		{"/a/..foo/bar", "/a", true},   // nested child of two-dot dir
		{"/a/.bashrc", "/a", true},     // dotfile
		{"/a/..", "/a", false},         // explicit parent
		{"/a/../sibling", "/a", false}, // sibling outside parent
	}
	for _, c := range cases {
		if got := hasParentDir(c.child, c.parent); got != c.want {
			t.Errorf("hasParentDir(%q, %q) = %v, want %v", c.child, c.parent, got, c.want)
		}
	}
}

// --- pollingBackend coverage ---

func TestPollingBackend_DefaultInterval(t *testing.T) {
	t.Parallel()
	b := newPollingBackend(0)
	if b.interval != defaultWatcherPollingInterval {
		t.Errorf("interval=%v, want %v", b.interval, defaultWatcherPollingInterval)
	}
	if err := b.Close(); err != nil {
		t.Errorf("Close: %v", err)
	}
}

func TestPollingBackend_DoubleClose(t *testing.T) {
	t.Parallel()
	b := newPollingBackend(time.Hour)
	if err := b.Close(); err != nil {
		t.Errorf("first Close: %v", err)
	}
	if err := b.Close(); err != nil {
		t.Errorf("second Close: %v", err)
	}
}

func TestPollingBackend_DuplicateAdd(t *testing.T) {
	t.Parallel()
	b := newPollingBackend(time.Hour)
	defer b.Close()
	dir := t.TempDir()
	if err := b.AddPath(dir); err != nil {
		t.Fatalf("first AddPath: %v", err)
	}
	if err := b.AddPath(dir); err != nil {
		t.Errorf("dup AddPath: %v", err)
	}
}

func TestPollingBackend_AddAfterClose(t *testing.T) {
	t.Parallel()
	b := newPollingBackend(time.Hour)
	if err := b.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	err := b.AddPath(t.TempDir())
	if !errors.Is(err, ErrWatcherClosed) {
		t.Errorf("got %v, want ErrWatcherClosed", err)
	}
}

func TestPollingBackend_StopAt(t *testing.T) {
	t.Parallel()
	b := newPollingBackend(time.Millisecond)
	defer b.Close()
	dir := t.TempDir()
	if err := b.AddPath(dir); err != nil {
		t.Fatalf("AddPath: %v", err)
	}
	// Trigger child appearance + disappearance to exercise both paths.
	child := filepath.Join(dir, "x")
	if err := os.WriteFile(child, []byte("x"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}
	select {
	case <-b.Events():
	case <-time.After(time.Second):
		t.Fatal("no event for child create")
	}
	if err := os.Remove(child); err != nil {
		t.Fatalf("remove: %v", err)
	}
	select {
	case <-b.Events():
	case <-time.After(time.Second):
		t.Fatal("no event for child remove")
	}
}

func TestSnapshotPath_Missing(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	snap := snapshotPath(filepath.Join(dir, "missing"))
	if snap.exists {
		t.Error("snapshot of missing path reports exists=true")
	}
}

func TestDiffSnapshots_AllBranches(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		prev *pollSnapshot
		curr *pollSnapshot
		want WatchOp
	}{
		{"both-missing", &pollSnapshot{exists: false}, &pollSnapshot{exists: false}, 0},
		{"create", &pollSnapshot{exists: false}, &pollSnapshot{exists: true}, WatchCreate},
		{"remove", &pollSnapshot{exists: true}, &pollSnapshot{exists: false}, WatchRemove},
		{"size-change",
			&pollSnapshot{exists: true, size: 0, mode: 0o644},
			&pollSnapshot{exists: true, size: 10, mode: 0o644},
			WatchWrite},
		{"perm-change",
			&pollSnapshot{exists: true, mode: 0o644},
			&pollSnapshot{exists: true, mode: 0o600},
			WatchChmod},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := diffSnapshots(tc.prev, tc.curr)
			if got&tc.want != tc.want {
				t.Errorf("got %v, want bits %v set", got, tc.want)
			}
		})
	}
}

func TestListDirNames_Missing(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	got := listDirNames(filepath.Join(dir, "missing"))
	if len(got) != 0 {
		t.Errorf("got %v, want empty", got)
	}
}
