//go:build linux

package fs

import (
	"fmt"
	"os"
	"strings"
)

// platformProcessStartTime reads /proc/<pid>/stat and extracts field
// 22 (starttime), the jiffies-since-boot count when the process
// started.
//
// /proc/<pid>/stat format:
//
//	<pid> (<comm-with-spaces>) <state> <ppid> ... <starttime> ...
//
// `comm` is parenthesized and may contain spaces; the parser finds
// the closing `)` and splits the suffix to address fields 3..N.
func platformProcessStartTime(pid int) (string, error) {
	path := fmt.Sprintf("/proc/%d/stat", pid)
	data, err := os.ReadFile(path)
	if err != nil {
		return "", wrapPathError(opProcStartTime, path, err)
	}
	s := string(data)
	rparen := strings.LastIndexByte(s, ')')
	if rparen < 0 || rparen+2 >= len(s) {
		return "", wrapPathError(opProcStartTime, path, ErrInvalidPath)
	}
	// starttime is the 20th field after the closing `)`: state, ppid,
	// pgrp, session, tty_nr, tpgid, flags, minflt, cminflt, majflt,
	// cmajflt, utime, stime, cutime, cstime, priority, nice,
	// num_threads, itrealvalue, starttime.
	suffix := strings.Fields(s[rparen+2:])
	const starttimeIdx = 19
	if len(suffix) <= starttimeIdx {
		return "", wrapPathError(opProcStartTime, path, ErrInvalidPath)
	}
	return suffix[starttimeIdx], nil
}
