package fstest

import (
	"fmt"
	stdfs "io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/go-rotini/fs"
)

// TestHarness is a sandbox for tests that exercise filesystem-aware
// code. It owns a temp directory rooted at `t.TempDir()` (so cleanup
// happens automatically via `t.Cleanup`) and exposes ergonomic
// helpers for placing fixtures, reading them back, and producing
// deterministic snapshots for golden-file comparison.
type TestHarness struct {
	t    *testing.T
	root string
}

// NewTestHarness returns a fresh [*TestHarness] rooted at
// `t.TempDir()`. Cleanup is automatic via the testing framework's
// per-test cleanup.
func NewTestHarness(t *testing.T) *TestHarness {
	t.Helper()
	return &TestHarness{t: t, root: t.TempDir()}
}

// NewHarnessAt returns a [*TestHarness] rooted at an existing
// directory. Unlike [NewTestHarness], this does NOT create the
// directory or register cleanup; the caller owns the root's
// lifecycle. Intended for acceptance-style tests that have already
// allocated a working directory and want the harness's [Snapshot] /
// [Path] ergonomics over it.
func NewHarnessAt(t *testing.T, root string) *TestHarness {
	t.Helper()
	return &TestHarness{t: t, root: root}
}

// Root returns the absolute path of the harness's root directory.
// Useful when handing off to code that takes a working directory.
func (h *TestHarness) Root() string { return h.root }

// Path returns the absolute path to rel within the harness. rel is
// joined under [Root]; absolute paths are interpreted relative to
// the harness root (the leading separator is stripped).
func (h *TestHarness) Path(rel string) string {
	cleaned := filepath.Clean(rel)
	if filepath.IsAbs(cleaned) {
		cleaned = cleaned[1:]
	}
	return filepath.Join(h.root, cleaned)
}

// Write atomically writes data to rel, creating any intermediate
// directories. Returns the absolute path. Calls `t.Fatalf` on
// error.
func (h *TestHarness) Write(rel string, data []byte) string {
	h.t.Helper()
	p := h.Path(rel)
	if err := os.MkdirAll(filepath.Dir(p), fs.Mode0755); err != nil {
		h.t.Fatalf("TestHarness.Write: MkdirAll: %v", err)
	}
	if err := fs.WriteFile(p, data); err != nil {
		h.t.Fatalf("TestHarness.Write: %v", err)
	}
	return p
}

// WriteString is shorthand for Write(rel, []byte(s)).
func (h *TestHarness) WriteString(rel, s string) string {
	h.t.Helper()
	return h.Write(rel, []byte(s))
}

// Read returns the contents of rel. Calls `t.Fatalf` on error.
func (h *TestHarness) Read(rel string) []byte {
	h.t.Helper()
	data, err := os.ReadFile(h.Path(rel))
	if err != nil {
		h.t.Fatalf("TestHarness.Read: %v", err)
	}
	return data
}

// Mkdir creates rel (and any missing parents) with mode 0o755.
// Returns the absolute path. Calls `t.Fatalf` on error.
func (h *TestHarness) Mkdir(rel string) string {
	h.t.Helper()
	p := h.Path(rel)
	if err := os.MkdirAll(p, fs.Mode0755); err != nil {
		h.t.Fatalf("TestHarness.Mkdir: %v", err)
	}
	return p
}

// Symlink creates a symlink at rel pointing at target. target is
// stored verbatim; relative targets are resolved against the
// symlink's own directory at lookup time, matching POSIX semantics.
// Returns the absolute path. Calls `t.Fatalf` on error.
func (h *TestHarness) Symlink(rel, target string) string {
	h.t.Helper()
	p := h.Path(rel)
	if err := os.MkdirAll(filepath.Dir(p), fs.Mode0755); err != nil {
		h.t.Fatalf("TestHarness.Symlink: MkdirAll: %v", err)
	}
	if err := os.Symlink(target, p); err != nil {
		h.t.Fatalf("TestHarness.Symlink: %v", err)
	}
	return p
}

// Remove deletes rel and any descendants. Calls `t.Fatalf` on
// error. Missing paths are tolerated (idempotent).
func (h *TestHarness) Remove(rel string) {
	h.t.Helper()
	if err := os.RemoveAll(h.Path(rel)); err != nil {
		h.t.Fatalf("TestHarness.Remove: %v", err)
	}
}

// Snapshot returns a deterministic representation of the harness's
// contents; sorted by path, with each entry rendered as either
// `DIR  <path>` or `FILE <path> mode=<perm> size=<size> <contents>`.
// Suitable for golden-file comparison.
//
// Symlinks render as `LINK <path> -> <target>` to capture the link
// target without dereferencing.
func (h *TestHarness) Snapshot() string {
	h.t.Helper()

	type entry struct {
		path     string
		mode     os.FileMode
		size     int64
		contents []byte
		linkTo   string
		isDir    bool
		isLink   bool
	}

	var entries []entry
	werr := filepath.WalkDir(h.root, func(path string, d stdfs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, rerr := filepath.Rel(h.root, path)
		if rerr != nil {
			return rerr //nolint:wrapcheck // surfaced through t.Fatalf below
		}
		if rel == "." {
			return nil
		}
		info, ierr := os.Lstat(path)
		if ierr != nil {
			return ierr //nolint:wrapcheck // surfaced through t.Fatalf below
		}
		e := entry{
			path:  filepath.ToSlash(rel),
			mode:  info.Mode(),
			isDir: info.IsDir(),
		}
		switch {
		case info.Mode()&os.ModeSymlink != 0:
			e.isLink = true
			lt, lerr := os.Readlink(path)
			if lerr == nil {
				e.linkTo = lt
			}
		case d.IsDir():
			// nothing extra
		default:
			//nolint:gosec // G122: snapshot reads its own harness tree; symlink TOCTOU is not a threat model here
			data, rerr := os.ReadFile(path)
			if rerr != nil {
				return rerr //nolint:wrapcheck // surfaced through t.Fatalf below
			}
			e.size = info.Size()
			e.contents = data
		}
		entries = append(entries, e)
		return nil
	})
	if werr != nil {
		h.t.Fatalf("TestHarness.Snapshot: %v", werr)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].path < entries[j].path })

	var b strings.Builder
	for _, e := range entries {
		switch {
		case e.isLink:
			fmt.Fprintf(&b, "LINK %s -> %s\n", e.path, e.linkTo)
		case e.isDir:
			fmt.Fprintf(&b, "DIR  %s\n", e.path)
		default:
			fmt.Fprintf(&b, "FILE %s mode=%o size=%d %s\n", e.path, e.mode.Perm(), e.size, e.contents)
		}
	}
	return b.String()
}
