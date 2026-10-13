//go:build js || wasip1

package fs

import (
	"errors"
	"syscall"
)

// isBrokenPipeErr reports whether err is EPIPE.
func isBrokenPipeErr(err error) bool {
	return errors.Is(err, syscall.EPIPE)
}
