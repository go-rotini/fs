package fs

import (
	"crypto/sha256"
	"encoding/hex"
	stdfs "io/fs"
	"os"
	"path/filepath"
	"sort"
)

// ScaffoldExtract copies src into dst verbatim, without templating.
// Used for "extract default resources on first run" workflows.
//
// On every call it computes a SHA-256 over src's names and contents
// and compares it with the marker file `<dst>/<versionMarker>`
// (default `.scaffold-version`; see [WithScaffoldVersionMarker]):
//
//   - Same hash: nothing is done.
//   - Different hash, or no marker: src is extracted and the marker
//     is written.
//
// The conflict policy still applies: by default existing files are
// kept, even when the source has changed. Use [ScaffoldOverwriteAll]
// to replace them.
func ScaffoldExtract(src stdfs.FS, dst string, opts ...ScaffoldOption) error {
	cfg := newScaffoldOptions(opts)

	hash, err := scaffoldHashFS(src)
	if err != nil {
		return wrapPathError(opScaffoldExtract, dst, err)
	}

	markerPath := filepath.Join(dst, cfg.versionMarker)
	if existing, rerr := os.ReadFile(markerPath); rerr == nil && string(existing) == hash {
		return nil // version unchanged
	}

	if err := scaffoldCopy(src, dst, cfg); err != nil {
		return err
	}

	// Write the marker. Use 0o644 + atomic write so an interrupted
	// extract doesn't leave a half-written marker.
	if err := WriteFile(markerPath, []byte(hash)); err != nil {
		return wrapPathError(opScaffoldExtract, markerPath, err)
	}
	return nil
}

// scaffoldHashFS produces a deterministic content-hash of src by
// hashing each entry's path (sorted) and contents.
func scaffoldHashFS(src stdfs.FS) (string, error) {
	var paths []string
	if err := stdfs.WalkDir(src, ".", func(path string, d stdfs.DirEntry, werr error) error {
		if werr != nil {
			return werr
		}
		if path == "." {
			return nil
		}
		paths = append(paths, path)
		return nil
	}); err != nil {
		return "", err //nolint:wrapcheck // outer caller wraps via *PathError
	}
	sort.Strings(paths)

	h := sha256.New()
	for _, p := range paths {
		_, _ = h.Write([]byte(p))
		_, _ = h.Write([]byte{0})
		info, err := stdfs.Stat(src, p)
		if err != nil {
			return "", err //nolint:wrapcheck // outer caller wraps via *PathError
		}
		if info.IsDir() {
			_, _ = h.Write([]byte("DIR\x00"))
			continue
		}
		data, err := stdfs.ReadFile(src, p)
		if err != nil {
			return "", err //nolint:wrapcheck // outer caller wraps via *PathError
		}
		_, _ = h.Write(data)
		_, _ = h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// scaffoldCopy writes src verbatim under dst, honoring the
// conflict policy. No template rendering; identical contents to the
// embedded FS.
func scaffoldCopy(src stdfs.FS, dst string, cfg scaffoldOptions) error {
	//nolint:wrapcheck // per-entry errors are already wrapped via wrapPathError below
	return stdfs.WalkDir(src, ".", func(path string, d stdfs.DirEntry, werr error) error {
		if werr != nil {
			return werr
		}
		if path == "." {
			return nil
		}
		dstPath := filepath.Join(dst, filepath.FromSlash(path))

		if d.IsDir() {
			if err := osMkdirAll(dstPath, Mode0755); err != nil {
				return wrapPathError(opScaffoldExtract, dstPath, err)
			}
			return nil
		}

		// Apply conflict policy for existing files.
		if Exists(dstPath) {
			switch cfg.onConflict {
			case ScaffoldSkipExisting:
				return nil
			case ScaffoldPromptInteractive:
				if cfg.promptFunc == nil {
					return wrapPathError(opScaffoldExtract, dstPath, ErrScaffoldPromptRequired)
				}
				op := cfg.promptFunc(dstPath, ScaffoldAction{
					SrcPath: path, DstPath: dstPath, Op: ScaffoldActionConflict, Reason: "exists",
				})
				if op == ScaffoldActionSkip {
					return nil
				}
			case ScaffoldOverwriteAll:
				// fall through to write
			}
		}

		if err := osMkdirAll(filepath.Dir(dstPath), Mode0755); err != nil {
			return wrapPathError(opScaffoldExtract, dstPath, err)
		}
		data, err := stdfs.ReadFile(src, path)
		if err != nil {
			return wrapPathError(opScaffoldExtract, path, err)
		}
		if err := WriteFile(dstPath, data); err != nil {
			return err
		}
		return nil
	})
}
