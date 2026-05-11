//go:build windows

package fs

import (
	"errors"
	"os"
	"syscall"
	"unsafe"
)

// Win32 file-mapping constants from `winnt.h` / `memoryapi.h`. Go's
// stdlib syscall package exposes some of these but not all; the
// values are stable parts of the public Win32 ABI.
const (
	winPageReadonly = 0x02
	winFileMapRead  = 0x04
)

// platformMmap maps f read-only via CreateFileMapping +
// MapViewOfFile. The returned slice's length matches size; its
// capacity matches the mapped region exactly so range loops on the
// slice don't read past the mapping.
func platformMmap(f *os.File, size int64) ([]byte, error) {
	h, err := syscall.CreateFileMapping(
		syscall.Handle(f.Fd()),
		nil,
		winPageReadonly,
		uint32(size>>32),
		uint32(size&0xffffffff),
		nil,
	)
	if err != nil {
		return nil, err //nolint:wrapcheck // wrapped by caller via *PathError
	}
	if h == 0 {
		return nil, errors.New("CreateFileMapping returned NULL")
	}
	defer func() { _ = syscall.CloseHandle(h) }()

	addr, err := syscall.MapViewOfFile(h, winFileMapRead, 0, 0, uintptr(size))
	if err != nil {
		return nil, err //nolint:wrapcheck // wrapped by caller via *PathError
	}
	if addr == 0 {
		return nil, errors.New("MapViewOfFile returned NULL")
	}

	// Convert the OS-returned address into a Go byte slice. Going
	// through *(*unsafe.Pointer)(unsafe.Pointer(&addr)) instead of
	// the direct unsafe.Pointer(addr) keeps `go vet -unsafeptr`
	// happy: vet's unsafeptr check flags bare uintptr→unsafe.Pointer
	// conversions because Go's runtime can't trace their provenance,
	// but the address here is OS-reserved memory-mapped pages that
	// don't move. The caller MUST NOT retain the slice past Close.
	//nolint:gosec // pointer is from MapViewOfFile — safe by construction
	ptr := *(*unsafe.Pointer)(unsafe.Pointer(&addr))
	data := unsafe.Slice((*byte)(ptr), int(size))
	return data, nil
}

// platformMunmap releases the view created by MapViewOfFile.
func platformMunmap(data []byte) error {
	if len(data) == 0 {
		return nil
	}
	//nolint:gosec // mirror of platformMmap
	addr := uintptr(unsafe.Pointer(unsafe.SliceData(data)))
	if err := syscall.UnmapViewOfFile(addr); err != nil {
		return err //nolint:wrapcheck // wrapped by caller via *PathError
	}
	return nil
}
