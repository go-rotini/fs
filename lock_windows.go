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
	// pass a NULL async event so this should never occur, but check
	// defensively.
	winErrIOPending = syscall.Errno(997)

	// LockFileEx locks a single sentinel byte at this offset rather
	// than the whole file. Windows byte-range locks are mandatory:
	// locking bytes 0..N would block every other handle (even within
	// the same process) from reading the lockfile's PID content. The
	// sentinel sits at 1<<62 (~4.6 EB), far beyond any realistic
	// lockfile size, so mutual exclusion still works (all holders
	// contend on the same byte) while the content area stays
	// readable; matching POSIX flock, which is advisory and never
	// blocks reads. The lock doesn't extend the file or allocate
	// space; it's purely a range reservation.
	lockSentinelOffsetLow  = uint32(0x00000000)
	lockSentinelOffsetHigh = uint32(0x40000000) // (1 << 62) >> 32
	lockSentinelLengthLow  = uint32(1)
	lockSentinelLengthHigh = uint32(0)
)

// osFlock locks the sentinel byte (see the constants above) via
// LockFileEx. Windows byte-range locks are per-handle, so closing
// the handle releases the lock; matching POSIX flock semantics from
// the caller's perspective.
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

	ol := syscall.Overlapped{
		Offset:     lockSentinelOffsetLow,
		OffsetHigh: lockSentinelOffsetHigh,
	}
	r1, _, e1 := procLockFileEx.Call(
		f.Fd(),
		uintptr(flags),
		0, // reserved
		uintptr(lockSentinelLengthLow),
		uintptr(lockSentinelLengthHigh),
		uintptr(unsafe.Pointer(&ol)),
	)
	if r1 != 0 {
		return nil
	}
	if errors.Is(e1, winErrLockViolation) || errors.Is(e1, syscall.ERROR_IO_PENDING) || errors.Is(e1, winErrIOPending) {
		// LOCKFILE_FAIL_IMMEDIATELY: the lock is held by another holder.
		return errLockBusy
	}
	return e1 //nolint:wrapcheck // wrapped by caller via *PathError
}

// unlockFile releases the lock taken by osFlock. The region must
// match exactly, so it uses the same sentinel offset/length. Closing
// the file also releases the lock; the explicit unlock surfaces the
// syscall error separately from the close error.
func unlockFile(f *os.File) error {
	ol := syscall.Overlapped{
		Offset:     lockSentinelOffsetLow,
		OffsetHigh: lockSentinelOffsetHigh,
	}
	r1, _, e1 := procUnlockFileEx.Call(
		f.Fd(),
		0, // reserved
		uintptr(lockSentinelLengthLow),
		uintptr(lockSentinelLengthHigh),
		uintptr(unsafe.Pointer(&ol)),
	)
	if r1 == 0 {
		return e1 //nolint:wrapcheck // wrapped by caller via *PathError
	}
	return nil
}

// platformPIDAlive is the Windows variant of [pidAlive]. It opens the
// process with PROCESS_QUERY_LIMITED_INFORMATION and inspects its exit
// code.
//
// When OpenProcess fails, the GetLastError code disambiguates:
// ERROR_INVALID_PARAMETER means no process owns this PID, so it's
// dead. Any other failure (notably ERROR_ACCESS_DENIED, which
// PROCESS_QUERY_LIMITED_INFORMATION can still hit for protected
// processes like anti-virus daemons) means something is running there
// that we can't probe, so it's treated as alive; the alternative
// would let stale-lock reclamation steal a lock from a live process.
func platformPIDAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	const (
		processQueryLimitedInfo = 0x1000
		stillActive             = uint32(259)       // STILL_ACTIVE
		errInvalidParameter     = syscall.Errno(87) // ERROR_INVALID_PARAMETER
	)
	procOpenProcess := kernel32.NewProc("OpenProcess")
	procGetExitCodeProcess := kernel32.NewProc("GetExitCodeProcess")
	procCloseHandle := kernel32.NewProc("CloseHandle")

	h, _, callErr := procOpenProcess.Call(uintptr(processQueryLimitedInfo), 0, uintptr(pid))
	if h == 0 {
		return !errors.Is(callErr, errInvalidParameter)
	}
	defer func() { _, _, _ = procCloseHandle.Call(h) }()

	var exitCode uint32
	r1, _, _ := procGetExitCodeProcess.Call(h, uintptr(unsafe.Pointer(&exitCode)))
	if r1 == 0 {
		return true // probe failed; conservative
	}
	return exitCode == stillActive
}
