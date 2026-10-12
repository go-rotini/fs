//go:build netbsd

package fs

import "syscall"

const ioctlGetTermios = syscall.TIOCGETA
