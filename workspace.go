package fs

import (
	"encoding/json"
	"errors"
	stdfs "io/fs"
	"os"
	"path/filepath"
	"strings"
)

// WorkspaceRoot describes one root inside a multi-root workspace.
type WorkspaceRoot struct {
	// Path is the absolute path of the workspace member.
	Path string

	// Kind is the kind of workspace declaration that produced this
	// root: "go.work", "package.json", "pnpm-workspace.yaml", or
	// "rush.json".
	Kind string
}

// WorkspaceRoots discovers the member roots of a multi-root
// workspace anchored at root. The detector looks at a small set of
// canonical manifest files and returns every member it can extract:
//
//   - `go.work` — uses each `use` directive; resolves relative paths.
//   - `package.json` with a `workspaces` field — array OR object
//     with a `packages` array; glob patterns are expanded
//     filesystem-side via [Glob].
//   - `pnpm-workspace.yaml` — parses the simple `packages:` list
//     form without depending on a YAML parser (a tiny line-oriented
//     parser handles the common case).
//
// Returns an empty slice when no workspace manifest is present.
// Returns the union of member roots across all detected manifests;
// duplicates (same absolute path under multiple manifests) are
// elided.
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
// is tiny — `use ( path1 path2 ... )` or `use path` — so we parse
// line by line rather than pulling in `go/build`.
func parseGoWork(path string) ([]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err //nolint:wrapcheck // caller distinguishes via errors.Is
	}
	var members []string
	inUseBlock := false
	for line := range strings.SplitSeq(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "//") {
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
// workspaces field. The field can be either an array of glob
// patterns or an object with a `packages` array. Globs are expanded
// against root via [Glob].
func parseNpmWorkspaces(path, root string) ([]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err //nolint:wrapcheck // caller distinguishes via errors.Is
	}
	var raw map[string]json.RawMessage
	if uerr := json.Unmarshal(data, &raw); uerr != nil {
		return nil, nil //nolint:nilerr // malformed package.json — silently report no members
	}
	ws, ok := raw["workspaces"]
	if !ok {
		return nil, nil
	}
	var globs []string
	// Try array form first.
	if jerr := json.Unmarshal(ws, &globs); jerr != nil {
		// Object form.
		var obj struct {
			Packages []string `json:"packages"`
		}
		if oerr := json.Unmarshal(ws, &obj); oerr != nil {
			return nil, nil //nolint:nilerr // malformed workspaces field — treat as no members
		}
		globs = obj.Packages
	}
	return expandGlobsInRoot(root, globs), nil
}

// parsePnpmWorkspaces extracts member globs from a
// pnpm-workspace.yaml.
//
// Supported subset: the `packages:` list form with `- "glob"` entries.
// A file that uses YAML features outside this subset (inline maps,
// anchors, multi-line strings, multiple top-level keys other than
// `packages:`) is rejected with [errPnpmWorkspaceUnsupported] rather
// than silently returning empty — the package would rather fail
// loudly than silently miss workspace members.
func parsePnpmWorkspaces(path, root string) ([]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err //nolint:wrapcheck // caller distinguishes via errors.Is
	}
	var globs []string
	inPackages := false
	sawPackagesKey := false
	for line := range strings.SplitSeq(string(data), "\n") {
		// Strip trailing comments (preserve the leading content).
		rawTrim := strings.TrimRight(line, "\r")
		trim := strings.TrimSpace(rawTrim)
		if trim == "" || strings.HasPrefix(trim, "#") {
			continue
		}

		// Top-level (non-indented) keys reset the packages section.
		isIndented := rawTrim != trim
		if !isIndented {
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
		// An inline-map `packages: [a, b]` or anchor form is not
		// supported — fail loudly so callers see the gap rather than
		// silently empty workspace roots.
		if strings.HasPrefix(trim, "packages:") {
			return nil, errPnpmWorkspaceUnsupported
		}

		if inPackages {
			if !strings.HasPrefix(trim, "- ") {
				// A non-list-item line inside packages: is malformed.
				// Quietly tolerate a fully-indented blank or comment
				// (handled above) but reject substantive lines.
				return nil, errPnpmWorkspaceUnsupported
			}
			globs = append(globs, unquote(strings.TrimSpace(trim[2:])))
		}
	}
	return expandGlobsInRoot(root, globs), nil
}

// errPnpmWorkspaceUnsupported flags YAML features the package's
// hand-rolled parser doesn't handle.
var errPnpmWorkspaceUnsupported = errors.New("fs: workspace: pnpm-workspace.yaml uses unsupported YAML feature (only `packages:` list form is supported)")

// expandGlobsInRoot turns relative glob patterns into absolute paths
// by joining with root and feeding through [Glob].
func expandGlobsInRoot(root string, globs []string) []string {
	var members []string
	for _, g := range globs {
		matches, err := Glob(filepath.Join(root, g))
		if err != nil || len(matches) == 0 {
			// Not a glob (or no matches) — treat as a literal path.
			members = append(members, filepath.Join(root, g))
			continue
		}
		members = append(members, matches...)
	}
	return members
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
