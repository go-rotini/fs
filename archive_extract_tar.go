package fs

import (
	"archive/tar"
	"errors"
	"io"
	"os"
	"path/filepath"
)

// extractTar reads a tar stream from r and writes entries under
// dst. Every entry's name and (for symlinks) link target are
// resolved through MustBeChildOf(dst, ...) before any filesystem
// write — this defends against tar-slip attacks where a crafted
// entry escapes the extraction root via `..` components.
func extractTar(r io.Reader, dst string, cfg archiveExtractOptions) error {
	tr := tar.NewReader(r)
	var used int64

	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return wrapPathError(opExtractArchive, dst, err)
		}
		if cfg.filter != nil && !cfg.filter(tarHeaderToArchiveHeader(hdr)) {
			continue
		}
		if err := extractTarEntry(hdr, tr, dst, cfg, &used); err != nil {
			return err
		}
	}
}

// extractTarEntry handles one tar entry. Split out from extractTar
// to keep cognitive complexity manageable.
func extractTarEntry(hdr *tar.Header, tr *tar.Reader, dst string, cfg archiveExtractOptions, used *int64) error {
	target, terr := resolveEntryPath(dst, hdr.Name)
	if terr != nil {
		return wrapPathError(opExtractArchive, hdr.Name, terr)
	}

	switch hdr.Typeflag {
	case tar.TypeDir:
		if err := osMkdirAll(target, safeMode(os.FileMode(hdr.Mode), true, cfg.preserveMode)); err != nil {
			return wrapPathError(opExtractArchive, target, err)
		}
	case tar.TypeReg:
		return extractTarRegular(hdr, tr, target, cfg, used)
	case tar.TypeSymlink:
		return extractTarSymlink(hdr, dst, target)
	case tar.TypeLink:
		return extractTarHardlink(hdr, dst, target)
	default:
		// Skip device nodes, FIFOs, etc. — not safe to recreate
		// from untrusted archives.
	}
	return nil
}

func extractTarRegular(hdr *tar.Header, tr *tar.Reader, target string, cfg archiveExtractOptions, used *int64) error {
	if err := osMkdirAll(filepath.Dir(target), Mode0755); err != nil {
		return wrapPathError(opExtractArchive, filepath.Dir(target), err)
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

func extractTarSymlink(hdr *tar.Header, dst, target string) error {
	if err := validateTarSymlinkTarget(dst, target, hdr.Linkname); err != nil {
		return wrapPathError(opExtractArchive, target, err)
	}
	if err := osMkdirAll(filepath.Dir(target), Mode0755); err != nil {
		return wrapPathError(opExtractArchive, filepath.Dir(target), err)
	}
	_ = osRemove(target) //nolint:errcheck // best-effort: missing target is the common case
	if err := osSymlink(hdr.Linkname, target); err != nil {
		return wrapPathError(opExtractArchive, target, err)
	}
	return nil
}

func extractTarHardlink(hdr *tar.Header, dst, target string) error {
	linkTarget, lerr := resolveEntryPath(dst, hdr.Linkname)
	if lerr != nil {
		return wrapPathError(opExtractArchive, hdr.Linkname, lerr)
	}
	if err := osMkdirAll(filepath.Dir(target), Mode0755); err != nil {
		return wrapPathError(opExtractArchive, filepath.Dir(target), err)
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
