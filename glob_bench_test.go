package fs

import (
	"path/filepath"
	"testing"
)

// BenchmarkGlob measures the cost of [Glob] over a fixture directory.
// Three patterns: literal (no metachars), single-star, character
// class. Patterns are chosen to exercise the matcher; the underlying
// directory has a mix of files so the glob has work to do.
func BenchmarkGlob(b *testing.B) {
	dir := b.TempDir()
	// 100 files: a.go, b.go, ... + 50 non-Go files.
	for i := range 100 {
		name := string(rune('a'+(i%26))) + itoa(i) + ".go"
		if err := WriteFile(filepath.Join(dir, name), nil); err != nil {
			b.Fatalf("setup: %v", err)
		}
	}
	for i := range 50 {
		if err := WriteFile(filepath.Join(dir, "data"+itoa(i)+".bin"), nil); err != nil {
			b.Fatalf("setup: %v", err)
		}
	}

	cases := []struct {
		name    string
		pattern string
	}{
		{"literal", filepath.Join(dir, "a0.go")},
		{"star", filepath.Join(dir, "*.go")},
		{"class", filepath.Join(dir, "[abc]*.go")},
	}
	for _, c := range cases {
		b.Run(c.name, func(b *testing.B) {
			b.ResetTimer()
			b.ReportAllocs()
			for b.Loop() {
				if _, err := Glob(c.pattern); err != nil {
					b.Fatalf("Glob: %v", err)
				}
			}
		})
	}
}

// BenchmarkGlobAny measures [GlobAny] over the same fixture with a
// 3-pattern list. Exposes the dedup overhead.
func BenchmarkGlobAny(b *testing.B) {
	dir := b.TempDir()
	for i := range 100 {
		name := string(rune('a'+(i%26))) + itoa(i) + ".go"
		if err := WriteFile(filepath.Join(dir, name), nil); err != nil {
			b.Fatalf("setup: %v", err)
		}
	}
	patterns := []string{
		filepath.Join(dir, "*.go"),
		filepath.Join(dir, "a*.go"),
		filepath.Join(dir, "[abc]*"),
	}

	b.ResetTimer()
	b.ReportAllocs()

	for b.Loop() {
		if _, err := GlobAny(patterns); err != nil {
			b.Fatalf("GlobAny: %v", err)
		}
	}
}

// BenchmarkMatch is the pure-pattern match without any FS work.
// Calls hot in walk filters; this benchmark is the place to spot a
// regression in [filepath.Match] dispatch.
func BenchmarkMatch(b *testing.B) {
	cases := []struct {
		name, pat, in string
	}{
		{"literal", "main.go", "main.go"},
		{"star", "*.go", "main.go"},
		{"class", "[abc]*.go", "a-test.go"},
		{"miss", "*.txt", "main.go"},
	}
	for _, c := range cases {
		b.Run(c.name, func(b *testing.B) {
			b.ResetTimer()
			b.ReportAllocs()
			for b.Loop() {
				if _, err := Match(c.pat, c.in); err != nil {
					b.Fatalf("Match: %v", err)
				}
			}
		})
	}
}
