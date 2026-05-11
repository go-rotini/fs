package fs_test

import (
	"fmt"

	"github.com/go-rotini/fs"
)

// Cache is the idiomatic compute-on-miss pattern: Get; on miss,
// compute the expensive result and Set it. Errors from Set surface
// to the caller; Get treats every failure as a miss.
func ExampleNewCache() {
	dir, cleanup, _ := fs.TempDir("", "cache-example-*")
	defer func() { _ = cleanup() }()

	c, err := fs.NewCache(dir)
	if err != nil {
		return
	}
	defer func() { _ = c.Close() }()

	key := "expensive-result-of-X"
	if v, ok := c.Get(key); ok {
		fmt.Println("hit:", string(v))
		return
	}

	// Miss path: compute the value and remember it for next time.
	computed := []byte("computed-once")
	if err := c.Set(key, computed); err != nil {
		return
	}
	fmt.Println("miss → computed and stored")
	// Output:
	// miss → computed and stored
}

// Versioned caches scope every entry under a per-app version string;
// bumping the version after an upgrade implicitly invalidates the
// entire previous-version cache. Old version subdirs are deleted on
// open so the cache directory does not grow unbounded.
func ExampleWithCacheVersion() {
	dir, cleanup, _ := fs.TempDir("", "cache-example-*")
	defer func() { _ = cleanup() }()

	c, err := fs.NewCache(dir, fs.WithCacheVersion("schema-v3"))
	if err != nil {
		return
	}
	defer func() { _ = c.Close() }()

	_ = c.Set("k", []byte("v"))
	stats, _ := c.Stats()
	fmt.Println("entries:", stats.Entries)
	// Output:
	// entries: 1
}
