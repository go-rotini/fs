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
// the runtime deterministic across platforms — the platform-native
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

	// Five rapid writes within the debounce window — should coalesce.
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
