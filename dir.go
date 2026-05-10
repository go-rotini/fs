package fs

import (
	"context"
	"errors"
	"io"
	stdfs "io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// MkdirOption configures [Mkdir] / [MkdirAll].
type MkdirOption func(*mkdirOptions)

type mkdirOptions struct {
	enforcePerm bool
}

// WithEnforcePerm makes [MkdirAll] chmod every path component it
// created so the requested perm is the actual perm on disk, bypassing
// the process umask. Components that already existed before the call
// are left untouched.
//
// Default false.
func WithEnforcePerm(b bool) MkdirOption {
	return func(o *mkdirOptions) { o.enforcePerm = b }
}

func newMkdirOptions(opts []MkdirOption) mkdirOptions {
	cfg := mkdirOptions{}
	for _, o := range opts {
		o(&cfg)
	}
	return cfg
}

// ListOption configures [ListDir].
type ListOption func(*listOptions)

type listOptions struct {
	skipHidden bool
	sorted     bool
	filter     func(stdfs.DirEntry) bool
}

// WithSkipHidden filters out entries whose name begins with ".". On
// Windows this still uses the dot-prefix convention; the
// FILE_ATTRIBUTE_HIDDEN bit is not consulted.
func WithSkipHidden(b bool) ListOption {
	return func(o *listOptions) { o.skipHidden = b }
}

// WithSorted makes [ListDir] sort entries by name. Default unsorted
// (filesystem order from [*os.File.ReadDir]).
func WithSorted(b bool) ListOption {
	return func(o *listOptions) { o.sorted = b }
}

// WithListFilter installs a predicate. Only entries for which the
// predicate returns true are included.
func WithListFilter(f func(stdfs.DirEntry) bool) ListOption {
	return func(o *listOptions) { o.filter = f }
}

func newListOptions(opts []ListOption) listOptions {
	cfg := listOptions{}
	for _, o := range opts {
		o(&cfg)
	}
	return cfg
}

const (
	opMkdir    = "mkdir"
	opMkdirAll = "mkdirall"
	opListDir  = "listdir"
	opIsEmpty  = "isempty"
	opDirSize  = "dirsize"
)

// Mkdir creates a single directory. Returns [ErrAlreadyExists] if path
// already exists. Use [MkdirAll] to silently create-if-missing.
func Mkdir(path string, perm os.FileMode) error {
	if err := os.Mkdir(path, perm); err != nil {
		return wrapPathError(opMkdir, path, err)
	}
	return nil
}

// MkdirAll creates path and any missing parents. A path that already
// exists as a directory is not an error. Pass [WithEnforcePerm] to
// chmod components this call creates so umask doesn't strip bits.
func MkdirAll(path string, perm os.FileMode, opts ...MkdirOption) error {
	cfg := newMkdirOptions(opts)

	var firstMissing string
	if cfg.enforcePerm {
		firstMissing = findFirstMissingComponent(path)
	}

	if err := os.MkdirAll(path, perm); err != nil {
		return wrapPathError(opMkdirAll, path, err)
	}

	if cfg.enforcePerm && firstMissing != "" {
		if err := chmodChainFromTo(firstMissing, path, perm); err != nil {
			return wrapPathError(opMkdirAll, path, err)
		}
	}
	return nil
}

// findFirstMissingComponent returns the highest path component that
// did not exist at call time, or "" if path itself already existed.
func findFirstMissingComponent(path string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		return path
	}
	cleaned := filepath.Clean(abs)

	var components []string
	cur := cleaned
	for {
		components = append([]string{cur}, components...)
		parent := filepath.Dir(cur)
		if parent == cur {
			break
		}
		cur = parent
	}

	for _, c := range components[1:] {
		if _, err := os.Lstat(c); err != nil {
			return c
		}
	}
	return ""
}

// chmodChainFromTo chmods every directory from `from` (inclusive,
// ancestor) down to `to` (inclusive, descendant) to perm.
func chmodChainFromTo(from, to string, perm os.FileMode) error {
	absFrom, err := filepath.Abs(from)
	if err != nil {
		return err //nolint:wrapcheck // caller wraps via *PathError
	}
	absTo, err := filepath.Abs(to)
	if err != nil {
		return err //nolint:wrapcheck // caller wraps via *PathError
	}
	cleanFrom := filepath.Clean(absFrom)
	cleanTo := filepath.Clean(absTo)

	cur := cleanTo
	for {
		if err := os.Chmod(cur, perm); err != nil {
			return err //nolint:wrapcheck // caller wraps via *PathError
		}
		if cur == cleanFrom {
			return nil
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			return nil
		}
		cur = parent
	}
}

// ListDir reads path and returns its entries. Default order is
// filesystem order; pass [WithSorted] for name-sorted output.
// [WithSkipHidden] drops dot-prefixed names; [WithListFilter] applies
// a custom predicate.
func ListDir(path string, opts ...ListOption) ([]stdfs.DirEntry, error) {
	cfg := newListOptions(opts)

	f, err := os.Open(path)
	if err != nil {
		return nil, wrapPathError(opListDir, path, err)
	}
	defer f.Close()

	entries, err := f.ReadDir(-1)
	if err != nil {
		return nil, wrapPathError(opListDir, path, err)
	}

	if cfg.skipHidden || cfg.filter != nil {
		filtered := entries[:0]
		for _, e := range entries {
			if cfg.skipHidden && strings.HasPrefix(e.Name(), ".") {
				continue
			}
			if cfg.filter != nil && !cfg.filter(e) {
				continue
			}
			filtered = append(filtered, e)
		}
		entries = filtered
	}

	if cfg.sorted {
		sort.Slice(entries, func(i, j int) bool {
			return entries[i].Name() < entries[j].Name()
		})
	}

	return entries, nil
}

// IsEmpty reports whether path is an empty directory. A non-directory
// returns ([ErrNotDir]); a missing path returns the wrapped
// [ErrNotFound].
func IsEmpty(path string) (bool, error) {
	info, err := os.Stat(path)
	if err != nil {
		return false, wrapPathError(opIsEmpty, path, err)
	}
	if !info.IsDir() {
		return false, wrapPathError(opIsEmpty, path, ErrNotDir)
	}

	f, err := os.Open(path)
	if err != nil {
		return false, wrapPathError(opIsEmpty, path, err)
	}
	defer f.Close()

	_, err = f.Readdirnames(1)
	if errors.Is(err, io.EOF) {
		return true, nil
	}
	if err != nil {
		return false, wrapPathError(opIsEmpty, path, err)
	}
	return false, nil
}

// DirSize sums the sizes of all regular files under path. Symlinks
// are not followed; non-regular entries (devices, FIFOs, sockets) are
// counted as zero. ctx is checked between entries; cancellation
// returns the partial total and ctx.Err().
func DirSize(ctx context.Context, path string) (int64, error) {
	if err := ctx.Err(); err != nil {
		return 0, err //nolint:wrapcheck // surfacing context error directly
	}

	var total int64
	walkErr := filepath.WalkDir(path, func(_ string, d stdfs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if cerr := ctx.Err(); cerr != nil {
			return cerr //nolint:wrapcheck // outer caller wraps via *PathError or surfaces context error
		}
		if !d.Type().IsRegular() {
			return nil
		}
		info, ierr := d.Info()
		if ierr != nil {
			return ierr //nolint:wrapcheck // outer caller wraps via *PathError
		}
		total += info.Size()
		return nil
	})
	if walkErr != nil {
		if errors.Is(walkErr, context.Canceled) || errors.Is(walkErr, context.DeadlineExceeded) {
			return total, walkErr //nolint:wrapcheck // surfacing context error directly
		}
		return total, wrapPathError(opDirSize, path, walkErr)
	}
	return total, nil
}
