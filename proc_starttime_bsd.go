//go:build darwin || freebsd

package fs

import (
	"os/exec"
	"strconv"
	"strings"
)

// platformProcessStartTime on darwin and freebsd shells out to `ps
// -o lstart=`. kinfo_proc layout varies across releases; hand-
// parsing the timeval offset within the struct is brittle, and the
// BSD `ps` variant ships on every supported release.
//
// Fork+exec cost (~5 ms) is acceptable; the function is called at
// most twice per [PIDLock] acquire (once on record, once on stale
// detection).
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
