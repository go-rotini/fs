package fs

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

const (
	opCacheGet    = "cache.get"
	opCacheSet    = "cache.set"
	opCacheDelete = "cache.delete"
	opCachePurge  = "cache.purge"
	opCacheStats  = "cache.stats"
	opNewCache    = "cache.new"

	// cacheEntryExt is the suffix every entry file gets. Distinguishes
	// cache entries from sub-shard directories during walks and lets a
	// user safely sweep `*.bin` under the cache dir without an
	// allow-list.
	cacheEntryExt = ".bin"

	// cacheShardLen is the number of hash hex chars used as the shard
	// directory name. Two chars → 256 shards: enough to keep dirent
	// counts bounded for caches up to ~100k entries on every supported
	// filesystem.
	cacheShardLen = 2

	// cacheDirPerm + cacheFilePerm match the package defaults for
	// MkdirAll and WriteFile. Caches typically live under
	// CacheDir / StateDir which are already 0o700 on POSIX; the per-
	// entry mode here is permissive so cooperating tools (e.g., the
	// user's shell) can inspect them.
	cacheDirPerm  os.FileMode = 0o755
	cacheFilePerm os.FileMode = 0o644
)

// ErrCacheClosed is returned by [Cache] operations after [Cache.Close]
// has been called.
var ErrCacheClosed = errors.New("fs: cache: closed")

// Cache is a directory-backed key/value store with TTL and total-size
// eviction. Keys are arbitrary strings; values are byte slices.
//
// Cache is safe for concurrent use by multiple goroutines. Multiple
// processes can share the same cache directory: writes are atomic via
// temp+rename; eviction sweeps may briefly over- or under-evict if
// two processes evict simultaneously, which is acceptable for a
// best-effort cache.
//
// Storage layout (relative to dir, with WithCacheVersion(v) applied):
//
//	<dir>/<version-or-empty>/<hash[0:2]>/<hash[2:]>.bin
//
// where hash = sha256(key). The version segment is omitted when the
// cache has no version configured. Each entry file holds only the
// value bytes; the file's modification time encodes the entry's
// creation time and drives TTL accounting.
//
// Eviction is mtime-based LRU when [WithCacheMaxBytes] is set: after
// every Set whose cumulative size pushes total bytes above the cap,
// the oldest entries are removed until total bytes is below the cap.
type Cache struct {
	dir string // effective dir = config dir + version subdir (if any)
	cfg cacheConfig

	mu     sync.Mutex
	closed bool
}

// CacheStats is a point-in-time snapshot of the cache state.
type CacheStats struct {
	// Entries is the number of value files currently on disk.
	Entries int

	// Bytes is the total size of every value file (excludes
	// directory overhead).
	Bytes int64
}

// cacheConfig holds the per-Cache options. The zero value means "no
// TTL, no size cap, no version, real wall clock".
type cacheConfig struct {
	ttl      time.Duration
	maxBytes int64
	version  string
	nowFn    func() time.Time
}

// CacheOption configures Cache construction. Pass via [NewCache].
type CacheOption func(*cacheConfig)

// WithCacheTTL sets the per-entry time-to-live. Entries older than d
// at Get time are deleted and reported as misses. Zero disables TTL
// (the default).
func WithCacheTTL(d time.Duration) CacheOption {
	return func(c *cacheConfig) {
		c.ttl = d
	}
}

// WithCacheMaxBytes sets a soft cap on total bytes across all entries.
// After each Set, the cache evicts oldest-by-mtime entries until the
// total is at or below n. Zero (the default) disables the cap.
func WithCacheMaxBytes(n int64) CacheOption {
	return func(c *cacheConfig) {
		c.maxBytes = n
	}
}

// WithCacheVersion namespaces all entries under a version segment in
// the cache directory. Bumping the version is equivalent to a full
// purge: new entries land under <dir>/<newVersion>/ and Get against
// the old version's path simply misses.
//
// On Cache open, sibling version directories under <dir> are removed
// so a long-lived cache doesn't accumulate every historical version.
// Pass "" (the default) to disable versioning entirely.
//
// Allowed characters: [A-Za-z0-9._-]. Other inputs are rejected by
// [NewCache] with [ErrInvalidPath].
func WithCacheVersion(v string) CacheOption {
	return func(c *cacheConfig) {
		c.version = v
	}
}

// WithCacheClock overrides the wall clock used for TTL accounting.
// Useful only in tests; production callers should leave the default.
func WithCacheClock(now func() time.Time) CacheOption {
	return func(c *cacheConfig) {
		c.nowFn = now
	}
}

// NewCache opens (or creates) a [Cache] rooted at dir. The directory
// is created with mode 0o755 if missing. Returns a non-nil error if
// dir is empty, malformed, or cannot be created.
//
// Each NewCache call creates an independent handle; concurrent
// handles in the same process serialize on internal mutexes and
// otherwise do not coordinate. Across processes, see the [Cache]
// concurrency notes.
func NewCache(dir string, opts ...CacheOption) (*Cache, error) {
	if dir == "" {
		return nil, wrapPathError(opNewCache, dir, ErrInvalidPath)
	}

	cfg := cacheConfig{nowFn: time.Now}
	for _, opt := range opts {
		opt(&cfg)
	}

	if cfg.version != "" && !isValidVersion(cfg.version) {
		return nil, wrapPathError(opNewCache, dir, ErrInvalidPath)
	}
	if cfg.nowFn == nil {
		cfg.nowFn = time.Now
	}

	effective := dir
	if cfg.version != "" {
		effective = filepath.Join(dir, cfg.version)
	}

	if err := MkdirAll(effective, cacheDirPerm); err != nil {
		return nil, err
	}

	c := &Cache{dir: effective, cfg: cfg}

	if cfg.version != "" {
		if err := c.sweepSiblingVersions(dir); err != nil {
			return nil, err
		}
	}

	return c, nil
}

// Close marks the cache as closed. Subsequent operations return
// [ErrCacheClosed]. Close does NOT remove files on disk; callers
// that want the directory gone should call [Cache.Purge] before
// Close or [os.RemoveAll] after. Close is idempotent.
func (c *Cache) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.closed = true
	return nil
}

// Get returns the cached value for key. ok is true on a hit; false
// when the entry is missing, expired, or otherwise unreadable.
//
// Get never returns an error — read failures are treated as misses
// so callers can write the idiomatic compute-on-miss form without
// error plumbing. The entry on disk is deleted if it was expired.
func (c *Cache) Get(key string) (value []byte, ok bool) {
	c.mu.Lock()
	closed := c.closed
	c.mu.Unlock()
	if closed {
		return nil, false
	}

	path := c.entryPath(key)
	info, err := os.Stat(path)
	if err != nil {
		return nil, false
	}
	if c.cfg.ttl > 0 {
		if c.cfg.nowFn().Sub(info.ModTime()) >= c.cfg.ttl {
			_ = os.Remove(path)
			return nil, false
		}
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, false
	}
	return data, true
}

// Set writes value to the cache under key, replacing any existing
// entry. The write is atomic (temp+rename) so concurrent readers
// never see a partial value. After the write, eviction runs when
// [WithCacheMaxBytes] is configured.
func (c *Cache) Set(key string, value []byte) error {
	c.mu.Lock()
	closed := c.closed
	c.mu.Unlock()
	if closed {
		return wrapPathError(opCacheSet, key, ErrCacheClosed)
	}

	path := c.entryPath(key)
	if err := MkdirAll(filepath.Dir(path), cacheDirPerm); err != nil {
		return err
	}
	if err := WriteFile(path, value, WithPerm(cacheFilePerm)); err != nil {
		return err
	}
	if c.cfg.maxBytes > 0 {
		if err := c.evictIfOverBudget(); err != nil {
			return err
		}
	}
	return nil
}

// Delete removes the entry for key. Returns nil if the entry was
// missing. Errors surface only for unrecoverable read/write failures.
func (c *Cache) Delete(key string) error {
	c.mu.Lock()
	closed := c.closed
	c.mu.Unlock()
	if closed {
		return wrapPathError(opCacheDelete, key, ErrCacheClosed)
	}

	path := c.entryPath(key)
	if err := os.Remove(path); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		return wrapPathError(opCacheDelete, path, err)
	}
	return nil
}

// Purge removes every entry under the cache's effective directory.
// The directory itself is recreated empty so the Cache remains usable.
// Idempotent.
func (c *Cache) Purge() error {
	c.mu.Lock()
	closed := c.closed
	c.mu.Unlock()
	if closed {
		return wrapPathError(opCachePurge, c.dir, ErrCacheClosed)
	}

	if err := RemoveAll(c.dir); err != nil {
		return err
	}
	if err := MkdirAll(c.dir, cacheDirPerm); err != nil {
		return err
	}
	return nil
}

// Stats walks the cache directory and returns the entry count and
// total bytes. O(n) in the number of entries; safe to call from
// admin / debug paths but avoid invoking on hot read paths.
func (c *Cache) Stats() (CacheStats, error) {
	c.mu.Lock()
	closed := c.closed
	c.mu.Unlock()
	if closed {
		return CacheStats{}, wrapPathError(opCacheStats, c.dir, ErrCacheClosed)
	}

	entries, _, err := c.walkEntries()
	if err != nil {
		return CacheStats{}, err
	}
	var total int64
	for _, e := range entries {
		total += e.size
	}
	return CacheStats{Entries: len(entries), Bytes: total}, nil
}

// entryPath maps key to its on-disk file path:
//
//	<dir>/<hashhex[:2]>/<hashhex[2:]>.bin
//
// The version, when present, is already part of c.dir.
func (c *Cache) entryPath(key string) string {
	sum := sha256.Sum256([]byte(key))
	h := hex.EncodeToString(sum[:])
	return filepath.Join(c.dir, h[:cacheShardLen], h[cacheShardLen:]+cacheEntryExt)
}

// cacheEntryFile is the per-entry record returned by walkEntries.
type cacheEntryFile struct {
	path  string
	mtime time.Time
	size  int64
}

// walkEntries enumerates every <hash>.bin under c.dir. Non-entry
// files and directories are ignored. Returns the entries plus the
// total bytes for the convenience of callers that need both numbers
// (eviction).
func (c *Cache) walkEntries() ([]cacheEntryFile, int64, error) {
	var (
		entries []cacheEntryFile
		total   int64
	)
	err := filepath.WalkDir(c.dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				return nil
			}
			return err
		}
		if d.IsDir() || filepath.Ext(d.Name()) != cacheEntryExt {
			return nil
		}
		info, ierr := d.Info()
		if ierr != nil {
			if errors.Is(ierr, fs.ErrNotExist) {
				return nil
			}
			return wrapPathError(opCacheStats, path, ierr)
		}
		entries = append(entries, cacheEntryFile{
			path:  path,
			mtime: info.ModTime(),
			size:  info.Size(),
		})
		total += info.Size()
		return nil
	})
	if err != nil {
		return nil, 0, wrapPathError(opCacheStats, c.dir, err)
	}
	return entries, total, nil
}

// evictIfOverBudget removes oldest-by-mtime entries until total bytes
// is at or below cfg.maxBytes. Called under no lock; concurrent
// callers may both run an eviction sweep which is fine: at worst the
// cache is briefly more aggressively trimmed than configured.
func (c *Cache) evictIfOverBudget() error {
	entries, total, err := c.walkEntries()
	if err != nil {
		return err
	}
	if total <= c.cfg.maxBytes {
		return nil
	}
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].mtime.Before(entries[j].mtime)
	})
	for _, e := range entries {
		if total <= c.cfg.maxBytes {
			break
		}
		if rerr := os.Remove(e.path); rerr != nil && !errors.Is(rerr, fs.ErrNotExist) {
			return wrapPathError(opCacheSet, e.path, rerr)
		}
		total -= e.size
	}
	return nil
}

// sweepSiblingVersions removes every sibling of c.dir under base whose
// name differs from cfg.version. Used at NewCache time to garbage-
// collect entries from previous schema versions.
func (c *Cache) sweepSiblingVersions(base string) error {
	dirents, err := os.ReadDir(base)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		return wrapPathError(opNewCache, base, err)
	}
	for _, e := range dirents {
		if !e.IsDir() || e.Name() == c.cfg.version {
			continue
		}
		if rerr := RemoveAll(filepath.Join(base, e.Name())); rerr != nil {
			return rerr
		}
	}
	return nil
}

// isValidVersion enforces a conservative allow-list for version
// strings so they can serve as path segments on every supported
// filesystem.
func isValidVersion(v string) bool {
	if v == "" || v == "." || v == ".." {
		return false
	}
	for _, r := range v {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= 'A' && r <= 'Z':
		case r >= '0' && r <= '9':
		case r == '.' || r == '_' || r == '-':
		default:
			return false
		}
	}
	return true
}
