package fs

import (
	"errors"
	"os"
)

// EnsureFile ensures path exists, writing defaults if it doesn't.
// Returns created=true when the file was created by this call,
// false when it already existed.
//
// Concurrent callers race-safely: the underlying [WriteFileExclusive]
// uses `O_EXCL`, so exactly one caller writes the defaults; the
// rest observe `created=false`. The first caller's content wins;
// later callers do not overwrite.
//
// opts are passed to [WriteFileExclusive]: for example [WithMkdirAll]
// creates parents and [WithPerm] sets the file mode.
func EnsureFile(path string, defaults []byte, opts ...WriteOption) (created bool, err error) {
	werr := WriteFileExclusive(path, defaults, opts...)
	if werr == nil {
		return true, nil
	}
	if errors.Is(werr, ErrAlreadyExists) {
		return false, nil
	}
	return false, werr
}

// EnsureDir ensures path exists as a directory, creating it (and
// any missing parents) when absent. perm is applied to newly-created
// components; pass [WithEnforcePerm] to chmod the chain. Returns
// created=true when the directory did not exist at call time.
//
// Concurrent calls on a missing path all succeed, but more than one
// may report created=true. For "exactly one creator wins", use
// [Mkdir] and check for [ErrAlreadyExists].
func EnsureDir(path string, perm os.FileMode, opts ...MkdirOption) (created bool, err error) {
	if IsDir(path) {
		return false, nil
	}
	if merr := MkdirAll(path, perm, opts...); merr != nil {
		return false, merr
	}
	return true, nil
}
