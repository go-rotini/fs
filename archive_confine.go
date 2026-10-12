package fs

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
)

// maxLinkHops bounds how many symlinks realPath follows, as the kernel bounds a lookup.
const maxLinkHops = 40

// extractRoot confines writes (an archive extraction, a scaffold) to their destination as the
// destination exists on disk, symlinks resolved, so a path can't reach outside it through a
// link already in the tree.
type extractRoot struct {
	op   string // the operation errors are reported under
	dir  string // the destination, absolute
	real string // the destination with every symlink resolved
}

func newExtractRoot(op, dst string) (extractRoot, error) {
	abs, err := filepath.Abs(dst)
	if err != nil {
		return extractRoot{}, wrapPathError(op, dst, err)
	}
	resolved, err := realPath(abs, 0)
	if err != nil {
		return extractRoot{}, wrapPathError(op, dst, err)
	}
	return extractRoot{op: op, dir: abs, real: resolved}, nil
}

// confine returns an error wrapping [ErrEscapesRoot] when p, followed through the symlinks
// that exist now, lies outside the destination.
func (r extractRoot) confine(p string) error {
	resolved, err := realPath(p, 0)
	if err != nil {
		return wrapPathError(r.op, p, err)
	}
	if ok, err := IsSubpath(r.real, resolved); err != nil || !ok {
		return wrapPathError(r.op, p, ErrEscapesRoot)
	}
	return nil
}

// realPath resolves the absolute path p one element at a time, following every symlink that
// exists and applying each ".." to the directory it is reached from, as the kernel does. p is
// split as written, never cleaned first: cleaning "link/.." would drop the link unfollowed.
// Elements past the first that doesn't exist are joined as written.
func realPath(p string, hops int) (string, error) {
	vol := filepath.VolumeName(p)
	cur := vol + string(filepath.Separator)
	elems := strings.FieldsFunc(p[len(vol):], func(r rune) bool { return r == '/' || r == filepath.Separator })
	for i, elem := range elems {
		switch elem {
		case ".":
			continue
		case "..":
			cur = filepath.Dir(cur)
			continue
		}
		next := filepath.Join(cur, elem)
		info, err := osLstat(next)
		if errors.Is(err, os.ErrNotExist) {
			return filepath.Join(append([]string{cur}, elems[i:]...)...), nil
		}
		if err != nil {
			return "", err
		}
		if info.Mode()&os.ModeSymlink == 0 {
			cur = next
			continue
		}
		if hops >= maxLinkHops {
			return "", ErrSymlinkLoop
		}
		target, err := osReadlink(next)
		if err != nil {
			return "", err
		}
		if !filepath.IsAbs(target) {
			target = cur + string(filepath.Separator) + target
		}
		if cur, err = realPath(target, hops+1); err != nil {
			return "", err
		}
	}
	return cur, nil
}

// madeLinks records the symlinks an extraction creates, to check again once every entry is
// written: an entry after a link can change where the link leads.
type madeLinks []string

// make creates target as a symlink to linkname, inside root as the tree stands now.
func (m *madeLinks) make(root extractRoot, target, linkname string) error {
	if err := prepareFile(root, target); err != nil {
		return err
	}
	_ = osRemove(target) //nolint:errcheck // best-effort: missing target is the common case
	if err := osSymlink(linkname, target); err != nil {
		return wrapPathError(opExtractArchive, target, err)
	}
	if err := root.confine(target); err != nil {
		_ = osRemove(target) //nolint:errcheck // best-effort: the error below is what matters
		return err
	}
	*m = append(*m, target)
	return nil
}

// check confirms every link still there leads inside root in the finished tree. A link that
// leads outside is removed, and the error wraps [ErrEscapesRoot].
func (m madeLinks) check(root extractRoot) error {
	for _, target := range m {
		if info, err := osLstat(target); err != nil || info.Mode()&os.ModeSymlink == 0 {
			continue // a later entry replaced it
		}
		if err := root.confine(target); err != nil {
			_ = osRemove(target) //nolint:errcheck // best-effort: the error below is what matters
			return err
		}
	}
	return nil
}

// replaceLink removes a symlink at target, so an entry written there replaces the link instead
// of writing through it.
func replaceLink(target string) error {
	info, err := osLstat(target)
	if err != nil || info.Mode()&os.ModeSymlink == 0 {
		return nil //nolint:nilerr // nothing there, or not a link: the write handles it
	}
	if err := osRemove(target); err != nil {
		return wrapPathError(opExtractArchive, target, err)
	}
	return nil
}

// prepareFile readies target for a file entry: its directory, checked against root and then
// created, and no symlink at target itself.
func prepareFile(root extractRoot, target string) error {
	dir := filepath.Dir(target)
	if err := root.confine(dir); err != nil {
		return err
	}
	if err := osMkdirAll(dir, Mode0755); err != nil {
		return wrapPathError(opExtractArchive, dir, err)
	}
	return replaceLink(target)
}
