package fs

import (
	"strings"
	"testing"
)

// --- FormatBytes ---

func TestFormatBytes_Boundaries(t *testing.T) {
	t.Parallel()
	cases := []struct {
		n    int64
		want string
	}{
		{0, "0 B"},
		{1, "1 B"},
		{1023, "1023 B"},
		{1024, "1 KiB"},
		{1536, "1.5 KiB"},
		{1 << 20, "1 MiB"},
		{1<<20 + 1<<19, "1.5 MiB"},
		{1 << 30, "1 GiB"},
		{1 << 40, "1 TiB"},
		{1 << 50, "1 PiB"},
		{1 << 60, "1 EiB"},
		{-1024, "-1 KiB"},
		{-1, "-1 B"},
	}
	for _, c := range cases {
		if got := FormatBytes(c.n); got != c.want {
			t.Errorf("FormatBytes(%d) = %q, want %q", c.n, got, c.want)
		}
	}
}

// --- ParseBytes: strict-SI by default ---

// IEC binary units (KiB, MiB, ...) are always 1024-based regardless
// of which ParseBytes variant is used; they're unambiguous.
func TestParseBytes_IECSuffixes(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in   string
		want int64
	}{
		{"0", 0},
		{"512", 512},
		{"1KiB", 1024},
		{"1 KiB", 1024},
		{"1.5KiB", 1536},
		{"1MiB", 1 << 20},
		{"1GiB", 1 << 30},
		{"1TiB", 1 << 40},
	}
	for _, c := range cases {
		got, err := ParseBytes(c.in)
		if err != nil {
			t.Errorf("ParseBytes(%q): %v", c.in, err)
			continue
		}
		if got != c.want {
			t.Errorf("ParseBytes(%q) = %d, want %d", c.in, got, c.want)
		}
	}
}

// ParseBytes treats bare SI suffixes (KB, MB, GB, ...) as strict
// SI: 1KB = 1000, matching kubectl / docker / kafka convention.
func TestParseBytes_SISuffixesStrict(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in   string
		want int64
	}{
		{"1KB", 1000},
		{"1MB", 1_000_000},
		{"1.5GB", 1_500_000_000},
		{"2TB", 2_000_000_000_000},
		{"1KiB", 1024}, // IEC stays 1024-based
		{"1MiB", 1 << 20},
	}
	for _, c := range cases {
		got, err := ParseBytes(c.in)
		if err != nil {
			t.Errorf("ParseBytes(%q): %v", c.in, err)
		}
		if got != c.want {
			t.Errorf("ParseBytes(%q) = %d, want %d", c.in, got, c.want)
		}
	}
}

// ParseBytesIEC reinterprets bare SI suffixes as 1024-based for
// interop with legacy "disk-vendor" tools.
func TestParseBytesIEC_SISuffixesAsBinary(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in   string
		want int64
	}{
		{"1KB", 1024},
		{"1MB", 1 << 20},
		{"1.5GB", 1<<30 + 1<<29},
		{"2tb", 2 << 40},
		{"1KiB", 1024}, // IEC suffixes still 1024-based
	}
	for _, c := range cases {
		got, err := ParseBytesIEC(c.in)
		if err != nil {
			t.Errorf("ParseBytesIEC(%q): %v", c.in, err)
		}
		if got != c.want {
			t.Errorf("ParseBytesIEC(%q) = %d, want %d", c.in, got, c.want)
		}
	}
}

func TestParseBytes_Errors(t *testing.T) {
	t.Parallel()
	for _, in := range []string{"", "  ", "abc", "1.5XYZ", "KiB"} {
		if _, err := ParseBytes(in); err == nil {
			t.Errorf("ParseBytes(%q) succeeded; want error", in)
		}
	}
}

// --- Round-trip ---

// FormatBytes always emits IEC binary units, so a round trip
// through ParseBytes (which honors IEC suffixes as 1024-based)
// recovers the original value.
func TestFormatBytes_RoundTrip(t *testing.T) {
	t.Parallel()
	canonical := []string{
		"512 B",
		"1 KiB",
		"1.5 KiB",
		"1 MiB",
		"1.5 MiB",
		"1 GiB",
	}
	for _, s := range canonical {
		n, err := ParseBytes(strings.ReplaceAll(s, " ", ""))
		if err != nil {
			t.Errorf("ParseBytes(%q): %v", s, err)
			continue
		}
		got := FormatBytes(n)
		if got != s {
			t.Errorf("round-trip: %q -> %d -> %q", s, n, got)
		}
	}
}

// --- ParseBytes large units ---

func TestParseBytes_ExtremeValues(t *testing.T) {
	t.Parallel()
	for _, in := range []string{"1PiB", "1EiB", "1.5p", "2eb"} {
		got, err := ParseBytes(in)
		if err != nil {
			t.Errorf("ParseBytes(%q): %v", in, err)
		}
		if got <= 0 {
			t.Errorf("ParseBytes(%q) = %d", in, got)
		}
	}
}
