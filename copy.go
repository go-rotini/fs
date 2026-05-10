package fs

import (
	"errors"
	"fmt"
	"io"
	stdfs "io/fs"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

// CopyOption configures [CopyFile], [CopyDir], [Move], and [Rename].
type CopyOption func(*copyOptions)

type copyOptions struct {
	overwrite      bool
	followSymlinks bool
	preserveMtime  bool
	filter         func(path string, e stdfs.DirEntry) bool
}

// WithOverwrite controls whether an existing destination is replaced.
// Default true.
func WithOverwrite(b bool) CopyOption {
	return func(o *copyOptions) { o.overwrite = b }
}

// WithFollowSymlinks dereferences symlinks at the source instead of
// recreating them at the destination. Default false (symlinks are
// copied as symlinks).
func WithFollowSymlinks(b bool) CopyOption {
	return func(o *copyOptions) { o.followSymlinks = b }
}

// WithMtime preserves the source modification time on the
// destination. Default true. Symlinks themselves do not have their
// mtime preserved (the stdlib doesn't expose lutimes).
func WithMtime(b bool) CopyOption {
	return func(o *copyOptions) { o.preserveMtime = b }
}

// WithFilter installs a per-entry predicate for [CopyDir]. Returning
// false skips the entry; for directories, the entire subtree is
// skipped. The path passed to fn is the absolute walk path under the
// source root.
func WithFilter(fn func(path string, e stdfs.DirEntry) bool) CopyOption {
	return func(o *copyOptions) { o.filter = fn }
}

func newCopyOptions(opts []CopyOption) copyOptions {
	cfg := copyOptions{
		overwrite:     true,
		preserveMtime: true,
	}
	for _, o := range opts {
		o(&cfg)
	}
	return cfg
}

const (
	opCopyFile = "copyfile"
	opCopyDir  = "copydir"
	opMove     = "move"
)

// CopyFile copies src to dst. The copy is atomic: bytes go to a temp
// file in dst's parent, then the temp is renamed over dst. Source
// mode and (by default) mtime are preserved on dst.
//
// If src is a symlink and [WithFollowSymlinks] is false (default),
// the symlink itself is recreated at dst — the link target is not
// dereferenced.
//
// Non-regular, non-symlink sources (devices, FIFOs, sockets) error
// with [ErrNotSupported].
func CopyFile(src, dst string, opts ...CopyOption) error {
	cfg := newCopyOptions(opts)
	return copyFileInternal(src, dst, cfg)
}

func copyFileInternal(src, dst string, cfg copyOptions) error {
	var srcInfo os.FileInfo
	var err error
	if cfg.followSymlinks {
		srcInfo, err = osStat(src)
	} else {
		srcInfo, err = osLstat(src)
	}
	if err != nil {
		return wrapPathError(opCopyFile, src, err)
	}

	if !cfg.followSymlinks && srcInfo.Mode()&os.ModeSymlink != 0 {
		return copySymlink(src, dst, cfg)
	}

	if !srcInfo.Mode().IsRegular() {
		return wrapPathError(opCopyFile, src, fmt.Errorf("%w: source is %s", ErrNotSupported, srcInfo.Mode().Type()))
	}

	if !cfg.overwrite {
		if _, lerr := osLstat(dst); lerr == nil {
			return wrapPathError(opCopyFile, dst, ErrAlreadyExists)
		}
	}

	sf, err := osOpen(src)
	if err != nil {
		return wrapPathError(opCopyFile, src, err)
	}
	defer sf.Close()

	dstDir := filepath.Dir(dst)
	tmp, err := os.CreateTemp(dstDir, filepath.Base(dst)+".tmp.*")
	if err != nil {
		return wrapPathError(opCopyFile, dst, err)
	}
	tmpPath := tmp.Name()
	cleanupTmp := func() { _ = os.Remove(tmpPath) }

	if _, cerr := io.Copy(tmp, hookedReader{sf}); cerr != nil {
		closeQuietly(tmp)
		cleanupTmp()
		return wrapPathError(opCopyFile, src, cerr)
	}

	if cerr := fileSync(tmp); cerr != nil {
		closeQuietly(tmp)
		cleanupTmp()
		return wrapPathError(opCopyFile, dst, cerr)
	}

	if cerr := fileClose(tmp); cerr != nil {
		cleanupTmp()
		return wrapPathError(opCopyFile, dst, cerr)
	}

	if cerr := osChmod(tmpPath, srcInfo.Mode().Perm()); cerr != nil {
		cleanupTmp()
		return wrapPathError(opCopyFile, dst, cerr)
	}

	if cfg.preserveMtime {
		if cerr := osChtimes(tmpPath, time.Now(), srcInfo.ModTime()); cerr != nil {
			cleanupTmp()
			return wrapPathError(opCopyFile, dst, cerr)
		}
	}

	if cerr := osRename(tmpPath, dst); cerr != nil {
		cleanupTmp()
		return wrapPathError(opCopyFile, dst, cerr)
	}

	if serr := syncDir(dstDir); serr != nil {
		return wrapPathError(opCopyFile, dst, serr)
	}
	return nil
}

func copySymlink(src, dst string, cfg copyOptions) error {
	target, err := os.Readlink(src)
	if err != nil {
		return wrapPathError(opCopyFile, src, err)
	}

	if _, lerr := os.Lstat(dst); lerr == nil {
		if !cfg.overwrite {
			return wrapPathError(opCopyFile, dst, ErrAlreadyExists)
		}
		if rerr := os.Remove(dst); rerr != nil {
			return wrapPathError(opCopyFile, dst, rerr)
		}
	}

	if serr := os.Symlink(target, dst); serr != nil {
		return wrapPathError(opCopyFile, dst, serr)
	}
	return nil
}

// CopyDir recursively copies src to dst. dst is created if missing
// (mode mirrors src). Symlinks are recreated as symlinks unless
// [WithFollowSymlinks]. Per-entry errors aggregate into a
// [*MultiError]; the walk continues so a partial copy surfaces every
// problem entry.
//
// [WithFilter] skips entries (and their subtrees, for directories)
// when the predicate returns false.
func CopyDir(src, dst string, opts ...CopyOption) error {
	cfg := newCopyOptions(opts)

	srcInfo, err := os.Stat(src)
	if err != nil {
		return wrapPathError(opCopyDir, src, err)
	}
	if !srcInfo.IsDir() {
		return wrapPathError(opCopyDir, src, ErrNotDir)
	}

	if merr := os.MkdirAll(dst, srcInfo.Mode().Perm()); merr != nil {
		return wrapPathError(opCopyDir, dst, merr)
	}

	multi := &MultiError{}
	walkErr := filepath.WalkDir(src, func(path string, d stdfs.DirEntry, werr error) error {
		if werr != nil {
			multi.Append(wrapPathError(opCopyDir, path, werr))
			return nil
		}
		if path == src {
			return nil
		}
		if cfg.filter != nil && !cfg.filter(path, d) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		rel, rerr := filepath.Rel(src, path)
		if rerr != nil {
			multi.Append(wrapPathError(opCopyDir, path, rerr))
			return nil
		}
		target := filepath.Join(dst, rel)

		switch {
		case d.IsDir():
			info, ierr := d.Info()
			if ierr != nil {
				multi.Append(wrapPathError(opCopyDir, path, ierr))
				return nil
			}
			if merr := os.MkdirAll(target, info.Mode().Perm()); merr != nil {
				multi.Append(wrapPathError(opCopyDir, target, merr))
			}
		case d.Type()&os.ModeSymlink != 0 && !cfg.followSymlinks:
			if cerr := copySymlink(path, target, cfg); cerr != nil {
				multi.Append(cerr)
			}
		default:
			if cerr := copyFileInternal(path, target, cfg); cerr != nil {
				multi.Append(cerr)
			}
		}
		return nil
	})

	if walkErr != nil {
		multi.Append(wrapPathError(opCopyDir, src, walkErr))
	}

	if len(multi.Errors) > 0 {
		return multi
	}
	return nil
}

// Rename renames src to dst via [os.Rename]. Strict: a cross-device
// rename returns the underlying error (typically [ErrCrossDevice] /
// EXDEV) without falling back. Use [Move] when you want the
// copy+remove fallback. opts is accepted for signature symmetry but
// unused; rename is atomic only within a single filesystem.
func Rename(src, dst string, opts ...CopyOption) error {
	_ = opts
	if err := osRename(src, dst); err != nil {
		return wrapPathError(opRename, src, err)
	}
	return nil
}

// Move renames src to dst, falling back to copy+remove when the
// rename fails because src and dst live on different filesystems
// (EXDEV on POSIX, ERROR_NOT_SAME_DEVICE on Windows). The fallback is
// NOT atomic from the caller's perspective: the destination appears
// after the copy succeeds, the source is removed last.
//
// Honors [WithOverwrite]. The atomic-rename path overwrites
// unconditionally on POSIX; a Lstat pre-check enforces
// [WithOverwrite(false)] before the rename attempt.
func Move(src, dst string, opts ...CopyOption) error {
	cfg := newCopyOptions(opts)

	if !cfg.overwrite {
		if _, lerr := os.Lstat(dst); lerr == nil {
			return wrapPathError(opMove, dst, ErrAlreadyExists)
		}
	}

	err := osRename(src, dst)
	if err == nil {
		return nil
	}
	if !isCrossDevice(err) {
		return wrapPathError(opMove, src, err)
	}

	info, lerr := os.Lstat(src)
	if lerr != nil {
		return wrapPathError(opMove, src, lerr)
	}

	if info.IsDir() {
		if cerr := CopyDir(src, dst, opts...); cerr != nil {
			return cerr
		}
		if rerr := os.RemoveAll(src); rerr != nil {
			return wrapPathError(opMove, src, rerr)
		}
		return nil
	}

	if cerr := CopyFile(src, dst, opts...); cerr != nil {
		return cerr
	}
	if rerr := os.Remove(src); rerr != nil {
		return wrapPathError(opMove, src, rerr)
	}
	return nil
}

// isCrossDevice reports whether err is a cross-filesystem rename
// error. syscall.EXDEV is defined on every supported platform with
// the platform-appropriate value (POSIX EXDEV / Windows
// ERROR_NOT_SAME_DEVICE).
func isCrossDevice(err error) bool {
	return errors.Is(err, syscall.EXDEV) || errors.Is(err, ErrCrossDevice)
}
