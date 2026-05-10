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
)

// DefaultMaxReadSize bounds the default size cap on every read
// operation in the package. 100 MiB defends against accidental DoS
// via /dev/zero, runaway log files, and similar pathological inputs
// without rejecting any plausible CLI config / fixture / corpus
// input.
const DefaultMaxReadSize int64 = 100 << 20

// ReadOption configures read operations.
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

// WithMaxSize sets the maximum bytes the read will consume. Zero or
// negative disables the cap (the read becomes unbounded — only do
// this when you know the source is a regular file of trustworthy
// size).
func WithMaxSize(n int64) ReadOption {
	return func(o *readOptions) {
		if n < 0 {
			n = 0
		}
		o.maxSize = n
	}
}

// WithExpand expands ~ and $VAR in the path before reading.
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
// is surfaced — it means the underlying reader broke between peek
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
// cap returns [ErrFileTooLarge].
//
// For sources whose size cannot be determined via Stat (FIFOs,
// character devices), the cap is enforced through an [io.LimitReader]
// at cap+1, so over-size streams still error rather than read forever.
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

// ReadFirstLine returns the first line of path. Returns
// [ErrEmptyFile] when the file is empty.
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
	line, err := br.ReadString('\n')
	if errors.Is(err, io.EOF) && line == "" {
		return "", wrapPathError(opRead, p, ErrEmptyFile)
	}
	if err != nil && !errors.Is(err, io.EOF) {
		return "", wrapPathError(opRead, p, err)
	}
	return strings.TrimRight(line, "\r\n"), nil
}

// OpenLines returns an iterator over path's lines. Trailing line
// endings are stripped per line. The caller MUST invoke the returned
// close function to release the file handle.
//
//	seq, closeFn, err := fs.OpenLines("/tmp/log")
//	if err != nil { return err }
//	defer closeFn()
//	for line := range seq {
//	    // process line
//	}
func OpenLines(path string, opts ...ReadOption) (iter.Seq[string], func() error, error) {
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

	seq := func(yield func(string) bool) {
		for scanner.Scan() {
			if !yield(scanner.Text()) {
				return
			}
		}
	}
	return seq, f.Close, nil
}

// OpenChunked returns an iterator that streams path's contents in
// chunks of size bytes. The iterator stops at EOF; callers can break
// early. The returned cleanup function closes the underlying file
// and is idempotent — safe to call multiple times.
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
	return seq, f.Close, nil
}

// ReadAt reads n bytes from path starting at offset. By default
// returns [ErrShortRead] if fewer than n bytes are available; pass
// [WithAllowShort] to return whatever was readable.
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
