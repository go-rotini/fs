package fs

import (
	"bufio"
	"os"
	"path"
	"path/filepath"
	"strings"
)

const (
	opLoadGitignore = "loadgitignore"

	// maxGitignoreDoubleStar caps how many `**` segments a single
	// pattern may contain. `matchSegments` is O(n^k) in k = `**`
	// count, so an adversarial pattern with many `**`s against a
	// deep path triggers exponential blowup. `git` itself caps near
	// 16; the package matches that.
	maxGitignoreDoubleStar = 16
)

// Gitignore is a compiled set of `.gitignore`-style ignore rules.
// Match reports whether a path is ignored. The matcher implements
// the gitignore spec for the cases CLI tooling cares about — leading
// `!` negation, trailing `/` directory-only, leading `/` anchoring,
// `**` recursive wildcard, `*`/`?`/`[...]` per-segment globs. Some
// edge cases of the full spec (interaction with already-tracked
// files in git's index, character-class collation in non-POSIX
// locales) are intentionally out of scope.
//
// A Gitignore is immutable after construction and safe for concurrent
// use.
type Gitignore struct {
	patterns []gitignorePattern
}

// gitignorePattern is one parsed line of a .gitignore file.
type gitignorePattern struct {
	// raw is the original line for diagnostic output.
	raw string

	// negated is true when the pattern began with `!`.
	negated bool

	// dirOnly is true when the pattern ended with `/`.
	dirOnly bool

	// anchored is true when the pattern had a `/` anywhere other than
	// the trailing position (or began with `/`). Anchored patterns
	// match only at the gitignore-root level, walking into the path
	// from the leftmost segment. Unanchored patterns match at any
	// depth.
	anchored bool

	// segments is the pattern split on `/`. Each segment is either a
	// literal/glob (matched per path component) or the special `**`
	// recursive wildcard.
	segments []string
}

// NewGitignore compiles patterns into a [*Gitignore]. The input is
// the same format as a `.gitignore` file's body: one pattern per
// line, `#` comments, blank lines ignored. Unlike [LoadGitignore],
// no file is read.
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
// a [*Gitignore]. The file's contents are parsed as a standard
// `.gitignore` file.
func LoadGitignore(filePath string) (*Gitignore, error) {
	f, err := os.Open(filePath)
	if err != nil {
		return nil, wrapPathError(opLoadGitignore, filePath, err)
	}
	defer func() { _ = f.Close() }()

	var lines []string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		lines = append(lines, sc.Text())
	}
	if serr := sc.Err(); serr != nil {
		return nil, wrapPathError(opLoadGitignore, filePath, serr)
	}
	return NewGitignore(lines), nil
}

// Match reports whether relPath is ignored. relPath must be relative
// to the directory the .gitignore was placed in, using POSIX
// separators (forward slashes). isDir indicates whether the entry is
// a directory; dir-only patterns (trailing `/`) only match when this
// is true.
//
// Match implements the gitignore precedence rule: later patterns
// override earlier ones. A negation (leading `!`) re-includes a
// previously-ignored path, except when an ancestor directory was
// itself ignored — see the package's WalkOption helper for the
// walk-time enforcement of that rule.
func (g *Gitignore) Match(relPath string, isDir bool) bool {
	if g == nil || len(g.patterns) == 0 {
		return false
	}
	relPath = strings.TrimPrefix(relPath, "./")
	relPath = strings.TrimPrefix(relPath, "/")
	if relPath == "" || relPath == "." {
		return false
	}

	ignored := false
	for _, p := range g.patterns {
		if p.dirOnly && !isDir {
			continue
		}
		if p.matches(relPath) {
			ignored = !p.negated
		}
	}
	return ignored
}

// parseGitignoreLine turns one .gitignore line into a compiled
// pattern. Returns ok=false for blank or comment lines.
func parseGitignoreLine(line string) (gitignorePattern, bool) {
	// Trim trailing CR for files written on Windows.
	line = strings.TrimRight(line, "\r")
	// Strip a trailing space (unless escaped). Real .gitignore allows
	// escaped trailing spaces via `\ `; that's a corner case we
	// intentionally skip — none of the popular open-source
	// .gitignore catalogs use it.
	line = strings.TrimRight(line, " \t")

	if line == "" || strings.HasPrefix(line, "#") {
		return gitignorePattern{}, false
	}

	p := gitignorePattern{raw: line}

	if strings.HasPrefix(line, "!") {
		p.negated = true
		line = line[1:]
	}
	// An escaped `\#` or `\!` at the start represents a literal char.
	if strings.HasPrefix(line, `\#`) || strings.HasPrefix(line, `\!`) {
		line = line[1:]
	}

	if strings.HasSuffix(line, "/") {
		p.dirOnly = true
		line = line[:len(line)-1]
	}

	// A pattern is anchored if it contains a slash anywhere (other
	// than the trailing-/ we just stripped) OR begins with `/`.
	if strings.HasPrefix(line, "/") {
		p.anchored = true
		line = line[1:]
	} else if strings.Contains(line, "/") {
		p.anchored = true
	}

	p.segments = strings.Split(line, "/")

	// Refuse patterns with so many `**` segments that matching
	// blows up exponentially. matchSegments tries every position
	// for each `**`; with k of them the search space is O(n^k).
	doubleStars := 0
	for _, seg := range p.segments {
		if seg == "**" {
			doubleStars++
		}
	}
	if doubleStars > maxGitignoreDoubleStar {
		return gitignorePattern{}, false
	}
	return p, true
}

// matches reports whether p matches relPath.
//
// Algorithm:
//
//  1. Split relPath into segments.
//  2. If anchored, attempt to match p.segments against the head of
//     relPath's segments (consuming `**` greedily as needed).
//  3. If unanchored, try matching at every starting index of
//     relPath's segments.
func (p gitignorePattern) matches(relPath string) bool {
	pathSegs := strings.Split(relPath, "/")

	if p.anchored {
		return matchSegments(p.segments, pathSegs)
	}
	// Unanchored: pattern can match at any depth.
	for i := 0; i <= len(pathSegs); i++ {
		if matchSegments(p.segments, pathSegs[i:]) {
			return true
		}
	}
	return false
}

// matchSegments runs pattern segments against path segments with `**`
// awareness. Returns true when the pattern consumes a prefix of the
// path (so a pattern "foo" matches the path "foo/bar" — the parent-
// dir ignore semantics).
func matchSegments(pat, p []string) bool {
	if len(pat) == 0 {
		return true
	}
	if pat[0] == "**" {
		// `**` consumes zero or more path segments. Try every
		// possible split.
		for i := 0; i <= len(p); i++ {
			if matchSegments(pat[1:], p[i:]) {
				return true
			}
		}
		return false
	}
	if len(p) == 0 {
		return false
	}
	matched, err := path.Match(pat[0], p[0])
	if err != nil || !matched {
		return false
	}
	return matchSegments(pat[1:], p[1:])
}

// WithWalkGitignore adds gitignore-based filtering to a [Walk]. The
// gitignore matcher's anchor is the walk root: all matched paths are
// computed relative to root using POSIX separators.
//
// A directory matched by the gitignore is pruned from the walk (its
// contents are not visited). File entries that match are skipped
// from fn but the walk continues.
func WithWalkGitignore(g *Gitignore) WalkOption {
	return func(o *walkOptions) {
		o.gitignore = g
	}
}

// gitignoreSkip reports whether the entry at rel (relative to the
// walk root, POSIX separators) should be skipped according to g.
// Returns ok=true when the skip should happen; pruneDir=true when
// the caller should additionally avoid descending. For nil g returns
// false/false unconditionally.
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
// path suitable for [*Gitignore.Match].
func toRelPosix(root, abs string) string {
	rel, err := filepath.Rel(root, abs)
	if err != nil {
		return ""
	}
	return filepath.ToSlash(rel)
}
