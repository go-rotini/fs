package fs

import (
	"path/filepath"
	"testing"
)

// BenchmarkCopyFile measures the cost of [CopyFile] across the
// canonical sizes. Includes the full open-source +
// CreateTemp+write+chmod+rename cycle that callers actually see.
func BenchmarkCopyFile(b *testing.B) {
	for _, sz := range benchSizes {
		b.Run(sz.name, func(b *testing.B) {
			dir := b.TempDir()
			src := writeBenchFile(b, dir, "src", sz.n)
			dst := filepath.Join(dir, "dst")

			b.ResetTimer()
			b.ReportAllocs()
			b.SetBytes(int64(sz.n))

			for b.Loop() {
				if err := CopyFile(src, dst); err != nil {
					b.Fatalf("CopyFile: %v", err)
				}
			}
		})
	}
}

// BenchmarkCopyDir measures recursive copy of a built fixture tree.
// Shallow / deep / realistic shapes expose the per-entry overhead
// vs the per-directory overhead.
func BenchmarkCopyDir(b *testing.B) {
	cases := []struct {
		shape benchShape
		n     int
	}{
		{shapeShallow, 100},
		{shapeDeep, 50},
		{shapeRealistic, 1000},
	}

	for _, c := range cases {
		b.Run(c.shape.String(), func(b *testing.B) {
			// Build the source ONCE; reused across iterations.
			srcDir := b.TempDir()
			buildBenchTree(b, srcDir, c.n, c.shape)

			b.ResetTimer()
			b.ReportAllocs()

			i := 0
			for b.Loop() {
				// Fresh destination per iteration; b.TempDir is a
				// shared directory for the whole benchmark run, but
				// individual sub-paths are still distinct.
				dst := filepath.Join(b.TempDir(), "dst-"+itoa(i))
				i++
				if err := CopyDir(srcDir, dst); err != nil {
					b.Fatalf("CopyDir: %v", err)
				}
			}
		})
	}
}

// BenchmarkRename measures the bare-rename happy path (same FS).
// Each iteration renames a freshly-created file so the rename
// always has work to do.
func BenchmarkRename(b *testing.B) {
	dir := b.TempDir()

	b.ResetTimer()
	b.ReportAllocs()

	i := 0
	for b.Loop() {
		src := filepath.Join(dir, "src-"+itoa(i))
		dst := filepath.Join(dir, "dst-"+itoa(i))
		i++
		if err := WriteFile(src, nil); err != nil {
			b.Fatalf("setup: %v", err)
		}
		if err := Rename(src, dst); err != nil {
			b.Fatalf("Rename: %v", err)
		}
	}
}

// BenchmarkMove measures the same-FS Move happy path (no EXDEV
// fallback). Useful for spotting regressions in the
// lstat+rename+remove pattern.
func BenchmarkMove(b *testing.B) {
	dir := b.TempDir()

	b.ResetTimer()
	b.ReportAllocs()

	i := 0
	for b.Loop() {
		src := filepath.Join(dir, "src-"+itoa(i))
		dst := filepath.Join(dir, "dst-"+itoa(i))
		i++
		if err := WriteFile(src, nil); err != nil {
			b.Fatalf("setup: %v", err)
		}
		if err := Move(src, dst); err != nil {
			b.Fatalf("Move: %v", err)
		}
	}
}
