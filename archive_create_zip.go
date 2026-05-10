package fs

import (
	"archive/zip"
	"io"
	stdfs "io/fs"
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
		return copyFileIntoZip(path, ww)
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

// copyFileIntoZip opens path and streams its bytes into ww. The
// open + close pair is deferred so any return path closes the
// source file exactly once.
func copyFileIntoZip(path string, ww io.Writer) error {
	f, oerr := osOpen(path)
	if oerr != nil {
		return oerr
	}
	defer f.Close()
	if _, cerr := io.Copy(ww, hookedReader{f}); cerr != nil {
		return cerr //nolint:wrapcheck // outer caller wraps
	}
	return nil
}
