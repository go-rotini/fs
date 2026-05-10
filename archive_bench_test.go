package fs

import (
	"path/filepath"
	"testing"
)

// BenchmarkCreateArchive measures archive creation over a realistic
// tree. Three formats: plain tar, gzip-compressed tar, zip.
func BenchmarkCreateArchive(b *testing.B) {
	formats := []struct {
		name string
		f    ArchiveFormat
	}{
		{"tar", ArchiveFormatTar},
		{"tar.gz", ArchiveFormatTarGz},
		{"zip", ArchiveFormatZip},
	}
	root := b.TempDir()
	buildBenchTree(b, root, 500, shapeRealistic)

	for _, f := range formats {
		b.Run(f.name, func(b *testing.B) {
			b.ResetTimer()
			b.ReportAllocs()

			i := 0
			for b.Loop() {
				out := filepath.Join(b.TempDir(), "a-"+itoa(i))
				i++
				if err := CreateArchiveFile(out, root, WithArchiveFormat(f.f)); err != nil {
					b.Fatalf("CreateArchiveFile: %v", err)
				}
			}
		})
	}
}

// BenchmarkExtractArchive measures the inverse path. Each format
// is pre-built once; the benchmark only times extraction.
func BenchmarkExtractArchive(b *testing.B) {
	formats := []struct {
		name string
		f    ArchiveFormat
		ext  string
	}{
		{"tar", ArchiveFormatTar, "tar"},
		{"tar.gz", ArchiveFormatTarGz, "tgz"},
		{"zip", ArchiveFormatZip, "zip"},
	}
	srcTree := b.TempDir()
	buildBenchTree(b, srcTree, 500, shapeRealistic)

	for _, f := range formats {
		b.Run(f.name, func(b *testing.B) {
			archive := filepath.Join(b.TempDir(), "a."+f.ext)
			if err := CreateArchiveFile(archive, srcTree, WithArchiveFormat(f.f)); err != nil {
				b.Fatalf("setup CreateArchiveFile: %v", err)
			}

			b.ResetTimer()
			b.ReportAllocs()

			i := 0
			for b.Loop() {
				dst := filepath.Join(b.TempDir(), "ex-"+itoa(i))
				i++
				if err := ExtractArchiveFile(archive, dst); err != nil {
					b.Fatalf("ExtractArchiveFile: %v", err)
				}
			}
		})
	}
}
