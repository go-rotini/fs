package fs

import (
	"fmt"
	"strconv"
	"strings"
)

// FormatBytes formats n as a human-readable IEC string ("1.5 GiB").
// Negative values are formatted with a leading minus. Values below
// 1024 are returned as raw byte counts ("999 B"). Output uses one
// decimal place for non-integer mantissas.
func FormatBytes(n int64) string {
	const unit = 1024
	if n < 0 {
		return "-" + FormatBytes(-n)
	}
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}

	suffixes := []string{"KiB", "MiB", "GiB", "TiB", "PiB", "EiB"}
	div := int64(unit)
	exp := 0
	for n >= div*unit && exp < len(suffixes)-1 {
		div *= unit
		exp++
	}
	value := float64(n) / float64(div)

	if value == float64(int64(value)) {
		return fmt.Sprintf("%d %s", int64(value), suffixes[exp])
	}
	return fmt.Sprintf("%.1f %s", value, suffixes[exp])
}

// ParseBytes parses a human-readable size string. Accepts both IEC
// units (`KiB`, `MiB`, `GiB`, `TiB`, `PiB`, `EiB`) and SI units
// (`KB`, `MB`, `GB`, `TB`, `PB`, `EB`); SI units are interpreted as
// powers of 1024 (matching common CLI usage). Use
// [ParseBytesStrict] for true 1000-base SI semantics.
//
// Whitespace between the number and the unit is optional. The unit
// is case-insensitive. A bare number (no unit) is treated as bytes.
// Decimal mantissas are supported ("1.5GB" → 1610612736).
func ParseBytes(s string) (int64, error) {
	return parseBytes(s, false)
}

// ParseBytesStrict is [ParseBytes] with strict SI semantics: KB=1000,
// MB=1000000, etc. IEC units (KiB, MiB) still mean 1024-based.
func ParseBytesStrict(s string) (int64, error) {
	return parseBytes(s, true)
}

func parseBytes(raw string, strict bool) (int64, error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return 0, fmt.Errorf("%w: empty input", ErrInvalidByteSize)
	}

	// Split number and suffix at the first non-digit/non-dot/non-sign byte.
	end := 0
	for end < len(s) {
		c := s[end]
		if (c >= '0' && c <= '9') || c == '.' || c == '+' || c == '-' {
			end++
			continue
		}
		break
	}
	if end == 0 {
		return 0, fmt.Errorf("%w: no numeric prefix in %q", ErrInvalidByteSize, raw)
	}

	numStr := s[:end]
	unit := strings.TrimSpace(strings.ToLower(s[end:]))

	value, err := strconv.ParseFloat(numStr, 64)
	if err != nil {
		return 0, fmt.Errorf("%w: invalid number %q: %w", ErrInvalidByteSize, numStr, err)
	}

	mult, ok := unitMultiplier(unit, strict)
	if !ok {
		return 0, fmt.Errorf("%w: unknown unit %q", ErrInvalidByteSize, unit)
	}
	return int64(value * float64(mult)), nil
}

// unitMultiplier resolves a normalized (lowercase, trimmed) unit
// string to a byte multiplier. Returns ok=false on unknown units.
func unitMultiplier(u string, strict bool) (int64, bool) {
	switch u {
	case "", "b":
		return 1, true
	case "kib":
		return 1 << 10, true
	case "mib":
		return 1 << 20, true
	case "gib":
		return 1 << 30, true
	case "tib":
		return 1 << 40, true
	case "pib":
		return 1 << 50, true
	case "eib":
		return 1 << 60, true
	case "k", "kb":
		if strict {
			return 1000, true
		}
		return 1 << 10, true
	case "m", "mb":
		if strict {
			return 1000 * 1000, true
		}
		return 1 << 20, true
	case "g", "gb":
		if strict {
			return 1000 * 1000 * 1000, true
		}
		return 1 << 30, true
	case "t", "tb":
		if strict {
			return 1000 * 1000 * 1000 * 1000, true
		}
		return 1 << 40, true
	case "p", "pb":
		if strict {
			return 1000 * 1000 * 1000 * 1000 * 1000, true
		}
		return 1 << 50, true
	case "e", "eb":
		if strict {
			return 1000 * 1000 * 1000 * 1000 * 1000 * 1000, true
		}
		return 1 << 60, true
	}
	return 0, false
}
