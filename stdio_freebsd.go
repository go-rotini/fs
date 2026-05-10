//go:build freebsd

package fs

import "syscall"

const ioctlGetTermios = syscall.TIOCGETA
