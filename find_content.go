package fs

import (
	"bufio"
	"bytes"
	"errors"
	stdfs "io/fs"
	"os"
	"regexp"
)

const opFindByContent = "findbycontent"

// findContentDefaultMaxFileSize caps per-file scanning when the
// caller doesn't set [WithFindByContentMaxSize].
const findContentDefaultMaxFileSize = 100 * 1024 * 1024

// ContentMatch is a single line that matched a [FindByContent] search.
type ContentMatch struct {
	// Path is the walk path of the file that contained the match
	// (absolute only when the search root is).
	Path string

	// Line is the 1-indexed line number where the match was found.
	Line int

	// Text is the matching line's contents with trailing CR/LF
	// stripped.
	Text string
}

// WithFindByContentMaxSize caps the size of files [FindByContent]
// and [FindByContentRegex] will scan. Files larger than n are
// skipped. Pass 0 or negative to use the default cap (100 MiB).
// Other functions that take a [WalkOption] ignore it.
func WithFindByContentMaxSize(n int64) WalkOption {
	return func(o *walkOptions) {
		if n > 0 {
			o.findContentMaxSize = n
		}
	}
}

// FindByContent walks root looking for regular files whose contents
// contain substr. Returns one [ContentMatch] per matching line. An
// empty substr is rejected with an error wrapping [ErrInvalidPath].
//
// Pass any [WalkOption] to scope the walk or tune content-search
// behavior (e.g., [WalkSkipPatterns], [WithWalkGitignore],
// [WithFindByContentMaxSize]).
//
// Scanning a file stops at the first line containing a NUL byte
// (treated as binary) or longer than 1 MiB; matches from earlier
// lines are kept. Files larger than the configured cap (default
// 100 MiB) are skipped. On error no matches are returned.
func FindByContent(root, substr string, opts ...WalkOption) ([]ContentMatch, error) {
	if substr == "" {
		return nil, wrapPathError(opFindByContent, root, ErrInvalidPath)
	}
	needle := []byte(substr)
	return findByContentImpl(root, opts, func(line []byte) bool {
		return bytes.Contains(line, needle)
	})
}

// FindByContentRegex is [FindByContent] matching each line against
// re. A nil re is rejected with an error wrapping [ErrInvalidPath].
func FindByContentRegex(root string, re *regexp.Regexp, opts ...WalkOption) ([]ContentMatch, error) {
	if re == nil {
		return nil, wrapPathError(opFindByContent, root, ErrInvalidPath)
	}
	return findByContentImpl(root, opts, re.Match)
}

func findByContentImpl(root string, opts []WalkOption, pred func([]byte) bool) ([]ContentMatch, error) {
	cfg := newWalkOptions(opts)
	maxFileSize := cfg.findContentMaxSize
	if maxFileSize <= 0 {
		maxFileSize = findContentDefaultMaxFileSize
	}

	var matches []ContentMatch
	// One scan buffer serves every file; a line becomes a string only
	// when it matches.
	buf := make([]byte, 0, 64*1024)
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
		sc.Buffer(buf, 1024*1024)
		lineNo := 0
		for sc.Scan() {
			lineNo++
			line := sc.Bytes()
			if bytes.IndexByte(line, 0) >= 0 {
				return nil // binary file
			}
			if pred(line) {
				matches = append(matches, ContentMatch{
					Path: path,
					Line: lineNo,
					Text: string(bytes.TrimRight(line, "\r")),
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
