package fs

import "syscall"

// exdevError returns the platform's EXDEV (cross-device link) error.
// Used by fault-injection tests to provoke the EXDEV branch in
// [Move] without needing a real cross-mount setup.
func exdevError() error { return syscall.EXDEV }
