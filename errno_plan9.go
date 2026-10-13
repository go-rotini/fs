package fs

import "strings"

// Plan 9 reports errors as strings rather than numbers.

func isNotEmptyErrno(err error) bool { return errorTextContains(err, "not empty") }

func isCrossDeviceErrno(err error) bool { return errorTextContains(err, "cross-device") }

func isLoopErrno(error) bool { return false }

// isBrokenPipeErr reports whether err is a write to a closed pipe.
func isBrokenPipeErr(err error) bool { return errorTextContains(err, "hungup") }

func errorTextContains(err error, s string) bool {
	return err != nil && strings.Contains(err.Error(), s)
}
