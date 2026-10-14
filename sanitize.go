package fs

import (
	"strings"
)

// SanitizeFilename returns name with characters illegal on Windows
// or POSIX stripped, suitable for use as a basename anywhere. The
// result is the input minus:
//
//   - ASCII control bytes (NUL through 0x1F)
//   - The Windows-illegal characters: < > : " | ? * / \
//   - Trailing dots and spaces (Windows trims them silently, leading
//     to surprising path resolution)
//
// If the cleaned name is a Windows reserved device name per
// [IsReservedName], an underscore is inserted before the first dot:
// "CON" becomes "CON_", "con.tar.gz" becomes "con_.tar.gz". Windows
// recognizes the device by the part before the first dot, so
// suffixing anywhere later ("CON.tar_.gz") would leave it reserved.
//
// An empty cleaned result falls back to `_`. Length is not checked.
func SanitizeFilename(name string) string {
	var b strings.Builder
	b.Grow(len(name))
	for _, r := range name {
		if r < 0x20 {
			continue
		}
		switch r {
		case '<', '>', ':', '"', '|', '?', '*', '/', '\\':
			continue
		}
		b.WriteRune(r)
	}
	out := strings.TrimRight(b.String(), ". ")
	if out == "" {
		return "_"
	}
	if !IsReservedName(out) {
		return out
	}
	// Reserved-stem rewrite: insert "_" before the first dot, or
	// append if there is no dot.
	if dot := strings.IndexByte(out, '.'); dot > 0 {
		return out[:dot] + "_" + out[dot:]
	}
	return out + "_"
}

// IsReservedName reports whether Windows treats name as a reserved
// device name: CON, PRN, AUX, NUL, COM1–COM9, LPT1–LPT9, or COM and
// LPT followed by a superscript ¹, ² or ³, compared
// case-insensitively. As on Windows, everything from the first dot
// on is ignored, as are spaces before it, so "CON.tar.gz", "nul.txt"
// and "AUX .log" are all reserved. The answer is the same on every
// platform, so files meant for Windows can be checked anywhere.
func IsReservedName(name string) bool {
	stem := name
	if dot := strings.IndexByte(stem, '.'); dot >= 0 {
		stem = stem[:dot]
	}
	stem = strings.TrimRight(stem, " ")
	_, ok := reservedNames[strings.ToUpper(stem)]
	return ok
}

var reservedNames = func() map[string]struct{} {
	set := map[string]struct{}{
		"CON": {}, "PRN": {}, "AUX": {}, "NUL": {},
	}
	for _, digit := range "123456789¹²³" {
		set["COM"+string(digit)] = struct{}{}
		set["LPT"+string(digit)] = struct{}{}
	}
	return set
}()
