//go:build darwin || dragonfly || freebsd || netbsd || openbsd

package fs

import (
	"os/exec"
	"strconv"
	"strings"
)

// platformProcessStartTime on darwin and the BSDs shells out to `/bin/ps
// -o lstart=`. kinfo_proc layout varies across releases; hand-
// parsing the timeval offset within the struct is brittle, and the
// BSD `ps` variant ships on every supported release.
//
// Fork+exec cost (~5 ms) is acceptable; the function is called at
// most twice per [PIDLock] acquire (once on record, once on stale
// detection).
//
// ps prints lstart in the local time zone and locale, so it runs with
// TZ=UTC and LC_ALL=C: the string is the same whoever asks.
func platformProcessStartTime(pid int) (string, error) {
	cmd := exec.Command("/bin/ps", "-o", "lstart=", "-p", strconv.Itoa(pid)) //nolint:gosec // pid is integer-typed; no shell injection risk
	cmd.Env = []string{"TZ=UTC", "LC_ALL=C"}
	out, err := cmd.Output()
	if err != nil {
		return "", wrapPathError(opProcStartTime, strconv.Itoa(pid), err)
	}
	s := strings.TrimSpace(string(out))
	if s == "" {
		return "", wrapPathError(opProcStartTime, strconv.Itoa(pid), ErrNotFound)
	}
	return s, nil
}
