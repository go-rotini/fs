package fs

import (
	"os"
	"path/filepath"
	"testing"
)

// BenchmarkFindUp measures the walk-toward-root cost. Three depths
// expose how the per-ancestor stat scales.
func BenchmarkFindUp(b *testing.B) {
	cases := []struct {
		name  string
		depth int
	}{
		{"depth-2", 2},
		{"depth-8", 8},
		{"depth-32", 32},
	}
	for _, c := range cases {
		b.Run(c.name, func(b *testing.B) {
			root := b.TempDir()
			// Marker at the root.
			if err := WriteFile(filepath.Join(root, ".env"), nil); err != nil {
				b.Fatalf("setup: %v", err)
			}
			// Build the deep chain.
			cur := root
			for i := range c.depth {
				cur = filepath.Join(cur, "d"+itoa(i))
				if err := os.Mkdir(cur, 0o755); err != nil {
					b.Fatalf("setup: %v", err)
				}
			}

			b.ResetTimer()
			b.ReportAllocs()

			for b.Loop() {
				if _, _, err := FindUp(".env", cur); err != nil {
					b.Fatalf("FindUp: %v", err)
				}
			}
		})
	}
}

// BenchmarkProjectRoot measures the cached helper. Each iteration
// hits the same start dir + marker set so the second-and-after
// iterations exercise the sync.Map fast path; the first iteration
// pays the walk cost. Useful for measuring cache-hit cost in long-
// running CLI sessions.
func BenchmarkProjectRoot(b *testing.B) {
	root := b.TempDir()
	if err := os.Mkdir(filepath.Join(root, ".git"), 0o755); err != nil {
		b.Fatalf("setup: %v", err)
	}
	leaf := filepath.Join(root, "src", "pkg", "internal")
	if err := os.MkdirAll(leaf, 0o755); err != nil {
		b.Fatalf("setup: %v", err)
	}

	b.ResetTimer()
	b.ReportAllocs()

	for b.Loop() {
		if _, err := ProjectRoot(leaf); err != nil {
			b.Fatalf("ProjectRoot: %v", err)
		}
	}
}

// BenchmarkFind measures the walk-backed [Find] over a realistic
// tree. Pattern matches a small subset of files so the per-match
// append doesn't dominate.
func BenchmarkFind(b *testing.B) {
	root := b.TempDir()
	buildBenchTree(b, root, 1000, shapeRealistic)

	b.ResetTimer()
	b.ReportAllocs()

	for b.Loop() {
		if _, err := Find(root, "*.go"); err != nil {
			b.Fatalf("Find: %v", err)
		}
	}
}
