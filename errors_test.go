package fs

import (
	"errors"
	stdfs "io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

// --- PathError ---

func TestPathError_Error(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		e    *PathError
		want string
	}{
		{
			name: "nil receiver",
			e:    nil,
			want: "fs: path error",
		},
		{
			name: "fully populated",
			e:    &PathError{Op: "read", Path: "/tmp/x", Cause: errors.New("boom")},
			want: "fs: read /tmp/x: boom",
		},
		{
			name: "missing op",
			e:    &PathError{Path: "/tmp/x", Cause: errors.New("boom")},
			want: "fs: /tmp/x: boom",
		},
		{
			name: "missing path",
			e:    &PathError{Op: "read", Cause: errors.New("boom")},
			want: "fs: read: boom",
		},
		{
			name: "only cause",
			e:    &PathError{Cause: errors.New("boom")},
			want: "fs: boom",
		},
		{
			name: "empty",
			e:    &PathError{},
			want: "fs: path error",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := c.e.Error(); got != c.want {
				t.Errorf("got %q, want %q", got, c.want)
			}
		})
	}
}

func TestPathError_Unwrap(t *testing.T) {
	t.Parallel()
	inner := errors.New("inner")
	pe := &PathError{Cause: inner}
	if got := pe.Unwrap(); got != inner {
		t.Errorf("Unwrap = %v, want %v", got, inner)
	}

	var nilPE *PathError
	if got := nilPE.Unwrap(); got != nil {
		t.Errorf("nil receiver Unwrap = %v, want nil", got)
	}
}

func TestPathError_Is_MatchesType(t *testing.T) {
	t.Parallel()
	pe := &PathError{Op: "read", Path: "/x", Cause: errors.New("y")}
	if !errors.Is(pe, &PathError{}) {
		t.Error("expected errors.Is(pe, &PathError{}) to be true")
	}
}

func TestPathError_Is_WalksCause(t *testing.T) {
	t.Parallel()
	// Errors built via wrapPathError (the package's entry-point
	// helper) normalize the stdlib sentinel into the package's
	// equivalent, so errors.Is matches both directions.
	pe := wrapPathError("read", "/missing", stdfs.ErrNotExist)

	if !errors.Is(pe, ErrNotFound) {
		t.Error("expected errors.Is(pe, fs.ErrNotFound) when cause was stdlib fs.ErrNotExist")
	}
	if !errors.Is(pe, stdfs.ErrNotExist) {
		t.Error("expected errors.Is(pe, stdfs.ErrNotExist)")
	}
}

func TestPathError_Is_RawCause_StdlibOnly(t *testing.T) {
	t.Parallel()
	// A PathError constructed directly with a stdlib cause (bypassing
	// wrapPathError) only matches the stdlib sentinel via the cause
	// chain. The package's ErrNotFound match relies on the
	// wrapPathError normalization. This documents the limitation:
	// callers should not construct *PathError directly.
	pe := &PathError{Op: "read", Path: "/missing", Cause: stdfs.ErrNotExist}
	if !errors.Is(pe, stdfs.ErrNotExist) {
		t.Error("expected errors.Is(pe, stdfs.ErrNotExist) for stdlib cause")
	}
}

// --- MultiError ---

func TestMultiError_Empty(t *testing.T) {
	t.Parallel()
	var m *MultiError
	if got := m.Error(); got != "fs: no errors" {
		t.Errorf("nil MultiError.Error() = %q", got)
	}

	m2 := &MultiError{}
	if got := m2.Error(); got != "fs: no errors" {
		t.Errorf("empty MultiError.Error() = %q", got)
	}
	if got := m2.Unwrap(); got != nil {
		t.Errorf("empty Unwrap() = %v, want nil", got)
	}
}

func TestMultiError_Single(t *testing.T) {
	t.Parallel()
	m := &MultiError{}
	m.Append(errors.New("only"))
	if got := m.Error(); got != "only" {
		t.Errorf("single-element Error = %q, want %q", got, "only")
	}
}

func TestMultiError_Multiple(t *testing.T) {
	t.Parallel()
	m := &MultiError{}
	m.Append(errors.New("first"))
	m.Append(errors.New("second"))
	got := m.Error()
	if !strings.HasPrefix(got, "fs: 2 errors:") {
		t.Errorf("Error = %q, missing header", got)
	}
	if !strings.Contains(got, "- first") || !strings.Contains(got, "- second") {
		t.Errorf("Error = %q, missing branches", got)
	}
}

func TestMultiError_Append_NilIgnored(t *testing.T) {
	t.Parallel()
	m := &MultiError{}
	m.Append(nil)
	m.Append(nil)
	if len(m.Errors) != 0 {
		t.Errorf("len = %d, want 0 (nils should be ignored)", len(m.Errors))
	}
}

func TestMultiError_UnwrapWalksBranches(t *testing.T) {
	t.Parallel()
	m := &MultiError{}
	m.Append(errors.New("first"))
	m.Append(ErrNotFound)
	if !errors.Is(m, ErrNotFound) {
		t.Error("errors.Is should walk MultiError branches")
	}
}

func TestMultiError_Is(t *testing.T) {
	t.Parallel()
	m := &MultiError{}
	if !errors.Is(m, &MultiError{}) {
		t.Error("MultiError.Is should match its own type")
	}
}

func TestMultiError_NilReceiver(t *testing.T) {
	t.Parallel()
	var m *MultiError
	if got := m.Unwrap(); got != nil {
		t.Errorf("nil.Unwrap() = %v, want nil", got)
	}
	if got := m.Error(); got != "fs: no errors" {
		t.Errorf("nil.Error() = %q", got)
	}
}

// --- PathError edge cases ---

func TestPathError_NilUnwrap(t *testing.T) {
	t.Parallel()
	var pe *PathError
	if got := pe.Unwrap(); got != nil {
		t.Errorf("nil PathError.Unwrap() = %v, want nil", got)
	}
}

func TestPathError_IsAcrossChain(t *testing.T) {
	t.Parallel()
	pe := &PathError{Op: "test", Path: "/x", Cause: ErrNotFound}
	if !errors.Is(pe, ErrNotFound) {
		t.Error("PathError.Is should walk to wrapped sentinel")
	}
	if !errors.Is(pe, &PathError{}) {
		t.Error("PathError.Is should match its own type")
	}
}

// --- Sentinels and stdlib interop ---

func TestSentinel_NotFound_MatchesStdlib(t *testing.T) {
	t.Parallel()
	if !errors.Is(ErrNotFound, stdfs.ErrNotExist) {
		t.Error("ErrNotFound must wrap stdfs.ErrNotExist")
	}
}

func TestSentinel_AlreadyExists_MatchesStdlib(t *testing.T) {
	t.Parallel()
	if !errors.Is(ErrAlreadyExists, stdfs.ErrExist) {
		t.Error("ErrAlreadyExists must wrap stdfs.ErrExist")
	}
}

func TestSentinel_Permission_MatchesStdlib(t *testing.T) {
	t.Parallel()
	if !errors.Is(ErrPermission, stdfs.ErrPermission) {
		t.Error("ErrPermission must wrap stdfs.ErrPermission")
	}
}

func TestSentinel_PathErrorWrapping_MatchesBoth(t *testing.T) {
	t.Parallel()
	// Wrapping syscall.ENOENT through wrapPathError normalizes the
	// cause to ErrNotFound, so the resulting PathError matches both
	// the package's ErrNotFound AND the stdlib's fs.ErrNotExist.
	pe := wrapPathError("read", "/x", syscall.ENOENT)
	if !errors.Is(pe, stdfs.ErrNotExist) {
		t.Error("PathError(ENOENT) should match stdfs.ErrNotExist")
	}
	if !errors.Is(pe, ErrNotFound) {
		t.Error("PathError(ENOENT) should match fs.ErrNotFound after normalization")
	}
}

func TestNormalizeCause_NotExist(t *testing.T) {
	t.Parallel()
	got := normalizeCause(stdfs.ErrNotExist)
	if !errors.Is(got, ErrNotFound) {
		t.Error("normalizeCause(ErrNotExist) should match ErrNotFound")
	}
	if !errors.Is(got, stdfs.ErrNotExist) {
		t.Error("normalizeCause should preserve stdlib match")
	}
}

func TestNormalizeCause_AlreadyExists(t *testing.T) {
	t.Parallel()
	got := normalizeCause(stdfs.ErrExist)
	if !errors.Is(got, ErrAlreadyExists) {
		t.Error("normalizeCause(ErrExist) should match ErrAlreadyExists")
	}
}

func TestNormalizeCause_Permission(t *testing.T) {
	t.Parallel()
	got := normalizeCause(stdfs.ErrPermission)
	if !errors.Is(got, ErrPermission) {
		t.Error("normalizeCause(ErrPermission) should match ErrPermission")
	}
}

func TestNormalizeCause_EXDEV(t *testing.T) {
	t.Parallel()
	got := normalizeCause(errTestEXDEV)
	if !errors.Is(got, ErrCrossDevice) {
		t.Error("normalizeCause(EXDEV) should match ErrCrossDevice")
	}
	if !errors.Is(got, errTestEXDEV) {
		t.Error("normalizeCause(EXDEV) should still match errTestEXDEV")
	}
}

func TestNormalizeCause_Unrecognized(t *testing.T) {
	t.Parallel()
	other := errors.New("unrelated")
	if got := normalizeCause(other); got != other {
		t.Errorf("unrecognized cause should pass through unchanged; got %v", got)
	}
}

// --- FormatError ---

func TestFormatError_Nil(t *testing.T) {
	t.Parallel()
	if got := FormatError(nil); got != "" {
		t.Errorf("FormatError(nil) = %q, want \"\"", got)
	}
}

func TestFormatError_Single_NoColor(t *testing.T) {
	t.Parallel()
	got := FormatError(errors.New("single"))
	if got != "single" {
		t.Errorf("got %q, want %q", got, "single")
	}
}

func TestFormatError_Single_Color(t *testing.T) {
	t.Parallel()
	got := FormatError(errors.New("single"), true)
	if !strings.HasPrefix(got, "\x1b[1;31m") || !strings.HasSuffix(got, "\x1b[0m") {
		t.Errorf("got %q, missing ANSI wrapping", got)
	}
}

func TestFormatError_Multi_OnePerLine(t *testing.T) {
	t.Parallel()
	m := &MultiError{}
	m.Append(errors.New("a"))
	m.Append(errors.New("b"))
	got := FormatError(m)
	lines := strings.Split(got, "\n")
	if len(lines) != 2 {
		t.Errorf("got %d lines, want 2: %q", len(lines), got)
	}
}

// --- wrapPathError ---

func TestWrapPathError_Nil(t *testing.T) {
	t.Parallel()
	if err := wrapPathError("op", "/p", nil); err != nil {
		t.Errorf("wrapPathError with nil cause = %v, want nil", err)
	}
}

func TestWrapPathError_NonNil(t *testing.T) {
	t.Parallel()
	cause := errors.New("inner")
	err := wrapPathError("op", "/p", cause)
	var pe *PathError
	if !errors.As(err, &pe) {
		t.Fatalf("err = %v, want *PathError", err)
	}
	if pe.Op != "op" || pe.Path != "/p" || pe.Cause != cause {
		t.Errorf("got %+v, want Op=op Path=/p Cause=inner", pe)
	}
}

func TestWrapPathError_DoesNotRepeatSentinel(t *testing.T) {
	t.Parallel()
	inner := wrapPathError("inner", "/p", stdfs.ErrExist)
	outer := wrapPathError("outer", "/p", inner)
	if n := strings.Count(outer.Error(), "already exists"); n != 1 {
		t.Errorf("%q says \"already exists\" %d times; want 1", outer, n)
	}
	if !errors.Is(outer, ErrAlreadyExists) || !errors.Is(outer, stdfs.ErrExist) {
		t.Errorf("%v lost its sentinels", outer)
	}
	if n := strings.Count(wrapPathError("op", "/p", ErrNotFound).Error(), "fs: not found"); n != 1 {
		t.Errorf("wrapping ErrNotFound repeats it %d times", n)
	}
}

// A sentinel's text doesn't repeat its io/fs equivalent's, which the OS error it wraps already
// says in its own words.
func TestSentinels_TextOmitsStdlibPhrase(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		sentinel, std error
		want          string
	}{
		{ErrNotFound, stdfs.ErrNotExist, "fs: not found"},
		{ErrAlreadyExists, stdfs.ErrExist, "fs: already exists"},
		{ErrPermission, stdfs.ErrPermission, "fs: permission denied"},
	} {
		if c.sentinel.Error() != c.want {
			t.Errorf("%q, want %q", c.sentinel, c.want)
		}
		if !errors.Is(c.sentinel, c.std) {
			t.Errorf("%q no longer matches %v", c.sentinel, c.std)
		}
	}
	_, err := os.Open(filepath.Join(t.TempDir(), "missing"))
	if msg := wrapPathError("read", "/p", err).Error(); strings.Contains(msg, stdfs.ErrNotExist.Error()) {
		t.Errorf("%q repeats %q", msg, stdfs.ErrNotExist)
	}
}

// A message names the path, and carries the "fs:" prefix, once; the error still matches the
// sentinel, the io/fs error and the OS *PathError.
func TestPathError_MessageNamesPathOnce(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "missing")
	_, err := ReadFile(path)
	if err == nil {
		t.Fatal("ReadFile of a missing file succeeded")
	}
	msg := err.Error()
	if n := strings.Count(msg, path); n != 1 {
		t.Errorf("%q names the path %d times; want 1", msg, n)
	}
	if n := strings.Count(msg, "fs: "); n != 1 {
		t.Errorf("%q has the fs: prefix %d times; want 1", msg, n)
	}
	if !errors.Is(err, ErrNotFound) || !errors.Is(err, stdfs.ErrNotExist) {
		t.Errorf("%v lost its sentinels", err)
	}
	if _, ok := errors.AsType[*stdfs.PathError](err); !ok {
		t.Errorf("%v no longer carries the OS *PathError", err)
	}
	other := wrapPathError("copy", "/a", &stdfs.PathError{Op: "open", Path: "/b", Err: syscall.ENOENT})
	if msg := other.Error(); !strings.Contains(msg, "open /b") {
		t.Errorf("%q drops the other path the OS error names", msg)
	}
}
