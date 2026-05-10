package fs

import (
	"errors"
	stdfs "io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
)

// FindOption configures the find-up family ([FindUp], [FindUpAll],
// [ProjectRoot]).
type FindOption func(*findOptions)

type findOptions struct {
	maxAncestors   int
	projectMarkers []string
	stopAt         string
}

const defaultMaxAncestors = 32

// defaultProjectMarkers is the marker set [ProjectRoot] uses when
// [WithProjectMarkers] is not specified.
func defaultProjectMarkers() []string {
	return []string{".git", "go.mod", "package.json", "Cargo.toml"}
}

// WithMaxAncestors bounds how many directories the find-up walk
// traverses before giving up. Default 32 — enough for any realistic
// project tree, while defending against pathological symlink loops
// or deeply-nested mounts.
func WithMaxAncestors(n int) FindOption {
	return func(o *findOptions) {
		if n > 0 {
			o.maxAncestors = n
		}
	}
}

// WithProjectMarkers replaces the default [ProjectRoot] marker list
// (`.git`, `go.mod`, `package.json`, `Cargo.toml`). An empty slice
// is ignored; the default list is preserved.
func WithProjectMarkers(markers []string) FindOption {
	return func(o *findOptions) {
		if len(markers) > 0 {
			o.projectMarkers = slices.Clone(markers)
		}
	}
}

// WithStopAt sets an absolute boundary the walk will not cross. The
// boundary is checked AFTER inspecting the directory, so a marker
// living directly inside stopAt is still found.
func WithStopAt(path string) FindOption {
	return func(o *findOptions) {
		if abs, err := filepath.Abs(path); err == nil {
			o.stopAt = filepath.Clean(abs)
		}
	}
}

func newFindOptions(opts []FindOption) findOptions {
	cfg := findOptions{
		maxAncestors:   defaultMaxAncestors,
		projectMarkers: defaultProjectMarkers(),
	}
	for _, o := range opts {
		o(&cfg)
	}
	return cfg
}

const (
	opFindUp      = "findup"
	opFindUpAll   = "findupall"
	opProjectRoot = "projectroot"
	opFind        = "find"
	opFindByRegex = "findbyregex"
	opFindFunc    = "findfunc"
)

// FindUp walks parent-by-parent from startDir looking for a basename
// matching name. name is matched as a glob pattern via
// [filepath.Match]. Returns the absolute path and ok=true on the
// first match; ok=false (and no error) when the walk exhausts without
// finding a match.
//
// The walk is bounded by [WithMaxAncestors] (default 32) and
// optionally by [WithStopAt].
func FindUp(name, startDir string, opts ...FindOption) (string, bool, error) {
	cfg := newFindOptions(opts)
	abs, err := filepath.Abs(startDir)
	if err != nil {
		return "", false, wrapPathError(opFindUp, startDir, err)
	}

	cur := filepath.Clean(abs)
	for range cfg.maxAncestors {
		match, mErr := matchInDir(cur, name)
		if mErr != nil {
			return "", false, wrapPathError(opFindUp, name, mErr)
		}
		if match != "" {
			return match, true, nil
		}
		if cfg.stopAt != "" && cur == cfg.stopAt {
			return "", false, nil
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			return "", false, nil
		}
		cur = parent
	}
	return "", false, nil
}

// FindUpAll returns every match from startDir up to filesystem root.
// Order is leaf-to-root: the closest match comes first.
func FindUpAll(name, startDir string, opts ...FindOption) ([]string, error) {
	cfg := newFindOptions(opts)
	abs, err := filepath.Abs(startDir)
	if err != nil {
		return nil, wrapPathError(opFindUpAll, startDir, err)
	}

	var matches []string
	cur := filepath.Clean(abs)
	for range cfg.maxAncestors {
		match, mErr := matchInDir(cur, name)
		if mErr != nil {
			return matches, wrapPathError(opFindUpAll, name, mErr)
		}
		if match != "" {
			matches = append(matches, match)
		}
		if cfg.stopAt != "" && cur == cfg.stopAt {
			return matches, nil
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			return matches, nil
		}
		cur = parent
	}
	return matches, nil
}

// matchInDir looks inside dir for an entry whose basename matches the
// glob pattern. Returns the joined match path, "" if no match, or an
// error if the pattern is invalid. Read errors on dir (e.g.,
// permission denied) are folded into "" so the find-up walk can
// continue.
func matchInDir(dir, pattern string) (string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", nil //nolint:nilerr // treat read failure as "no match in this directory"
	}
	for _, e := range entries {
		ok, mErr := filepath.Match(pattern, e.Name())
		if mErr != nil {
			return "", mErr //nolint:wrapcheck // caller wraps via *PathError
		}
		if ok {
			return filepath.Join(dir, e.Name()), nil
		}
	}
	return "", nil
}

// ProjectRoot returns the first ancestor of startDir that contains
// any of the project markers (default `.git`, `go.mod`,
// `package.json`, `Cargo.toml`; override with [WithProjectMarkers]).
// Errors with [ErrNotFound] when the walk exhausts without a match.
//
// Results are memoized per-process. The cache key includes
// abs(startDir), the marker set, the stopAt boundary, and
// maxAncestors — same inputs always return the same result without
// re-walking the filesystem. Cache entries are never invalidated;
// long-lived processes that re-arrange project layouts on disk
// should not rely on this helper picking up the change.
func ProjectRoot(startDir string, opts ...FindOption) (string, error) {
	cfg := newFindOptions(opts)
	abs, err := filepath.Abs(startDir)
	if err != nil {
		return "", wrapPathError(opProjectRoot, startDir, err)
	}
	abs = filepath.Clean(abs)

	key := projectRootKey(abs, cfg)
	if v, ok := projectRootCache.Load(key); ok {
		if cached, ok := v.(string); ok {
			return cached, nil
		}
	}

	cur := abs
	for range cfg.maxAncestors {
		for _, marker := range cfg.projectMarkers {
			if Exists(filepath.Join(cur, marker)) {
				projectRootCache.Store(key, cur)
				return cur, nil
			}
		}
		if cfg.stopAt != "" && cur == cfg.stopAt {
			return "", wrapPathError(opProjectRoot, startDir, ErrNotFound)
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			return "", wrapPathError(opProjectRoot, startDir, ErrNotFound)
		}
		cur = parent
	}
	return "", wrapPathError(opProjectRoot, startDir, ErrNotFound)
}

var projectRootCache sync.Map

// projectRootKey builds a deterministic cache key. The marker list is
// sorted so the same set in different orders maps to the same key.
func projectRootKey(abs string, cfg findOptions) string {
	markers := slices.Clone(cfg.projectMarkers)
	slices.Sort(markers)
	var b strings.Builder
	b.WriteString(abs)
	b.WriteByte('|')
	b.WriteString(strings.Join(markers, "\x00"))
	b.WriteByte('|')
	b.WriteString(cfg.stopAt)
	b.WriteByte('|')
	b.WriteString(strconv.Itoa(cfg.maxAncestors))
	return b.String()
}

// FirstExisting returns the first path in paths that exists. Useful
// for fallback chains like
// `["./config.yaml", "~/.myapp/config.yaml", "/etc/myapp/config.yaml"]`.
// Permission errors on a candidate are folded into "doesn't exist"
// (see [Exists]), so the walk continues to the next candidate.
func FirstExisting(paths []string) (string, bool) {
	for _, p := range paths {
		if Exists(p) {
			return p, true
		}
	}
	return "", false
}

// Find returns paths under root whose basename matches pattern (a
// [filepath.Match] glob). Paths are returned as absolute paths when
// root is absolute; relative-to-cwd otherwise. The root entry itself
// is included if its basename matches.
//
// Walk-style options (skip-hidden, skip-names, max-depth, etc.) land
// in Phase 11 along with [Walk]; this signature will gain variadic
// options at that point.
func Find(root, pattern string) ([]string, error) {
	return findInternal(root, opFind, func(d stdfs.DirEntry, _ string) (bool, error) {
		ok, err := filepath.Match(pattern, d.Name())
		if err != nil {
			return false, err //nolint:wrapcheck // caller wraps via *PathError
		}
		return ok, nil
	})
}

// FindByRegex is [Find] using a [regexp.Regexp] matched against the
// basename instead of a glob.
func FindByRegex(root string, re *regexp.Regexp) ([]string, error) {
	return findInternal(root, opFindByRegex, func(d stdfs.DirEntry, _ string) (bool, error) {
		return re.MatchString(d.Name()), nil
	})
}

// FindFunc returns paths under root for which pred returns true. pred
// receives the walk path (absolute when root is absolute) and the
// entry's [os.FileInfo].
func FindFunc(root string, pred func(path string, info os.FileInfo) bool) ([]string, error) {
	return findInternal(root, opFindFunc, func(d stdfs.DirEntry, path string) (bool, error) {
		info, err := d.Info()
		if err != nil {
			return false, err //nolint:wrapcheck // caller wraps via *PathError
		}
		return pred(path, info), nil
	})
}

func findInternal(root, op string, match func(stdfs.DirEntry, string) (bool, error)) ([]string, error) {
	var matches []string
	werr := filepath.WalkDir(root, func(path string, d stdfs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		ok, mErr := match(d, path)
		if mErr != nil {
			return mErr
		}
		if ok {
			matches = append(matches, path)
		}
		return nil
	})
	if werr != nil {
		if errors.Is(werr, stdfs.ErrNotExist) {
			return nil, wrapPathError(op, root, werr)
		}
		return matches, wrapPathError(op, root, werr)
	}
	return matches, nil
}
