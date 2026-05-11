//go:build darwin || freebsd

package fs

import (
	"os/exec"
	"strconv"
	"strings"
)

// platformProcessStartTime on darwin / freebsd shells out to `ps`
// asking for the long-form start timestamp (`lstart`). The output
// is human-readable but stable for the lifetime of the process —
// a recycled PID gets a fresh `lstart` value.
//
// Why ps instead of sysctl?  kinfo_proc layout varies by darwin /
// freebsd release; hand-parsing the timeval offset within the
// struct is brittle. `ps -o lstart=` is part of POSIX's BSD ps
// variant and ships on every supported macOS / FreeBSD release.
//
// Fork+exec cost (~5 ms) is acceptable: this function is called at
// most twice per [PIDLock] acquire (once to record, once on stale-
// detection), not on a hot path.
//
// If `ps` is unavailable (e.g., a stripped container image),
// returns the syscall error wrapped in *PathError. Callers using
// this with [WithPIDLockFingerprint] can treat the error as "no
// fingerprint" — the bare PID-alive probe still defends against
// the dead-PID case.
func platformProcessStartTime(pid int) (string, error) {
	out, err := exec.Command("ps", "-o", "lstart=", "-p", strconv.Itoa(pid)).Output() //nolint:gosec // pid is integer-typed; no shell injection risk
	if err != nil {
		return "", wrapPathError(opProcStartTime, strconv.Itoa(pid), err)
	}
	s := strings.TrimSpace(string(out))
	if s == "" {
		return "", wrapPathError(opProcStartTime, strconv.Itoa(pid), ErrNotFound)
	}
	return s, nil
}
