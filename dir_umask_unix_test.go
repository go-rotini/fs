//go:build !windows

package fs

import "syscall"

func syscallUmask(mask int) int { return syscall.Umask(mask) }
