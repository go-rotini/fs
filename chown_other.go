//go:build !unix

package fs

func chownRecursive(root string, _, _ int) error {
	return wrapPathError(opChown, root, ErrNotSupported)
}
