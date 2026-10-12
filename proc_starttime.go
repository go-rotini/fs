package fs

// ProcessStartTime returns a stable, opaque string identifying the
// start instant of the OS process with the given PID. A process that
// later reuses the same PID gets a different string, so comparing
// the two distinguishes the original process from a recycled one
// (on macOS and the BSDs the resolution is one second).
//
// It is the usual fingerprint for [WithPIDLockFingerprint]:
//
//	h, err := fs.PIDLock(path, fs.WithPIDLockFingerprint(func(pid int) string {
//	    s, _ := fs.ProcessStartTime(pid)
//	    return s
//	}))
//
// The returned string is opaque; callers should not parse it.
//
// Platform implementations:
//
//   - Linux: parses `starttime` (field 22) from /proc/<pid>/stat.
//     The value is jiffies-since-boot.
//   - Darwin and the BSDs: shells out to `ps -o lstart=`.
//   - Windows: GetProcessTimes via kernel32.dll for the creation
//     FILETIME (100-nanosecond ticks since 1601 UTC), rendered in
//     decimal.
//
// Returns an empty string and a non-nil error if pid <= 0 (wrapping
// [ErrInvalidPath]) or the OS-specific probe fails, for example
// because the process does not exist.
func ProcessStartTime(pid int) (string, error) {
	if pid <= 0 {
		return "", wrapPathError(opProcStartTime, "", ErrInvalidPath)
	}
	return platformProcessStartTime(pid)
}

const opProcStartTime = "process.starttime"
