package fs

import (
	"path/filepath"
	"testing"
)

func BenchmarkRotatorWrite(b *testing.B) {
	line := []byte("2026-10-12T10:00:00Z level=info msg=\"request served\" status=200 dur=1.2ms\n")
	for _, tc := range []struct {
		name string
		opts []RotatorOption
	}{
		{"NoLimits", nil},
		{"MaxBytes", []RotatorOption{WithRotateMaxBytes(1 << 40)}},
	} {
		b.Run(tc.name, func(b *testing.B) {
			r, err := NewRotator(filepath.Join(b.TempDir(), "app.log"), tc.opts...)
			if err != nil {
				b.Fatal(err)
			}
			defer func() { _ = r.Close() }()
			b.ReportAllocs()
			for b.Loop() {
				if _, err := r.Write(line); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
