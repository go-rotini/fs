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

// --- ParseBytes (lenient) ---

func TestParseBytes_IECUnits(t *testing.T) {
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

func TestParseBytes_SIUnitsLenient(t *testing.T) {
	t.Parallel()
	// Lenient mode treats KB/MB/GB as 1024-based.
	cases := []struct {
		in   string
		want int64
	}{
		{"1KB", 1024},
		{"1MB", 1 << 20},
		{"1.5GB", 1<<30 + 1<<29},
		{"2tb", 2 << 40},
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

func TestParseBytesStrict_SI(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in   string
		want int64
	}{
		{"1KB", 1000},
		{"1MB", 1_000_000},
		{"1.5GB", 1_500_000_000},
		{"1KiB", 1024}, // IEC stays 1024-based
		{"1MiB", 1 << 20},
	}
	for _, c := range cases {
		got, err := ParseBytesStrict(c.in)
		if err != nil {
			t.Errorf("ParseBytesStrict(%q): %v", c.in, err)
		}
		if got != c.want {
			t.Errorf("ParseBytesStrict(%q) = %d, want %d", c.in, got, c.want)
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
		// strings.NoSpace removes the space so ParseBytes accepts both forms.
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
