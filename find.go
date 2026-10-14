package fs

import (
	"errors"
	stdfs "io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
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

// WithMaxAncestors bounds how many directories, starting with the
// start directory, the find-up walk inspects before giving up.
// Default 32. Values n <= 0 are ignored.
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

// WithStopAt sets a directory the walk will not climb above; a
// relative path is made absolute first. The boundary is checked after
// inspecting each directory, so a match directly inside it is still
// found.
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

// FindUp walks parent-by-parent from startDir looking for an entry
// whose basename matches name, a [filepath.Match] glob. It returns the
// absolute path and ok=true on the first match; ok=false (and no
// error) when the walk ends without one. Unreadable directories count
// as no match; an error means an invalid pattern or start directory.
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

// FindUpAll is [FindUp] but returns every match (at most one per
// directory) instead of the first. Order is leaf-to-root: the
// closest match comes first.
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
// The walk is bounded like [FindUp]; when it ends without a match the
// error wraps [ErrNotFound].
//
// Every call walks the filesystem; results are not cached.
func ProjectRoot(startDir string, opts ...FindOption) (string, error) {
	cfg := newFindOptions(opts)
	abs, err := filepath.Abs(startDir)
	if err != nil {
		return "", wrapPathError(opProjectRoot, startDir, err)
	}
	abs = filepath.Clean(abs)

	cur := abs
	for range cfg.maxAncestors {
		for _, marker := range cfg.projectMarkers {
			if Exists(filepath.Join(cur, marker)) {
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

// FirstExisting returns the first path in paths that exists, for
// fallback chains such as a local, per-user and system-wide config
// file. Paths are used as given (no ~ or $VAR expansion; see
// [Expand]). A candidate that cannot be statted, for example because
// of a permission error, counts as missing (see [Exists]).
func FirstExisting(paths []string) (string, bool) {
	for _, p := range paths {
		if Exists(p) {
			return p, true
		}
	}
	return "", false
}

// FindWithExtensions returns the first existing file named stem+"."+ext (for
// each ext in exts, in order) within dir, plus true; or ("", false) when none
// exist. A leading "." on an ext is accepted and ignored, so both "yaml" and
// ".yaml" address foo.yaml. It is the common "config.yml / config.yaml /
// config.toml / config.json" fallback, built on [FirstExisting].
func FindWithExtensions(dir, stem string, exts []string) (string, bool) {
	candidates := make([]string, 0, len(exts))
	for _, ext := range exts {
		candidates = append(candidates, filepath.Join(dir, stem+"."+strings.TrimPrefix(ext, ".")))
	}
	return FirstExisting(candidates)
}

// Find returns paths under root whose basename matches pattern (a
// [filepath.Match] glob). Returned paths are root joined with the
// entry, so they are absolute only when root is. The root entry
// itself is included if its basename matches.
//
// Honors every [WalkOption]. If root does not exist the result is
// nil; on any other error the matches found so far are returned with
// it.
func Find(root, pattern string, opts ...WalkOption) ([]string, error) {
	return findInternal(root, opFind, opts, func(d stdfs.DirEntry, _ string) (bool, error) {
		ok, err := filepath.Match(pattern, d.Name())
		if err != nil {
			return false, err //nolint:wrapcheck // caller wraps via *PathError
		}
		return ok, nil
	})
}

// FindByRegex is [Find] using a [regexp.Regexp] matched against the
// basename instead of a glob.
func FindByRegex(root string, re *regexp.Regexp, opts ...WalkOption) ([]string, error) {
	return findInternal(root, opFindByRegex, opts, func(d stdfs.DirEntry, _ string) (bool, error) {
		return re.MatchString(d.Name()), nil
	})
}

// FindFunc returns paths under root for which pred returns true. pred
// receives the walk path (absolute when root is absolute) and the
// entry's [os.FileInfo]. Results and errors are as for [Find].
func FindFunc(root string, pred func(path string, info os.FileInfo) bool, opts ...WalkOption) ([]string, error) {
	return findInternal(root, opFindFunc, opts, func(d stdfs.DirEntry, path string) (bool, error) {
		info, err := d.Info()
		if err != nil {
			return false, err //nolint:wrapcheck // caller wraps via *PathError
		}
		return pred(path, info), nil
	})
}

func findInternal(root, op string, opts []WalkOption, match func(stdfs.DirEntry, string) (bool, error)) ([]string, error) {
	var matches []string
	werr := Walk(root, func(path string, d stdfs.DirEntry, walkErr error) error {
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
	}, opts...)
	if werr != nil {
		if errors.Is(werr, stdfs.ErrNotExist) {
			return nil, wrapPathError(op, root, werr)
		}
		return matches, wrapPathError(op, root, werr)
	}
	return matches, nil
}
