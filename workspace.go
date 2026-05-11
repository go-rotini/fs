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

	// Kind identifies the manifest form that produced this root.
	// One of "go.work", "package.json", or "pnpm-workspace.yaml".
	Kind string
}

// WorkspaceRoots discovers the member roots of a multi-root
// workspace anchored at root. The detector inspects a small set of
// canonical manifest files:
//
//   - go.work: each `use` directive.
//   - package.json with a `workspaces` field: array of globs or an
//     object with a `packages` array. Globs are expanded via [Glob].
//   - pnpm-workspace.yaml: the `packages:` list form. See
//     [parsePnpmWorkspaces] for the supported subset.
//
// Returns an empty slice when no manifest is present. Duplicate
// member paths across manifests are deduplicated.
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

func expandGlobsInRoot(root string, globs []string) []string {
	var members []string
	for _, g := range globs {
		matches, err := Glob(filepath.Join(root, g))
		if err != nil || len(matches) == 0 {
			// Treat a non-glob or no-match entry as a literal path.
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
