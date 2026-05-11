package fs

import (
	"errors"
	stdfs "io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// WalkFunc is the per-entry callback invoked by [Walk]. Return
// [filepath.SkipDir] to skip the parent directory; return
// [filepath.SkipAll] to terminate the walk early. Any other non-nil
// error aborts the walk and is returned by [Walk].
type WalkFunc func(path string, e stdfs.DirEntry, err error) error

// WalkOption configures [Walk] and the walk-backed search functions
// ([Find], [FindByRegex], [FindFunc]).
type WalkOption func(*walkOptions)

type walkOptions struct {
	skipHidden     bool
	skipNames      []string
	skipPatterns   []string
	maxDepth       int
	followSymlinks bool
	errorHandler   func(path string, err error) error
	gitignore      *Gitignore
}

func newWalkOptions(opts []WalkOption) walkOptions {
	cfg := walkOptions{}
	for _, o := range opts {
		o(&cfg)
	}
	return cfg
}

// WalkSkipHidden skips hidden entries during the walk. Hidden is
// dot-prefix on POSIX and (additionally) the FILE_ATTRIBUTE_HIDDEN
// bit on Windows. Skipping a directory prunes its subtree.
//
// Named "WalkSkipHidden" rather than "WithSkipHidden" because the
// latter is already taken by [WithSkipHidden] on [ListOption]; Go
// doesn't allow function-name overloading across return types.
func WalkSkipHidden(b bool) WalkOption {
	return func(o *walkOptions) { o.skipHidden = b }
}

// WalkSkipNames skips entries whose basename exactly matches any
// element of names. Skipping a directory prunes its subtree.
func WalkSkipNames(names []string) WalkOption {
	return func(o *walkOptions) { o.skipNames = slices.Clone(names) }
}

// WalkSkipPatterns skips entries whose basename matches any of the
// [filepath.Match] glob patterns. Skipping a directory prunes its
// subtree.
func WalkSkipPatterns(patterns []string) WalkOption {
	return func(o *walkOptions) { o.skipPatterns = slices.Clone(patterns) }
}

// WalkMaxDepth bounds recursion. The root is depth 0; n=1 means root
// + immediate children. n<=0 (default) is unbounded.
func WalkMaxDepth(n int) WalkOption {
	return func(o *walkOptions) { o.maxDepth = n }
}

// WalkFollowSymlinks dereferences symlinks during the walk. Symlink
// loops are detected by tracking resolved real paths via
// [filepath.EvalSymlinks]; an already-visited target is silently
// skipped.
//
// Named "WalkFollowSymlinks" to disambiguate from
// [WithFollowSymlinks] on [CopyOption].
func WalkFollowSymlinks(b bool) WalkOption {
	return func(o *walkOptions) { o.followSymlinks = b }
}

// WalkErrorHandler intercepts per-entry errors during the walk.
// Return nil to continue, [filepath.SkipDir] to skip the parent, or
// any non-nil error to abort.
func WalkErrorHandler(fn func(path string, err error) error) WalkOption {
	return func(o *walkOptions) { o.errorHandler = fn }
}

const opWalk = "walk"

// Walk visits every entry under root, invoking fn for each. It wraps
// [filepath.WalkDir] when [WalkFollowSymlinks] is false (default)
// and uses a custom recursive walker that tracks resolved real paths
// when symlinks are followed.
//
// Filter options ([WalkSkipHidden], [WalkSkipNames],
// [WalkSkipPatterns]) prune subtrees: when a directory matches a
// skip rule, the entire subtree is omitted.
//
// [WalkMaxDepth] bounds recursion. [WalkErrorHandler] intercepts
// per-entry errors so a single unreadable directory doesn't abort
// the walk.
func Walk(root string, fn WalkFunc, opts ...WalkOption) error {
	cfg := newWalkOptions(opts)

	var werr error
	if cfg.followSymlinks {
		werr = walkSymlinkAware(root, fn, cfg)
	} else {
		werr = walkNoSymlinks(root, fn, cfg)
	}

	if errors.Is(werr, filepath.SkipAll) || errors.Is(werr, filepath.SkipDir) {
		return nil
	}
	if werr != nil {
		return wrapPathError(opWalk, root, werr)
	}
	return nil
}

func walkNoSymlinks(root string, fn WalkFunc, cfg walkOptions) error {
	cleanRoot := filepath.Clean(root)
	rootDepth := pathSeparatorCount(cleanRoot)

	//nolint:wrapcheck // outer Walk wraps via *PathError
	return filepath.WalkDir(root, func(path string, d stdfs.DirEntry, walkErr error) error {
		if walkErr != nil {
			if cfg.errorHandler != nil {
				return cfg.errorHandler(path, walkErr)
			}
			return walkErr
		}

		if cfg.maxDepth > 0 {
			depth := pathSeparatorCount(filepath.Clean(path)) - rootDepth
			if depth > cfg.maxDepth {
				if d.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
		}

		if path != cleanRoot && shouldSkip(path, d, cfg) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		if path != cleanRoot && cfg.gitignore != nil {
			if skip, prune := gitignoreSkip(cfg.gitignore, toRelPosix(cleanRoot, path), d.IsDir()); skip {
				if prune {
					return filepath.SkipDir
				}
				return nil
			}
		}

		return fn(path, d, nil)
	})
}

func walkSymlinkAware(root string, fn WalkFunc, cfg walkOptions) error {
	visited := map[string]struct{}{}
	cleanRoot := filepath.Clean(root)
	return walkSymlinkRec(cleanRoot, cleanRoot, 0, fn, cfg, visited)
}

func walkSymlinkRec(root, path string, depth int, fn WalkFunc, cfg walkOptions, visited map[string]struct{}) error {
	info, err := os.Stat(path)
	if err != nil {
		if cfg.errorHandler != nil {
			return cfg.errorHandler(path, err)
		}
		return err //nolint:wrapcheck // outer Walk wraps via *PathError
	}

	resolved, rerr := filepath.EvalSymlinks(path)
	if rerr != nil {
		resolved = path
	}
	if _, seen := visited[resolved]; seen {
		return nil
	}
	visited[resolved] = struct{}{}

	d := stdfs.FileInfoToDirEntry(info)
	if cerr := fn(path, d, nil); cerr != nil {
		if errors.Is(cerr, filepath.SkipDir) {
			return nil
		}
		return cerr
	}

	if !info.IsDir() {
		return nil
	}
	if cfg.maxDepth > 0 && depth >= cfg.maxDepth {
		return nil
	}

	entries, err := os.ReadDir(path)
	if err != nil {
		if cfg.errorHandler != nil {
			if herr := cfg.errorHandler(path, err); herr != nil {
				return herr
			}
			return nil
		}
		return err //nolint:wrapcheck // outer Walk wraps via *PathError
	}

	for _, e := range entries {
		child := filepath.Join(path, e.Name())
		if shouldSkip(child, e, cfg) {
			continue
		}
		if cfg.gitignore != nil {
			if skip, _ := gitignoreSkip(cfg.gitignore, toRelPosix(root, child), e.IsDir()); skip {
				continue
			}
		}
		if rerr := walkSymlinkRec(root, child, depth+1, fn, cfg, visited); rerr != nil {
			if errors.Is(rerr, filepath.SkipAll) {
				return rerr
			}
			return rerr
		}
	}
	return nil
}

// shouldSkip reports whether the entry should be filtered out by the
// configured skip rules. The path argument is used only by the
// platform-specific hidden-attribute lookup on Windows.
func shouldSkip(path string, e stdfs.DirEntry, cfg walkOptions) bool {
	name := e.Name()
	if cfg.skipHidden && isHiddenEntry(path, e) {
		return true
	}
	if len(cfg.skipNames) > 0 && slices.Contains(cfg.skipNames, name) {
		return true
	}
	for _, pat := range cfg.skipPatterns {
		ok, err := filepath.Match(pat, name)
		if err == nil && ok {
			return true
		}
	}
	return false
}

// pathSeparatorCount counts os-separator characters in the cleaned
// path, providing a stable depth metric.
func pathSeparatorCount(path string) int {
	return strings.Count(path, string(filepath.Separator))
}
