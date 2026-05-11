//go:build !windows

package fs

import (
	"os"
	"syscall"
)

// platformMmap maps the first size bytes of f into the process
// address space as a read-only PROT_READ+MAP_SHARED region.
func platformMmap(f *os.File, size int64) ([]byte, error) {
	if size > int64(^uint(0)>>1) {
		// Size doesn't fit in a Go int — refusing to attempt the
		// syscall rather than truncate silently.
		return nil, syscall.EINVAL
	}
	data, err := syscall.Mmap(int(f.Fd()), 0, int(size), syscall.PROT_READ, syscall.MAP_SHARED)
	if err != nil {
		return nil, err //nolint:wrapcheck // wrapped by caller via *PathError
	}
	return data, nil
}

// platformMunmap releases the mapping returned by platformMmap.
func platformMunmap(data []byte) error {
	if len(data) == 0 {
		return nil
	}
	if err := syscall.Munmap(data); err != nil {
		return err //nolint:wrapcheck // wrapped by caller via *PathError
	}
	return nil
}
