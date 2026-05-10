package fs

import (
	"testing"
)

// BenchmarkReadFile measures the cost of [ReadFile] across the
// canonical [benchSizes]. SetBytes lets `go test -bench` report
// MB/s, which is the number callers actually care about.
func BenchmarkReadFile(b *testing.B) {
	for _, sz := range benchSizes {
		b.Run(sz.name, func(b *testing.B) {
			dir := b.TempDir()
			path := writeBenchFile(b, dir, "in", sz.n)

			b.ResetTimer()
			b.ReportAllocs()
			b.SetBytes(int64(sz.n))

			for b.Loop() {
				if _, err := ReadFile(path); err != nil {
					b.Fatalf("ReadFile: %v", err)
				}
			}
		})
	}
}

// BenchmarkReadFile_Unbounded is [ReadFile] with the size cap
// disabled. Comparing against the bounded variant exposes the cost
// of the cap-check + LimitReader wrap.
func BenchmarkReadFile_Unbounded(b *testing.B) {
	dir := b.TempDir()
	path := writeBenchFile(b, dir, "in", 1<<20)

	b.ResetTimer()
	b.ReportAllocs()
	b.SetBytes(1 << 20)

	for b.Loop() {
		if _, err := ReadFile(path, WithMaxSize(0)); err != nil {
			b.Fatalf("ReadFile: %v", err)
		}
	}
}

// BenchmarkReadLines measures [ReadLines] over a fixture of N lines.
// 1024 lines is the typical "config or .env file" shape; 65536 is
// the typical "log tail" shape.
func BenchmarkReadLines(b *testing.B) {
	cases := []struct {
		name  string
		lines int
	}{
		{"1KLines", 1024},
		{"64KLines", 64 * 1024},
	}
	for _, c := range cases {
		b.Run(c.name, func(b *testing.B) {
			dir := b.TempDir()
			payload := make([]byte, 0, c.lines*8)
			for range c.lines {
				payload = append(payload, "line-x\n"...)
			}
			path := writeBenchFile(b, dir, "in", 0)
			if err := WriteFile(path, payload); err != nil {
				b.Fatalf("WriteFile: %v", err)
			}

			b.ResetTimer()
			b.ReportAllocs()
			b.SetBytes(int64(len(payload)))

			for b.Loop() {
				if _, err := ReadLines(path); err != nil {
					b.Fatalf("ReadLines: %v", err)
				}
			}
		})
	}
}

// BenchmarkOpenLines streams 64K lines via the iterator. Measures
// the iter.Seq2 dispatch cost + scanner overhead vs the upfront
// allocation of ReadLines.
func BenchmarkOpenLines(b *testing.B) {
	const lines = 64 * 1024
	dir := b.TempDir()
	payload := make([]byte, 0, lines*8)
	for range lines {
		payload = append(payload, "line-x\n"...)
	}
	path := writeBenchFile(b, dir, "in", 0)
	if err := WriteFile(path, payload); err != nil {
		b.Fatalf("WriteFile: %v", err)
	}

	b.ResetTimer()
	b.ReportAllocs()
	b.SetBytes(int64(len(payload)))

	for b.Loop() {
		seq, closeFn, err := OpenLines(path)
		if err != nil {
			b.Fatalf("OpenLines: %v", err)
		}
		var count int
		for _, lerr := range seq {
			if lerr != nil {
				b.Fatalf("iter: %v", lerr)
			}
			count++
		}
		_ = closeFn()
		if count != lines {
			b.Fatalf("got %d lines, want %d", count, lines)
		}
	}
}

// BenchmarkOpenChunked streams a 1 MiB file in 64 KiB chunks.
func BenchmarkOpenChunked(b *testing.B) {
	const size = 1 << 20
	dir := b.TempDir()
	path := writeBenchFile(b, dir, "in", size)

	b.ResetTimer()
	b.ReportAllocs()
	b.SetBytes(int64(size))

	for b.Loop() {
		seq, closeFn, err := OpenChunked(path, 64*1024)
		if err != nil {
			b.Fatalf("OpenChunked: %v", err)
		}
		for _, cerr := range seq {
			if cerr != nil {
				b.Fatalf("iter: %v", cerr)
			}
		}
		_ = closeFn()
	}
}

// BenchmarkReadFirstLine measures the "just the first line"
// shortcut. Hot in CLI tools that read shebangs / version stamps.
func BenchmarkReadFirstLine(b *testing.B) {
	dir := b.TempDir()
	path := writeBenchFile(b, dir, "in", 0)
	if err := WriteFile(path, []byte("first\nsecond\nthird\n")); err != nil {
		b.Fatalf("WriteFile: %v", err)
	}

	b.ResetTimer()
	b.ReportAllocs()

	for b.Loop() {
		if _, err := ReadFirstLine(path); err != nil {
			b.Fatalf("ReadFirstLine: %v", err)
		}
	}
}
