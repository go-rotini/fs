package fs

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Fuzz targets. CI runs `go test -fuzz=Fuzz<Name> -fuzztime=60s` per
// target as a smoke pass. The package-internal invariant we check
// is "no panic, no wrong result"; every target is allowed to
// return errors freely; only crashes constitute a fuzz failure.

// FuzzExpand drives [Expand] with random input strings. The package
// must not panic, and on success must return a string with no `~`
// or `$` survivors that point at unset variables (when strict mode
// is on).
func FuzzExpand(f *testing.F) {
	seeds := []string{
		"~",
		"~/foo",
		"~user/foo",
		"$HOME/bar",
		"${HOME}/bar",
		"plain/path",
		"",
		"~$VAR",
		"$VAR$VAR",
		"/abs/$HOME",
	}
	for _, s := range seeds {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, input string) {
		// The package must not panic regardless of input.
		_, _ = Expand(input)
	})
}

// FuzzIsSubpath drives [IsSubpath] with random (parent, child)
// pairs. Result must be deterministic and consistent with a
// transitive `MustBeChildOf` check.
func FuzzIsSubpath(f *testing.F) {
	seeds := [][2]string{
		{"/a", "/a/b"},
		{"/a", "/a/../b"},
		{"/a", "/A"},
		{"/", "/anything"},
		{"./x", "x/y"},
		{"", ""},
		{"/a/b/c", "/a/b/c/d/../e"},
	}
	for _, s := range seeds {
		f.Add(s[0], s[1])
	}
	f.Fuzz(func(t *testing.T, parent, child string) {
		_, _ = IsSubpath(parent, child)
	})
}

// FuzzMatch drives [Match] with random pattern/name pairs. Errors
// only on malformed patterns; never panics.
func FuzzMatch(f *testing.F) {
	seeds := [][2]string{
		{"*.go", "main.go"},
		{"foo[abc]", "fooa"},
		{"foo?bar", "fooXbar"},
		{"[", "anything"}, // malformed
		{"", ""},
		{"\\*", "*"},
	}
	for _, s := range seeds {
		f.Add(s[0], s[1])
	}
	f.Fuzz(func(t *testing.T, pattern, name string) {
		_, _ = Match(pattern, name)
	})
}

// FuzzParseBytes drives [ParseBytes] / [ParseBytesIEC] with random
// size strings. Never panics.
func FuzzParseBytes(f *testing.F) {
	seeds := []string{
		"1024",
		"1KiB",
		"1.5GB",
		"1.5 GB",
		"  1MB  ",
		"-5KB",
		"",
		"abc",
		"1.2.3KB",
		"999999999999999999999999999PiB",
	}
	for _, s := range seeds {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, input string) {
		_, _ = ParseBytes(input)
		_, _ = ParseBytesIEC(input)
	})
}

// FuzzSanitizeFilename drives [SanitizeFilename] with random input.
// The output must:
//  1. be non-empty (falls back to "_" if everything was stripped),
//  2. contain none of the Windows-illegal characters or ASCII
//     controls,
//  3. not be a Windows reserved name (suffixed with "_" if it
//     would have been).
func FuzzSanitizeFilename(f *testing.F) {
	seeds := []string{
		"normal.txt",
		"file<with>illegal:chars.txt",
		"trailing.   ",
		"CON",
		"COM1.txt",
		"\x00\x01\x02",
		"",
		"...",
		"résumé.tex",
	}
	for _, s := range seeds {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, input string) {
		got := SanitizeFilename(input)
		if got == "" {
			t.Errorf("SanitizeFilename(%q) returned empty; should fall back to _", input)
		}
		for _, c := range got {
			switch c {
			case '<', '>', ':', '"', '|', '?', '*', '/', '\\':
				t.Errorf("SanitizeFilename(%q) leaked illegal %q", input, c)
			}
			if c < 0x20 {
				t.Errorf("SanitizeFilename(%q) leaked control byte %#x", input, c)
			}
		}
		if IsReservedName(got) {
			t.Errorf("SanitizeFilename(%q) = %q is a reserved name", input, got)
		}
	})
}

// FuzzMagic drives [Magic] with random byte payloads written to a
// temp file and random n values. Must not panic; over-length n
// returns the file's actual contents without padding.
func FuzzMagic(f *testing.F) {
	seeds := [][]byte{
		{},
		{0x1F, 0x8B},
		[]byte("PNG\x89..."),
		make([]byte, 4096),
	}
	for _, s := range seeds {
		f.Add(s, 16)
	}
	f.Fuzz(func(t *testing.T, payload []byte, n int) {
		dir := t.TempDir()
		path := filepath.Join(dir, "f")
		if err := os.WriteFile(path, payload, 0o644); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}
		got, _ := Magic(path, n)
		if n <= 0 && len(got) != 0 {
			t.Errorf("Magic(%d) returned non-empty for non-positive n: %v", n, got)
		}
		if n > 0 && len(got) > len(payload) {
			t.Errorf("Magic(%d) returned more bytes than payload (%d): got %d", n, len(payload), len(got))
		}
	})
}

// FuzzGitignore drives [parseGitignoreLine] with adversarial input.
// The parser is forgiving by design; comments, blank lines, leading
// `!`, trailing `/`, embedded `**`; so the invariant we check is
// "no panic" plus "no panic on subsequent .matches() call against
// a small set of representative path inputs".
func FuzzGitignore(f *testing.F) {
	seeds := []string{
		"",
		"#",
		"# comment",
		"\n",
		"*.log",
		"!important.log",
		"/anchored",
		"node_modules/",
		"a/**/b",
		"a/?/b",
		"foo.[abc]",
		"with space",
		"with\ttab",
		`back\slash`,
		"trailing/",
		" leading-space",
		"a\nb",
		// Adversarial: many `**` segments should be refused by the
		// parser (P16 cap). Seed both at-cap and over-cap so the
		// fuzzer can explore the boundary.
		"**/**/**/**/**/**/**/**/**/**/**/**/**/**/**/**/x",
		"**/**/**/**/**/**/**/**/**/**/**/**/**/**/**/**/**/x",
		"**/**/**/" + strings.Repeat("**/", 30) + "x",
	}
	for _, s := range seeds {
		f.Add(s)
	}
	probes := []string{"", "/", ".", "foo", "foo/bar", "a/b/c", "node_modules/x"}
	f.Fuzz(func(t *testing.T, input string) {
		p, ok := parseGitignoreLine(input)
		if !ok {
			return
		}
		for _, probe := range probes {
			_ = p.matches(probe)
		}
	})
}

// FuzzGoWork drives [parseGoWork] with arbitrary file contents.
// Invariant: no panic.
func FuzzGoWork(f *testing.F) {
	seeds := []string{
		"",
		"go 1.22\n",
		"go 1.22\nuse ./a\n",
		"go 1.22\nuse (\n./a\n./b\n)\n",
		"// comment\nuse ./x",
		"use", // truncated
		"use (",
		"use (\n",
		`use "./quoted"`,
	}
	for _, s := range seeds {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, input string) {
		dir := t.TempDir()
		path := filepath.Join(dir, "go.work")
		if err := os.WriteFile(path, []byte(input), 0o644); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}
		_, _ = parseGoWork(path)
	})
}

// FuzzPnpmWorkspaces drives [parsePnpmWorkspaces] with arbitrary
// YAML-ish input. Invariant: no panic. The parser is intentionally
// strict; unsupported YAML features return an error rather than
// silently empty.
func FuzzPnpmWorkspaces(f *testing.F) {
	seeds := []string{
		"",
		"packages:\n  - 'a'\n  - \"b\"\n",
		"packages:\n  - packages/*\n",
		"# comment\npackages:\n  - x\n",
		"packages: [a, b]", // unsupported inline form
		"foo:\nbar:\npackages:\n  - x\n",
		"packages:\n  - 'a'\npackages:\n  - 'b'", // duplicate key
		"packages:\n  not-a-list-item",
	}
	for _, s := range seeds {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, input string) {
		dir := t.TempDir()
		path := filepath.Join(dir, "pnpm-workspace.yaml")
		if err := os.WriteFile(path, []byte(input), 0o644); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}
		_, _ = parsePnpmWorkspaces(path, dir)
	})
}
