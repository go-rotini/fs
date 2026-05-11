package fs

// ProcessStartTime returns a stable, opaque string identifying the
// start instant of the OS process with the given PID. Two processes
// that reuse the same PID produce different start-time strings, so
// comparing fingerprints against the same PID at two different
// moments reliably distinguishes the original process from a
// recycled one.
//
// Intended as the canonical argument to [WithPIDLockFingerprint]:
//
//	h, err := fs.PIDLock(path, fs.WithPIDLockFingerprint(fs.ProcessStartTime))
//
// The returned string is opaque; callers should not parse it.
//
// Platform implementations:
//
//   - Linux: parses `starttime` (field 22) from /proc/<pid>/stat.
//     The value is jiffies-since-boot.
//   - Darwin / FreeBSD: shells out to `ps -o lstart=`.
//   - Windows: GetProcessTimes via kernel32.dll for the creation
//     FILETIME (100-nanosecond ticks since 1601 UTC), rendered in
//     decimal.
//
// Returns an empty string and a non-nil error if the OS-specific
// probe fails. Callers using this with [WithPIDLockFingerprint] can
// treat an error or empty result as "no fingerprint"; the bare
// PID-alive probe still defends against the dead-PID case.
func ProcessStartTime(pid int) (string, error) {
	if pid <= 0 {
		return "", wrapPathError(opProcStartTime, "", ErrInvalidPath)
	}
	return platformProcessStartTime(pid)
}

const opProcStartTime = "process.starttime"
