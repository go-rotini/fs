//go:build !unix && !windows

package fs

import "os"

// Advisory locks are not available on this platform: every lock
// operation fails with [ErrNotSupported].

func osFlock(*os.File, lockKind) error { return ErrNotSupported }

func unlockFile(*os.File) error { return nil }

func lockFile(*os.File) error { return ErrNotSupported }

// platformPIDAlive reports false: processes can't be probed here.
func platformPIDAlive(int) bool { return false }
