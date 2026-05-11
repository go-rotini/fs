package fs

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func newTestCache(t *testing.T, opts ...CacheOption) *Cache {
	t.Helper()
	c, err := NewCache(t.TempDir(), opts...)
	if err != nil {
		t.Fatalf("NewCache: %v", err)
	}
	return c
}

func TestCache_GetSetRoundtrip(t *testing.T) {
	t.Parallel()
	c := newTestCache(t)

	if err := c.Set("foo", []byte("bar")); err != nil {
		t.Fatalf("Set: %v", err)
	}

	got, ok := c.Get("foo")
	if !ok {
		t.Fatal("Get returned ok=false for a key just Set")
	}
	if string(got) != "bar" {
		t.Errorf("Get = %q; want %q", got, "bar")
	}
}

func TestCache_GetMiss(t *testing.T) {
	t.Parallel()
	c := newTestCache(t)

	if _, ok := c.Get("never-set"); ok {
		t.Error("Get returned ok=true for an unset key")
	}
}

func TestCache_Overwrite(t *testing.T) {
	t.Parallel()
	c := newTestCache(t)

	if err := c.Set("k", []byte("v1")); err != nil {
		t.Fatalf("first Set: %v", err)
	}
	if err := c.Set("k", []byte("v2")); err != nil {
		t.Fatalf("second Set: %v", err)
	}

	got, ok := c.Get("k")
	if !ok || string(got) != "v2" {
		t.Errorf("Get = (%q,%v); want (v2,true)", got, ok)
	}
}

func TestCache_Delete(t *testing.T) {
	t.Parallel()
	c := newTestCache(t)

	if err := c.Set("k", []byte("v")); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if err := c.Delete("k"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, ok := c.Get("k"); ok {
		t.Error("Get returned ok=true after Delete")
	}
	// Idempotent: a second Delete of a missing key is a nil-error
	// no-op.
	if err := c.Delete("k"); err != nil {
		t.Errorf("second Delete: %v", err)
	}
}

func TestCache_Purge(t *testing.T) {
	t.Parallel()
	c := newTestCache(t)

	for i := 0; i < 5; i++ {
		if err := c.Set(fmt.Sprintf("k%d", i), []byte("v")); err != nil {
			t.Fatalf("Set #%d: %v", i, err)
		}
	}
	if err := c.Purge(); err != nil {
		t.Fatalf("Purge: %v", err)
	}
	for i := 0; i < 5; i++ {
		if _, ok := c.Get(fmt.Sprintf("k%d", i)); ok {
			t.Errorf("Get k%d returned ok=true after Purge", i)
		}
	}
	// Purge is idempotent; the cache must still be usable.
	if err := c.Purge(); err != nil {
		t.Errorf("second Purge: %v", err)
	}
	if err := c.Set("after-purge", []byte("v")); err != nil {
		t.Errorf("Set after Purge: %v", err)
	}
}

func TestCache_TTLExpiration(t *testing.T) {
	t.Parallel()

	now := time.Now()
	clock := &mockClock{t: now}
	c := newTestCache(t, WithCacheTTL(100*time.Millisecond), WithCacheClock(clock.Now))

	if err := c.Set("k", []byte("v")); err != nil {
		t.Fatalf("Set: %v", err)
	}

	// Backdate the file's mtime past the TTL; simpler than waiting.
	path := c.entryPath("k")
	old := now.Add(-time.Second)
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatalf("Chtimes: %v", err)
	}

	if _, ok := c.Get("k"); ok {
		t.Error("expired entry returned ok=true")
	}
	// Get on an expired entry deletes the file.
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("expired entry not removed; stat err=%v", err)
	}
}

func TestCache_TTLZeroMeansNoExpiry(t *testing.T) {
	t.Parallel()
	c := newTestCache(t) // default TTL = 0

	if err := c.Set("k", []byte("v")); err != nil {
		t.Fatalf("Set: %v", err)
	}
	// Backdate by an hour.
	path := c.entryPath("k")
	old := time.Now().Add(-time.Hour)
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatalf("Chtimes: %v", err)
	}

	if _, ok := c.Get("k"); !ok {
		t.Error("Get returned ok=false with TTL=0 (no expiry)")
	}
}

func TestCache_MaxBytesEviction(t *testing.T) {
	t.Parallel()
	c := newTestCache(t, WithCacheMaxBytes(50))

	// Each entry is 20 bytes; cap is 50; entries 1-2 fit, entry 3
	// triggers eviction of the oldest.
	for i := 0; i < 3; i++ {
		val := make([]byte, 20)
		// Use distinct content so writes don't dedup at any layer.
		for j := range val {
			val[j] = byte('a' + i)
		}
		if err := c.Set(fmt.Sprintf("k%d", i), val); err != nil {
			t.Fatalf("Set #%d: %v", i, err)
		}
		// Space out mtimes so eviction has a deterministic oldest.
		path := c.entryPath(fmt.Sprintf("k%d", i))
		when := time.Now().Add(time.Duration(i) * time.Second)
		if err := os.Chtimes(path, when, when); err != nil {
			t.Fatalf("Chtimes #%d: %v", i, err)
		}
	}

	// k0 (oldest) should be evicted; k1, k2 remain.
	// Trigger one more Set to force an eviction sweep at the now-
	// settled mtimes.
	if err := c.Set("trigger", []byte("xx")); err != nil {
		t.Fatalf("Set trigger: %v", err)
	}

	stats, err := c.Stats()
	if err != nil {
		t.Fatalf("Stats: %v", err)
	}
	if stats.Bytes > 50 {
		t.Errorf("Bytes = %d; want <= 50 after eviction", stats.Bytes)
	}

	if _, ok := c.Get("k0"); ok {
		t.Error("k0 (oldest) was not evicted")
	}
}

func TestCache_StatsCountsEntries(t *testing.T) {
	t.Parallel()
	c := newTestCache(t)

	for i := 0; i < 7; i++ {
		if err := c.Set(fmt.Sprintf("k%d", i), []byte("xyz")); err != nil {
			t.Fatalf("Set #%d: %v", i, err)
		}
	}
	stats, err := c.Stats()
	if err != nil {
		t.Fatalf("Stats: %v", err)
	}
	if stats.Entries != 7 {
		t.Errorf("Entries = %d; want 7", stats.Entries)
	}
	if stats.Bytes != 7*3 {
		t.Errorf("Bytes = %d; want %d", stats.Bytes, 7*3)
	}
}

func TestCache_VersionedIsolation(t *testing.T) {
	t.Parallel()
	base := t.TempDir()

	cv1, err := NewCache(base, WithCacheVersion("v1"))
	if err != nil {
		t.Fatalf("NewCache(v1): %v", err)
	}
	if err := cv1.Set("k", []byte("v1-data")); err != nil {
		t.Fatalf("Set v1: %v", err)
	}

	// A new handle at v2 should see no entries and should remove v1.
	cv2, err := NewCache(base, WithCacheVersion("v2"))
	if err != nil {
		t.Fatalf("NewCache(v2): %v", err)
	}
	if _, ok := cv2.Get("k"); ok {
		t.Error("v2 Get returned ok=true for an entry written in v1")
	}
	if _, err := os.Stat(filepath.Join(base, "v1")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("v1 sibling not swept; stat err=%v", err)
	}
	// Writes to v2 land under base/v2/.
	if err := cv2.Set("k", []byte("v2-data")); err != nil {
		t.Fatalf("Set v2: %v", err)
	}
	if _, err := os.Stat(filepath.Join(base, "v2")); err != nil {
		t.Errorf("v2 dir missing: %v", err)
	}
}

func TestCache_VersionRejectsInvalidChars(t *testing.T) {
	t.Parallel()
	cases := []string{"../escape", "with/slash", "back\\slash", "has\x00null", "spa ce"}
	for _, v := range cases {
		_, err := NewCache(t.TempDir(), WithCacheVersion(v))
		if !errors.Is(err, ErrInvalidPath) {
			t.Errorf("NewCache(version=%q) err = %v; want ErrInvalidPath", v, err)
		}
	}
}

func TestCache_EmptyDirRejected(t *testing.T) {
	t.Parallel()
	if _, err := NewCache(""); !errors.Is(err, ErrInvalidPath) {
		t.Errorf("err = %v; want ErrInvalidPath", err)
	}
}

func TestCache_CloseRejectsFurtherOps(t *testing.T) {
	t.Parallel()
	c := newTestCache(t)
	if err := c.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := c.Set("k", []byte("v")); !errors.Is(err, ErrCacheClosed) {
		t.Errorf("Set after Close err = %v; want ErrCacheClosed", err)
	}
	if _, ok := c.Get("k"); ok {
		t.Error("Get after Close returned ok=true")
	}
	if err := c.Delete("k"); !errors.Is(err, ErrCacheClosed) {
		t.Errorf("Delete after Close err = %v; want ErrCacheClosed", err)
	}
	if err := c.Purge(); !errors.Is(err, ErrCacheClosed) {
		t.Errorf("Purge after Close err = %v; want ErrCacheClosed", err)
	}
	if _, err := c.Stats(); !errors.Is(err, ErrCacheClosed) {
		t.Errorf("Stats after Close err = %v; want ErrCacheClosed", err)
	}
	// Close is idempotent.
	if err := c.Close(); err != nil {
		t.Errorf("second Close: %v", err)
	}
}

func TestCache_ConcurrentSets(t *testing.T) {
	t.Parallel()
	c := newTestCache(t)

	const goroutines = 16
	const opsPerG = 32
	var wg sync.WaitGroup
	var errCount atomic.Int32
	wg.Add(goroutines)
	for g := 0; g < goroutines; g++ {
		go func(gid int) {
			defer wg.Done()
			for i := 0; i < opsPerG; i++ {
				if err := c.Set(fmt.Sprintf("g%d-k%d", gid, i), []byte("v")); err != nil {
					errCount.Add(1)
				}
			}
		}(g)
	}
	wg.Wait()

	if got := errCount.Load(); got != 0 {
		t.Errorf("Set errors: %d", got)
	}
	stats, err := c.Stats()
	if err != nil {
		t.Fatalf("Stats: %v", err)
	}
	if stats.Entries != goroutines*opsPerG {
		t.Errorf("Entries = %d; want %d", stats.Entries, goroutines*opsPerG)
	}
}

func TestCache_KeysAreShardedOnDisk(t *testing.T) {
	t.Parallel()
	c := newTestCache(t)

	if err := c.Set("hello", []byte("world")); err != nil {
		t.Fatalf("Set: %v", err)
	}

	// Confirm the layout matches what entryPath documents: there
	// should be a 2-char shard dir under c.dir and a .bin file inside.
	dirents, err := os.ReadDir(c.dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	if len(dirents) != 1 || !dirents[0].IsDir() || len(dirents[0].Name()) != cacheShardLen {
		t.Errorf("shard dirent = %v; want a single 2-char dir", dirents)
	}
	shard := dirents[0].Name()
	files, err := os.ReadDir(filepath.Join(c.dir, shard))
	if err != nil {
		t.Fatalf("ReadDir shard: %v", err)
	}
	if len(files) != 1 || filepath.Ext(files[0].Name()) != cacheEntryExt {
		t.Errorf("entry file dirent = %v; want one .bin file", files)
	}
}

// mockClock is a deterministic time source for TTL tests.
type mockClock struct {
	mu sync.Mutex
	t  time.Time
}

func (m *mockClock) Now() time.Time {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.t
}

func TestCache_GetWithError(t *testing.T) {
	t.Parallel()
	c := newTestCache(t)

	// Miss on a fresh cache: ok=false, err=nil.
	if v, ok, err := c.GetWithError("missing"); v != nil || ok || err != nil {
		t.Errorf("miss: (v,ok,err) = (%v,%v,%v); want (nil,false,nil)", v, ok, err)
	}

	// Hit after Set.
	if err := c.Set("k", []byte("v")); err != nil {
		t.Fatalf("Set: %v", err)
	}
	v, ok, err := c.GetWithError("k")
	if err != nil {
		t.Fatalf("GetWithError after Set: err=%v", err)
	}
	if !ok || string(v) != "v" {
		t.Errorf("hit = (%q,%v); want (v,true)", v, ok)
	}

	// After Close, GetWithError surfaces ErrCacheClosed.
	if err := c.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if _, _, err := c.GetWithError("k"); !errors.Is(err, ErrCacheClosed) {
		t.Errorf("GetWithError after Close err=%v; want ErrCacheClosed", err)
	}
}

func TestCache_Entries(t *testing.T) {
	t.Parallel()
	c := newTestCache(t)

	for i := 0; i < 4; i++ {
		if err := c.Set(fmt.Sprintf("k%d", i), []byte("vv")); err != nil {
			t.Fatalf("Set %d: %v", i, err)
		}
	}

	count := 0
	seen := map[string]struct{}{}
	for e := range c.Entries() {
		count++
		if e.HashedKey == "" {
			t.Errorf("entry %d: empty HashedKey", count)
		}
		if e.Size <= 0 {
			t.Errorf("entry %d: Size=%d; want >0", count, e.Size)
		}
		if e.ModTime.IsZero() {
			t.Errorf("entry %d: zero ModTime", count)
		}
		seen[e.HashedKey] = struct{}{}
	}
	if count != 4 || len(seen) != 4 {
		t.Errorf("count=%d unique=%d; want 4,4", count, len(seen))
	}

	// Iteration on a closed cache yields nothing.
	if err := c.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	closedCount := 0
	for range c.Entries() {
		closedCount++
	}
	if closedCount != 0 {
		t.Errorf("closed iter yielded %d; want 0", closedCount)
	}
}

func TestCache_EntriesBreakStopsIteration(t *testing.T) {
	t.Parallel()
	c := newTestCache(t)
	for i := 0; i < 5; i++ {
		_ = c.Set(fmt.Sprintf("k%d", i), []byte("x"))
	}
	yielded := 0
	for range c.Entries() {
		yielded++
		break
	}
	if yielded != 1 {
		t.Errorf("yielded=%d; want 1 before break", yielded)
	}
}

func TestCacheHashFromPath(t *testing.T) {
	t.Parallel()
	dir := "/tmp/cache"
	// Shard layout: <dir>/ab/cdef.bin
	got := cacheHashFromPath(dir, filepath.Join(dir, "ab", "cdef.bin"))
	if got != "abcdef" {
		t.Errorf("got=%q; want abcdef", got)
	}

	// Path outside baseDir.
	if cacheHashFromPath(dir, "/elsewhere/file.bin") == "" {
		t.Error("expected something for relative-able path")
	}

	// Single-segment path (no shard).
	if got := cacheHashFromPath(dir, filepath.Join(dir, "lone.bin")); got != "lone" {
		t.Errorf("single-segment got=%q; want lone", got)
	}
}

func TestCache_SetClosedRejected(t *testing.T) {
	t.Parallel()
	c := newTestCache(t)
	if err := c.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := c.Set("k", []byte("v")); !errors.Is(err, ErrCacheClosed) {
		t.Errorf("err=%v; want ErrCacheClosed", err)
	}
}

func TestCache_PurgeRecreatesDir(t *testing.T) {
	t.Parallel()
	c := newTestCache(t)
	_ = c.Set("k", []byte("v"))
	if err := c.Purge(); err != nil {
		t.Fatalf("Purge: %v", err)
	}
	// Directory still exists and is usable.
	if _, err := os.Stat(c.dir); err != nil {
		t.Errorf("dir missing after Purge: %v", err)
	}
	if err := c.Set("k2", []byte("v")); err != nil {
		t.Errorf("Set after Purge: %v", err)
	}
}

func TestCache_StatsClosedRejected(t *testing.T) {
	t.Parallel()
	c := newTestCache(t)
	_ = c.Close()
	if _, err := c.Stats(); !errors.Is(err, ErrCacheClosed) {
		t.Errorf("err=%v; want ErrCacheClosed", err)
	}
}

func TestCache_DeleteClosedRejected(t *testing.T) {
	t.Parallel()
	c := newTestCache(t)
	_ = c.Close()
	if err := c.Delete("k"); !errors.Is(err, ErrCacheClosed) {
		t.Errorf("err=%v; want ErrCacheClosed", err)
	}
}

func TestCache_NewCacheSweepsOldVersions(t *testing.T) {
	t.Parallel()
	base := t.TempDir()

	// Pre-create a sibling that should be swept.
	if err := os.MkdirAll(filepath.Join(base, "old-v"), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(base, "old-v", "stale.txt"), []byte("x"), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}

	if _, err := NewCache(base, WithCacheVersion("new-v")); err != nil {
		t.Fatalf("NewCache: %v", err)
	}
	if _, err := os.Stat(filepath.Join(base, "old-v")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("old-v not swept; stat err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(base, "new-v")); err != nil {
		t.Errorf("new-v missing: %v", err)
	}
}

func TestCache_WalkEntriesEmpty(t *testing.T) {
	t.Parallel()
	c := newTestCache(t)
	stats, err := c.Stats()
	if err != nil {
		t.Fatalf("Stats: %v", err)
	}
	if stats.Entries != 0 || stats.Bytes != 0 {
		t.Errorf("empty cache stats = %+v; want zeroes", stats)
	}
}

func TestIsValidVersion(t *testing.T) {
	t.Parallel()
	good := []string{"v1", "v1.0.0", "snapshot_2026", "a-b-c", "X9"}
	for _, v := range good {
		if !isValidVersion(v) {
			t.Errorf("isValidVersion(%q) = false; want true", v)
		}
	}
	bad := []string{"", ".", "..", "with space", "slash/x", "back\\slash", "tab\there", "x\x00null", "emoji😀"}
	for _, v := range bad {
		if isValidVersion(v) {
			t.Errorf("isValidVersion(%q) = true; want false", v)
		}
	}
}
