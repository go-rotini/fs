//go:build windows

package fs

import (
	"errors"
	"fmt"
	"strconv"
	"syscall"
	"unsafe"
)

// platformProcessStartTime on Windows uses OpenProcess +
// GetProcessTimes to fetch the creation FILETIME (100-nanosecond
// ticks since 1601-01-01 UTC), rendered in decimal.
//
// Requires PROCESS_QUERY_LIMITED_INFORMATION on the target process.
// Protected processes (some anti-virus and kernel services) deny
// access; the function returns an error in that case.
func platformProcessStartTime(pid int) (string, error) {
	const processQueryLimitedInfo = 0x1000
	procOpenProcess := kernel32.NewProc("OpenProcess")
	procGetProcessTimes := kernel32.NewProc("GetProcessTimes")
	procCloseHandle := kernel32.NewProc("CloseHandle")

	h, _, callErr := procOpenProcess.Call(uintptr(processQueryLimitedInfo), 0, uintptr(pid))
	if h == 0 {
		return "", wrapPathError(opProcStartTime, strconv.Itoa(pid), callErr)
	}
	defer func() { _, _, _ = procCloseHandle.Call(h) }()

	var (
		creation, exit, kernel, user syscall.Filetime
	)
	r1, _, gptErr := procGetProcessTimes.Call(
		h,
		uintptr(unsafe.Pointer(&creation)),
		uintptr(unsafe.Pointer(&exit)),
		uintptr(unsafe.Pointer(&kernel)),
		uintptr(unsafe.Pointer(&user)),
	)
	if r1 == 0 {
		return "", wrapPathError(opProcStartTime, strconv.Itoa(pid), gptErr)
	}

	// Combine the two 32-bit halves into a single 64-bit tick count.
	ticks := (uint64(creation.HighDateTime) << 32) | uint64(creation.LowDateTime)
	if ticks == 0 {
		return "", wrapPathError(opProcStartTime, strconv.Itoa(pid), errors.New("GetProcessTimes returned zero creation time"))
	}
	return fmt.Sprintf("%d", ticks), nil
}
