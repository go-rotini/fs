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

	// maxGitignoreDoubleStar caps the number of `**` segments in a
	// single pattern. matchSegments is O(n^k) in k (the `**` count);
	// an adversarial pattern against a deep path could exponentially
	// blow up matching otherwise. git itself caps near 16.
	maxGitignoreDoubleStar = 16
)

// Gitignore is a compiled set of .gitignore-style ignore rules.
// Match reports whether a path is ignored. The matcher implements
// the standard gitignore syntax: leading `!` negation, trailing `/`
// directory-only, leading or embedded `/` anchoring, `**` recursive
// wildcard, and the `*`/`?`/`[...]` per-segment globs. Edge cases
// outside the standard syntax (interaction with git's index,
// non-POSIX character-class collation) are out of scope.
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
}

// NewGitignore compiles patterns into a [*Gitignore]. The input has
// the same format as a .gitignore file body: one pattern per line,
// `#` comments, blank lines ignored.
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
// a [*Gitignore].
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
// separators. isDir indicates whether the entry is a directory;
// dir-only patterns (trailing `/`) match only when this is true.
//
// Match implements the gitignore precedence rule: later patterns
// override earlier ones. A negation (leading `!`) re-includes a
// previously-ignored path; see [WithWalkGitignore] for ancestor-
// directory enforcement during walks.
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
	if doubleStars > maxGitignoreDoubleStar {
		return gitignorePattern{}, false
	}
	return p, true
}

// matches reports whether p matches relPath.
//
// For anchored patterns, p.segments are matched against the head of
// relPath's segments. For unanchored patterns, matching is attempted
// at every starting index. `**` consumes zero or more path segments.
func (p gitignorePattern) matches(relPath string) bool {
	pathSegs := strings.Split(relPath, "/")

	if p.anchored {
		return matchSegments(p.segments, pathSegs)
	}
	for i := 0; i <= len(pathSegs); i++ {
		if matchSegments(p.segments, pathSegs[i:]) {
			return true
		}
	}
	return false
}

// matchSegments runs pattern segments against path segments with
// `**` awareness. Returns true when the pattern consumes a prefix of
// the path, giving the standard parent-dir-ignore semantics (pattern
// "foo" matches path "foo/bar").
func matchSegments(pat, p []string) bool {
	if len(pat) == 0 {
		return true
	}
	if pat[0] == "**" {
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
// path suitable for [*Gitignore.Match].
func toRelPosix(root, abs string) string {
	rel, err := filepath.Rel(root, abs)
	if err != nil {
		return ""
	}
	return filepath.ToSlash(rel)
}
