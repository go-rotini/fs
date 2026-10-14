package fs

import (
	"path"
	"path/filepath"
	"strings"
	"sync"
)

const (
	opLoadGitignore = "loadgitignore"

	// maxGitignoreDoubleStar caps the number of `**` segments in a
	// single pattern; patterns with more are dropped.
	maxGitignoreDoubleStar = 16

	// maxGitignoreFileSize is the largest ignore file git reads.
	maxGitignoreFileSize = 100 << 20
)

// Gitignore is a compiled set of .gitignore-style ignore rules.
// Match reports whether a path is ignored. The matcher implements
// the standard gitignore syntax: leading `!` negation, trailing `/`
// directory-only, leading or embedded `/` anchoring, `**` recursive
// wildcard, and the `*`/`?`/`[...]` per-segment globs ([path.Match]
// syntax). Not supported: escaped trailing spaces (`\ `), and
// patterns with more than 16 `**` segments, which are dropped. A
// pattern that matches a directory also matches every path below it.
//
// A Gitignore is immutable after construction and safe for concurrent
// use.
type Gitignore struct {
	patterns []gitignorePattern
}

type gitignorePattern struct {
	raw      string
	negated  bool
	dirOnly  bool
	anchored bool
	segments []string
	// doubleStars counts the `**` segments.
	doubleStars int
}

// NewGitignore compiles patterns into a [*Gitignore]. Each element is
// one line of a .gitignore file; blank lines and `#` comments are
// skipped.
func NewGitignore(patterns []string) *Gitignore {
	g := &Gitignore{}
	for _, line := range patterns {
		if p, ok := parseGitignoreLine(line); ok {
			g.patterns = append(g.patterns, p)
		}
	}
	return g
}

// LoadGitignore reads patterns from filePath and compiles them into
// a [*Gitignore]. As git does, it skips a leading UTF-8 byte order
// mark and accepts lines of any length; a file over 100 MiB, which git
// ignores, returns an error wrapping [ErrFileTooLarge].
func LoadGitignore(filePath string) (*Gitignore, error) {
	data, err := ReadFile(filePath, WithMaxSize(maxGitignoreFileSize))
	if err != nil {
		return nil, withOp(opLoadGitignore, err)
	}
	return NewGitignore(strings.Split(string(StripUTF8BOM(data)), "\n")), nil
}

// Match reports whether relPath is ignored. relPath must be relative
// to the directory the .gitignore was placed in, using POSIX
// separators. isDir indicates whether the entry is a directory: a
// dir-only pattern (trailing `/`) matches relPath itself only when it
// is a directory, and matches everything below a directory it
// matches. A trailing `/**` matches everything below a directory but
// not the directory itself.
//
// Match implements the gitignore precedence rule: later patterns
// override earlier ones. A negation (leading `!`) re-includes a
// previously-ignored path; see [WithWalkGitignore] for
// ancestor-directory enforcement during walks.
func (g *Gitignore) Match(relPath string, isDir bool) bool {
	if g == nil || len(g.patterns) == 0 {
		return false
	}
	relPath = strings.TrimPrefix(relPath, "./")
	relPath = strings.TrimPrefix(relPath, "/")
	if relPath == "" || relPath == "." {
		return false
	}

	pathSegs := strings.Split(relPath, "/")
	m := segMatcherPool.Get().(*segMatcher) //nolint:forcetypeassert // the pool only holds *segMatcher
	defer segMatcherPool.Put(m)
	ignored := false
	for _, p := range g.patterns {
		if p.matches(m, pathSegs, isDir) {
			ignored = !p.negated
		}
	}
	return ignored
}

// parseGitignoreLine turns one .gitignore line into a compiled
// pattern. Returns ok=false for blank or comment lines, or for
// patterns whose `**` count exceeds [maxGitignoreDoubleStar].
func parseGitignoreLine(line string) (gitignorePattern, bool) {
	line = strings.TrimRight(line, "\r")
	// Strip trailing whitespace. Real .gitignore allows escaped
	// trailing spaces via `\ `; that corner case is not supported.
	line = strings.TrimRight(line, " \t")

	if line == "" || strings.HasPrefix(line, "#") {
		return gitignorePattern{}, false
	}

	p := gitignorePattern{raw: line}

	if strings.HasPrefix(line, "!") {
		p.negated = true
		line = line[1:]
	}
	// Escaped `\#` or `\!` at the start is a literal char.
	if strings.HasPrefix(line, `\#`) || strings.HasPrefix(line, `\!`) {
		line = line[1:]
	}

	if strings.HasSuffix(line, "/") {
		p.dirOnly = true
		line = line[:len(line)-1]
	}

	// A pattern is anchored if it begins with `/` or contains `/`
	// anywhere other than the (already-stripped) trailing position.
	if strings.HasPrefix(line, "/") {
		p.anchored = true
		line = line[1:]
	} else if strings.Contains(line, "/") {
		p.anchored = true
	}

	p.segments = strings.Split(line, "/")

	doubleStars := 0
	for _, seg := range p.segments {
		if seg == "**" {
			doubleStars++
		}
	}
	p.doubleStars = doubleStars
	if doubleStars > maxGitignoreDoubleStar {
		return gitignorePattern{}, false
	}
	return p, true
}

// matches reports whether p matches the path split into segs.
//
// A pattern matches when it matches a run of whole segments that
// starts at the first segment (anchored) or at any segment
// (unanchored); what follows the run lies below a matched directory.
// A run that ends at the last segment is the entry itself, which a
// dir-only pattern matches only when isDir. `**` matches zero or more
// segments, except at the end of the pattern, where it matches one or
// more.
func (p gitignorePattern) matches(m *segMatcher, segs []string, isDir bool) bool {
	if p.doubleStars == 0 {
		// Without `**` a run is exactly len(p.segments) long, so each
		// start is one fixed comparison.
		n := len(p.segments)
		last := len(segs) - n
		if p.anchored {
			last = min(last, 0)
		}
		for s := 0; s <= last; s++ {
			if (s+n < len(segs) || !p.dirOnly || isDir) && segmentsMatch(p.segments, segs[s:s+n]) {
				return true
			}
		}
		return false
	}
	for _, end := range m.run(p.segments, segs, p.anchored, false) {
		if end < len(segs) || !p.dirOnly || isDir {
			return true
		}
	}
	return false
}

// segmentsMatch reports whether each segment glob in pat matches the
// segment at the same index of segs; they have the same length.
func segmentsMatch(pat, segs []string) bool {
	for i, g := range pat {
		if ok, err := path.Match(g, segs[i]); err != nil || !ok {
			return false
		}
	}
	return true
}

// segMatcherPool reuses matcher buffers across [Gitignore.Match] calls.
var segMatcherPool = sync.Pool{New: func() any { return new(segMatcher) }}

// segMatcher simulates a pattern as a set of positions in its segment
// list, advanced one path segment at a time. That is O(pattern ×
// path) for any number of `**`, where trying every split of the path
// for each `**` would be exponential. Its buffers are reused across
// patterns.
type segMatcher struct {
	cur, next []bool
	ends      []int
}

// run returns the path indexes at which some run of segments ending
// there is fully matched by pat, in increasing order. Unless all is
// set, it stops at the first index before the last segment, since
// that alone decides a gitignore match.
func (m *segMatcher) run(pat, segs []string, anchored, all bool) []int {
	n := len(pat)
	m.cur = resetBools(m.cur, n+1)
	m.next = resetBools(m.next, n+1)
	m.ends = m.ends[:0]

	m.cur[0] = true
	closeOver(pat, m.cur)
	for j := 0; ; j++ {
		if m.cur[n] {
			m.ends = append(m.ends, j)
			if j < len(segs) && !all {
				return m.ends
			}
		}
		if j == len(segs) {
			return m.ends
		}
		clear(m.next)
		for k := range n {
			if !m.cur[k] {
				continue
			}
			if pat[k] == "**" {
				m.next[k] = true
				if k == n-1 {
					m.next[n] = true // a trailing ** has consumed a segment
				}
				continue
			}
			if ok, err := path.Match(pat[k], segs[j]); err == nil && ok {
				m.next[k+1] = true
			}
		}
		if !anchored {
			m.next[0] = true // a run may also start at the next segment
		}
		closeOver(pat, m.next)
		m.cur, m.next = m.next, m.cur
	}
}

// closeOver adds, for every reachable `**` that isn't the last
// segment, the position after it: the `**` matching zero segments.
func closeOver(pat []string, set []bool) {
	for k := range len(pat) - 1 {
		if set[k] && pat[k] == "**" {
			set[k+1] = true
		}
	}
}

func resetBools(b []bool, n int) []bool {
	if cap(b) < n {
		return make([]bool, n)
	}
	b = b[:n]
	clear(b)
	return b
}

// WithWalkGitignore adds gitignore-based filtering to a [Walk]. The
// matcher's anchor is the walk root; matched paths are computed
// relative to root using POSIX separators.
//
// A directory matched by the gitignore is pruned from the walk (its
// contents are not visited). Matched file entries are skipped from
// fn but the walk continues.
func WithWalkGitignore(g *Gitignore) WalkOption {
	return func(o *walkOptions) {
		o.gitignore = g
	}
}

// gitignoreSkip reports whether the entry at rel (relative to the
// walk root, POSIX separators) should be skipped. pruneDir indicates
// whether descent into the entry should also be skipped (true only
// when the entry is itself a matched directory). For nil g returns
// false unconditionally.
func gitignoreSkip(g *Gitignore, rel string, isDir bool) (skip, pruneDir bool) {
	if g == nil || rel == "" || rel == "." {
		return false, false
	}
	if g.Match(rel, isDir) {
		return true, isDir
	}
	return false, false
}

// toRelPosix converts an absolute walk path to a root-relative POSIX
// path suitable for [Gitignore.Match].
func toRelPosix(root, abs string) string {
	rel, err := filepath.Rel(root, abs)
	if err != nil {
		return ""
	}
	return filepath.ToSlash(rel)
}
