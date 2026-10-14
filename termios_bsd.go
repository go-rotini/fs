//go:build darwin || dragonfly || freebsd || netbsd || openbsd

package fs

import "golang.org/x/sys/unix"

const ioctlGetTermios = unix.TIOCGETA
