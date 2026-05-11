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
// caller doesn't set [WithFindByContentMaxSize].
const findContentDefaultMaxFileSize = 100 * 1024 * 1024

// ContentMatch is a single line that matched a [FindByContent] search.
type ContentMatch struct {
	// Path is the absolute path of the file that contained the match.
	Path string

	// Line is the 1-indexed line number where the match was found.
	Line int

	// Text is the matching line's contents with trailing CR/LF
	// stripped.
	Text string
}

// WithFindByContentMaxSize caps the size of files [FindByContent]
// will scan. Files larger than n are skipped. Pass 0 or negative to
// use the default cap (100 MiB).
//
// Returned as a [WalkOption] so callers pass it alongside other walk
// filters. Setting it on a plain [Walk] is a no-op.
func WithFindByContentMaxSize(n int64) WalkOption {
	return func(o *walkOptions) {
		if n > 0 {
			o.findContentMaxSize = n
		}
	}
}

// FindByContent walks root looking for files whose contents contain
// substr. Returns one [ContentMatch] per matching line.
//
// Pass any [WalkOption] to scope the walk or tune content-search
// behavior (e.g., [WalkSkipPatterns], [WithWalkGitignore],
// [WithFindByContentMaxSize]).
//
// Lines containing a NUL byte are skipped (matching grep's
// --binary-files=without-match behavior). Files larger than the
// configured cap (default 100 MiB) are skipped.
func FindByContent(root, substr string, opts ...WalkOption) ([]ContentMatch, error) {
	if substr == "" {
		return nil, wrapPathError(opFindByContent, root, ErrInvalidPath)
	}
	return findByContentImpl(root, opts, func(line string) bool {
		return strings.Contains(line, substr)
	})
}

// FindByContentRegex is the regex variant of [FindByContent].
// re.MatchString is used per line.
func FindByContentRegex(root string, re *regexp.Regexp, opts ...WalkOption) ([]ContentMatch, error) {
	if re == nil {
		return nil, wrapPathError(opFindByContent, root, ErrInvalidPath)
	}
	return findByContentImpl(root, opts, re.MatchString)
}

func findByContentImpl(root string, opts []WalkOption, pred func(string) bool) ([]ContentMatch, error) {
	cfg := newWalkOptions(opts)
	maxFileSize := cfg.findContentMaxSize
	if maxFileSize <= 0 {
		maxFileSize = findContentDefaultMaxFileSize
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
		if !info.Mode().IsRegular() || info.Size() > maxFileSize {
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
	}, opts...)
	if err != nil {
		return nil, err
	}
	return matches, nil
}
