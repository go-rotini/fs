package fs

import (
	"path/filepath"
	"testing"
)

// BenchmarkWriteFile measures the atomic-by-default write across
// the canonical sizes. Includes the temp+rename overhead — that's
// the actual per-call cost callers see.
func BenchmarkWriteFile(b *testing.B) {
	for _, sz := range benchSizes {
		b.Run(sz.name, func(b *testing.B) {
			data := benchPayload(sz.n)
			dir := b.TempDir()
			path := filepath.Join(dir, "out")

			b.ResetTimer()
			b.ReportAllocs()
			b.SetBytes(int64(sz.n))

			for b.Loop() {
				if err := WriteFile(path, data); err != nil {
					b.Fatalf("WriteFile: %v", err)
				}
			}
		})
	}
}

// BenchmarkWriteFile_Direct measures the non-atomic write path.
// Comparing against the default surface exposes the temp+rename
// cost callers opt out of with [WithAtomic(false)].
func BenchmarkWriteFile_Direct(b *testing.B) {
	for _, sz := range benchSizes {
		b.Run(sz.name, func(b *testing.B) {
			data := benchPayload(sz.n)
			dir := b.TempDir()
			path := filepath.Join(dir, "out")

			b.ResetTimer()
			b.ReportAllocs()
			b.SetBytes(int64(sz.n))

			for b.Loop() {
				if err := WriteFile(path, data, WithAtomic(false)); err != nil {
					b.Fatalf("WriteFile: %v", err)
				}
			}
		})
	}
}

// BenchmarkWriteFile_Sync forces an fsync after the write. On most
// platforms this is the dominant cost — useful for sizing the
// "default-sync for overwrites" choice in the docs.
func BenchmarkWriteFile_Sync(b *testing.B) {
	const size = 1 << 16
	data := benchPayload(size)
	dir := b.TempDir()
	path := filepath.Join(dir, "out")

	b.ResetTimer()
	b.ReportAllocs()
	b.SetBytes(int64(size))

	for b.Loop() {
		if err := WriteFile(path, data, WithSync(true)); err != nil {
			b.Fatalf("WriteFile: %v", err)
		}
	}
}

// BenchmarkWriteFileExclusive measures the O_EXCL "create or fail"
// path. Each iteration writes to a fresh path so the EXCL check
// actually has work to do.
func BenchmarkWriteFileExclusive(b *testing.B) {
	const size = 1 << 16
	data := benchPayload(size)
	dir := b.TempDir()

	b.ResetTimer()
	b.ReportAllocs()
	b.SetBytes(int64(size))

	i := 0
	for b.Loop() {
		path := filepath.Join(dir, "out-"+itoa(i))
		i++
		if err := WriteFileExclusive(path, data); err != nil {
			b.Fatalf("WriteFileExclusive: %v", err)
		}
	}
}

// BenchmarkAppend measures the append-to-existing-file path. Each
// iteration appends to the same file so the open-for-append + write
// + close cycle is what's measured.
func BenchmarkAppend(b *testing.B) {
	const chunk = 256
	data := benchPayload(chunk)
	dir := b.TempDir()
	path := filepath.Join(dir, "log")
	// Pre-create to skip the create branch on first iteration.
	if err := WriteFile(path, nil); err != nil {
		b.Fatalf("setup: %v", err)
	}

	b.ResetTimer()
	b.ReportAllocs()
	b.SetBytes(int64(chunk))

	for b.Loop() {
		if err := Append(path, data); err != nil {
			b.Fatalf("Append: %v", err)
		}
	}
}

// BenchmarkOpenWrite_Finalize measures the full Open → Write →
// Finalize cycle (the streaming variant). Compares against
// BenchmarkWriteFile to expose the overhead a caller pays for
// streaming control.
func BenchmarkOpenWrite_Finalize(b *testing.B) {
	const size = 1 << 16
	data := benchPayload(size)
	dir := b.TempDir()
	path := filepath.Join(dir, "out")

	b.ResetTimer()
	b.ReportAllocs()
	b.SetBytes(int64(size))

	for b.Loop() {
		f, finalize, err := OpenWrite(path)
		if err != nil {
			b.Fatalf("OpenWrite: %v", err)
		}
		if _, err := f.Write(data); err != nil {
			b.Fatalf("Write: %v", err)
		}
		if err := finalize(); err != nil {
			b.Fatalf("finalize: %v", err)
		}
	}
}

// itoa avoids strconv import to keep the benchmark file's import
// graph minimal. Trivial decimal formatter for non-negative ints.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}
