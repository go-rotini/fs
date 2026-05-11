package fs

import "bytes"

// LineEnding identifies a line-ending convention.
type LineEnding int

const (
	// LineNone means no line endings were detected.
	LineNone LineEnding = iota

	// LineLF is Unix-style "\n".
	LineLF

	// LineCRLF is Windows-style "\r\n".
	LineCRLF

	// LineCR is classic Mac-style "\r".
	LineCR

	// LineMixed indicates more than one line-ending family is
	// present in the input.
	LineMixed
)

// String renders the [LineEnding] as a short human-readable label.
func (l LineEnding) String() string {
	switch l {
	case LineNone:
		return "none"
	case LineLF:
		return "lf"
	case LineCRLF:
		return "crlf"
	case LineCR:
		return "cr"
	case LineMixed:
		return "mixed"
	}
	return unknownLabel
}

// utf8BOM is the three-byte UTF-8 byte-order mark.
var utf8BOM = []byte{0xEF, 0xBB, 0xBF}

// StripUTF8BOM returns b with a leading UTF-8 BOM removed, or b
// unchanged when no BOM is present. A BOM that does NOT appear at
// offset 0 is not stripped; it's a real character anywhere else.
func StripUTF8BOM(b []byte) []byte {
	if len(b) >= len(utf8BOM) && bytes.Equal(b[:len(utf8BOM)], utf8BOM) {
		return b[len(utf8BOM):]
	}
	return b
}

// DetectLineEnding scans b and reports which line-ending family is
// in use. [LineMixed] is returned when more than one family appears.
// A trailing CR followed by LF is counted as a single CRLF.
func DetectLineEnding(b []byte) LineEnding {
	var hasLF, hasCRLF, hasCR bool
	for i := 0; i < len(b); i++ {
		switch b[i] {
		case '\r':
			if i+1 < len(b) && b[i+1] == '\n' {
				hasCRLF = true
				i++
				continue
			}
			hasCR = true
		case '\n':
			hasLF = true
		}
	}
	types := 0
	var single LineEnding
	if hasLF {
		types++
		single = LineLF
	}
	if hasCRLF {
		types++
		single = LineCRLF
	}
	if hasCR {
		types++
		single = LineCR
	}
	switch {
	case types == 0:
		return LineNone
	case types > 1:
		return LineMixed
	default:
		return single
	}
}

// NormalizeLineEndings rewrites every CR / LF / CRLF in b as target.
// A final line without a terminator is preserved as-is.
//
// target = [LineNone] is a no-op (returns b unchanged).
func NormalizeLineEndings(b []byte, target LineEnding) []byte {
	var sep string
	switch target {
	case LineLF:
		sep = "\n"
	case LineCRLF:
		sep = "\r\n"
	case LineCR:
		sep = "\r"
	case LineNone, LineMixed:
		return b
	default:
		return b
	}
	var out bytes.Buffer
	out.Grow(len(b))
	for i := 0; i < len(b); i++ {
		c := b[i]
		if c == '\r' && i+1 < len(b) && b[i+1] == '\n' {
			out.WriteString(sep)
			i++
			continue
		}
		if c == '\r' || c == '\n' {
			out.WriteString(sep)
			continue
		}
		out.WriteByte(c)
	}
	return out.Bytes()
}
