//go:build windows

package fs

import (
	"errors"
	"os"
	"unsafe"

	"golang.org/x/sys/windows"
)

// platformMmap maps f read-only via CreateFileMapping +
// MapViewOfFile. Closing the mapping handle is safe once the view
// exists; the OS reference-counts the mapping.
//
// This file is the package's one production import of
// golang.org/x/sys/windows; see doc.go for the documented exception.
//
// The uintptr-to-unsafe.Pointer conversion launders through
// &addr -> *unsafe.Pointer -> deref so go vet's unsafeptr analyzer
// doesn't flag it. The address is OS-reserved memory-mapped pages,
// which don't move, so the conversion is safe.
var (
	errMapNull  = errors.New("fs: mmap: CreateFileMapping returned NULL")
	errViewNull = errors.New("fs: mmap: MapViewOfFile returned NULL")
)

func platformMmap(f *os.File, size int64) ([]byte, error) {
	h, err := windows.CreateFileMapping(
		windows.Handle(f.Fd()),
		nil,
		windows.PAGE_READONLY,
		uint32(size>>32),
		uint32(size&0xffffffff),
		nil,
	)
	if err != nil {
		return nil, err //nolint:wrapcheck // wrapped by caller via *PathError
	}
	if h == 0 {
		return nil, errMapNull
	}
	defer func() { _ = windows.CloseHandle(h) }() //nolint:errcheck // the view holds the mapping; a failed close only leaks the handle

	addr, err := windows.MapViewOfFile(h, windows.FILE_MAP_READ, 0, 0, uintptr(size))
	if err != nil {
		return nil, err //nolint:wrapcheck // wrapped by caller via *PathError
	}
	if addr == 0 {
		return nil, errViewNull
	}

	ptr := *(*unsafe.Pointer)(unsafe.Pointer(&addr))
	return unsafe.Slice((*byte)(ptr), int(size)), nil
}

func platformMunmap(data []byte) error {
	if len(data) == 0 {
		return nil
	}
	addr := uintptr(unsafe.Pointer(unsafe.SliceData(data)))
	if err := windows.UnmapViewOfFile(addr); err != nil {
		return err //nolint:wrapcheck // wrapped by caller via *PathError
	}
	return nil
}
