package fs

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"iter"
	"os"
	"strings"
	"sync"
)

// DefaultMaxReadSize is the default [WithMaxSize] cap: 100 MiB, enough
// for any plausible config or fixture while stopping runaway reads of
// inputs such as /dev/zero.
const DefaultMaxReadSize int64 = 100 << 20

// ReadOption configures [ReadFile], [ReadLines], [ReadFirstLine],
// [OpenLines] and [ReadAt].
type ReadOption func(*readOptions)

type readOptions struct {
	maxSize    int64
	expand     bool
	allowShort bool
}

func newReadOptions(opts []ReadOption) readOptions {
	cfg := readOptions{maxSize: DefaultMaxReadSize}
	for _, o := range opts {
		o(&cfg)
	}
	return cfg
}

// WithMaxSize sets the maximum bytes [ReadFile] and [ReadLines] will
// read, and the maximum line length for [OpenLines] and
// [ReadFirstLine]. Zero or negative disables the cap; only do that
// when the source is known to be of trustworthy size. [ReadAt]
// ignores it.
func WithMaxSize(n int64) ReadOption {
	return func(o *readOptions) {
		if n < 0 {
			n = 0
		}
		o.maxSize = n
	}
}

// WithExpand expands ~ and $VAR in the path with [Expand] before
// reading.
func WithExpand() ReadOption {
	return func(o *readOptions) { o.expand = true }
}

// WithAllowShort permits [ReadAt] to return fewer than n bytes
// without surfacing [ErrShortRead]. The returned slice is whatever
// was readable up to EOF.
func WithAllowShort(b bool) ReadOption {
	return func(o *readOptions) { o.allowShort = b }
}

// Operation labels for *PathError.Op on read-layer errors.
const (
	opOpen = "open"
	opRead = "read"
)

// resolveReadPath applies WithExpand if requested.
func resolveReadPath(path string, cfg readOptions) (string, error) {
	if !cfg.expand {
		return path, nil
	}
	return Expand(path)
}

// skipBOM advances br past a leading UTF-8 BOM if one is present.
// A peek failure (file shorter than 3 bytes, or no BOM) leaves br
// untouched; only a discard failure after a successful peek-of-BOM
// is surfaced; it means the underlying reader broke between peek
// and discard, which would surprise downstream callers.
func skipBOM(br *bufio.Reader) error {
	peek, peekErr := br.Peek(len(utf8BOM))
	if peekErr != nil {
		return nil //nolint:nilerr // short reader is fine here.
	}
	if !bytes.Equal(peek, utf8BOM) {
		return nil
	}
	if _, err := br.Discard(len(utf8BOM)); err != nil {
		return fmt.Errorf("discard BOM: %w", err)
	}
	return nil
}

// ReadFile reads path and returns its contents. Bounded by
// [WithMaxSize] (default [DefaultMaxReadSize]); a file exceeding the
// cap returns an error wrapping [ErrFileTooLarge]. The cap also holds
// for sources whose size Stat cannot report, such as FIFOs and
// character devices.
func ReadFile(path string, opts ...ReadOption) ([]byte, error) {
	cfg := newReadOptions(opts)
	p, err := resolveReadPath(path, cfg)
	if err != nil {
		return nil, err
	}
	f, err := os.Open(p)
	if err != nil {
		return nil, wrapPathError(opOpen, p, err)
	}
	defer func() { _ = f.Close() }()

	var reader io.Reader = f
	if cfg.maxSize > 0 {
		// Fast-fail for regular files: stat first, refuse before
		// allocating.
		if info, statErr := f.Stat(); statErr == nil && info.Mode().IsRegular() && info.Size() > cfg.maxSize {
			return nil, wrapPathError(opRead, p, ErrFileTooLarge)
		}
		reader = io.LimitReader(f, cfg.maxSize+1)
	}
	data, err := io.ReadAll(reader)
	if err != nil {
		return nil, wrapPathError(opRead, p, err)
	}
	if cfg.maxSize > 0 && int64(len(data)) > cfg.maxSize {
		return nil, wrapPathError(opRead, p, ErrFileTooLarge)
	}
	return data, nil
}

// ReadFileMax is shorthand for ReadFile(path, WithMaxSize(maxSize)).
func ReadFileMax(path string, maxSize int64) ([]byte, error) {
	return ReadFile(path, WithMaxSize(maxSize))
}

// ReadLines reads path as a sequence of lines. The trailing line
// ending (LF, CRLF, or bare CR) is stripped from each line; a
// leading UTF-8 BOM is stripped from the file. Bounded by
// [WithMaxSize].
func ReadLines(path string, opts ...ReadOption) ([]string, error) {
	data, err := ReadFile(path, opts...)
	if err != nil {
		return nil, err
	}
	data = StripUTF8BOM(data)
	return splitLines(data), nil
}

// splitLines returns b broken into lines on every LF, CR, or CRLF.
// A trailing line ending leaves no empty trailing entry; a final
// line without a terminator is included as the last entry.
func splitLines(b []byte) []string {
	if len(b) == 0 {
		return []string{}
	}
	out := make([]string, 0, 8)
	start := 0
	for i := 0; i < len(b); i++ {
		c := b[i]
		if c != '\n' && c != '\r' {
			continue
		}
		out = append(out, string(b[start:i]))
		// CRLF: consume the LF too.
		if c == '\r' && i+1 < len(b) && b[i+1] == '\n' {
			i++
		}
		start = i + 1
	}
	if start < len(b) {
		out = append(out, string(b[start:]))
	}
	return out
}

// ReadFirstLine returns the first line of path, without a leading
// UTF-8 BOM or the trailing LF or CRLF. Only LF ends a line. For an
// empty file the error wraps [ErrEmptyFile]. A first line longer than
// the [WithMaxSize] cap (default [DefaultMaxReadSize]) stops the read
// with an error wrapping [ErrFileTooLarge].
func ReadFirstLine(path string, opts ...ReadOption) (string, error) {
	cfg := newReadOptions(opts)
	p, err := resolveReadPath(path, cfg)
	if err != nil {
		return "", err
	}
	f, err := os.Open(p)
	if err != nil {
		return "", wrapPathError(opOpen, p, err)
	}
	defer func() { _ = f.Close() }()

	br := bufio.NewReader(f)
	if err := skipBOM(br); err != nil {
		return "", wrapPathError(opRead, p, err)
	}
	line, err := readFirstLine(br, cfg.maxSize)
	if errors.Is(err, io.EOF) && len(line) == 0 {
		return "", wrapPathError(opRead, p, ErrEmptyFile)
	}
	if err != nil && !errors.Is(err, io.EOF) {
		return "", wrapPathError(opRead, p, err)
	}
	return strings.TrimRight(string(line), "\r\n"), nil
}

// readFirstLine reads through the first LF, failing with [ErrFileTooLarge] once the line,
// without its LF or CRLF, is longer than maxSize (when maxSize > 0).
func readFirstLine(br *bufio.Reader, maxSize int64) ([]byte, error) {
	var line []byte
	for {
		chunk, err := br.ReadSlice('\n')
		line = append(line, chunk...)
		if maxSize > 0 && int64(len(bytes.TrimRight(line, "\r\n"))) > maxSize {
			return nil, ErrFileTooLarge
		}
		if !errors.Is(err, bufio.ErrBufferFull) {
			return line, err //nolint:wrapcheck // the caller wraps via *PathError
		}
	}
}

// OpenLines returns an iterator over path's lines. A leading UTF-8 BOM
// is skipped, and each line's trailing LF or CRLF is stripped. The
// caller must invoke the returned close function to release the file;
// it is idempotent and safe to defer.
//
// The iterator yields (line, nil) for each line, and ("", err) once
// at the end if reading failed, for example on an I/O error or a line
// longer than the [WithMaxSize] cap (never less than 64 KiB, and
// exactly 64 KiB when the cap is disabled). Check the error in the
// loop body:
//
//	seq, closeFn, err := fs.OpenLines("/tmp/log")
//	if err != nil { return err }
//	defer closeFn()
//	for line, err := range seq {
//	    if err != nil {
//	        return err
//	    }
//	    // process line
//	}
func OpenLines(path string, opts ...ReadOption) (iter.Seq2[string, error], func() error, error) {
	cfg := newReadOptions(opts)
	p, err := resolveReadPath(path, cfg)
	if err != nil {
		return nil, nil, err
	}
	f, err := os.Open(p)
	if err != nil {
		return nil, nil, wrapPathError(opOpen, p, err)
	}

	br := bufio.NewReader(f)
	if bomErr := skipBOM(br); bomErr != nil {
		_ = f.Close()
		return nil, nil, wrapPathError(opRead, p, bomErr)
	}
	scanner := bufio.NewScanner(br)
	if cfg.maxSize > 0 {
		// bufio.Scanner takes an int cap; clamp the int64 maxSize
		// safely and never below the default 64 KiB scratch buffer.
		const minScannerBuf = 64 * 1024
		buf := make([]byte, 0, minScannerBuf)
		scanner.Buffer(buf, max(int(cfg.maxSize), minScannerBuf))
	}

	seq := func(yield func(string, error) bool) {
		for scanner.Scan() {
			if !yield(scanner.Text(), nil) {
				return
			}
		}
		if serr := scanner.Err(); serr != nil {
			yield("", wrapPathError(opRead, p, serr))
		}
	}
	return seq, idempotentClose(f, opOpen, p), nil
}

// OpenChunked returns an iterator that streams path's contents in
// chunks of size bytes (the last may be shorter); each chunk is a
// new slice. The iterator stops at EOF; callers can break early. The
// returned cleanup function closes the file and is idempotent.
//
// If size <= 0, a 64 KiB default is used.
func OpenChunked(path string, size int) (iter.Seq2[[]byte, error], func() error, error) {
	const defaultChunkSize = 64 * 1024
	if size <= 0 {
		size = defaultChunkSize
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, nil, wrapPathError(opOpen, path, err)
	}
	seq := func(yield func([]byte, error) bool) {
		for {
			buf := make([]byte, size)
			n, err := io.ReadFull(f, buf)
			if n > 0 {
				if !yield(buf[:n], nil) {
					return
				}
			}
			if err == nil {
				continue
			}
			if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
				return
			}
			yield(nil, wrapPathError(opRead, path, err))
			return
		}
	}
	return seq, idempotentClose(f, opOpen, path), nil
}

// idempotentClose returns a close function that closes f at most
// once. Subsequent calls return the same result as the first. The
// op / path are used to wrap a non-nil Close error in *PathError so
// callers can `errors.Is(err, fs.ErrPermission)` without juggling
// the raw [*os.PathError] back into the package's envelope.
func idempotentClose(f *os.File, op, path string) func() error {
	var (
		once sync.Once
		err  error
	)
	return func() error {
		once.Do(func() {
			if cerr := f.Close(); cerr != nil {
				err = wrapPathError(op, path, cerr)
			}
		})
		return err
	}
}

// ReadAt reads n bytes from path starting at offset. If fewer than n
// bytes are available the error wraps [ErrShortRead], unless
// [WithAllowShort] is set. A negative n returns an error wrapping
// [ErrInvalidPath].
func ReadAt(path string, offset int64, n int, opts ...ReadOption) ([]byte, error) {
	cfg := newReadOptions(opts)
	p, err := resolveReadPath(path, cfg)
	if err != nil {
		return nil, err
	}
	if n < 0 {
		return nil, wrapPathError(opRead, p, ErrInvalidPath)
	}
	f, err := os.Open(p)
	if err != nil {
		return nil, wrapPathError(opOpen, p, err)
	}
	defer func() { _ = f.Close() }()

	buf := make([]byte, n)
	got, err := f.ReadAt(buf, offset)
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, wrapPathError(opRead, p, err)
	}
	if got < n && !cfg.allowShort {
		return nil, wrapPathError(opRead, p, ErrShortRead)
	}
	return buf[:got], nil
}
