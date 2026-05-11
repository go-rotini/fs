package fs

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

// ProjectKind enumerates the project types [ProjectType] can detect.
// Multiple kinds can apply to the same root (monorepos commonly
// have several).
type ProjectKind string

// Canonical ProjectKind values. New kinds may be added in minor
// releases; consumers should treat unknown values as opaque tags
// rather than relying on enum exhaustiveness.
const (
	ProjectKindGo     ProjectKind = "go"
	ProjectKindNode   ProjectKind = "node"
	ProjectKindRust   ProjectKind = "rust"
	ProjectKindPython ProjectKind = "python"
	ProjectKindRuby   ProjectKind = "ruby"
	ProjectKindJava   ProjectKind = "java"
	ProjectKindDotnet ProjectKind = "dotnet"
	ProjectKindPHP    ProjectKind = "php"
	ProjectKindMake   ProjectKind = "make"
	ProjectKindDocker ProjectKind = "docker"
)

// projectMarker maps a filename to the kind it identifies. Order
// matters only for deterministic output — the detector reads the
// directory once and reports every match.
type projectMarker struct {
	file string
	kind ProjectKind
}

var (
	projectMarkersMu sync.RWMutex
	projectMarkers   = []projectMarker{
		{"go.mod", ProjectKindGo},
		{"go.work", ProjectKindGo},
		{"package.json", ProjectKindNode},
		{"Cargo.toml", ProjectKindRust},
		{"pyproject.toml", ProjectKindPython},
		{"setup.py", ProjectKindPython},
		{"requirements.txt", ProjectKindPython},
		{"Gemfile", ProjectKindRuby},
		{"pom.xml", ProjectKindJava},
		{"build.gradle", ProjectKindJava},
		{"build.gradle.kts", ProjectKindJava},
		{"composer.json", ProjectKindPHP},
		{"Makefile", ProjectKindMake},
		{"makefile", ProjectKindMake},
		{"Dockerfile", ProjectKindDocker},
	}
)

// RegisterProjectKind adds custom marker filenames for a [ProjectKind]
// so [ProjectType] recognizes them in addition to the built-in set.
// Useful for monorepos with bespoke conventions or for tooling that
// wants to detect framework-specific manifests (e.g.,
// `bazel.WORKSPACE`).
//
// Safe to call from any goroutine. Registrations are additive — they
// don't replace built-in markers — and persist for the lifetime of
// the process.
func RegisterProjectKind(kind ProjectKind, markers ...string) {
	if kind == "" || len(markers) == 0 {
		return
	}
	projectMarkersMu.Lock()
	defer projectMarkersMu.Unlock()
	for _, m := range markers {
		if m == "" {
			continue
		}
		projectMarkers = append(projectMarkers, projectMarker{file: m, kind: kind})
	}
}

// ProjectType inspects root and returns every recognized project
// kind whose marker file exists in the directory. Returns an empty
// slice when none match.
//
// The returned slice is sorted alphabetically so the output is
// deterministic for golden-file tests. Detection is filename-only
// — no parsing of go.mod / package.json / etc. — so a directory
// with a stray `Gemfile` is reported as Ruby even if it's
// effectively a Go project.
//
// For .NET, the marker is any `*.csproj` / `*.fsproj` / `*.sln` in
// root.
func ProjectType(root string) ([]ProjectKind, error) {
	dirents, err := readDirSorted(root)
	if err != nil {
		return nil, err
	}

	projectMarkersMu.RLock()
	markers := projectMarkers
	projectMarkersMu.RUnlock()

	seen := map[ProjectKind]struct{}{}
	for _, name := range dirents {
		for _, m := range markers {
			if name == m.file {
				seen[m.kind] = struct{}{}
			}
		}
		// .NET detection — any of the project / solution files.
		if strings.HasSuffix(name, ".csproj") ||
			strings.HasSuffix(name, ".fsproj") ||
			strings.HasSuffix(name, ".sln") {
			seen[ProjectKindDotnet] = struct{}{}
		}
	}

	out := make([]ProjectKind, 0, len(seen))
	for k := range seen {
		out = append(out, k)
	}
	sort.Slice(out, func(i, j int) bool {
		return string(out[i]) < string(out[j])
	})
	return out, nil
}

// readDirSorted returns the basenames of root's children, sorted.
// Returns wrapPathError-wrapped errors on read failure.
func readDirSorted(root string) ([]string, error) {
	root = filepath.Clean(root)
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, wrapPathError("projecttype", root, err)
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	sort.Strings(names)
	return names, nil
}
