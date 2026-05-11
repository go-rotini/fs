package fs

import (
	"errors"
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"runtime"
	"strings"
)

// Operation labels used in *PathError.Op for path-layer errors.
const (
	opAbs          = "abs"
	opExpand       = "expand"
	opRel          = "rel"
	opEvalSymlinks = "evalsymlinks"
)

// goosWindows is the runtime.GOOS value for Windows builds. Centralized
// so per-platform branches throughout the package read uniformly.
const goosWindows = "windows"

// errUnsetExpansion is the cause-level sentinel returned when [Expand]
// is in strict mode and a referenced variable is unset.
var errUnsetExpansion = errors.New("variable referenced in expansion is unset")

// PathOption configures path operations.
type PathOption func(*pathOptions)

type pathOptions struct {
	strictExpansion bool
}

// WithStrictExpansion makes [Expand] return an error when an
// environment variable is referenced but unset, rather than expanding
// to the empty string.
func WithStrictExpansion() PathOption {
	return func(o *pathOptions) {
		o.strictExpansion = true
	}
}

// Expand resolves ~, ~user, $VAR, and ${VAR} in path. Resolution is
// purely lexical; the filesystem is consulted only when ~user is
// supplied and the user must be looked up via os/user.
//
//	Expand("~/.config/myapp")        -> "/Users/alice/.config/myapp"
//	Expand("$HOME/notes")            -> "/Users/alice/notes"
//	Expand("${XDG_CACHE_HOME}/data") -> "/Users/alice/.cache/data"
//	Expand("~bob/notes")             -> "/Users/bob/notes"  (or error)
//
// An unset env var expands to "" by default; pass [WithStrictExpansion]
// to error instead.
func Expand(path string, opts ...PathOption) (string, error) {
	cfg := pathOptions{}
	for _, opt := range opts {
		opt(&cfg)
	}
	if path == "" {
		return "", nil
	}
	if strings.ContainsRune(path, '\x00') {
		return "", &PathError{Op: opExpand, Path: path, Cause: ErrInvalidPath}
	}

	if strings.HasPrefix(path, "~") {
		expanded, err := expandHome(path)
		if err != nil {
			return "", err
		}
		path = expanded
	}

	return expandVars(path, cfg.strictExpansion)
}

// expandHome resolves a leading ~ or ~user. The function is invoked
// only when path starts with "~".
func expandHome(path string) (string, error) {
	// Find the first separator after the tilde; everything between
	// "~" and the separator is the username.
	end := 1
	for end < len(path) && path[end] != '/' && path[end] != filepath.Separator {
		end++
	}
	username := path[1:end]
	rest := path[end:]

	var home string
	if username == "" {
		h, err := os.UserHomeDir()
		if err != nil {
			return "", &PathError{Op: opExpand, Path: path, Cause: err}
		}
		home = h
	} else {
		u, err := user.Lookup(username)
		if err != nil {
			return "", &PathError{Op: opExpand, Path: path, Cause: err}
		}
		home = u.HomeDir
	}
	return home + rest, nil
}

// expandVars resolves $VAR and ${VAR} references. strict=true causes
// an unset variable to return an error instead of expanding to "".
func expandVars(path string, strict bool) (string, error) {
	var b strings.Builder
	b.Grow(len(path))
	for i := 0; i < len(path); {
		if path[i] != '$' {
			b.WriteByte(path[i])
			i++
			continue
		}
		// Scan a $NAME or ${NAME} reference.
		name, consumed := scanVarRef(path[i:])
		if consumed == 0 {
			b.WriteByte(path[i])
			i++
			continue
		}
		v, ok := os.LookupEnv(name)
		if !ok {
			if strict {
				return "", &PathError{
					Op:    opExpand,
					Path:  path,
					Cause: fmt.Errorf("%w: %q", errUnsetExpansion, name),
				}
			}
			// Unset to expand to "".
		}
		b.WriteString(v)
		i += consumed
	}
	return b.String(), nil
}

// scanVarRef parses a $VAR or ${VAR} reference at the start of s.
// Returns the name and the number of bytes consumed; consumed=0 means
// no recognizable reference (lone $ or $<non-ident>).
func scanVarRef(s string) (name string, consumed int) {
	if len(s) < 2 || s[0] != '$' {
		return "", 0
	}
	if s[1] == '{' {
		end := strings.IndexByte(s, '}')
		if end < 0 {
			return "", 0
		}
		return s[2:end], end + 1
	}
	end := 1
	for end < len(s) && isVarRefByte(s[end]) {
		end++
	}
	if end == 1 {
		return "", 0
	}
	return s[1:end], end
}

func isVarRefByte(c byte) bool {
	switch {
	case c >= 'A' && c <= 'Z':
		return true
	case c >= 'a' && c <= 'z':
		return true
	case c >= '0' && c <= '9':
		return true
	case c == '_':
		return true
	}
	return false
}

// Abs returns the absolute form of path, resolving any leading ~ and
// $VAR via [Expand] first.
func Abs(path string, opts ...PathOption) (string, error) {
	expanded, err := Expand(path, opts...)
	if err != nil {
		return "", err
	}
	abs, err := filepath.Abs(expanded)
	if err != nil {
		return "", &PathError{Op: opAbs, Path: path, Cause: err}
	}
	return abs, nil
}

// Clean wraps [filepath.Clean].
func Clean(path string) string { return filepath.Clean(path) }

// Join wraps [filepath.Join].
func Join(elem ...string) string { return filepath.Join(elem...) }

// JoinSlash returns elem joined with `/` regardless of platform.
// Useful for portable URI-like paths.
func JoinSlash(elem ...string) string {
	clean := make([]string, 0, len(elem))
	for _, e := range elem {
		if e == "" {
			continue
		}
		clean = append(clean, e)
	}
	return strings.Join(clean, "/")
}

// Dir wraps [filepath.Dir].
func Dir(path string) string { return filepath.Dir(path) }

// Base wraps [filepath.Base].
func Base(path string) string { return filepath.Base(path) }

// Ext wraps [filepath.Ext].
func Ext(path string) string { return filepath.Ext(path) }

// Stem returns the basename of path without its final extension.
//
//	Stem("foo/bar.tar.gz") == "bar.tar"
//	Stem("README")         == "README"
//	Stem(".env")           == ".env"   (no extension)
func Stem(path string) string {
	b := filepath.Base(path)
	if b == "" || b == "." || b == ".." {
		return b
	}
	// A leading dot doesn't count as an extension separator
	// (.env, .gitignore have no stem-vs-ext split).
	ext := filepath.Ext(b)
	if ext == "" || ext == b {
		return b
	}
	return b[:len(b)-len(ext)]
}

// Split wraps [filepath.Split].
func Split(path string) (dir, file string) { return filepath.Split(path) }

// IsAbs wraps [filepath.IsAbs].
func IsAbs(path string) bool { return filepath.IsAbs(path) }

// Rel wraps [filepath.Rel].
func Rel(base, target string) (string, error) {
	r, err := filepath.Rel(base, target)
	if err != nil {
		return "", &PathError{Op: opRel, Path: target, Cause: err}
	}
	return r, nil
}

// ToSlash wraps [filepath.ToSlash].
func ToSlash(path string) string { return filepath.ToSlash(path) }

// FromSlash wraps [filepath.FromSlash].
func FromSlash(path string) string { return filepath.FromSlash(path) }

// EqualPath reports whether a and b refer to the same path after
// [Clean]. Comparison is case-insensitive on Windows (mirroring NTFS
// default behavior).
func EqualPath(a, b string) bool {
	ca := filepath.Clean(a)
	cb := filepath.Clean(b)
	if runtime.GOOS == goosWindows {
		return strings.EqualFold(ca, cb)
	}
	return ca == cb
}

// IsSubpath reports whether child resolves to a path inside parent
// after [Abs] + [Clean]. Returns (false, nil) for paths outside
// (never returning a permissive answer on resolution error). Returns
// (false, err) only on resolution failure.
//
// IsSubpath does not follow symlinks. Use [EvalSymlinksWithin] when
// the constraint must hold even after symlink resolution.
func IsSubpath(parent, child string) (bool, error) {
	pa, err := filepath.Abs(parent)
	if err != nil {
		return false, &PathError{Op: opAbs, Path: parent, Cause: err}
	}
	ca, err := filepath.Abs(child)
	if err != nil {
		return false, &PathError{Op: opAbs, Path: child, Cause: err}
	}
	pa = filepath.Clean(pa)
	ca = filepath.Clean(ca)

	// filepath.Rel can fail when the paths can't be made relative
	// (different drive letters on Windows, mixed abs/rel inputs). A
	// path that can't be made relative to parent is, by definition,
	// not a subpath; surface that as ok=false rather than an error.
	rel, relErr := filepath.Rel(pa, ca)
	if relErr != nil {
		return false, nil //nolint:nilerr // unrelatable paths are not subpaths
	}
	if rel == "." {
		return true, nil
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return false, nil
	}
	return true, nil
}

// MustBeChildOf returns nil when child resolves inside parent, or
// [ErrEscapesRoot] otherwise. Use to constrain user-supplied paths to
// a sandbox.
func MustBeChildOf(parent, child string) error {
	ok, err := IsSubpath(parent, child)
	if err != nil {
		return err
	}
	if !ok {
		return &PathError{Op: "child-check", Path: child, Cause: ErrEscapesRoot}
	}
	return nil
}

// EvalSymlinksWithin resolves all symlinks in path while ensuring the
// resolved path stays inside parent. Returns [ErrEscapesRoot] when
// the resolution leaves the parent.
//
// Both parent and path are themselves passed through
// [filepath.EvalSymlinks] before the containment check, so symlinked
// roots (e.g., macOS's /var to /private/var) compare correctly.
func EvalSymlinksWithin(parent, path string) (string, error) {
	resolvedPath, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", &PathError{Op: opEvalSymlinks, Path: path, Cause: err}
	}
	resolvedParent, err := filepath.EvalSymlinks(parent)
	if err != nil {
		return "", &PathError{Op: opEvalSymlinks, Path: parent, Cause: err}
	}
	parentAbs, err := filepath.Abs(resolvedParent)
	if err != nil {
		return "", &PathError{Op: opAbs, Path: parent, Cause: err}
	}
	resolvedAbs, err := filepath.Abs(resolvedPath)
	if err != nil {
		return "", &PathError{Op: opAbs, Path: resolvedPath, Cause: err}
	}
	ok, err := IsSubpath(parentAbs, resolvedAbs)
	if err != nil {
		return "", err
	}
	if !ok {
		return "", &PathError{Op: opEvalSymlinks, Path: path, Cause: ErrEscapesRoot}
	}
	return resolvedAbs, nil
}
