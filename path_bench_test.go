package fs

import "testing"

// BenchmarkExpand covers the path-expansion fast paths.
// no-expansion (literal), tilde, $VAR, ${VAR}. Hot in CLI tools
// that resolve user input before opening files.
func BenchmarkExpand(b *testing.B) {
	cases := []struct {
		name, in string
	}{
		{"literal", "/etc/passwd"},
		{"tilde", "~/notes"},
		{"var", "$HOME/notes"},
		{"braced", "${HOME}/notes"},
	}
	for _, c := range cases {
		b.Run(c.name, func(b *testing.B) {
			b.ResetTimer()
			b.ReportAllocs()
			for b.Loop() {
				if _, err := Expand(c.in); err != nil {
					b.Fatalf("Expand: %v", err)
				}
			}
		})
	}
}

// BenchmarkAbs measures the [Abs] helper. Wraps Expand +
// [filepath.Abs]; useful for catching regressions in either side.
func BenchmarkAbs(b *testing.B) {
	b.ResetTimer()
	b.ReportAllocs()
	for b.Loop() {
		if _, err := Abs("./relative/path/under/cwd"); err != nil {
			b.Fatalf("Abs: %v", err)
		}
	}
}

// BenchmarkMustBeChildOf is the path-confinement predicate
// archive extraction calls per-entry. A slowdown here directly
// translates to slower archive extraction.
func BenchmarkMustBeChildOf(b *testing.B) {
	const parent = "/var/lib/myapp"
	cases := []struct {
		name, child string
	}{
		{"inside", "/var/lib/myapp/data/cfg"},
		{"escape", "/var/lib/myapp/../etc/passwd"},
		{"sibling", "/var/lib/other"},
	}
	for _, c := range cases {
		b.Run(c.name, func(b *testing.B) {
			b.ResetTimer()
			b.ReportAllocs()
			for b.Loop() {
				_ = MustBeChildOf(parent, c.child)
			}
		})
	}
}

// BenchmarkExists is the cheapest filesystem predicate; hot in
// CLI tools that probe a config-file fallback chain.
func BenchmarkExists(b *testing.B) {
	dir := b.TempDir()
	path := writeBenchFile(b, dir, "in", 0)
	if err := WriteFile(path, []byte{'x'}); err != nil {
		b.Fatalf("setup: %v", err)
	}

	b.ResetTimer()
	b.ReportAllocs()

	for b.Loop() {
		_ = Exists(path)
	}
}
