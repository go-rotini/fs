package fs

import (
	"archive/tar"
	"errors"
	"io"
	"os"
	"path/filepath"
)

// extractTar reads a tar stream from r and writes entries under root. Every entry's name and
// link target must stay inside root: by name, and as the path resolves on disk through the
// symlinks there at that moment, the archive's own included. Once every entry is written, each
// symlink the archive made is checked again, since a later entry can change where it leads.
func extractTar(r io.Reader, root extractRoot, cfg archiveExtractOptions) error {
	dst := root.dir
	tr := tar.NewReader(r)
	var used int64
	var links madeLinks

	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return links.check(root)
		}
		if err != nil {
			return wrapPathError(opExtractArchive, dst, err)
		}
		if cfg.filter != nil && !cfg.filter(tarHeaderToArchiveHeader(hdr)) {
			continue
		}
		if err := extractTarEntry(hdr, tr, root, &links, cfg, &used); err != nil {
			return err
		}
	}
}

// extractTarEntry handles one tar entry. Split out from extractTar
// to keep cognitive complexity manageable.
func extractTarEntry(hdr *tar.Header, tr *tar.Reader, root extractRoot, links *madeLinks, cfg archiveExtractOptions, used *int64) error {
	target, terr := resolveEntryPath(root.dir, hdr.Name)
	if terr != nil {
		return wrapPathError(opExtractArchive, hdr.Name, terr)
	}

	switch hdr.Typeflag {
	case tar.TypeDir:
		if err := root.confine(target); err != nil {
			return err
		}
		if err := osMkdirAll(target, safeMode(os.FileMode(hdr.Mode), true, cfg.preserveMode)); err != nil {
			return wrapPathError(opExtractArchive, target, err)
		}
	case tar.TypeReg:
		return extractTarRegular(hdr, tr, root, target, cfg, used)
	case tar.TypeSymlink:
		if err := validateTarSymlinkTarget(root.dir, target, hdr.Linkname); err != nil {
			return wrapPathError(opExtractArchive, target, err)
		}
		return links.make(root, target, hdr.Linkname)
	case tar.TypeLink:
		return extractTarHardlink(hdr, root, target)
	default:
		// Skip device nodes, FIFOs, etc.; not safe to recreate
		// from untrusted archives.
	}
	return nil
}

func extractTarRegular(hdr *tar.Header, tr *tar.Reader, root extractRoot, target string, cfg archiveExtractOptions, used *int64) error {
	if err := prepareFile(root, target); err != nil {
		return err
	}
	f, err := osOpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC,
		safeMode(os.FileMode(hdr.Mode), false, cfg.preserveMode))
	if err != nil {
		return wrapPathError(opExtractArchive, target, err)
	}
	if _, cerr := io.Copy(limitWrap(f, used, cfg.maxBytes), tr); cerr != nil {
		closeQuietly(f)
		if errors.Is(cerr, ErrArchiveTooLarge) {
			return wrapPathError(opExtractArchive, target, ErrArchiveTooLarge)
		}
		return wrapPathError(opExtractArchive, target, cerr)
	}
	if err := fileClose(f); err != nil {
		return wrapPathError(opExtractArchive, target, err)
	}
	return nil
}

// extractTarHardlink links target to an entry already extracted. Both paths must resolve
// inside root, so a link can't reach a file outside it through a symlink already in the tree.
func extractTarHardlink(hdr *tar.Header, root extractRoot, target string) error {
	linkTarget, lerr := resolveEntryPath(root.dir, hdr.Linkname)
	if lerr != nil {
		return wrapPathError(opExtractArchive, hdr.Linkname, lerr)
	}
	if err := root.confine(linkTarget); err != nil {
		return err
	}
	if err := prepareFile(root, target); err != nil {
		return err
	}
	_ = osRemove(target) //nolint:errcheck // best-effort: missing target is the common case
	if err := osLink(linkTarget, target); err != nil {
		return wrapPathError(opExtractArchive, target, err)
	}
	return nil
}

// validateTarSymlinkTarget verifies that a symlink pointing at
// `linkname` from `linkPath` (already validated as inside dst)
// would not point outside dst once resolved relative to its own
// directory.
func validateTarSymlinkTarget(dst, linkPath, linkname string) error {
	// linkname may be relative or absolute. Resolve it against
	// linkPath's directory and ensure the resulting path is still
	// under dst.
	var resolved string
	if filepath.IsAbs(linkname) {
		// Absolute symlink targets are typically dangerous; require
		// them to fall inside dst.
		resolved = filepath.Clean(linkname)
	} else {
		resolved = filepath.Clean(filepath.Join(filepath.Dir(linkPath), linkname))
	}
	if err := MustBeChildOf(dst, resolved); err != nil {
		return ErrEscapesRoot
	}
	return nil
}

// tarHeaderToArchiveHeader normalizes a tar header into the
// package's [ArchiveHeader] for filter callbacks.
func tarHeaderToArchiveHeader(hdr *tar.Header) ArchiveHeader {
	return ArchiveHeader{
		Name:       hdr.Name,
		Size:       hdr.Size,
		Mode:       os.FileMode(hdr.Mode),
		ModTime:    hdr.ModTime,
		IsDir:      hdr.Typeflag == tar.TypeDir,
		LinkTarget: hdr.Linkname,
	}
}
