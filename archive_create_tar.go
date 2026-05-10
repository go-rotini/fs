package fs

import (
	"archive/tar"
	"compress/gzip"
	"io"
	stdfs "io/fs"
	"os"
	"path/filepath"
)

// createTar walks root and writes its contents as a tar (or
// gzip-wrapped tar if useGzip) to w. Symlinks are recorded with
// their target. Modes are preserved from the source filesystem.
func createTar(w io.Writer, root string, cfg archiveCreateOptions, useGzip bool) error {
	out := w
	var gz *gzip.Writer
	if useGzip {
		gz = gzip.NewWriter(w)
		out = gz
	}
	tw := tar.NewWriter(out)

	walkErr := filepath.WalkDir(root, func(path string, d stdfs.DirEntry, werr error) error {
		return tarWalkHandler(root, path, d, werr, tw, cfg)
	})

	if walkErr != nil {
		_ = tw.Close()
		if gz != nil {
			_ = gz.Close()
		}
		return wrapPathError(opCreateArchive, root, walkErr)
	}
	if err := tw.Close(); err != nil {
		if gz != nil {
			_ = gz.Close()
		}
		return wrapPathError(opCreateArchive, root, err)
	}
	if gz != nil {
		if err := gz.Close(); err != nil {
			return wrapPathError(opCreateArchive, root, err)
		}
	}
	return nil
}

// tarWalkHandler is the per-entry callback used during tar creation.
// Split out from createTar to keep cognitive complexity manageable.
func tarWalkHandler(root, path string, d stdfs.DirEntry, werr error, tw *tar.Writer, cfg archiveCreateOptions) error {
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

	var linkTarget string
	if info.Mode()&os.ModeSymlink != 0 {
		lt, lerr := osReadlink(path)
		if lerr != nil {
			return lerr
		}
		linkTarget = lt
	}

	hdr, herr := tar.FileInfoHeader(info, linkTarget)
	if herr != nil {
		return herr //nolint:wrapcheck // outer caller wraps
	}
	hdr.Name = filepath.ToSlash(rel)
	if d.IsDir() {
		hdr.Name += "/"
	}
	if err := tw.WriteHeader(hdr); err != nil {
		return err //nolint:wrapcheck // outer caller wraps
	}

	if !info.Mode().IsRegular() {
		return nil
	}
	return copyFileIntoTar(path, tw)
}

// copyFileIntoTar opens path and streams its bytes to the tar
// writer's current entry.
func copyFileIntoTar(path string, tw *tar.Writer) error {
	f, oerr := osOpen(path)
	if oerr != nil {
		return oerr
	}
	defer f.Close()
	if _, cerr := io.Copy(tw, hookedReader{f}); cerr != nil {
		return cerr //nolint:wrapcheck // outer caller wraps
	}
	return nil
}
