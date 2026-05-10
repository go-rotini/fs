package fs

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
)

const opMagic = "magic"

// Magic returns the first n bytes of path. Useful for callers that
// want to inspect a file's leading bytes against known signatures
// (the package does not enumerate signatures itself — that's
// thousands of formats; compose with your own table or a third-party
// magic-bytes library).
//
// If path is shorter than n bytes, the returned slice is shorter
// than n (no error). n <= 0 returns an empty slice without opening
// the file.
func Magic(path string, n int) ([]byte, error) {
	if n <= 0 {
		return []byte{}, nil
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, wrapPathError(opMagic, path, err)
	}
	defer f.Close()

	buf := make([]byte, n)
	read, err := io.ReadFull(f, buf)
	if errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, io.EOF) {
		return buf[:read], nil
	}
	if err != nil {
		return nil, wrapPathError(opMagic, path, err)
	}
	return buf, nil
}

// ExtFormat returns a lowercase format identifier inferred from the
// extension of path. Returns "" for unrecognized or absent
// extensions. Multi-extension filenames (e.g., `foo.tar.gz`) are
// resolved by the LAST extension only, mirroring [filepath.Ext];
// callers that need compound-extension awareness compose with
// [filepath.Ext] themselves.
func ExtFormat(path string) string {
	ext := strings.ToLower(filepath.Ext(path))
	if ext == "" {
		return ""
	}
	return extFormats[ext[1:]]
}

// extFormats maps lowercase extensions (without the leading dot) to
// canonical format identifiers. The table is intentionally small —
// covers the formats CLI tools handle directly. Image, audio, and
// video formats are deliberately omitted; tools that work with
// those have purpose-built libraries.
var extFormats = map[string]string{
	// Config / data.
	"json": "json",
	"yaml": "yaml",
	"yml":  "yaml",
	"toml": "toml",
	"xml":  "xml",
	"ini":  "ini",
	"csv":  "csv",
	"tsv":  "tsv",

	// Markup / text.
	"html": "html",
	"htm":  "html",
	"md":   "markdown",
	"txt":  "txt",
	"log":  "log",

	// Archives / compression.
	"tar": "tar",
	"gz":  "gz",
	"tgz": "gz",
	"bz2": "bz2",
	"xz":  "xz",
	"zst": "zst",
	"zip": "zip",
	"7z":  "7z",
}
