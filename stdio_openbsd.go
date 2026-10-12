//go:build openbsd

package fs

import "syscall"

const ioctlGetTermios = syscall.TIOCGETA
