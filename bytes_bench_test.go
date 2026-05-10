package fs

import "testing"

// BenchmarkParseBytes covers the canonical inputs CLI tools see.
// String parsing is allocation-sensitive; this benchmark is the
// place to spot a regression in the prefix-walk or the unit table.
func BenchmarkParseBytes(b *testing.B) {
	cases := []struct {
		name, in string
	}{
		{"bare", "12345"},
		{"kib", "10KiB"},
		{"mib", "100MiB"},
		{"gib", "1GiB"},
		{"decimal", "1.5GB"},
		{"si-kb", "10KB"},
	}
	for _, c := range cases {
		b.Run(c.name, func(b *testing.B) {
			b.ResetTimer()
			b.ReportAllocs()
			for b.Loop() {
				if _, err := ParseBytes(c.in); err != nil {
					b.Fatalf("ParseBytes(%q): %v", c.in, err)
				}
			}
		})
	}
}

// BenchmarkFormatBytes covers the formatter at every order of
// magnitude.
func BenchmarkFormatBytes(b *testing.B) {
	cases := []struct {
		name string
		n    int64
	}{
		{"small", 999},
		{"kib", 4096},
		{"mib", 5 << 20},
		{"gib", 7 << 30},
		{"tib", 3 << 40},
	}
	for _, c := range cases {
		b.Run(c.name, func(b *testing.B) {
			b.ResetTimer()
			b.ReportAllocs()
			for b.Loop() {
				_ = FormatBytes(c.n)
			}
		})
	}
}
