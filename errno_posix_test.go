//go:build !plan9

package fs

import "syscall"

// errTestELOOP and errTestEXDEV are the platform's too-many-links and
// cross-device errors.
var (
	errTestELOOP error = syscall.ELOOP
	errTestEXDEV error = syscall.EXDEV
)
