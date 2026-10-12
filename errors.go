package fs

import (
	"errors"
	"fmt"
	stdfs "io/fs"
	"strings"
	"syscall"
)

// unknownLabel is the canonical string returned by stringer-style
// helpers ([ArchiveFormat.String], [HashAlgo.String], [LineEnding],
// [FilesystemType], etc.) when a value cannot be classified.
const unknownLabel = "unknown"

// PathError wraps an underlying filesystem error with the operation
// and path that produced it. The package returns *PathError from
// nearly every function that touches a path.
type PathError struct {
	// Op names the operation, such as "copyfile" or "walk".
	Op string
	// Path is the path the operation failed on; it may be empty.
	Path string
	// Cause is the underlying error.
	Cause error
}

// Error renders as "fs: <op> <path>: <cause>", leaving out an empty op
// or path. For programmatic inspection use the Op, Path and Cause
// fields rather than parsing the string.
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

// Is reports true for any *PathError target, and otherwise matches
// target against the cause chain, so callers can compare against
// either this package's sentinels or the stdlib's
// [io/fs.ErrNotExist] / [io/fs.ErrPermission] / [io/fs.ErrExist].
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
// ([CopyDir], [RemoveContents]). Its Unwrap() []error method lets
// [errors.Is] and [errors.As] walk every branch.
type MultiError struct {
	// Errors holds the collected errors in the order they occurred.
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

// Sentinel errors, matched with [errors.Is].
//
// ErrNotFound, ErrAlreadyExists and ErrPermission wrap their [io/fs]
// equivalents, and errors returned by this package carry both forms,
// so errors.Is(err, fs.ErrNotFound) and
// errors.Is(err, io/fs.ErrNotExist) both match.
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

	// ErrCrossDevice is returned when a rename or hard link crosses
	// filesystems (EXDEV). [Move] falls back to copy and remove on
	// it; [Rename] and [Hardlink] return it.
	ErrCrossDevice = errors.New("fs: cross-device link")

	// ErrFileTooLarge is returned when a bounded read exceeds the cap
	// configured via [WithMaxSize].
	ErrFileTooLarge = errors.New("fs: file size exceeds limit")

	// ErrEmptyFile is returned by [ReadFirstLine] when the file is
	// empty.
	ErrEmptyFile = errors.New("fs: empty file")

	// ErrEscapesRoot is returned by [MustBeChildOf] and
	// [EvalSymlinksWithin] when a path escapes its constraint, and by
	// [ExtractArchive] when an entry resolves outside the destination.
	ErrEscapesRoot = errors.New("fs: path escapes constraint root")

	// ErrSymlinkLoop is returned by [EvalSymlinks] after too many
	// hops, and by [OpenNoFollow] when the final component is a
	// symlink.
	ErrSymlinkLoop = errors.New("fs: symlink loop detected")

	// ErrInvalidPath is returned for malformed path-like inputs, such
	// as a path containing a NUL byte or an invalid cache version.
	ErrInvalidPath = errors.New("fs: invalid path")

	// ErrHashMismatch is returned by [HashCompare] on mismatch.
	ErrHashMismatch = errors.New("fs: hash mismatch")

	// ErrNotEmpty reports a directory that was expected to be empty
	// but is not. No function in this package currently returns it.
	ErrNotEmpty = errors.New("fs: directory not empty")

	// ErrShortRead is returned by [ReadAt] when fewer bytes were
	// available than the caller requested. [WithAllowShort] disables
	// it.
	ErrShortRead = errors.New("fs: short read")

	// ErrBrokenPipe is returned by [WriteStdout] / [WriteStderr] when
	// the consumer has closed the pipe (typical pattern: `mytool |
	// head`).
	ErrBrokenPipe = errors.New("fs: broken pipe")

	// ErrNotSupported is returned for operations or queries the
	// platform or input does not support (e.g., file owner on Windows,
	// copying a device file).
	ErrNotSupported = errors.New("fs: not supported on this platform")

	// ErrInvalidByteSize is returned by [ParseBytes] / [ParseBytesIEC]
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
	case errors.Is(cause, syscall.EXDEV):
		return fmt.Errorf("%w: %w", ErrCrossDevice, cause)
	default:
		return cause
	}
}
