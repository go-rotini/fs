package fs

import (
	"errors"
	"os"
	"sync"
)

const opMmap = "mmap"

// errMmapEmpty is wrapped into the *PathError when [Mmap] is called
// on a zero-byte file.
var errMmapEmpty = errors.New("fs: mmap: cannot map empty file")

// Mapping is a read-only memory-mapped view of a file. The backing
// file is held open for the lifetime of the Mapping; Close releases
// both the mapping and the file.
//
// Mapping is safe for concurrent reads but not for concurrent Close.
// The byte slice returned by Data must not be retained past Close.
type Mapping struct {
	path string
	f    *os.File
	data []byte
	once sync.Once
	err  error
}

// Mmap opens path and memory-maps its entire contents read-only.
//
// POSIX: mmap(2) via [syscall.Mmap] with PROT_READ | MAP_SHARED. The
// file is opened O_RDONLY.
//
// Windows: CreateFileMapping + MapViewOfFile with PAGE_READONLY |
// FILE_MAP_READ.
//
// Empty files cannot be mapped (POSIX EINVAL, Windows
// ERROR_FILE_INVALID); Mmap returns an error rather than papering
// over the platform behavior.
func Mmap(path string) (*Mapping, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, wrapPathError(opMmap, path, err)
	}
	info, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return nil, wrapPathError(opMmap, path, err)
	}
	if info.IsDir() {
		_ = f.Close()
		return nil, wrapPathError(opMmap, path, ErrIsDir)
	}
	if info.Size() == 0 {
		_ = f.Close()
		return nil, wrapPathError(opMmap, path, errMmapEmpty)
	}

	data, err := platformMmap(f, info.Size())
	if err != nil {
		_ = f.Close()
		return nil, wrapPathError(opMmap, path, err)
	}
	return &Mapping{path: path, f: f, data: data}, nil
}

// Data returns the mapped byte slice. Writes to the slice produce
// undefined behavior; the mapping is opened read-only but Go does
// not enforce that at the slice level.
func (m *Mapping) Data() []byte {
	if m == nil {
		return nil
	}
	return m.data
}

// Len returns the length of the mapped region in bytes.
func (m *Mapping) Len() int {
	if m == nil {
		return 0
	}
	return len(m.data)
}

// Close releases the mapping and the underlying file handle.
// Idempotent.
func (m *Mapping) Close() error {
	if m == nil {
		return nil
	}
	m.once.Do(func() {
		if m.data != nil {
			if err := platformMunmap(m.data); err != nil {
				m.err = wrapPathError(opMmap, m.path, err)
			}
			m.data = nil
		}
		if m.f != nil {
			if err := m.f.Close(); err != nil && m.err == nil {
				m.err = wrapPathError(opMmap, m.path, err)
			}
			m.f = nil
		}
	})
	return m.err
}
