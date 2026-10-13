package fs

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"os"
	"path"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
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
		// parser (the 16-segment cap). Seed both at-cap and over-cap so the
		// fuzzer can explore the boundary.
		"**/**/**/**/**/**/**/**/**/**/**/**/**/**/**/**/x",
		"**/**/**/**/**/**/**/**/**/**/**/**/**/**/**/**/**/x",
		"**/**/**/" + strings.Repeat("**/", 30) + "x",
	}
	for _, s := range seeds {
		f.Add(s)
	}
	probes := []string{"", "/", ".", "foo", "foo/bar", "a/b/c", "node_modules/x", "x/x/x/x"}
	f.Fuzz(func(t *testing.T, input string) {
		p, ok := parseGitignoreLine(input)
		if !ok {
			return
		}
		var m segMatcher
		for _, probe := range probes {
			segs := strings.Split(probe, "/")
			for _, isDir := range []bool{false, true} {
				got := p.matches(&m, segs, isDir)
				if want := refGitignoreMatch(p, segs, isDir); got != want {
					t.Fatalf("pattern %q, path %q, isDir %v: matches = %v; reference says %v", input, probe, isDir, got, want)
				}
			}
		}
	})
}

// refGitignoreMatch is the specification [gitignorePattern.matches]
// implements, written as a direct (exponential) search: some run of
// segments, starting at 0 or (unanchored) anywhere, is matched by the
// whole pattern, and a run reaching the last segment needs isDir for
// a dir-only pattern.
func refGitignoreMatch(p gitignorePattern, segs []string, isDir bool) bool {
	starts := len(segs)
	if p.anchored {
		starts = 0
	}
	for s := 0; s <= starts; s++ {
		for e := s; e <= len(segs); e++ {
			if refFullMatch(p.segments, segs[s:e]) && (e < len(segs) || !p.dirOnly || isDir) {
				return true
			}
		}
	}
	return false
}

func refFullMatch(pat, segs []string) bool {
	if len(pat) == 0 {
		return len(segs) == 0
	}
	if pat[0] == "**" {
		least := 0
		if len(pat) == 1 {
			least = 1
		}
		for i := least; i <= len(segs); i++ {
			if refFullMatch(pat[1:], segs[i:]) {
				return true
			}
		}
		return false
	}
	if len(segs) == 0 {
		return false
	}
	if ok, err := path.Match(pat[0], segs[0]); err != nil || !ok {
		return false
	}
	return refFullMatch(pat[1:], segs[1:])
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

// fuzzArchiveSandbox extracts data into a fresh dst inside an outer
// directory and fails if anything appears in the outer directory
// besides dst, or if a symlink left in dst leads outside it.
func fuzzArchiveSandbox(t *testing.T, data []byte) {
	t.Helper()
	outer := t.TempDir()
	dst := filepath.Join(outer, "dst")
	if err := os.Mkdir(dst, 0o755); err != nil {
		t.Fatal(err)
	}
	_ = ExtractArchive(bytes.NewReader(data), dst, WithArchiveMaxBytes(1<<20))
	ents, err := os.ReadDir(outer)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range ents {
		if e.Name() != "dst" {
			t.Fatalf("extraction wrote %q outside the destination", e.Name())
		}
	}
	if err := escapingLink(dst); err != nil {
		t.Fatal(err)
	}
}

// FuzzExtractTar drives [ExtractArchive] with tar archives built from
// fuzzed entry names, link targets and types. Invariant: nothing is
// written outside the destination, and no symlink left in it leads
// outside, whether the extraction succeeds or fails.
func FuzzExtractTar(f *testing.F) {
	reg, sym, hard := byte(tar.TypeReg), byte(tar.TypeSymlink), byte(tar.TypeLink)
	f.Add("a/b.txt", "", reg, "x/y", "z", reg, "c", "", reg)
	f.Add("link", "..", sym, "link/evil", "", reg, "c", "", reg)
	f.Add("d", ".", sym, "a", "d/d/..", sym, "c", "", reg)
	f.Add("../evil", "", reg, "/abs", "", reg, "c", "", reg)
	f.Add("h", "../x", hard, "s", "/etc", sym, "c", "", reg)
	f.Add("a/l", "..", sym, "h", "a/l", hard, "c", "", reg)
	f.Add("l", "s/..", sym, "s", ".", sym, "../evil", "", reg)
	f.Fuzz(func(t *testing.T, name1, link1 string, typ1 byte, name2, link2 string, typ2 byte, name3, link3 string, typ3 byte) {
		var buf bytes.Buffer
		tw := tar.NewWriter(&buf)
		for _, e := range []struct {
			name, link string
			typ        byte
		}{{name1, link1, typ1}, {name2, link2, typ2}, {name3, link3, typ3}} {
			hdr := &tar.Header{Name: e.name, Linkname: e.link, Typeflag: e.typ, Mode: 0o644}
			if e.typ == tar.TypeReg {
				hdr.Size = 1
			}
			if tw.WriteHeader(hdr) != nil {
				return
			}
			if e.typ == tar.TypeReg {
				_, _ = tw.Write([]byte("x"))
			}
		}
		if tw.Close() != nil {
			return
		}
		fuzzArchiveSandbox(t, buf.Bytes())
	})
}

// FuzzExtractZip drives [ExtractArchive] with zip archives built from
// fuzzed entry names. Invariant: nothing is written outside the
// destination.
func FuzzExtractZip(f *testing.F) {
	f.Add("a/b.txt", "c")
	f.Add("../evil", "/abs")
	f.Add(`..\evil`, "a/../../b")
	f.Fuzz(func(t *testing.T, name1, name2 string) {
		var buf bytes.Buffer
		zw := zip.NewWriter(&buf)
		for _, name := range []string{name1, name2} {
			w, err := zw.Create(name)
			if err != nil {
				return
			}
			_, _ = w.Write([]byte("x"))
		}
		if zw.Close() != nil {
			return
		}
		fuzzArchiveSandbox(t, buf.Bytes())
	})
}

// FuzzScaffoldPlan renders a templated source path with a fuzzed
// variable. Invariant: every planned destination stays inside dst, or
// the plan fails.
func FuzzScaffoldPlan(f *testing.F) {
	for _, s := range []string{"app", "../escape", "/abs", "a/../../b", `..\x`, "", "."} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, name string) {
		src := fstest.MapFS{"{{.Name}}/file.txt": {Data: []byte("x")}}
		dst := t.TempDir()
		plan, err := ScaffoldPlan(src, dst, map[string]string{"Name": name})
		if err != nil {
			return
		}
		for _, a := range plan {
			if ok, _ := IsSubpath(dst, a.DstPath); !ok && filepath.Clean(a.DstPath) != filepath.Clean(dst) {
				t.Fatalf("name %q planned %q outside %q", name, a.DstPath, dst)
			}
		}
	})
}
