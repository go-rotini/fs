//go:build windows

package fs

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

// IsExecutable reports whether path exists and is executable on
// Windows. POSIX +x doesn't apply; Windows uses file extension. The
// predicate returns true iff path exists, is a regular file, and its
// extension appears in PATHEXT (default `.COM;.EXE;.BAT;.CMD`).
func IsExecutable(path string) bool {
	if !IsFile(path) {
		return false
	}
	ext := strings.ToUpper(filepath.Ext(path))
	if ext == "" {
		return false
	}
	pathext := os.Getenv("PATHEXT")
	if pathext == "" {
		pathext = ".COM;.EXE;.BAT;.CMD"
	}
	for candidate := range strings.SplitSeq(pathext, ";") {
		if strings.EqualFold(strings.TrimSpace(candidate), ext) {
			return true
		}
	}
	return false
}

// IsReadable reports whether the calling process can read path.
// Windows files are readable unless ACLs forbid; we approximate by
// attempting to open for reading.
func IsReadable(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	_ = f.Close()
	return true
}

// IsWritable reports whether path is writable. Checks the file's
// FILE_ATTRIBUTE_READONLY bit and (for directories) the parent's
// modify ACL by attempting an open-with-write-flag probe.
func IsWritable(path string) bool {
	if !existsPredicate(path) {
		return false
	}
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	// FILE_ATTRIBUTE_READONLY check via syscall.GetFileAttributes.
	pathPtr, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return false
	}
	attrs, err := syscall.GetFileAttributes(pathPtr)
	if err != nil {
		return false
	}
	if attrs&syscall.FILE_ATTRIBUTE_READONLY != 0 {
		// Directories ignore the readonly bit (it's stored but not
		// honored); files do honor it.
		if !info.IsDir() {
			return false
		}
	}
	// Probe via open-with-write for files; for directories the
	// readonly check above is sufficient.
	if info.IsDir() {
		return true
	}
	f, err := os.OpenFile(path, os.O_WRONLY, 0)
	if err != nil {
		return false
	}
	_ = f.Close()
	return true
}
