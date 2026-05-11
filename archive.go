package fs

import (
	"bufio"
	"compress/gzip"
	"errors"
	"io"
	"os"
	"path/filepath"
	"time"
)

// ArchiveFormat identifies an archive container.
type ArchiveFormat int

const (
	// ArchiveFormatUnknown is the zero value; sniffers return it
	// when no recognizable magic is present.
	ArchiveFormatUnknown ArchiveFormat = iota
	// ArchiveFormatTar is a plain (uncompressed) ustar archive.
	ArchiveFormatTar
	// ArchiveFormatTarGz is a gzip-compressed tar.
	ArchiveFormatTarGz
	// ArchiveFormatZip is a zip archive (zlib-compressed inside the
	// container; archive/zip handles per-entry decompression).
	ArchiveFormatZip
)

// String renders the canonical lowercase name (e.g., "tar", "tar.gz",
// "zip", "unknown").
func (f ArchiveFormat) String() string {
	switch f {
	case ArchiveFormatTar:
		return "tar"
	case ArchiveFormatTarGz:
		return "tar.gz"
	case ArchiveFormatZip:
		return "zip"
	default:
		return unknownLabel
	}
}

// ArchiveHeader is the package's normalized archive-entry header.
// Both [ExtractArchive] and the create-side filters operate on this
// shape so callers don't have to handle tar.Header / zip.FileHeader
// separately.
type ArchiveHeader struct {
	// Name is the archive-internal path (no leading slash, no `..`).
	Name string
	// Size is the entry's byte size.
	Size int64
	// Mode is the entry's mode bits.
	Mode os.FileMode
	// ModTime is the entry's last-modification time.
	ModTime time.Time
	// IsDir is true for directory entries.
	IsDir bool
	// LinkTarget is populated for symlink entries (tar); zip doesn't
	// surface symlinks natively.
	LinkTarget string
}

// ErrArchiveTooLarge is returned by [ExtractArchive] when the
// cumulative extracted size exceeds the [WithArchiveMaxBytes] cap.
var ErrArchiveTooLarge = errors.New("fs: archive too large")

// ErrArchiveFormatUnknown is returned by [ExtractArchive] when the
// leading bytes don't match any recognized archive container, and
// by [CreateArchive] when an out-of-range [ArchiveFormat] is
// requested.
var ErrArchiveFormatUnknown = errors.New("fs: archive format unknown")

const (
	opExtractArchive = "extractarchive"
	opCreateArchive  = "createarchive"
	opOpenArchive    = "openautoarchive"
)

// ExtractArchive auto-detects the format of r and extracts entries
// under dst. Every entry path is resolved through
// [MustBeChildOf](dst, ...) before any filesystem write; this
// defends against zip-slip / tar-slip attacks where a crafted entry
// named `../../etc/passwd` would otherwise escape the extraction
// root.
//
// Modes are masked to safe defaults (0o644 files, 0o755 dirs)
// unless [WithPreserveMode]. Cumulative bytes written are capped by
// [WithArchiveMaxBytes] (default 10 GiB); over-cap aborts with
// [ErrArchiveTooLarge].
//
// [WithArchiveFilter] skips entries when its predicate returns false.
func ExtractArchive(r io.Reader, dst string, opts ...ArchiveExtractOption) error {
	cfg := newArchiveExtractOptions(opts)
	return extractArchive(r, dst, cfg)
}

// ExtractArchiveFile is shorthand for opening path and calling
// [ExtractArchive].
func ExtractArchiveFile(path, dst string, opts ...ArchiveExtractOption) error {
	f, err := os.Open(path)
	if err != nil {
		return wrapPathError(opExtractArchive, path, err)
	}
	defer f.Close()
	return ExtractArchive(f, dst, opts...)
}

// CreateArchive walks root and writes every regular file and
// directory entry to w as an archive in the format selected by
// [WithArchiveFormat] (default [ArchiveFormatTar]).
//
// [WithArchiveCreateFilter] skips entries when its predicate returns
// false.
func CreateArchive(w io.Writer, root string, opts ...ArchiveCreateOption) error {
	cfg := newArchiveCreateOptions(opts)
	switch cfg.format {
	case ArchiveFormatTar:
		return createTar(w, root, cfg, false)
	case ArchiveFormatTarGz:
		return createTar(w, root, cfg, true)
	case ArchiveFormatZip:
		return createZip(w, root, cfg)
	default:
		return wrapPathError(opCreateArchive, root, ErrArchiveFormatUnknown)
	}
}

// CreateArchiveFile is shorthand for creating a file at path and
// calling [CreateArchive]. The file is closed on return.
func CreateArchiveFile(path, root string, opts ...ArchiveCreateOption) error {
	f, err := os.Create(path)
	if err != nil {
		return wrapPathError(opCreateArchive, path, err)
	}
	defer f.Close()
	if err := CreateArchive(f, root, opts...); err != nil {
		return err
	}
	return f.Sync() //nolint:wrapcheck // surfacing the sync error directly
}

// OpenAutoArchive opens path, sniffs its leading bytes, and returns
// a transparent decompressor. Currently supports gzip (`.gz`); a
// plain (uncompressed) input is returned as-is. Other codecs (xz,
// zstd, lz4) are deferred to post-v0.1 because none ship in stdlib.
func OpenAutoArchive(path string) (io.ReadCloser, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, wrapPathError(opOpenArchive, path, err)
	}

	br := bufio.NewReader(f)
	header, perr := br.Peek(2)
	// Peek may return io.EOF for files <2 bytes; we handle short
	// input via the len() check below, so a Peek error is benign here.
	_ = perr
	if len(header) >= 2 && header[0] == 0x1f && header[1] == 0x8b {
		gz, gerr := gzip.NewReader(br)
		if gerr != nil {
			_ = f.Close()
			return nil, wrapPathError(opOpenArchive, path, gerr)
		}
		return &gzipReadCloser{Reader: gz, file: f}, nil
	}
	return &bufferedReadCloser{Reader: br, file: f}, nil
}

// gzipReadCloser composes the gzip reader's Close (which flushes its
// internal state) with the underlying file's Close.
type gzipReadCloser struct {
	*gzip.Reader

	file *os.File
}

func (g *gzipReadCloser) Close() error {
	// errors.Join keeps both errors in the chain; gzip Close
	// flushes internal state, and we never want a benign gzip
	// success to mask a file Close failure (or vice versa).
	return errors.Join(g.Reader.Close(), g.file.Close())
}

// bufferedReadCloser exposes Close on the underlying file while
// reads go through the buffered reader.
type bufferedReadCloser struct {
	io.Reader

	file *os.File
}

func (b *bufferedReadCloser) Close() error {
	return b.file.Close() //nolint:wrapcheck // surfacing the underlying file Close error directly
}

// extractArchive is the format-dispatch helper. It buffers the first
// `archiveSniffSize` bytes to detect the format, then re-attaches
// them to the stream.
func extractArchive(r io.Reader, dst string, cfg archiveExtractOptions) error {
	br := bufio.NewReader(r)
	sniff, perr := br.Peek(archiveSniffSize)
	// Short streams produce io.EOF; detectArchiveFormat handles a
	// short or missing prefix gracefully.
	_ = perr
	format := detectArchiveFormat(sniff)

	// G301 doesn't apply: archive entries set their own modes; root dir
	// matches stdlib MkdirAll's 0o755 default.
	if err := osMkdirAll(dst, 0o755); err != nil {
		return wrapPathError(opExtractArchive, dst, err)
	}

	switch format {
	case ArchiveFormatTar:
		return extractTar(br, dst, cfg)
	case ArchiveFormatTarGz:
		gz, err := gzip.NewReader(br)
		if err != nil {
			return wrapPathError(opExtractArchive, dst, err)
		}
		defer gz.Close()
		return extractTar(gz, dst, cfg)
	case ArchiveFormatZip:
		return extractZipFromStream(br, dst, cfg)
	default:
		return wrapPathError(opExtractArchive, dst, ErrArchiveFormatUnknown)
	}
}

// resolveEntryPath joins entryName under dst and verifies it does
// not escape via `..`. Absolute names are stripped of their leading
// separator before joining. Returns the absolute on-disk target.
func resolveEntryPath(dst, entryName string) (string, error) {
	cleaned := filepath.Clean(entryName)
	if filepath.IsAbs(cleaned) {
		// Strip the leading separator (works on POSIX); on Windows
		// a drive prefix is also illegal; handled by the IsAbs check
		// + the MustBeChildOf check below.
		cleaned = cleaned[1:]
	}
	full := filepath.Join(dst, cleaned)
	if err := MustBeChildOf(dst, full); err != nil {
		return "", err
	}
	return full, nil
}

// safeMode masks an entry mode to a narrow default unless preserve
// is true.
func safeMode(mode os.FileMode, isDir, preserve bool) os.FileMode {
	if preserve {
		return mode & os.ModePerm
	}
	if isDir {
		return Mode0755
	}
	return Mode0644
}

// limitWrap wraps w with a counter that bounds total bytes against
// limit; over-cap returns ErrArchiveTooLarge. limit <= 0 disables
// the cap (returns w unchanged).
func limitWrap(w io.Writer, used *int64, limit int64) io.Writer {
	if limit <= 0 {
		return w
	}
	return &cappedWriter{w: w, used: used, limit: limit}
}

type cappedWriter struct {
	w     io.Writer
	used  *int64
	limit int64
}

func (c *cappedWriter) Write(p []byte) (int, error) {
	remaining := c.limit - *c.used
	if remaining <= 0 {
		return 0, ErrArchiveTooLarge
	}
	if int64(len(p)) > remaining {
		// Write only what fits, then surface the cap error so the
		// caller stops streaming.
		n, err := c.w.Write(p[:remaining])
		*c.used += int64(n)
		if err != nil {
			return n, err
		}
		return n, ErrArchiveTooLarge
	}
	n, err := c.w.Write(p)
	*c.used += int64(n)
	return n, err
}
