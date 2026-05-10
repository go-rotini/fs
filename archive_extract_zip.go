package fs

import (
	"archive/zip"
	"io"
	"os"
	"path/filepath"
)

// extractZipFromStream materializes the zip stream to a temp file
// (archive/zip needs random-access; the stdlib offers no streaming
// reader), then dispatches to extractZipFile.
//
// The buffer step is bounded: we copy at most cfg.maxBytes+1 bytes
// to the temp file before aborting.
func extractZipFromStream(r io.Reader, dst string, cfg archiveExtractOptions) error {
	tmp, err := os.CreateTemp("", "rotini-zip-stream-*.zip")
	if err != nil {
		return wrapPathError(opExtractArchive, dst, err)
	}
	defer func() {
		name := tmp.Name()
		_ = tmp.Close()
		_ = os.Remove(name)
	}()

	limit := cfg.maxBytes
	if limit <= 0 {
		// No cap — copy whole stream.
		if _, err := io.Copy(tmp, r); err != nil {
			return wrapPathError(opExtractArchive, dst, err)
		}
	} else {
		// Cap+1 so an exactly-cap-sized archive succeeds; cap+1 trips.
		copied, err := io.Copy(tmp, io.LimitReader(r, limit+1))
		if err != nil {
			return wrapPathError(opExtractArchive, dst, err)
		}
		if copied > limit {
			return wrapPathError(opExtractArchive, dst, ErrArchiveTooLarge)
		}
	}

	if _, err := tmp.Seek(0, io.SeekStart); err != nil {
		return wrapPathError(opExtractArchive, dst, err)
	}
	info, err := tmp.Stat()
	if err != nil {
		return wrapPathError(opExtractArchive, dst, err)
	}
	zr, err := zip.NewReader(tmp, info.Size())
	if err != nil {
		return wrapPathError(opExtractArchive, dst, err)
	}
	return extractZipReader(zr, dst, cfg)
}

func extractZipReader(zr *zip.Reader, dst string, cfg archiveExtractOptions) error {
	var used int64
	for _, file := range zr.File {
		ah := zipFileToArchiveHeader(file)
		if cfg.filter != nil && !cfg.filter(ah) {
			continue
		}

		target, terr := resolveEntryPath(dst, file.Name)
		if terr != nil {
			return wrapPathError(opExtractArchive, file.Name, terr)
		}

		if file.FileInfo().IsDir() {
			if err := os.MkdirAll(target, safeMode(file.Mode(), true, cfg.preserveMode)); err != nil {
				return wrapPathError(opExtractArchive, target, err)
			}
			continue
		}

		if err := os.MkdirAll(filepath.Dir(target), Mode0755); err != nil {
			return wrapPathError(opExtractArchive, filepath.Dir(target), err)
		}
		rc, err := file.Open()
		if err != nil {
			return wrapPathError(opExtractArchive, file.Name, err)
		}
		f, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC,
			safeMode(file.Mode(), false, cfg.preserveMode))
		if err != nil {
			_ = rc.Close()
			return wrapPathError(opExtractArchive, target, err)
		}
		//nolint:gosec // G110: limitWrap caps cumulative bytes against cfg.maxBytes (default 10 GiB)
		if _, cerr := io.Copy(limitWrap(f, &used, cfg.maxBytes), rc); cerr != nil {
			_ = f.Close()
			_ = rc.Close()
			return wrapPathError(opExtractArchive, target, cerr)
		}
		if err := rc.Close(); err != nil {
			_ = f.Close()
			return wrapPathError(opExtractArchive, file.Name, err)
		}
		if err := f.Close(); err != nil {
			return wrapPathError(opExtractArchive, target, err)
		}
	}
	return nil
}

func zipFileToArchiveHeader(f *zip.File) ArchiveHeader {
	return ArchiveHeader{
		Name:    f.Name,
		Size:    int64(f.UncompressedSize64),
		Mode:    f.Mode(),
		ModTime: f.Modified,
		IsDir:   f.FileInfo().IsDir(),
	}
}
