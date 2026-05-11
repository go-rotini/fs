//go:build linux

package fs

import (
	"fmt"
	"os"
	"strings"
)

// platformProcessStartTime reads /proc/<pid>/stat and extracts field
// 22 (`starttime`), the jiffies-since-boot count when the process
// started. Combined with the kernel boot ID this uniquely identifies
// a process across PID reuse.
//
// /proc/<pid>/stat format:
//
//	<pid> (<comm-with-spaces>) <state> <ppid> ... <starttime> ...
//
// `comm` is parenthesized and may contain spaces, so we find the
// closing `)` and split the suffix to address fields 3..N reliably.
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
	// Fields after `comm` are space-separated. starttime is the 22nd
	// field overall; that's the 20th field after `)` (state, ppid,
	// pgrp, session, tty_nr, tpgid, flags, minflt, cminflt, majflt,
	// cmajflt, utime, stime, cutime, cstime, priority, nice,
	// num_threads, itrealvalue, starttime).
	suffix := strings.Fields(s[rparen+2:])
	const starttimeIdx = 19 // 0-indexed within the after-`)` slice
	if len(suffix) <= starttimeIdx {
		return "", wrapPathError(opProcStartTime, path, ErrInvalidPath)
	}
	return suffix[starttimeIdx], nil
}
