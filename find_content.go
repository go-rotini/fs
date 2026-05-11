package fs

import (
	"bufio"
	"errors"
	stdfs "io/fs"
	"os"
	"regexp"
	"strings"
)

const opFindByContent = "findbycontent"

// findContentDefaultMaxFileSize caps per-file scanning when the
// caller doesn't set [WithFindByContentMaxSize]. 100 MiB matches the
// package's default read cap.
const findContentDefaultMaxFileSize = 100 * 1024 * 1024

// ContentMatch is a single line that matched a [FindByContent]
// search.
type ContentMatch struct {
	// Path is the absolute path of the file that contained the
	// match.
	Path string

	// Line is the 1-indexed line number where the match was found.
	Line int

	// Text is the matching line's contents with trailing CR/LF
	// stripped.
	Text string
}

// findContentConfig holds per-search options not covered by
// [WalkOption]. Lives in this file rather than the walk options to
// keep the FindByContent surface self-contained.
type findContentConfig struct {
	maxFileSize int64
}

// FindByContentOption configures [FindByContent] / [FindByContentRegex].
// Distinct from [WalkOption] so walk filters and content-search
// knobs can be passed without ambiguity.
type FindByContentOption func(*findContentConfig)

// WithFindByContentMaxSize caps the size of files [FindByContent]
// will scan. Files larger than n are skipped (no match). Pass 0 or
// negative to use the default cap (100 MiB).
func WithFindByContentMaxSize(n int64) FindByContentOption {
	return func(c *findContentConfig) {
		if n > 0 {
			c.maxFileSize = n
		}
	}
}

// findContentOptions partitions a mixed opts slice into walk and
// content-search options. Used so callers can pass either form to
// the variadic API.
type findContentOptions struct {
	walk    []WalkOption
	content []FindByContentOption
}

// FindByContent walks root looking for files whose contents contain
// substr. Returns one [ContentMatch] per matching line (so a single
// file with N matches contributes N entries to the result).
//
// Pass [WalkOption] values (e.g., [WalkSkipPatterns]) to scope the
// walk, and [FindByContentOption] values (e.g.,
// [WithFindByContentMaxSize]) to tune content-search behavior. Both
// kinds can be mixed in the variadic list.
//
// Binary detection: lines containing a NUL byte are skipped silently,
// matching `grep --binary-files=without-match` behavior. Files larger
// than the configured size cap (default 100 MiB) are skipped.
func FindByContent(root, substr string, opts ...any) ([]ContentMatch, error) {
	if substr == "" {
		return nil, wrapPathError(opFindByContent, root, ErrInvalidPath)
	}
	parsed := partitionFindContentOptions(opts)
	return findByContentImpl(root, parsed, func(line string) bool {
		return strings.Contains(line, substr)
	})
}

// FindByContentRegex is the regex variant of [FindByContent].
// re.MatchString is used per line.
func FindByContentRegex(root string, re *regexp.Regexp, opts ...any) ([]ContentMatch, error) {
	if re == nil {
		return nil, wrapPathError(opFindByContent, root, ErrInvalidPath)
	}
	parsed := partitionFindContentOptions(opts)
	return findByContentImpl(root, parsed, re.MatchString)
}

// partitionFindContentOptions splits a mixed variadic into walk +
// content option lists. Unknown types are silently ignored — the
// `any` parameter is the only way to accept two unrelated option
// types in one variadic without forcing a wrapper struct on every
// caller. Any future option-type unification can replace this.
func partitionFindContentOptions(opts []any) findContentOptions {
	var out findContentOptions
	for _, o := range opts {
		switch v := o.(type) {
		case WalkOption:
			out.walk = append(out.walk, v)
		case FindByContentOption:
			out.content = append(out.content, v)
		}
	}
	return out
}

func findByContentImpl(root string, parsed findContentOptions, pred func(string) bool) ([]ContentMatch, error) {
	cfg := findContentConfig{maxFileSize: findContentDefaultMaxFileSize}
	for _, opt := range parsed.content {
		opt(&cfg)
	}

	var matches []ContentMatch
	err := Walk(root, func(path string, d stdfs.DirEntry, werr error) error {
		if werr != nil {
			return werr
		}
		if d.IsDir() {
			return nil
		}
		info, ierr := d.Info()
		if ierr != nil {
			return ierr //nolint:wrapcheck // outer Walk wraps via *PathError
		}
		if !info.Mode().IsRegular() || info.Size() > cfg.maxFileSize {
			return nil
		}

		f, oerr := os.Open(path)
		if oerr != nil {
			if errors.Is(oerr, stdfs.ErrNotExist) {
				return nil
			}
			return oerr //nolint:wrapcheck // outer Walk wraps via *PathError
		}
		defer func() { _ = f.Close() }()

		sc := bufio.NewScanner(f)
		sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
		lineNo := 0
		for sc.Scan() {
			lineNo++
			line := sc.Text()
			if strings.IndexByte(line, 0) >= 0 {
				return nil // binary file
			}
			if pred(line) {
				matches = append(matches, ContentMatch{
					Path: path,
					Line: lineNo,
					Text: strings.TrimRight(line, "\r"),
				})
			}
		}
		return nil
	}, parsed.walk...)
	if err != nil {
		return nil, err
	}
	return matches, nil
}
