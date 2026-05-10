package fs

import (
	"errors"
	"fmt"
	stdfs "io/fs"
	"strings"
)

// unknownLabel is the canonical string returned by stringer-style
// helpers ([ArchiveFormat.String], [HashAlgo.String], [LineEnding],
// [FilesystemType], etc.) when a value cannot be classified.
const unknownLabel = "unknown"

// PathError wraps an underlying filesystem error with the operation
// and path that produced it. The package returns *PathError from every
// op-with-a-path entry point; Cause is typically a *os.PathError or a
// syscall.Errno.
type PathError struct {
	Op    string
	Path  string
	Cause error
}

// Error renders as "fs: <op> <path>: <cause>".
func (e *PathError) Error() string {
	if e == nil {
		return "fs: path error"
	}
	switch {
	case e.Op == "" && e.Path == "":
		if e.Cause != nil {
			return fmt.Sprintf("fs: %v", e.Cause)
		}
		return "fs: path error"
	case e.Op == "":
		return fmt.Sprintf("fs: %s: %v", e.Path, e.Cause)
	case e.Path == "":
		return fmt.Sprintf("fs: %s: %v", e.Op, e.Cause)
	default:
		return fmt.Sprintf("fs: %s %s: %v", e.Op, e.Path, e.Cause)
	}
}

// Unwrap returns Cause for [errors.Unwrap] and [errors.Is] / [errors.As].
func (e *PathError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}

// Is matches another *PathError by type and walks the cause chain so
// callers can compare against either this package's sentinels or the
// stdlib's [io/fs.ErrNotExist] / [io/fs.ErrPermission] / [io/fs.ErrExist].
func (e *PathError) Is(target error) bool {
	if e == nil {
		return target == nil
	}
	if _, ok := target.(*PathError); ok {
		return true
	}
	return errors.Is(e.Cause, target)
}

// MultiError aggregates per-entry errors from bulk operations
// (CopyDir, RemoveAll under WithStrict, Walk error handler).
// Implements the Go 1.20+ Unwrap() []error convention so [errors.Is]
// and [errors.As] walk every branch.
type MultiError struct {
	Errors []error
}

// Error renders the aggregate; single-element MultiError defers to its
// child's Error.
func (m *MultiError) Error() string {
	if m == nil || len(m.Errors) == 0 {
		return "fs: no errors"
	}
	if len(m.Errors) == 1 {
		return m.Errors[0].Error()
	}
	var b strings.Builder
	fmt.Fprintf(&b, "fs: %d errors:", len(m.Errors))
	for _, e := range m.Errors {
		fmt.Fprintf(&b, "\n  - %s", e.Error())
	}
	return b.String()
}

// Unwrap returns the aggregated errors so [errors.Is] / [errors.As]
// walk every branch.
func (m *MultiError) Unwrap() []error {
	if m == nil {
		return nil
	}
	return m.Errors
}

// Is matches any *MultiError by type.
func (*MultiError) Is(target error) bool {
	_, ok := target.(*MultiError)
	return ok
}

// Append appends err to the aggregator. Nil errors are ignored.
func (m *MultiError) Append(err error) {
	if err == nil {
		return
	}
	m.Errors = append(m.Errors, err)
}

// Sentinel errors. The struct-pointer sentinels (those constructed as
// &XError{}) match by type via the corresponding Is method; the
// errors.New sentinels match by value identity.
//
// Where applicable, sentinels wrap stdlib equivalents so
// errors.Is(err, fs.ErrNotFound) matches both this package's
// ErrNotFound AND the stdlib's [io/fs.ErrNotExist].
var (
	// ErrNotFound matches stdlib [io/fs.ErrNotExist] via wrapping.
	ErrNotFound = fmt.Errorf("fs: not found: %w", stdfs.ErrNotExist)

	// ErrAlreadyExists matches stdlib [io/fs.ErrExist] via wrapping.
	ErrAlreadyExists = fmt.Errorf("fs: already exists: %w", stdfs.ErrExist)

	// ErrPermission matches stdlib [io/fs.ErrPermission] via wrapping.
	ErrPermission = fmt.Errorf("fs: permission denied: %w", stdfs.ErrPermission)

	// ErrNotDir is returned when a directory was expected but the
	// path is something else.
	ErrNotDir = errors.New("fs: not a directory")

	// ErrIsDir is returned when a regular file was expected.
	ErrIsDir = errors.New("fs: is a directory")

	// ErrCrossDevice is returned when a Rename across filesystems
	// fails (POSIX EXDEV / Windows ERROR_NOT_SAME_DEVICE). [Move]
	// falls back to Copy + Remove on this; [Rename] surfaces it.
	ErrCrossDevice = errors.New("fs: cross-device link")

	// ErrFileTooLarge is returned when a bounded read exceeds the cap
	// configured via [WithMaxSize].
	ErrFileTooLarge = errors.New("fs: file size exceeds limit")

	// ErrEmptyFile is returned by [ReadFirstLine] when the file is
	// empty.
	ErrEmptyFile = errors.New("fs: empty file")

	// ErrEscapesRoot is returned by [IsSubpath] / [MustBeChildOf] /
	// [EvalSymlinksWithin] when a path tries to escape its
	// constraint, and by archive extraction when an entry resolves
	// outside the destination root.
	ErrEscapesRoot = errors.New("fs: path escapes constraint root")

	// ErrSymlinkLoop is returned by [EvalSymlinks] after too many
	// hops, and by [OpenNoFollow] when the final component is a
	// symlink.
	ErrSymlinkLoop = errors.New("fs: symlink loop detected")

	// ErrInvalidPath is returned for malformed paths (NUL bytes,
	// path-separator characters in app-name args, etc.).
	ErrInvalidPath = errors.New("fs: invalid path")

	// ErrHashMismatch is returned by [HashCompare] on mismatch.
	ErrHashMismatch = errors.New("fs: hash mismatch")

	// ErrNotEmpty is returned when a directory expected to be empty
	// is not.
	ErrNotEmpty = errors.New("fs: directory not empty")

	// ErrShortRead is returned by [ReadAt] when fewer bytes were
	// available than the caller requested. [WithAllowShort] disables
	// it.
	ErrShortRead = errors.New("fs: short read")

	// ErrBrokenPipe is returned by [WriteStdout] / [WriteStderr] when
	// the consumer has closed the pipe (typical pattern: `mytool |
	// head`).
	ErrBrokenPipe = errors.New("fs: broken pipe")

	// ErrNotSupported is returned for queries that the platform
	// doesn't expose (e.g., birth time on Linux without statx, owner
	// on Windows).
	ErrNotSupported = errors.New("fs: not supported on this platform")

	// ErrInvalidByteSize is returned by [ParseBytes] / [ParseBytesStrict]
	// when the input is empty, has no numeric prefix, or has an
	// unrecognized unit suffix.
	ErrInvalidByteSize = errors.New("fs: invalid byte size")
)

// FormatError renders err into a human-readable string. *MultiError
// renders each branch on its own line; *PathError renders as
// "fs: <op> <path>: <cause>". Pass color=true to wrap each message in
// ANSI bold-red. Returns "" when err is nil.
func FormatError(err error, color ...bool) string {
	if err == nil {
		return ""
	}
	useColor := len(color) > 0 && color[0]
	var multi *MultiError
	if errors.As(err, &multi) && multi != nil && len(multi.Errors) > 0 {
		var b strings.Builder
		for i, sub := range multi.Errors {
			if i > 0 {
				b.WriteByte('\n')
			}
			b.WriteString(formatSingleErr(sub, useColor))
		}
		return b.String()
	}
	return formatSingleErr(err, useColor)
}

func formatSingleErr(err error, useColor bool) string {
	s := err.Error()
	if useColor {
		return "\x1b[1;31m" + s + "\x1b[0m"
	}
	return s
}

// wrapPathError wraps cause as a *PathError for op on path, returning
// nil when cause is nil. Internal helper for the package's IO
// entrypoints.
//
// Stdlib sentinels are normalized to the package's equivalents so
// that errors.Is(err, ErrNotFound) matches whether the underlying
// cause was the stdlib's [io/fs.ErrNotExist], a [syscall.ENOENT], or
// any other form. Without this normalization, errors.Is would only
// walk one direction (the package's sentinels wrap stdlib's, not the
// reverse).
func wrapPathError(op, path string, cause error) error {
	if cause == nil {
		return nil
	}
	return &PathError{Op: op, Path: path, Cause: normalizeCause(cause)}
}

// normalizeCause replaces a recognized stdlib sentinel in cause's
// chain with the package's wrapping form, so a single errors.Is call
// matches both. Returns cause unchanged for unrecognized errors.
func normalizeCause(cause error) error {
	switch {
	case errors.Is(cause, stdfs.ErrNotExist):
		return fmt.Errorf("%w: %w", ErrNotFound, cause)
	case errors.Is(cause, stdfs.ErrExist):
		return fmt.Errorf("%w: %w", ErrAlreadyExists, cause)
	case errors.Is(cause, stdfs.ErrPermission):
		return fmt.Errorf("%w: %w", ErrPermission, cause)
	default:
		return cause
	}
}
