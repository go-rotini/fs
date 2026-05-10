package fs

import "os"

// ArchiveExtractOption configures [ExtractArchive].
type ArchiveExtractOption func(*archiveExtractOptions)

type archiveExtractOptions struct {
	preserveMode bool
	filter       func(ArchiveHeader) bool
	maxBytes     int64
}

const defaultArchiveMaxBytes = 10 << 30 // 10 GiB

func newArchiveExtractOptions(opts []ArchiveExtractOption) archiveExtractOptions {
	cfg := archiveExtractOptions{
		preserveMode: false,
		maxBytes:     defaultArchiveMaxBytes,
	}
	for _, o := range opts {
		o(&cfg)
	}
	return cfg
}

// WithPreserveMode preserves entry permission bits from the archive.
// Default false (modes are masked to 0o644 for files, 0o755 for
// dirs). Enable with care — archives from untrusted sources can
// contain setuid bits or other surprising mode flags.
func WithPreserveMode(b bool) ArchiveExtractOption {
	return func(o *archiveExtractOptions) { o.preserveMode = b }
}

// WithArchiveFilter installs a per-entry predicate. Entries for
// which fn returns false are skipped (no filesystem write).
//
// Named WithArchiveFilter rather than WithFilter because
// [WithFilter] is already a [CopyOption].
func WithArchiveFilter(fn func(ArchiveHeader) bool) ArchiveExtractOption {
	return func(o *archiveExtractOptions) { o.filter = fn }
}

// WithArchiveMaxBytes caps the cumulative extracted bytes; archives
// that exceed return [ErrArchiveTooLarge]. Default 10 GiB. Set to
// zero or negative to disable.
func WithArchiveMaxBytes(n int64) ArchiveExtractOption {
	return func(o *archiveExtractOptions) { o.maxBytes = n }
}

// ArchiveCreateOption configures [CreateArchive].
type ArchiveCreateOption func(*archiveCreateOptions)

type archiveCreateOptions struct {
	format ArchiveFormat
	filter func(path string, info os.FileInfo) bool
}

func newArchiveCreateOptions(opts []ArchiveCreateOption) archiveCreateOptions {
	cfg := archiveCreateOptions{
		format: ArchiveFormatTar,
	}
	for _, o := range opts {
		o(&cfg)
	}
	return cfg
}

// WithArchiveFormat sets the output container format. Default
// [ArchiveFormatTar]. [ArchiveFormatTarGz] wraps the tar stream in
// gzip; [ArchiveFormatZip] writes a zip archive.
func WithArchiveFormat(f ArchiveFormat) ArchiveCreateOption {
	return func(o *archiveCreateOptions) { o.format = f }
}

// WithArchiveCreateFilter installs a per-walk-entry predicate.
// Entries (and subtrees, if directories) for which fn returns false
// are skipped.
func WithArchiveCreateFilter(fn func(path string, info os.FileInfo) bool) ArchiveCreateOption {
	return func(o *archiveCreateOptions) { o.filter = fn }
}
