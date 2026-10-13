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
// or path. An OS error cause is shown without the sentinel that
// classifies it, and by its error text alone when it names the same
// path, such as "fs: readfile a.txt: no such file or directory"; both
// still match through [errors.Is] and [errors.As]. For programmatic
// inspection use the Op, Path and Cause fields rather than parsing the
// string.
func (e *PathError) Error() string {
	if e == nil {
		return "fs: path error"
	}
	cause := causeText(e.Path, e.Cause)
	switch {
	case e.Op == "" && e.Path == "":
		if e.Cause == nil {
			return "fs: path error"
		}
		return "fs: " + cause
	case e.Op == "":
		return "fs: " + e.Path + ": " + cause
	case e.Path == "":
		return "fs: " + e.Op + ": " + cause
	default:
		return "fs: " + e.Op + " " + e.Path + ": " + cause
	}
}

// Unwrap returns Cause for [errors.Unwrap] and [errors.Is] / [errors.As].
func (e *PathError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}

// Is reports true for any *PathError target, whatever its fields, and
// otherwise matches target against the cause chain, so callers can
// compare against either this package's sentinels or the stdlib's
// [io/fs.ErrNotExist] / [io/fs.ErrPermission] / [io/fs.ErrExist].
// errors.Is(err, &PathError{...}) therefore only tests the type; use
// [errors.As] to inspect Op, Path or Cause.
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
	ErrNotFound error = &stdSentinelError{msg: "fs: not found", std: stdfs.ErrNotExist}

	// ErrAlreadyExists matches stdlib [io/fs.ErrExist] via wrapping.
	ErrAlreadyExists error = &stdSentinelError{msg: "fs: already exists", std: stdfs.ErrExist}

	// ErrPermission matches stdlib [io/fs.ErrPermission] via wrapping.
	ErrPermission error = &stdSentinelError{msg: "fs: permission denied", std: stdfs.ErrPermission}

	// ErrNotDir is returned when a directory was expected but the
	// path is something else.
	ErrNotDir = errors.New("fs: not a directory")

	// ErrIsDir is returned when a regular file was expected.
	ErrIsDir = errors.New("fs: is a directory")

	// ErrCrossDevice is returned when a rename or hard link crosses
	// filesystems (EXDEV, or ERROR_NOT_SAME_DEVICE on Windows). [Move]
	// falls back to copy and remove on it; [Rename] and [Hardlink]
	// return it.
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
	// but is not, such as one passed to [Remove] or a [Rename] onto a
	// directory with entries. Like the OS error it wraps, it also
	// matches [io/fs.ErrExist].
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

// stdSentinelError is a sentinel that wraps its [io/fs] equivalent
// without repeating that error's text, which the OS error it is
// joined with already says.
type stdSentinelError struct {
	msg string
	std error
}

func (e *stdSentinelError) Error() string { return e.msg }
func (e *stdSentinelError) Unwrap() error { return e.std }

// classifiedError joins an OS error with the package sentinel that classifies it, so
// [errors.Is] matches both.
type classifiedError struct {
	class error // a package sentinel, such as ErrNotFound
	cause error // the OS error
}

func (e *classifiedError) Error() string   { return e.class.Error() + ": " + e.cause.Error() }
func (e *classifiedError) Unwrap() []error { return []error{e.class, e.cause} }

// causeText renders cause for a [PathError] on path. A sentinel joined to an OS error is left
// out, since the OS error says the same, and an OS error that names path itself is reduced to
// its error text, so the message names the path once.
func causeText(path string, cause error) string {
	if cause == nil {
		return "<nil>"
	}
	if c, ok := cause.(*classifiedError); ok { //nolint:errorlint // only the cause itself, not deeper wraps
		cause = c.cause
	}
	if pe, ok := cause.(*stdfs.PathError); ok && pe.Path == path && pe.Err != nil { //nolint:errorlint // only the cause itself
		return pe.Err.Error()
	}
	return cause.Error()
}

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

// withOp reports err, a [*PathError] from a helper, under op.
func withOp(op string, err error) error {
	if pe, ok := errors.AsType[*PathError](err); ok {
		pe.Op = op
	}
	return err
}

// normalizeCause replaces a recognized stdlib sentinel in cause's
// chain with the package's wrapping form, so a single errors.Is call
// matches both. Returns cause unchanged for unrecognized errors and
// for causes that already carry one of the package's sentinels.
func normalizeCause(cause error) error {
	switch {
	case errors.Is(cause, ErrNotFound), errors.Is(cause, ErrNotEmpty),
		errors.Is(cause, ErrAlreadyExists), errors.Is(cause, ErrPermission),
		errors.Is(cause, ErrCrossDevice):
		return cause
	case errors.Is(cause, stdfs.ErrNotExist):
		return &classifiedError{class: ErrNotFound, cause: cause}
	case isNotEmptyErrno(cause):
		return &classifiedError{class: ErrNotEmpty, cause: cause}
	case errors.Is(cause, stdfs.ErrExist):
		return &classifiedError{class: ErrAlreadyExists, cause: cause}
	case errors.Is(cause, stdfs.ErrPermission):
		return &classifiedError{class: ErrPermission, cause: cause}
	case isCrossDeviceErrno(cause):
		return &classifiedError{class: ErrCrossDevice, cause: cause}
	default:
		return cause
	}
}
