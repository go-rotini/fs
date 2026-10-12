package fs

import (
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
)

// FormatBytes formats n as a human-readable IEC string ("1.5 GiB").
// Negative values are formatted with a leading minus. Values below
// 1024 are returned as raw byte counts ("999 B"). Output uses one
// decimal place for non-integer mantissas.
func FormatBytes(n int64) string {
	if n < 0 {
		// The magnitude of math.MinInt64 doesn't fit in an int64, so negate in uint64.
		return "-" + formatMagnitude(-uint64(n))
	}
	return formatMagnitude(uint64(n))
}

func formatMagnitude(n uint64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}

	suffixes := []string{"KiB", "MiB", "GiB", "TiB", "PiB", "EiB"}
	div := uint64(unit)
	exp := 0
	for n >= div*unit && exp < len(suffixes)-1 {
		div *= unit
		exp++
	}
	value := float64(n) / float64(div)

	if value == math.Trunc(value) {
		return fmt.Sprintf("%d %s", uint64(value), suffixes[exp])
	}
	return fmt.Sprintf("%.1f %s", value, suffixes[exp])
}

// ParseBytes parses a human-readable size string with strict SI
// semantics: `KB` = 1000, `MB` = 1000000, etc. IEC binary units
// (`KiB`, `MiB`, ...) keep their canonical 1024-based meaning.
//
// This is the same convention `kubectl`, `docker`, `kafka`, and
// most other modern CLI tools use. For the legacy "disk-vendor"
// idiom where bare `KB` means 1024, see [ParseBytesIEC].
//
// Accepted units (case-insensitive):
//
//	bare / B               -> 1
//	K / KB                 -> 1000          KiB -> 1024
//	M / MB                 -> 1000²         MiB -> 1024²
//	G / GB                 -> 1000³         GiB -> 1024³
//	T / TB                 -> 1000⁴         TiB -> 1024⁴
//	P / PB                 -> 1000⁵         PiB -> 1024⁵
//	E / EB                 -> 1000⁶         EiB -> 1024⁶
//
// Whitespace between the number and the unit is optional. A bare
// number (no unit) is treated as bytes. Decimal mantissas are
// supported ("1.5GB" is 1500000000); a fractional byte count is
// truncated. A leading sign is accepted. Errors wrap
// [ErrInvalidByteSize], including a size that doesn't fit in an
// int64 ("10EB").
func ParseBytes(s string) (int64, error) {
	return parseBytes(s, false)
}

// ParseBytesIEC is [ParseBytes] but with the legacy "disk-vendor"
// idiom where bare `KB` / `MB` / `GB` mean powers of 1024 (so
// `1KB == 1024`). IEC-suffixed units (`KiB`, `MiB`, ...) are
// unchanged.
//
// Use this when interoperating with tools that quote disk sizes in
// the old `KB == 1024` convention. New code should prefer
// [ParseBytes].
func ParseBytesIEC(s string) (int64, error) {
	return parseBytes(s, true)
}

func parseBytes(raw string, iec bool) (int64, error) {
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

	mult, ok := unitMultiplier(unit, iec)
	if !ok {
		return 0, fmt.Errorf("%w: unknown unit %q", ErrInvalidByteSize, unit)
	}
	overflow := fmt.Errorf("%w: %q overflows int64", ErrInvalidByteSize, raw)

	// Whole numbers are multiplied exactly; a float64 would round
	// values near the int64 limits.
	if !strings.Contains(numStr, ".") {
		n, err := strconv.ParseInt(numStr, 10, 64)
		if errors.Is(err, strconv.ErrRange) {
			return 0, overflow
		}
		if err != nil {
			return 0, fmt.Errorf("%w: invalid number %q: %w", ErrInvalidByteSize, numStr, err)
		}
		if n > math.MaxInt64/mult || n < math.MinInt64/mult {
			return 0, overflow
		}
		return n * mult, nil
	}

	value, err := strconv.ParseFloat(numStr, 64)
	if err != nil {
		return 0, fmt.Errorf("%w: invalid number %q: %w", ErrInvalidByteSize, numStr, err)
	}
	// 2⁶³ is exact in float64: anything at or above it overflows,
	// while -2⁶³ itself fits.
	const limit = float64(1 << 63)
	v := value * float64(mult)
	if v >= limit || v < -limit {
		return 0, overflow
	}
	return int64(v), nil
}

// unitMultiplier resolves a normalized (lowercase, trimmed) unit
// string to a byte multiplier. When iec=true, bare SI units
// (K, KB, M, MB, ...) are interpreted as powers of 1024; when
// iec=false (the default for [ParseBytes]), they're strict SI
// powers of 1000. The IEC-suffixed units (kib, mib, ...) are
// always 1024-based.
func unitMultiplier(u string, iec bool) (int64, bool) {
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
		if iec {
			return 1 << 10, true
		}
		return 1000, true
	case "m", "mb":
		if iec {
			return 1 << 20, true
		}
		return 1000 * 1000, true
	case "g", "gb":
		if iec {
			return 1 << 30, true
		}
		return 1000 * 1000 * 1000, true
	case "t", "tb":
		if iec {
			return 1 << 40, true
		}
		return 1000 * 1000 * 1000 * 1000, true
	case "p", "pb":
		if iec {
			return 1 << 50, true
		}
		return 1000 * 1000 * 1000 * 1000 * 1000, true
	case "e", "eb":
		if iec {
			return 1 << 60, true
		}
		return 1000 * 1000 * 1000 * 1000 * 1000 * 1000, true
	}
	return 0, false
}
