package fs

import (
	"archive/zip"
	"io"
	stdfs "io/fs"
	"os"
	"path/filepath"
)

// createZip walks root and writes its contents as a zip to w.
// Directories are recorded as zero-byte entries with trailing slash;
// regular files have their contents copied; symlinks are skipped
// (zip has no native symlink type).
func createZip(w io.Writer, root string, cfg archiveCreateOptions) error {
	zw := zip.NewWriter(w)

	walkErr := filepath.WalkDir(root, func(path string, d stdfs.DirEntry, werr error) error {
		if werr != nil {
			return werr
		}
		info, ierr := d.Info()
		if ierr != nil {
			return ierr //nolint:wrapcheck // outer caller wraps
		}
		if cfg.filter != nil && !cfg.filter(path, info) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		rel, rerr := filepath.Rel(root, path)
		if rerr != nil {
			return rerr //nolint:wrapcheck // outer caller wraps
		}
		if rel == "." {
			return nil
		}

		// Skip non-regular, non-dir entries (symlinks, devices).
		if !d.IsDir() && !info.Mode().IsRegular() {
			return nil
		}

		hdr, herr := zip.FileInfoHeader(info)
		if herr != nil {
			return herr //nolint:wrapcheck // outer caller wraps
		}
		hdr.Name = filepath.ToSlash(rel)
		if d.IsDir() {
			hdr.Name += "/"
		}
		hdr.Method = zip.Deflate

		ww, err := zw.CreateHeader(hdr)
		if err != nil {
			return err //nolint:wrapcheck // outer caller wraps
		}
		if d.IsDir() {
			return nil
		}

		//nolint:gosec // G304/G122: zip creation is run on a trusted source tree by the caller
		f, oerr := os.Open(path)
		if oerr != nil {
			return oerr //nolint:wrapcheck // outer caller wraps
		}
		if _, cerr := io.Copy(ww, f); cerr != nil {
			_ = f.Close()
			return cerr //nolint:wrapcheck // outer caller wraps
		}
		return f.Close()
	})

	if walkErr != nil {
		_ = zw.Close()
		return wrapPathError(opCreateArchive, root, walkErr)
	}
	if err := zw.Close(); err != nil {
		return wrapPathError(opCreateArchive, root, err)
	}
	return nil
}
