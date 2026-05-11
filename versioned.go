package fs

import (
	"errors"
	"fmt"
	stdfs "io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	opWriteVersioned = "writeversioned"
	opListVersions   = "listversions"
	opRestoreVersion = "restoreversion"
	opPruneVersions  = "pruneversions"

	// versionedInfix sits between the live filename and the timestamp
	// suffix to make versioned backups visually distinct from
	// adjacent files and from log-rotator output.
	//
	//   /etc/myapp/config.yaml
	//   /etc/myapp/config.yaml.bak.20260510-120000.000
	versionedInfix = ".bak."

	// versionedTimestampLayout is the same sortable layout as
	// log rotation. Lex-sortable → chronological-sortable.
	versionedTimestampLayout = "20060102-150405.000"
)

// VersionInfo describes a single on-disk backup version.
type VersionInfo struct {
	// Path is the absolute path to the backup file.
	Path string

	// Created is the wall-clock time recorded in the version's
	// filename suffix. Always UTC.
	Created time.Time

	// Size is the backup file's size in bytes at list time.
	Size int64
}

// VersionedOption configures versioned backup behavior.
type VersionedOption func(*versionedConfig)

type versionedConfig struct {
	keep   int
	maxAge time.Duration
	perm   os.FileMode
	nowFn  func() time.Time
}

// WithVersionsKeep retains the n most recent versions and removes
// older ones after every successful write. Zero (the default)
// disables count-based pruning.
func WithVersionsKeep(n int) VersionedOption {
	return func(c *versionedConfig) {
		c.keep = n
	}
}

// WithVersionsMaxAge removes versions whose recorded creation time
// is older than d before the current wall clock. Zero (the default)
// disables age-based pruning.
func WithVersionsMaxAge(d time.Duration) VersionedOption {
	return func(c *versionedConfig) {
		c.maxAge = d
	}
}

// WithVersionsPerm overrides the file mode used for newly-written
// backup files. Default 0o644 — matches [WriteFile]. For backups of
// secret files use 0o600.
func WithVersionsPerm(perm os.FileMode) VersionedOption {
	return func(c *versionedConfig) {
		c.perm = perm
	}
}

// WithVersionsClock overrides the wall clock used for timestamp
// generation and age pruning. Useful only in tests.
func WithVersionsClock(now func() time.Time) VersionedOption {
	return func(c *versionedConfig) {
		c.nowFn = now
	}
}

// WriteFileVersioned writes data to path atomically. If path already
// exists, its current contents are first renamed to
// "<path>.bak.<timestamp>" so the prior version is preserved.
// Returns the backup path (empty string if there was no prior file)
// alongside any write error.
//
// After the write, pruning runs per [WithVersionsKeep] and
// [WithVersionsMaxAge]. Pruning errors are returned but do not
// invalidate the just-completed write — callers can inspect the
// error and proceed.
//
// The rename-to-backup step is atomic; the live file at path is
// briefly absent between the rename and the new file's appearance.
// On POSIX this window is sub-microsecond; readers using
// read-and-handle-NotFound see at most a transient miss.
func WriteFileVersioned(path string, data []byte, opts ...VersionedOption) (backup string, err error) {
	cfg := newVersionedConfig(opts)

	dir := filepath.Dir(path)
	if err := MkdirAll(dir, 0o755); err != nil {
		return "", err
	}

	if Exists(path) {
		backup = uniqueVersionedName(path, cfg)
		if rerr := os.Rename(path, backup); rerr != nil {
			return "", wrapPathError(opWriteVersioned, path, rerr)
		}
	}

	if werr := WriteFile(path, data, WithPerm(cfg.perm)); werr != nil {
		return backup, werr
	}

	if perr := pruneVersions(path, cfg); perr != nil {
		return backup, perr
	}
	return backup, nil
}

// ListVersions returns the on-disk backup versions of path, newest
// first. Returns an empty slice if path has no backups. Returns nil
// + error if the directory containing path cannot be read.
func ListVersions(path string, _ ...VersionedOption) ([]VersionInfo, error) {
	dir := filepath.Dir(path)
	base := filepath.Base(path)
	prefix := base + versionedInfix

	dirents, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, stdfs.ErrNotExist) {
			return nil, nil
		}
		return nil, wrapPathError(opListVersions, dir, err)
	}

	var versions []VersionInfo
	for _, e := range dirents {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasPrefix(name, prefix) {
			continue
		}
		stamp := strings.TrimPrefix(name, prefix)
		// Strip the collision-suffix added by uniqueVersionedName, if
		// any. The disambiguator uses '_' as a separator since '-' is
		// already part of the timestamp layout.
		if idx := strings.Index(stamp, "_"); idx > 0 {
			stamp = stamp[:idx]
		}
		created, perr := time.Parse(versionedTimestampLayout, stamp)
		if perr != nil {
			continue
		}
		info, ierr := e.Info()
		if ierr != nil {
			if errors.Is(ierr, stdfs.ErrNotExist) {
				continue
			}
			return nil, wrapPathError(opListVersions, name, ierr)
		}
		versions = append(versions, VersionInfo{
			Path:    filepath.Join(dir, name),
			Created: created,
			Size:    info.Size(),
		})
	}

	sort.Slice(versions, func(i, j int) bool {
		return versions[i].Created.After(versions[j].Created)
	})
	return versions, nil
}

// RestoreVersion writes the contents of versionPath to path. The
// current contents of path (if any) are first saved as a new
// versioned backup so the restore itself is reversible.
//
// versionPath does not have to be a file produced by
// [WriteFileVersioned]; any readable file works. The versioned-
// backup naming convention only applies to what RestoreVersion
// creates on its way to the restore.
func RestoreVersion(path, versionPath string, opts ...VersionedOption) error {
	data, err := os.ReadFile(versionPath)
	if err != nil {
		return wrapPathError(opRestoreVersion, versionPath, err)
	}
	if _, werr := WriteFileVersioned(path, data, opts...); werr != nil {
		return werr
	}
	return nil
}

// newVersionedConfig applies opts to a default-initialized config.
func newVersionedConfig(opts []VersionedOption) versionedConfig {
	cfg := versionedConfig{
		perm:  0o644,
		nowFn: time.Now,
	}
	for _, opt := range opts {
		opt(&cfg)
	}
	if cfg.nowFn == nil {
		cfg.nowFn = time.Now
	}
	if cfg.perm == 0 {
		cfg.perm = 0o644
	}
	return cfg
}

// versionedName returns a fresh versioned-backup name for path. The
// timestamp suffix is always UTC for cross-machine portability.
func versionedName(path string, cfg versionedConfig) string {
	stamp := cfg.nowFn().UTC().Format(versionedTimestampLayout)
	return fmt.Sprintf("%s%s%s", path, versionedInfix, stamp)
}

// uniqueVersionedName returns a versionedName whose target path is
// not currently in use. Sub-millisecond writes can produce the same
// timestamp; rather than relying on infinite clock resolution we
// append `_1`, `_2`, ... (underscore, since `-` already appears in
// the timestamp layout) until os.Stat reports ErrNotExist.
//
// The retry stops at 1000 attempts, which is far more than any
// realistic burst could exercise; if we somehow exhaust it, the
// final candidate is returned anyway and the rename will fail
// loudly upstream rather than silently overwrite.
func uniqueVersionedName(path string, cfg versionedConfig) string {
	base := versionedName(path, cfg)
	candidate := base
	for i := 1; i < 1000; i++ {
		if _, err := os.Stat(candidate); errors.Is(err, stdfs.ErrNotExist) {
			return candidate
		}
		candidate = fmt.Sprintf("%s_%d", base, i)
	}
	return candidate
}

// pruneVersions removes versioned backups of path that violate the
// keep or max-age policy. Called after each WriteFileVersioned
// success.
func pruneVersions(path string, cfg versionedConfig) error {
	if cfg.keep == 0 && cfg.maxAge == 0 {
		return nil
	}

	versions, err := ListVersions(path)
	if err != nil {
		return err
	}

	// Apply age first so the keep policy operates on what's left.
	if cfg.maxAge > 0 {
		cutoff := cfg.nowFn().Add(-cfg.maxAge)
		filtered := versions[:0]
		for _, v := range versions {
			if v.Created.Before(cutoff) {
				if rerr := os.Remove(v.Path); rerr != nil && !errors.Is(rerr, stdfs.ErrNotExist) {
					return wrapPathError(opPruneVersions, v.Path, rerr)
				}
				continue
			}
			filtered = append(filtered, v)
		}
		versions = filtered
	}

	if cfg.keep > 0 && len(versions) > cfg.keep {
		for _, v := range versions[cfg.keep:] {
			if rerr := os.Remove(v.Path); rerr != nil && !errors.Is(rerr, stdfs.ErrNotExist) {
				return wrapPathError(opPruneVersions, v.Path, rerr)
			}
		}
	}
	return nil
}
