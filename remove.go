package fs

import (
	"errors"
	stdfs "io/fs"
	"os"
	"path/filepath"
)

// RemoveOption configures remove operations.
type RemoveOption func(*removeOptions)

type removeOptions struct {
	strict bool
}

// WithStrict makes the operation error on missing paths instead of
// returning nil. Mirrors stdlib's [os.Remove] / [os.RemoveAll]
// behavior; the package's default is the opposite (idempotent
// missing-target removal).
func WithStrict(b bool) RemoveOption {
	return func(o *removeOptions) { o.strict = b }
}

func newRemoveOptions(opts []RemoveOption) removeOptions {
	cfg := removeOptions{}
	for _, o := range opts {
		o(&cfg)
	}
	return cfg
}

// Operation labels for *PathError.Op on remove-layer errors.
const (
	opRemove         = "remove"
	opRemoveAll      = "removeall"
	opRemoveContents = "removecontents"
)

// Remove removes path. Idempotent: a missing path returns nil. Pass
// [WithStrict] to error on missing paths (mirroring stdlib's
// [os.Remove]).
//
// For directories, only empty directories succeed; use [RemoveAll]
// for recursive removal.
func Remove(path string, opts ...RemoveOption) error {
	cfg := newRemoveOptions(opts)
	err := os.Remove(path)
	if err == nil {
		return nil
	}
	if !cfg.strict && errors.Is(err, stdfs.ErrNotExist) {
		return nil
	}
	return wrapPathError(opRemove, path, err)
}

// RemoveAll is the recursive variant of [Remove]. Idempotent by
// default. With [WithStrict] enabled, a missing target errors with
// [ErrNotFound].
func RemoveAll(path string, opts ...RemoveOption) error {
	cfg := newRemoveOptions(opts)
	if cfg.strict {
		if _, err := os.Lstat(path); err != nil {
			return wrapPathError(opRemoveAll, path, err)
		}
	}
	if err := os.RemoveAll(path); err != nil {
		return wrapPathError(opRemoveAll, path, err)
	}
	return nil
}

// RemoveContents removes every entry under path but leaves path
// itself in place. Errors aggregate into a [*MultiError] so a
// partial failure surfaces every problem entry rather than the
// first.
//
// Idempotent on a missing path by default (returns nil); pass
// [WithStrict] to error instead. A path that exists but is not a
// directory always errors with [ErrNotDir], regardless of the strict
// flag.
func RemoveContents(path string, opts ...RemoveOption) error {
	cfg := newRemoveOptions(opts)
	info, err := os.Stat(path)
	if err != nil {
		if !cfg.strict && errors.Is(err, stdfs.ErrNotExist) {
			return nil
		}
		return wrapPathError(opRemoveContents, path, err)
	}
	if !info.IsDir() {
		return wrapPathError(opRemoveContents, path, ErrNotDir)
	}

	entries, err := os.ReadDir(path)
	if err != nil {
		return wrapPathError(opRemoveContents, path, err)
	}

	multi := &MultiError{}
	for _, e := range entries {
		child := filepath.Join(path, e.Name())
		if rerr := os.RemoveAll(child); rerr != nil {
			multi.Append(wrapPathError(opRemoveAll, child, rerr))
		}
	}
	if len(multi.Errors) > 0 {
		return multi
	}
	return nil
}
