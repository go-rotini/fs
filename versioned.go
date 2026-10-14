package fs

import (
	"errors"
	"fmt"
	stdfs "io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	opWriteVersioned = "writeversioned"
	opListVersions   = "listversions"
	opRestoreVersion = "restoreversion"
	opPruneVersions  = "pruneversions"

	// versionedInfix separates the live filename from the timestamp
	// suffix so backups are visually distinct (e.g.,
	// config.yaml.bak.20260510-120000.000).
	versionedInfix = ".bak."

	// versionedTimestampLayout is lex-sortable, so name-sort equals
	// chronological-sort.
	versionedTimestampLayout = "20060102-150405.000"
)

// VersionInfo describes a single on-disk backup version.
type VersionInfo struct {
	// Path is the backup file's path, in the same directory as the
	// original (absolute when the original path was).
	Path string

	// Created is the wall-clock time recorded in the version's
	// filename suffix. Always UTC.
	Created time.Time

	// Size is the backup file's size in bytes at list time.
	Size int64
}

// VersionedOption configures [WriteFileVersioned] and [RestoreVersion].
type VersionedOption func(*versionedConfig)

type versionedConfig struct {
	keep     int
	maxAge   time.Duration
	perm     os.FileMode
	maxBytes int64
	nowFn    func() time.Time
}

// WithVersionsKeep retains the n most recent versions and removes
// older ones after every successful write. Zero disables count-based
// pruning.
func WithVersionsKeep(n int) VersionedOption {
	return func(c *versionedConfig) {
		c.keep = n
	}
}

// WithVersionsMaxAge removes versions whose recorded creation time
// is older than d. Zero disables age-based pruning.
func WithVersionsMaxAge(d time.Duration) VersionedOption {
	return func(c *versionedConfig) {
		c.maxAge = d
	}
}

// WithVersionsPerm sets the mode of the new file written at path
// (use 0o600 for secret files). Without it, the new file keeps the
// mode of the file it replaces, or 0o644 when there was none. Backups
// keep the mode of the file they were made from.
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

// WithVersionsMaxBytes caps the byte size [RestoreVersion] will read
// from a backup file into memory. Default 100 MiB, also used for
// n <= 0. Larger files fail with an error wrapping [ErrFileTooLarge].
func WithVersionsMaxBytes(n int64) VersionedOption {
	return func(c *versionedConfig) {
		c.maxBytes = n
	}
}

// WriteFileVersioned writes data to path atomically. If path already
// exists, the prior version is first preserved as
// "<path>.bak.<UTC timestamp>" (layout 20060102-150405.000, with a
// "_N" suffix if that name is taken): a hard link to the old file, or
// a copy where hard links aren't supported, so path never goes
// missing. Missing parent directories are created. Returns the backup
// path (empty if there was no prior file) alongside any write error.
//
// After the write, pruning runs per [WithVersionsKeep] and
// [WithVersionsMaxAge]. Pruning errors are returned but do not
// invalidate the just-completed write.
//
// Like [WriteFile], the write keeps the old file's mode unless
// [WithVersionsPerm] is given, and replaces a symlink at path rather
// than writing through it.
func WriteFileVersioned(path string, data []byte, opts ...VersionedOption) (backup string, err error) {
	if path == "" {
		return "", wrapPathError(opWriteVersioned, path, ErrInvalidPath)
	}
	cfg := newVersionedConfig(opts)

	dir := filepath.Dir(path)
	if err := MkdirAll(dir, 0o755); err != nil {
		return "", err
	}

	if Exists(path) {
		backup, err = uniqueVersionedName(path, cfg)
		if err != nil {
			return "", wrapPathError(opWriteVersioned, path, err)
		}
		if berr := backupExisting(path, backup); berr != nil {
			return "", wrapPathError(opWriteVersioned, path, berr)
		}
	}

	var wopts []WriteOption
	if cfg.perm != 0 {
		wopts = append(wopts, WithPerm(cfg.perm))
	}
	if werr := WriteFile(path, data, wopts...); werr != nil {
		return backup, werr
	}

	if perr := pruneVersions(path, cfg); perr != nil {
		return backup, perr
	}
	return backup, nil
}

// ListVersions returns the on-disk backup versions of path, newest
// first. Returns no versions, and no error, if path has no backups or
// its directory does not exist.
func ListVersions(path string) ([]VersionInfo, error) {
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
	seq := map[string]int{} // collision suffix per version path
	for _, e := range dirents {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasPrefix(name, prefix) {
			continue
		}
		stamp := strings.TrimPrefix(name, prefix)
		// Split off the collision suffix added by uniqueVersionedName;
		// it orders versions made in the same millisecond.
		n := 0
		if idx := strings.Index(stamp, "_"); idx > 0 {
			n, _ = strconv.Atoi(stamp[idx+1:]) //nolint:errcheck // a suffix that isn't a number sorts first
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
		p := filepath.Join(dir, name)
		seq[p] = n
		versions = append(versions, VersionInfo{
			Path:    p,
			Created: created,
			Size:    info.Size(),
		})
	}

	sort.Slice(versions, func(i, j int) bool {
		if !versions[i].Created.Equal(versions[j].Created) {
			return versions[i].Created.After(versions[j].Created)
		}
		return seq[versions[i].Path] > seq[versions[j].Path]
	})
	return versions, nil
}

// RestoreVersion writes the contents of versionPath to path. The
// current contents of path (if any) are first saved as a new
// versioned backup so the restore itself is reversible.
//
// versionPath does not have to be a file produced by
// [WriteFileVersioned]; any readable file works. The version file is
// read into memory bounded by [WithVersionsMaxBytes] (default 100 MiB).
func RestoreVersion(path, versionPath string, opts ...VersionedOption) error {
	cfg := newVersionedConfig(opts)
	data, err := ReadFile(versionPath, WithMaxSize(cfg.maxBytes))
	if err != nil {
		return wrapPathError(opRestoreVersion, versionPath, err)
	}
	if _, werr := WriteFileVersioned(path, data, opts...); werr != nil {
		return werr
	}
	return nil
}

func newVersionedConfig(opts []VersionedOption) versionedConfig {
	const defaultMaxBytes = 100 * 1024 * 1024
	cfg := versionedConfig{
		maxBytes: defaultMaxBytes,
		nowFn:    time.Now,
	}
	for _, opt := range opts {
		opt(&cfg)
	}
	if cfg.nowFn == nil {
		cfg.nowFn = time.Now
	}
	cfg.perm = cfg.perm.Perm()
	if cfg.maxBytes <= 0 {
		cfg.maxBytes = defaultMaxBytes
	}
	return cfg
}

func versionedName(path string, cfg versionedConfig) string {
	stamp := cfg.nowFn().UTC().Format(versionedTimestampLayout)
	return fmt.Sprintf("%s%s%s", path, versionedInfix, stamp)
}

// uniqueVersionedName returns a versionedName not currently in use.
// Writes in the same millisecond share a timestamp; each later one
// gets a "_N" suffix one above the highest already present, so the
// suffix keeps recording the order even after pruning removed some.
func uniqueVersionedName(path string, cfg versionedConfig) (string, error) {
	base := versionedName(path, cfg)
	baseName := filepath.Base(base)
	ents, err := os.ReadDir(filepath.Dir(path))
	if err != nil {
		return "", err //nolint:wrapcheck // wrapped by caller via *PathError
	}
	next := 0
	for _, e := range ents {
		name := e.Name()
		switch {
		case name == baseName:
			next = max(next, 1)
		case strings.HasPrefix(name, baseName+"_"):
			if n, aerr := strconv.Atoi(name[len(baseName)+1:]); aerr == nil && n >= next {
				next = n + 1
			}
		}
	}
	if next == 0 {
		return base, nil
	}
	return fmt.Sprintf("%s_%d", base, next), nil
}

// pruneVersions removes versioned backups of path that violate the
// keep or max-age policy. Called after each successful
// WriteFileVersioned.
func pruneVersions(path string, cfg versionedConfig) error {
	if cfg.keep == 0 && cfg.maxAge == 0 {
		return nil
	}

	versions, err := ListVersions(path)
	if err != nil {
		return err
	}

	// Age pruning runs first so the keep policy operates on the
	// survivors.
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
