package fs

import "testing"

// BenchmarkHash runs each algorithm over the canonical sizes. The
// underlying hash cost dominates; what the benchmark measures is
// the wrapper overhead; file open, io.Copy, hex encoding.
func BenchmarkHash(b *testing.B) {
	algos := []struct {
		name string
		algo HashAlgo
	}{
		{"sha256", HashSHA256},
		{"sha512", HashSHA512},
		{"sha1", HashSHA1},
		{"md5", HashMD5},
	}
	for _, sz := range benchSizes {
		// 10 MiB at sub-benchmark time per algorithm is unnecessary;
		// cap at 1 MiB for the per-algo comparison.
		if sz.n > 1<<20 {
			continue
		}
		for _, a := range algos {
			b.Run(a.name+"/"+sz.name, func(b *testing.B) {
				dir := b.TempDir()
				path := writeBenchFile(b, dir, "in", sz.n)

				b.ResetTimer()
				b.ReportAllocs()
				b.SetBytes(int64(sz.n))

				for b.Loop() {
					if _, err := Hash(path, a.algo); err != nil {
						b.Fatalf("Hash: %v", err)
					}
				}
			})
		}
	}
}

// BenchmarkHashWriter measures the streaming variant; the cost of
// piping bytes through [HashWriter] vs reading from disk. Useful
// for the io.MultiWriter idiom in [doc.go]'s HashWriter example.
func BenchmarkHashWriter(b *testing.B) {
	const size = 1 << 20
	payload := benchPayload(size)

	b.ResetTimer()
	b.ReportAllocs()
	b.SetBytes(int64(size))

	for b.Loop() {
		h := HashWriter(HashSHA256)
		if _, err := h.Write(payload); err != nil {
			b.Fatalf("Write: %v", err)
		}
		_ = h.Hex()
	}
}
