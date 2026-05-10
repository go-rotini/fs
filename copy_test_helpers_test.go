package fs

import "syscall"

// stubEXDEV returns syscall.EXDEV. Wrapped behind a helper so tests
// don't reach into syscall directly and so the test code remains
// readable on platforms where the value differs (POSIX 18 vs Windows
// 0x11).
func stubEXDEV() error { return syscall.EXDEV }
