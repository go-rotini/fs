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
// If the cleaned stem matches a Windows reserved device name (CON,
// PRN, AUX, NUL, COM1–COM9, LPT1–LPT9), regardless of case or
// extension, an underscore is inserted IMMEDIATELY AFTER the stem
// (before any extension): "CON" → "CON_", "CON.txt" → "CON_.txt".
// This ensures the result no longer parses as a reserved device name
// on Windows — Windows recognizes the device by stem, so suffixing
// the whole filename ("CON.txt_") leaves the file still reserved.
//
// An empty cleaned result falls back to `_`. SanitizeFilename does
// NOT validate length — Windows MAX_PATH (260) and component-length
// limits are filesystem-dependent; callers constrain those
// separately if they matter.
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

// IsReservedName reports whether name (or its base before the last
// extension) matches a Windows reserved device name. Comparison is
// case-insensitive. Reserved names: CON, PRN, AUX, NUL, COM1–COM9,
// LPT1–LPT9.
//
// On any platform that lacks these reservations, the result is still
// returned — the predicate is portability-safe rather than
// platform-conditional, so callers writing files for cross-platform
// consumption (archive extraction, scaffolding) catch reserved
// names regardless of the host OS.
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
