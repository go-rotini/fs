//go:build aix || linux || solaris

package fs

import "golang.org/x/sys/unix"

const ioctlGetTermios = unix.TCGETS
