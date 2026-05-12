package fs

import (
	"testing"
)

// --- SanitizeFilename ---

func TestSanitizeFilename_StripsIllegal(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in, want string
	}{
		{"plain.txt", "plain.txt"},
		{"a<b>c.txt", "abc.txt"},
		{`a"b|c.txt`, "abc.txt"},
		{"a:b?c*d.txt", "abcd.txt"},
		{"a/b\\c.txt", "abc.txt"},
		{"a\x00b.txt", "ab.txt"},
		{"a\x01\x07b.txt", "ab.txt"}, // ASCII control bytes
		{"trailing.   ", "trailing"}, // trim trailing dots and spaces
		{"trailing... ", "trailing"},
	}
	for _, c := range cases {
		got := SanitizeFilename(c.in)
		if got != c.want {
			t.Errorf("SanitizeFilename(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestSanitizeFilename_AllStrippedFallsBack(t *testing.T) {
	t.Parallel()
	got := SanitizeFilename(`<>:?"|/*\`)
	if got != "_" {
		t.Errorf("all-illegal input to %q, want _", got)
	}
	if got := SanitizeFilename("...   "); got != "_" {
		t.Errorf("only-trailing-trim input to %q, want _", got)
	}
}

func TestSanitizeFilename_ReservedSuffixed(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in, want string
	}{
		{"CON", "CON_"},
		{"con", "con_"},
		{"CON.txt", "CON_.txt"},
		{"COM1", "COM1_"},
		{"com9.log", "com9_.log"},
		{"LPT3", "LPT3_"},
		{"lpt3.txt", "lpt3_.txt"},
	}
	for _, c := range cases {
		got := SanitizeFilename(c.in)
		if got != c.want {
			t.Errorf("SanitizeFilename(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestSanitizeFilename_PreservesUnicode(t *testing.T) {
	t.Parallel()
	got := SanitizeFilename("résumé.tex")
	if got != "résumé.tex" {
		t.Errorf("got %q, want unicode preserved", got)
	}
}

// --- IsReservedName ---

func TestIsReservedName_Reserved(t *testing.T) {
	t.Parallel()
	for _, name := range []string{
		"CON", "PRN", "AUX", "NUL",
		"COM1", "COM2", "COM3", "COM4", "COM5", "COM6", "COM7", "COM8", "COM9",
		"LPT1", "LPT9",
		"con", "prn", "Con.txt", "NUL.log",
		"com5.bin",
	} {
		if !IsReservedName(name) {
			t.Errorf("IsReservedName(%q) = false, want true", name)
		}
	}
}

func TestIsReservedName_NotReserved(t *testing.T) {
	t.Parallel()
	for _, name := range []string{
		"", "FOO", "CON1", "COM0", "COM10", "LPT0", "LPT10",
		"console", "com",
		"normal.txt",
	} {
		if IsReservedName(name) {
			t.Errorf("IsReservedName(%q) = true, want false", name)
		}
	}
}

// LongPath has platform-specific behavior; see longpath_unix_test.go
// and longpath_windows_test.go.
