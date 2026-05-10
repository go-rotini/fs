//go:build darwin

package fs

import "syscall"

const ioctlGetTermios = syscall.TIOCGETA
