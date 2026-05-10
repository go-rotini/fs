//go:build linux

package fs

import "syscall"

const ioctlGetTermios = syscall.TCGETS
