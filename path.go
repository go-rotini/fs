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
// Inside braces the POSIX shell operators are supported, one pass (a
// word is not expanded again):
//
//	${VAR:-word}  word if VAR is unset or empty
//	${VAR-word}   word if VAR is unset
//	${VAR:+word}  word if VAR is set and not empty, else ""
//	${VAR+word}   word if VAR is set, else ""
//	${VAR:?msg}   error if VAR is unset or empty
//	${VAR?msg}    error if VAR is unset
//
// Any other ${...} form returns an error wrapping [ErrInvalidPath].
//
// The tilde is only recognized at the start of path, and the home
// directory it expands to is used as is: a `$` in it is not expanded.
// A `$` not followed by a name, or an unclosed `${`, is kept
// literally. An unset env var expands to "" by default; pass
// [WithStrictExpansion] to error instead. A path containing a NUL
// byte returns an error wrapping [ErrInvalidPath].
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

	// As in a shell, the home directory a tilde expands to is not
	// expanded again: variables are expanded in the rest only.
	home, rest := "", path
	if strings.HasPrefix(path, "~") {
		var err error
		if home, rest, err = expandHome(path); err != nil {
			return "", err
		}
	}
	expanded, err := expandVars(rest, cfg.strictExpansion)
	if err != nil {
		return "", &PathError{Op: opExpand, Path: path, Cause: err}
	}
	return home + expanded, nil
}

// expandHome resolves a leading ~ or ~user, returning the home
// directory and the rest of path after the user name. The function is
// invoked only when path starts with "~".
func expandHome(path string) (home, rest string, err error) {
	// Find the first separator after the tilde; everything between
	// "~" and the separator is the username.
	end := 1
	for end < len(path) && path[end] != '/' && path[end] != filepath.Separator {
		end++
	}
	username := path[1:end]
	rest = path[end:]

	if username == "" {
		h, herr := os.UserHomeDir()
		if herr != nil {
			return "", "", &PathError{Op: opExpand, Path: path, Cause: herr}
		}
		return h, rest, nil
	}
	u, lerr := user.Lookup(username)
	if lerr != nil {
		return "", "", &PathError{Op: opExpand, Path: path, Cause: lerr}
	}
	return u.HomeDir, rest, nil
}

// expandVars resolves $VAR and ${...} references. strict=true causes
// an unset plain reference to return an error instead of expanding to "".
// The caller wraps the error.
func expandVars(path string, strict bool) (string, error) {
	var b strings.Builder
	b.Grow(len(path))
	for i := 0; i < len(path); {
		if path[i] != '$' {
			b.WriteByte(path[i])
			i++
			continue
		}
		ref, consumed := scanVarRef(path[i:])
		if consumed == 0 {
			b.WriteByte(path[i])
			i++
			continue
		}
		v, err := ref.eval(strict)
		if err != nil {
			return "", err
		}
		b.WriteString(v)
		i += consumed
	}
	return b.String(), nil
}

// varRef is one parsed $NAME or ${NAME[op word]} reference. invalid is set
// for a ${...} whose content is not a name or a name plus a supported
// operator.
type varRef struct {
	name, op, word string
	invalid        bool
}

// eval resolves r against the environment.
func (r varRef) eval(strict bool) (string, error) {
	if r.invalid {
		return "", fmt.Errorf("%w: unsupported expansion ${%s}", ErrInvalidPath, r.word)
	}
	v, set := os.LookupEnv(r.name)
	switch r.op {
	case ":-":
		if v == "" {
			return r.word, nil
		}
	case "-":
		if !set {
			return r.word, nil
		}
	case ":+":
		if v == "" {
			return "", nil
		}
		return r.word, nil
	case "+":
		if !set {
			return "", nil
		}
		return r.word, nil
	case ":?", "?":
		if (r.op == ":?" && v == "") || !set {
			msg := r.word
			if msg == "" {
				msg = "unset or empty"
			}
			return "", fmt.Errorf("%w: %q: %s", errUnsetExpansion, r.name, msg)
		}
	default:
		if !set && strict {
			return "", fmt.Errorf("%w: %q", errUnsetExpansion, r.name)
		}
	}
	return v, nil
}

// scanVarRef parses a $NAME or ${...} reference at the start of s and
// returns it with the number of bytes consumed; consumed=0 means no
// reference (a lone $, $<non-name>, or an unclosed ${).
func scanVarRef(s string) (ref varRef, consumed int) {
	if len(s) < 2 || s[0] != '$' {
		return varRef{}, 0
	}
	if s[1] == '{' {
		end := strings.IndexByte(s, '}')
		if end < 0 {
			return varRef{}, 0
		}
		return parseBraced(s[2:end]), end + 1
	}
	end := 1
	for end < len(s) && isVarRefByte(s[end]) {
		end++
	}
	if end == 1 {
		return varRef{}, 0
	}
	return varRef{name: s[1:end]}, end
}

// parseBraced reads the content of ${...}: a name, optionally followed
// directly by one of the operators :- - :+ + :? ? and its word.
func parseBraced(expr string) varRef {
	n := 0
	for n < len(expr) && isVarRefByte(expr[n]) {
		n++
	}
	if n == 0 {
		return varRef{invalid: true, word: expr}
	}
	name, rest := expr[:n], expr[n:]
	if rest == "" {
		return varRef{name: name}
	}
	for _, op := range [...]string{":-", ":+", ":?", "-", "+", "?"} {
		if strings.HasPrefix(rest, op) {
			return varRef{name: name, op: op, word: rest[len(op):]}
		}
	}
	return varRef{invalid: true, word: expr}
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

// Abs returns the absolute form of path, resolving ~ and $VAR via
// [Expand] first. opts are passed to [Expand].
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

// JoinSlash returns elem joined with `/` regardless of platform,
// skipping empty elements. Unlike [Join] it does not clean the
// result. Useful for portable URI-like paths.
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

// Rel wraps [filepath.Rel], returning errors as a [*PathError].
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

// IsSubpath reports whether child is parent or a path inside it,
// after making both absolute with [filepath.Abs] (no ~ or $VAR
// expansion). Returns (false, nil) for paths outside, including
// paths that cannot be made relative to parent (such as a different
// Windows drive); (false, err) only when a path cannot be made
// absolute. The comparison is case-sensitive on every platform.
//
// IsSubpath is lexical and does not follow symlinks. Use [EvalSymlinksWithin] when
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

// MustBeChildOf returns nil when [IsSubpath] reports child inside
// parent (child equal to parent counts), or a [*PathError] wrapping
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

// EvalSymlinksWithin resolves all symlinks in path and returns the
// absolute result, or an error wrapping [ErrEscapesRoot] when the
// resolved path is outside parent. path must exist.
//
// Both parent and path are themselves passed through
// [filepath.EvalSymlinks] before the containment check, so symlinked
// roots (e.g., macOS's /var to /private/var) compare correctly. On
// Windows, directory junctions are followed too, and a path through any
// other reparse point that redirects a lookup fails with
// [ErrEscapesRoot].
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
	if parentAbs, err = resolveJunctions(parentAbs); err != nil {
		return "", wrapPathError(opEvalSymlinks, parent, err)
	}
	resolvedAbs, err := filepath.Abs(resolvedPath)
	if err != nil {
		return "", &PathError{Op: opAbs, Path: resolvedPath, Cause: err}
	}
	if resolvedAbs, err = resolveJunctions(resolvedAbs); err != nil {
		return "", wrapPathError(opEvalSymlinks, path, err)
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
