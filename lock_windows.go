//go:build windows

package fs

import (
	"errors"
	"os"
	"syscall"
	"unsafe"
)

var (
	procLockFileEx   = kernel32.NewProc("LockFileEx")
	procUnlockFileEx = kernel32.NewProc("UnlockFileEx")
)

// Win32 LockFileEx flag constants (from `winbase.h`). Go's stdlib
// [syscall] package on Windows does not expose these; the constants
// are stable parts of the public Win32 ABI.
const (
	winLockfileFailImmediately = 0x00000001 // LOCKFILE_FAIL_IMMEDIATELY
	winLockfileExclusiveLock   = 0x00000002 // LOCKFILE_EXCLUSIVE_LOCK
	// errLockViolation is the GetLastError code returned by LockFileEx
	// when LOCKFILE_FAIL_IMMEDIATELY was set and the region is held by
	// another holder.
	winErrLockViolation = syscall.Errno(33)
	// errIOPending is returned for asynchronous LockFileEx calls; we
	// pass a NULL OVERLAPPED so this should never occur, but check
	// defensively.
	winErrIOPending = syscall.Errno(997)
)

// osFlock locks the entire file via LockFileEx. Windows file locks
// are mandatory (the OS enforces them) and per-handle, so closing
// the handle releases the lock; matching POSIX flock semantics from
// the caller's perspective.
//
// We lock the maximum possible range (0..0xFFFFFFFFFFFFFFFF) so any
// future read/write through the handle is covered. The OVERLAPPED
// argument is required by LockFileEx but we zero it (no offset, no
// async event).
func osFlock(f *os.File, kind lockKind) error {
	var flags uint32
	switch kind {
	case lockKindExclusive:
		flags = winLockfileExclusiveLock
	case lockKindExclusiveNoBlock:
		flags = winLockfileExclusiveLock | winLockfileFailImmediately
	case lockKindShared:
		flags = 0 // LockFileEx default = shared
	default:
		return errInvalidLockKind
	}

	var ol syscall.Overlapped
	const (
		lockLow  = uint32(0xFFFFFFFF)
		lockHigh = uint32(0xFFFFFFFF)
	)

	r1, _, e1 := procLockFileEx.Call(
		f.Fd(),
		uintptr(flags),
		0, // reserved
		uintptr(lockLow),
		uintptr(lockHigh),
		uintptr(unsafe.Pointer(&ol)),
	)
	if r1 != 0 {
		return nil
	}
	if errors.Is(e1, winErrLockViolation) || errors.Is(e1, syscall.ERROR_IO_PENDING) || errors.Is(e1, winErrIOPending) {
		// LOCKFILE_FAIL_IMMEDIATELY to lock is held.
		return errLockBusy
	}
	return e1 //nolint:wrapcheck // wrapped by caller via *PathError
}

// unlockFile releases the lock taken by osFlock. Closing the file
// also releases the lock; the explicit unlock surfaces the syscall
// error separately from the close error.
func unlockFile(f *os.File) error {
	var ol syscall.Overlapped
	const (
		lockLow  = uint32(0xFFFFFFFF)
		lockHigh = uint32(0xFFFFFFFF)
	)
	r1, _, e1 := procUnlockFileEx.Call(
		f.Fd(),
		0, // reserved
		uintptr(lockLow),
		uintptr(lockHigh),
		uintptr(unsafe.Pointer(&ol)),
	)
	if r1 == 0 {
		return e1 //nolint:wrapcheck // wrapped by caller via *PathError
	}
	return nil
}

// platformPIDAlive is the Windows variant of [pidAlive]. On Windows,
// [os.FindProcess] calls OpenProcess(PROCESS_QUERY_INFORMATION) and
// returns an error when the PID is invalid or the process has exited.
//
// PROCESS_QUERY_INFORMATION is denied for protected processes
// (anti-virus, some kernel services). We treat those as alive: a
// SECURITY-impossible-to-probe PID has SOMETHING running at it, even
// if we can't tell what. The alternative (treating denial as "dead")
// would let stale-lock reclamation steal a lock from a still-running
// antivirus daemon.
func platformPIDAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	const (
		processQueryLimitedInfo = 0x1000
		stillActive             = uint32(259) // STILL_ACTIVE
	)
	procOpenProcess := kernel32.NewProc("OpenProcess")
	procGetExitCodeProcess := kernel32.NewProc("GetExitCodeProcess")
	procCloseHandle := kernel32.NewProc("CloseHandle")

	h, _, _ := procOpenProcess.Call(uintptr(processQueryLimitedInfo), 0, uintptr(pid))
	if h == 0 {
		// PID not found or access denied. We can't open the handle,
		// so we conservatively treat it as alive (see comment above).
		// The exception is when the PID is so high that it's
		// unreasonable; but we can't tell that without enumerating
		// all PIDs, so we accept the false-positive.
		return true
	}
	defer func() { _, _, _ = procCloseHandle.Call(h) }()

	var exitCode uint32
	r1, _, _ := procGetExitCodeProcess.Call(h, uintptr(unsafe.Pointer(&exitCode)))
	if r1 == 0 {
		return true // probe failed; conservative
	}
	return exitCode == stillActive
}
