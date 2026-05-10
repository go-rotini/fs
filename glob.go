package fs

import (
	"fmt"
	"path/filepath"
)

const (
	opMatch = "match"
	opGlob  = "glob"
)

// Match reports whether name matches the [filepath.Match] glob
// pattern. Wraps the stdlib function with the package's error
// envelope for consistency.
func Match(pattern, name string) (bool, error) {
	ok, err := filepath.Match(pattern, name)
	if err != nil {
		return false, fmt.Errorf("fs: %s: %w", opMatch, err)
	}
	return ok, nil
}

// Glob returns paths matching pattern after applying [Expand]
// (`~`/`$VAR` resolution) to the pattern. Pattern syntax is
// [filepath.Match]. A pattern with no matches returns (nil, nil) —
// no-match is not an error, mirroring stdlib's [filepath.Glob].
//
// Honors [WithStrictExpansion] via [PathOption].
func Glob(pattern string, opts ...PathOption) ([]string, error) {
	expanded, err := Expand(pattern, opts...)
	if err != nil {
		return nil, err
	}
	matches, err := filepath.Glob(expanded)
	if err != nil {
		return nil, wrapPathError(opGlob, pattern, err)
	}
	return matches, nil
}

// GlobAny returns the deduplicated union of [Glob] over each pattern.
// Order: first occurrence of each match is preserved across patterns.
// On error from any pattern, the partial union accumulated so far is
// returned alongside the error.
func GlobAny(patterns []string, opts ...PathOption) ([]string, error) {
	seen := make(map[string]struct{})
	var union []string
	for _, p := range patterns {
		matches, err := Glob(p, opts...)
		if err != nil {
			return union, err
		}
		for _, m := range matches {
			if _, ok := seen[m]; ok {
				continue
			}
			seen[m] = struct{}{}
			union = append(union, m)
		}
	}
	return union, nil
}
