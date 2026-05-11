package fs

// ProcessStartTime returns a stable, opaque string identifying the
// start instant of the OS process with the given PID. Two processes
// that reuse the same PID (the kernel allocates a recycled PID for
// a new process after the original exits) produce different start-
// time strings — so comparing two start-time fingerprints against
// the same PID at two different moments reliably distinguishes the
// original process from a recycled imposter.
//
// Intended as the canonical argument to [WithPIDLockFingerprint]:
//
//	h, err := fs.PIDLock(path, fs.WithPIDLockFingerprint(fs.ProcessStartTime))
//
// The returned string is opaque; callers should not parse it. The
// only guarantee is "same process = same string; different process
// = different string."
//
// Platform implementations:
//
//   - Linux: parses `starttime` (field 22) from `/proc/<pid>/stat`.
//     The value is jiffies-since-boot; combined with the PID it
//     uniquely identifies the process across PID recycles within a
//     single boot.
//   - Darwin / FreeBSD: queries `kern.proc.pid.<pid>` via
//     [syscall.Sysctl] to obtain `kinfo_proc.ki_start` /
//     `kp_proc.p_starttime`, a `timeval` rendered as
//     `<sec>.<usec>`.
//   - Windows: calls `GetProcessTimes` via `kernel32.dll` for the
//     creation FILETIME (100-nanosecond ticks since 1601 UTC),
//     rendered in decimal.
//
// Returns an empty string and a non-nil error if the OS-specific
// probe fails (process not found, permission denied, syscall
// unavailable). Callers using this with [WithPIDLockFingerprint]
// can treat an error / empty result as "no fingerprint" — the bare
// PID-alive probe still defends against the dead-PID case.
func ProcessStartTime(pid int) (string, error) {
	if pid <= 0 {
		return "", wrapPathError(opProcStartTime, "", ErrInvalidPath)
	}
	return platformProcessStartTime(pid)
}

const opProcStartTime = "process.starttime"
