package fs

import (
	stdfs "io/fs"
	"testing"
)

// BenchmarkWalk_10kTree is the headline benchmark the package's
// review called out: traverse a tree of ~10k files and count
// every entry. The tree is built once outside the timer; the
// benchmark measures only the walk + per-entry callback.
func BenchmarkWalk_10kTree(b *testing.B) {
	root := b.TempDir()
	// shapeRealistic with n=10000 lays down 10 top-level dirs of
	// 1000 files each plus a 1000-file nested branch.
	buildBenchTree(b, root, 10_000, shapeRealistic)

	b.ResetTimer()
	b.ReportAllocs()

	for b.Loop() {
		var count int
		if err := Walk(root, func(_ string, _ stdfs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			count++
			return nil
		}); err != nil {
			b.Fatalf("Walk: %v", err)
		}
		if count < 10_000 {
			b.Fatalf("walk visited %d entries, want >=10000", count)
		}
	}
}

// BenchmarkWalk_Shape compares the three canonical tree shapes at
// a fixed file count. Highlights how directory fan-out vs depth
// affects walk cost.
func BenchmarkWalk_Shape(b *testing.B) {
	const n = 1000
	for _, shape := range []benchShape{shapeShallow, shapeDeep, shapeRealistic} {
		b.Run(shape.String(), func(b *testing.B) {
			root := b.TempDir()
			buildBenchTree(b, root, n, shape)

			b.ResetTimer()
			b.ReportAllocs()

			for b.Loop() {
				if err := Walk(root, func(_ string, _ stdfs.DirEntry, err error) error {
					return err
				}); err != nil {
					b.Fatalf("Walk: %v", err)
				}
			}
		})
	}
}

// BenchmarkWalk_SkipNames measures the cost of the skip-name
// filter against a realistic tree that includes well-known
// "always skip" directories. Mirrors what CLI tools like ripgrep
// / fd / make end up paying.
func BenchmarkWalk_SkipNames(b *testing.B) {
	root := b.TempDir()
	buildBenchTree(b, root, 1000, shapeRealistic)

	b.ResetTimer()
	b.ReportAllocs()

	for b.Loop() {
		if err := Walk(root, func(_ string, _ stdfs.DirEntry, err error) error {
			return err
		}, WithSkipNames([]string{".git", "node_modules", ".terraform"})); err != nil {
			b.Fatalf("Walk: %v", err)
		}
	}
}

// BenchmarkWalk_FollowSymlinks measures the symlink-aware walker
// path (uses [filepath.EvalSymlinks] per entry to detect loops).
// Higher per-entry cost than the default; this benchmark is the
// place to spot regressions.
func BenchmarkWalk_FollowSymlinks(b *testing.B) {
	root := b.TempDir()
	buildBenchTree(b, root, 1000, shapeRealistic)

	b.ResetTimer()
	b.ReportAllocs()

	for b.Loop() {
		if err := Walk(root, func(_ string, _ stdfs.DirEntry, err error) error {
			return err
		}, WalkFollowSymlinks(true)); err != nil {
			b.Fatalf("Walk: %v", err)
		}
	}
}

// BenchmarkWalk_MaxDepth measures the bounded-depth walker over a
// deep tree. Compares against an unbounded walk over the same tree
// to expose the bound-check cost.
func BenchmarkWalk_MaxDepth(b *testing.B) {
	root := b.TempDir()
	buildBenchTree(b, root, 100, shapeDeep)

	b.ResetTimer()
	b.ReportAllocs()

	for b.Loop() {
		if err := Walk(root, func(_ string, _ stdfs.DirEntry, err error) error {
			return err
		}, WithMaxDepth(10)); err != nil {
			b.Fatalf("Walk: %v", err)
		}
	}
}
