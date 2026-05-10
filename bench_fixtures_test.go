package fs

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

// Shared fixture helpers for the *_bench_test.go files.
//
// Conventions used across the benchmark suite:
//
//   - Every benchmark builds its fixtures BEFORE calling
//     b.ResetTimer(), so the timer only measures the operation under
//     test, not the setup cost.
//   - Sized payloads use [benchPayload]; the content is deterministic
//     (so the compiler can't constant-fold it away) but not random
//     (so benchmarks are reproducible across runs).
//   - Tree fixtures use [benchTree] with named shapes; the file
//     basenames and contents are deterministic.
//   - Benchmarks that touch the filesystem write under b.TempDir()
//     so the testing framework owns cleanup, even for runs killed
//     mid-iteration.

// benchPayload returns a deterministic byte slice of size bytes.
// The contents cycle through the lowercase alphabet so the result
// is incompressible enough that gzip / zip can't optimize benchmark
// payloads down to a few bytes.
func benchPayload(size int) []byte {
	out := make([]byte, size)
	for i := range out {
		out[i] = byte('a' + (i % 26))
	}
	return out
}

// benchSizes are the canonical sizes used by IO-bound benchmarks:
// roughly a CLI config, a small file, a medium file, and a large
// (but still under the 100 MiB ReadFile cap) file.
var benchSizes = []struct {
	name string
	n    int
}{
	{"1KiB", 1 << 10},
	{"64KiB", 1 << 16},
	{"1MiB", 1 << 20},
	{"10MiB", 10 << 20},
}

// benchShape names a tree topology produced by [buildBenchTree].
type benchShape int

const (
	// shapeShallow places benchNFiles files directly inside root.
	shapeShallow benchShape = iota
	// shapeDeep places one file at each level of a benchNFiles-deep
	// chain (root/d0/d1/.../d{N-1}/f).
	shapeDeep
	// shapeRealistic produces a mixed-shape project tree:
	// ~10 directories with ~N/10 files each, plus a few nested
	// subdirectories. Approximates what `find . -type f | wc -l`
	// would report for a typical mid-sized project.
	shapeRealistic
)

func (s benchShape) String() string {
	switch s {
	case shapeShallow:
		return "shallow"
	case shapeDeep:
		return "deep"
	case shapeRealistic:
		return "realistic"
	}
	return "unknown"
}

// buildBenchTree materializes a tree of n files under root in the
// given shape. Contents are deterministic; sizes are small (a single
// payload byte per file) so the benchmark cost reflects traversal,
// not I/O. Returns the root directory.
//
// Callers typically pass b.TempDir() as root.
func buildBenchTree(tb testing.TB, root string, n int, shape benchShape) string {
	tb.Helper()
	if err := os.MkdirAll(root, 0o755); err != nil {
		tb.Fatalf("MkdirAll %s: %v", root, err)
	}

	switch shape {
	case shapeShallow:
		for i := range n {
			p := filepath.Join(root, "f"+strconv.Itoa(i))
			if err := os.WriteFile(p, []byte{'x'}, 0o644); err != nil {
				tb.Fatalf("WriteFile %s: %v", p, err)
			}
		}
	case shapeDeep:
		// Cap depth at 64 — beyond that we hit PATH_MAX on most
		// platforms (macOS APFS is ~1024 chars; the segment prefix
		// "d999/" alone burns 5 chars per level). If n > maxDepth,
		// the extra files are placed as siblings of the deepest
		// file so the requested file count is honored.
		const maxDepth = 64
		depth := min(n, maxDepth)
		cur := root
		for i := range depth {
			cur = filepath.Join(cur, "d"+strconv.Itoa(i))
			if err := os.Mkdir(cur, 0o755); err != nil {
				tb.Fatalf("Mkdir %s: %v", cur, err)
			}
			if err := os.WriteFile(filepath.Join(cur, "f"), []byte{'x'}, 0o644); err != nil {
				tb.Fatalf("WriteFile %s: %v", cur, err)
			}
		}
		for i := depth; i < n; i++ {
			p := filepath.Join(cur, "extra"+strconv.Itoa(i))
			if err := os.WriteFile(p, []byte{'x'}, 0o644); err != nil {
				tb.Fatalf("WriteFile %s: %v", p, err)
			}
		}
	case shapeRealistic:
		// 10 top-level dirs, each with N/10 files, plus a 2-level
		// nested "deep" branch holding 10% of the files.
		const topDirs = 10
		perDir := n / topDirs
		for d := range topDirs {
			dir := filepath.Join(root, "pkg"+strconv.Itoa(d))
			if err := os.Mkdir(dir, 0o755); err != nil {
				tb.Fatalf("Mkdir %s: %v", dir, err)
			}
			for i := range perDir {
				p := filepath.Join(dir, "f"+strconv.Itoa(i)+".go")
				if err := os.WriteFile(p, []byte{'x'}, 0o644); err != nil {
					tb.Fatalf("WriteFile %s: %v", p, err)
				}
			}
		}
		// Nested branch: root/internal/a/b/c/...
		nested := filepath.Join(root, "internal")
		if err := os.MkdirAll(filepath.Join(nested, "a", "b", "c"), 0o755); err != nil {
			tb.Fatalf("MkdirAll nested: %v", err)
		}
		for i := range n / 10 {
			p := filepath.Join(nested, "a", "b", "c", "n"+strconv.Itoa(i))
			if err := os.WriteFile(p, []byte{'x'}, 0o644); err != nil {
				tb.Fatalf("WriteFile %s: %v", p, err)
			}
		}
	}

	return root
}

// writeBenchFile writes size deterministic bytes to a path inside b.TempDir
// and returns the path. Used by Read* benchmarks that need a
// pre-existing fixture file.
func writeBenchFile(tb testing.TB, dir string, name string, size int) string {
	tb.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, benchPayload(size), 0o644); err != nil {
		tb.Fatalf("WriteFile %s: %v", p, err)
	}
	return p
}
