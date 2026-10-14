package fs

import (
	"encoding/json"
	"errors"
	stdfs "io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// WorkspaceRoot describes one root inside a multi-root workspace.
type WorkspaceRoot struct {
	// Path is the absolute path of the workspace member.
	Path string

	// Kind identifies the manifest form that produced this root.
	// One of "go.work", "package.json", or "pnpm-workspace.yaml".
	Kind string
}

// WorkspaceRoots discovers the member roots of a multi-root
// workspace anchored at root. The detector inspects a small set of
// canonical manifest files in root:
//
//   - go.work: each `use` directive.
//   - package.json with a `workspaces` field: array of globs or an
//     object with a `packages` array. A malformed file yields no
//     members.
//   - pnpm-workspace.yaml: the top-level `packages:` key followed by
//     `- glob` lines; other top-level keys are ignored, and any other
//     form of `packages` (or a second one) returns an error.
//
// Globs are matched relative to root with [path/filepath.Glob] syntax;
// `~` and environment variables are not expanded, since the manifest
// may come from an untrusted checkout. A `**` segment matches any
// number of directories (skipping node_modules and hidden
// directories), and a glob starting with `!` excludes the members it
// matches. An entry that matches nothing is kept as a literal path.
// Returns no roots when no manifest is present. Duplicate member
// paths across manifests are deduplicated.
func WorkspaceRoots(root string) ([]WorkspaceRoot, error) {
	root = filepath.Clean(root)
	var out []WorkspaceRoot
	seen := map[string]struct{}{}

	add := func(path, kind string) {
		abs, err := filepath.Abs(path)
		if err != nil {
			return
		}
		if _, ok := seen[abs]; ok {
			return
		}
		seen[abs] = struct{}{}
		out = append(out, WorkspaceRoot{Path: abs, Kind: kind})
	}

	if members, err := parseGoWork(filepath.Join(root, "go.work")); err == nil {
		for _, m := range members {
			add(filepath.Join(root, m), "go.work")
		}
	} else if !errors.Is(err, stdfs.ErrNotExist) {
		return nil, err
	}

	if members, err := parseNpmWorkspaces(filepath.Join(root, "package.json"), root); err == nil {
		for _, m := range members {
			add(m, "package.json")
		}
	} else if !errors.Is(err, stdfs.ErrNotExist) {
		return nil, err
	}

	if members, err := parsePnpmWorkspaces(filepath.Join(root, "pnpm-workspace.yaml"), root); err == nil {
		for _, m := range members {
			add(m, "pnpm-workspace.yaml")
		}
	} else if !errors.Is(err, stdfs.ErrNotExist) {
		return nil, err
	}

	return out, nil
}

// parseGoWork extracts member paths from a go.work file. The grammar
// (`use ( path1 path2 ... )` or `use path`) is small enough to parse
// line by line.
func parseGoWork(path string) ([]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err //nolint:wrapcheck // caller distinguishes via errors.Is
	}
	var members []string
	inUseBlock := false
	for line := range strings.SplitSeq(string(data), "\n") {
		line = strings.TrimSpace(stripGoWorkComment(line))
		if line == "" {
			continue
		}
		if inUseBlock {
			if line == ")" {
				inUseBlock = false
				continue
			}
			members = append(members, unquote(line))
			continue
		}
		if line == "use (" {
			inUseBlock = true
			continue
		}
		if strings.HasPrefix(line, "use ") {
			members = append(members, unquote(strings.TrimSpace(line[len("use "):])))
		}
	}
	return members, nil
}

// parseNpmWorkspaces extracts member paths from a package.json's
// `workspaces` field. The field is either an array of glob patterns
// or an object with a `packages` array. Globs are expanded via [Glob].
func parseNpmWorkspaces(path, root string) ([]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err //nolint:wrapcheck // caller distinguishes via errors.Is
	}
	var raw map[string]json.RawMessage
	if uerr := json.Unmarshal(data, &raw); uerr != nil {
		return nil, nil //nolint:nilerr // malformed package.json: report no members
	}
	ws, ok := raw["workspaces"]
	if !ok {
		return nil, nil
	}
	var globs []string
	if jerr := json.Unmarshal(ws, &globs); jerr != nil {
		var obj struct {
			Packages []string `json:"packages"`
		}
		if oerr := json.Unmarshal(ws, &obj); oerr != nil {
			return nil, nil //nolint:nilerr // malformed workspaces field: report no members
		}
		globs = obj.Packages
	}
	return expandGlobsInRoot(root, globs), nil
}

// parsePnpmWorkspaces extracts member globs from a pnpm-workspace.yaml.
//
// Supported subset: the `packages:` list form with `- "glob"` entries.
// Files using YAML features outside this subset (inline maps, anchors,
// multi-line strings, multiple top-level keys) are rejected with
// [errPnpmWorkspaceUnsupported] rather than silently returning empty.
func parsePnpmWorkspaces(path, root string) ([]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err //nolint:wrapcheck // caller distinguishes via errors.Is
	}
	var globs []string
	inPackages := false
	sawPackagesKey := false
	for line := range strings.SplitSeq(string(data), "\n") {
		rawTrim := strings.TrimRight(line, "\r")
		trim := strings.TrimSpace(rawTrim)
		if trim == "" || strings.HasPrefix(trim, "#") {
			continue
		}

		// Top-level (non-indented) keys end any open packages section.
		if rawTrim == trim {
			inPackages = false
		}

		if trim == "packages:" {
			if sawPackagesKey {
				return nil, errPnpmWorkspaceUnsupported
			}
			sawPackagesKey = true
			inPackages = true
			continue
		}
		// Inline-map `packages: [a, b]` and similar forms are not
		// supported; fail loudly rather than silently miss members.
		if strings.HasPrefix(trim, "packages:") {
			return nil, errPnpmWorkspaceUnsupported
		}

		if inPackages {
			if !strings.HasPrefix(trim, "- ") {
				return nil, errPnpmWorkspaceUnsupported
			}
			globs = append(globs, unquote(strings.TrimSpace(trim[2:])))
		}
	}
	return expandGlobsInRoot(root, globs), nil
}

var errPnpmWorkspaceUnsupported = errors.New("fs: workspace: pnpm-workspace.yaml uses unsupported YAML feature (only `packages:` list form is supported)")

// stripGoWorkComment drops a `//` comment that isn't inside a quoted
// path.
func stripGoWorkComment(line string) string {
	var quote byte
	for i := 0; i < len(line); i++ {
		c := line[i]
		switch {
		case quote != 0:
			if c == '\\' && quote == '"' {
				i++
			} else if c == quote {
				quote = 0
			}
		case c == '"' || c == '`':
			quote = c
		case c == '/' && i+1 < len(line) && line[i+1] == '/':
			return line[:i]
		}
	}
	return line
}

func expandGlobsInRoot(root string, globs []string) []string {
	var members, excludes []string
	for _, g := range globs {
		if rest, ok := strings.CutPrefix(g, "!"); ok {
			excludes = append(excludes, filepath.ToSlash(filepath.Clean(rest)))
			continue
		}
		matches := workspaceGlob(root, g)
		if len(matches) == 0 {
			// Treat a non-glob or no-match entry as a literal path.
			members = append(members, filepath.Join(root, g))
			continue
		}
		members = append(members, matches...)
	}
	if len(excludes) == 0 {
		return members
	}
	kept := members[:0]
	for _, m := range members {
		rel, err := filepath.Rel(root, m)
		if err != nil || !workspaceExcluded(filepath.ToSlash(rel), excludes) {
			kept = append(kept, m)
		}
	}
	return kept
}

func workspaceExcluded(rel string, excludes []string) bool {
	var m segMatcher
	segs := strings.Split(rel, "/")
	for _, ex := range excludes {
		if fullSegmentMatch(&m, strings.Split(ex, "/"), segs) {
			return true
		}
	}
	return false
}

// fullSegmentMatch reports whether pat matches all of segs.
func fullSegmentMatch(m *segMatcher, pat, segs []string) bool {
	ends := m.run(pat, segs, true, true)
	return len(ends) > 0 && ends[len(ends)-1] == len(segs)
}

// workspaceGlob matches g against root. Without `**` it is
// [path/filepath.Glob]; with it, root's directories are walked.
func workspaceGlob(root, g string) []string {
	g = filepath.ToSlash(filepath.Clean(g))
	if !slices.Contains(strings.Split(g, "/"), "**") {
		matches, err := filepath.Glob(filepath.Join(root, filepath.FromSlash(g)))
		if err != nil {
			return nil
		}
		return matches
	}
	pat := strings.Split(g, "/")
	var m segMatcher
	var out []string
	_ = filepath.WalkDir(root, func(p string, d stdfs.DirEntry, err error) error { //nolint:errcheck // unreadable directories are skipped
		if err != nil || !d.IsDir() {
			return nil //nolint:nilerr // skip what can't be read
		}
		if p == root {
			return nil
		}
		if name := d.Name(); name == "node_modules" || strings.HasPrefix(name, ".") {
			return filepath.SkipDir
		}
		rel, rerr := filepath.Rel(root, p)
		if rerr != nil {
			return nil //nolint:nilerr // can't happen for a path under root
		}
		if fullSegmentMatch(&m, pat, strings.Split(filepath.ToSlash(rel), "/")) {
			out = append(out, p)
		}
		return nil
	})
	return out
}

// unquote strips matching surrounding double or single quotes.
func unquote(s string) string {
	if len(s) < 2 {
		return s
	}
	first, last := s[0], s[len(s)-1]
	if (first == '"' || first == '\'') && first == last {
		return s[1 : len(s)-1]
	}
	return s
}
