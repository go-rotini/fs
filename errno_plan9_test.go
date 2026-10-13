package fs

import "syscall"

// errTestELOOP and errTestEXDEV are how Plan 9 reports too many links and a
// cross-device rename: as strings.
var (
	errTestELOOP error = syscall.ErrorString("too many links")
	errTestEXDEV error = syscall.ErrorString("cross-device link")
)
