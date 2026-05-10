package fs

import (
	"bytes"
	"testing"
)

func TestStripUTF8BOM_Present(t *testing.T) {
	t.Parallel()
	in := append([]byte{0xEF, 0xBB, 0xBF}, []byte("hello")...)
	got := StripUTF8BOM(in)
	if string(got) != "hello" {
		t.Errorf("got %q, want %q", got, "hello")
	}
}

func TestStripUTF8BOM_Absent(t *testing.T) {
	t.Parallel()
	in := []byte("hello")
	got := StripUTF8BOM(in)
	if !bytes.Equal(got, in) {
		t.Errorf("got %q, want unchanged", got)
	}
}

func TestStripUTF8BOM_Short(t *testing.T) {
	t.Parallel()
	cases := [][]byte{nil, {}, {0xEF}, {0xEF, 0xBB}}
	for _, c := range cases {
		got := StripUTF8BOM(c)
		if !bytes.Equal(got, c) {
			t.Errorf("input %v: got %v", c, got)
		}
	}
}

func TestStripUTF8BOM_NotAtOffsetZero(t *testing.T) {
	t.Parallel()
	in := append([]byte("hello\n"), 0xEF, 0xBB, 0xBF, 'x')
	got := StripUTF8BOM(in)
	if !bytes.Equal(got, in) {
		t.Errorf("BOM not at offset 0 should be preserved")
	}
}

func TestDetectLineEnding(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		in   []byte
		want LineEnding
	}{
		{"empty", []byte(""), LineNone},
		{"no-newlines", []byte("foo bar"), LineNone},
		{"lf", []byte("foo\nbar\n"), LineLF},
		{"crlf", []byte("foo\r\nbar\r\n"), LineCRLF},
		{"cr", []byte("foo\rbar\r"), LineCR},
		{"mixed-lf-crlf", []byte("foo\nbar\r\n"), LineMixed},
		{"mixed-lf-cr", []byte("foo\nbar\r"), LineMixed},
		{"trailing-cr-then-lf-is-crlf", []byte("a\r\n"), LineCRLF},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := DetectLineEnding(c.in); got != c.want {
				t.Errorf("got %v, want %v", got, c.want)
			}
		})
	}
}

func TestNormalizeLineEndings_LFToCRLF(t *testing.T) {
	t.Parallel()
	in := []byte("a\nb\nc")
	got := NormalizeLineEndings(in, LineCRLF)
	want := []byte("a\r\nb\r\nc")
	if !bytes.Equal(got, want) {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestNormalizeLineEndings_CRLFToLF(t *testing.T) {
	t.Parallel()
	in := []byte("a\r\nb\r\nc")
	got := NormalizeLineEndings(in, LineLF)
	want := []byte("a\nb\nc")
	if !bytes.Equal(got, want) {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestNormalizeLineEndings_MixedToLF(t *testing.T) {
	t.Parallel()
	in := []byte("a\nb\r\nc\rd")
	got := NormalizeLineEndings(in, LineLF)
	want := []byte("a\nb\nc\nd")
	if !bytes.Equal(got, want) {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestNormalizeLineEndings_TrailingNoTerminator(t *testing.T) {
	t.Parallel()
	in := []byte("only one line")
	got := NormalizeLineEndings(in, LineLF)
	if !bytes.Equal(got, in) {
		t.Errorf("got %q, want unchanged", got)
	}
}

func TestNormalizeLineEndings_NoTarget(t *testing.T) {
	t.Parallel()
	in := []byte("a\nb")
	got := NormalizeLineEndings(in, LineNone)
	if !bytes.Equal(got, in) {
		t.Errorf("LineNone target should pass through unchanged")
	}
}

func TestLineEnding_String(t *testing.T) {
	t.Parallel()
	cases := map[LineEnding]string{
		LineNone:  "none",
		LineLF:    "lf",
		LineCRLF:  "crlf",
		LineCR:    "cr",
		LineMixed: "mixed",
	}
	for le, want := range cases {
		if got := le.String(); got != want {
			t.Errorf("%v.String() = %q, want %q", le, got, want)
		}
	}
}
