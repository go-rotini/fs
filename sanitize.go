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
// [IsReservedName], an underscore is inserted before the last
// extension: "CON" becomes "CON_", "con.txt" becomes "con_.txt".
// Windows recognizes the device by stem, so suffixing the whole
// filename ("CON.txt_") would leave it reserved.
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
	// Reserved-stem rewrite: insert "_" before the last dot, or
	// append if there is no dot. Defends against Windows treating
	// "CON.txt_" as the CON device.
	if dot := strings.LastIndex(out, "."); dot > 0 {
		return out[:dot] + "_" + out[dot:]
	}
	return out + "_"
}

// IsReservedName reports whether name, with its last extension removed,
// is a Windows reserved device name: CON, PRN, AUX, NUL, COM1–COM9 or
// LPT1–LPT9, compared case-insensitively. The answer is the same on
// every platform, so files meant for Windows can be checked anywhere.
// Only the last extension is removed, so "CON.tar.gz" is not reported.
func IsReservedName(name string) bool {
	base := name
	if dot := strings.LastIndex(base, "."); dot >= 0 {
		base = base[:dot]
	}
	upper := strings.ToUpper(base)
	_, ok := reservedNames[upper]
	return ok
}

var reservedNames = func() map[string]struct{} {
	set := map[string]struct{}{
		"CON": {}, "PRN": {}, "AUX": {}, "NUL": {},
	}
	for i := 1; i <= 9; i++ {
		set["COM"+string(rune('0'+i))] = struct{}{}
		set["LPT"+string(rune('0'+i))] = struct{}{}
	}
	return set
}()
