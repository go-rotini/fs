//go:build !unix && !windows

package fs

import "os"

// Memory mapping is not available on this platform.

func platformMmap(*os.File, int64) ([]byte, error) { return nil, ErrNotSupported }

func platformMunmap([]byte) error { return nil }
