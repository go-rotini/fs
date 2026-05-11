package fs

import (
	"errors"
	"os"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

const (
	opLock    = "lock"
	opUnlock  = "unlock"
	opPIDLock = "pidlock"

	// lockPollInterval is the wait between TryLock attempts inside
	// LockTimeout. 50 ms is short enough to keep blocked-wait latency
	// under 100 ms in practice while imposing essentially no CPU cost.
	lockPollInterval = 50 * time.Millisecond

	// lockFileMode is the perm bits for newly-created lockfiles.
	// 0o644 — same as WriteFile's default — so other users on the
	// machine can probe state without elevated privileges.
	lockFileMode os.FileMode = 0o644
)

// Sentinel errors specific to the lock surface.
var (
	// ErrLockTimeout is returned by [LockTimeout] when the requested
	// duration elapses before the lock could be acquired.
	ErrLockTimeout = errors.New("fs: lock: timeout exceeded")

	// ErrStaleLock is returned by [PIDLock] when the on-disk PID file
	// references a dead or recycled process and the lock is reclaimed
	// on behalf of the caller. The returned [*LockHandle] is valid;
	// the error is informational and can be checked via [errors.Is].
	ErrStaleLock = errors.New("fs: lock: stale lock reclaimed")
)

// LockHandle represents an acquired advisory lock on a path. Release
// the lock by calling [LockHandle.Release]; the call is idempotent
// (subsequent calls return nil).
//
// LockHandle is not safe for concurrent use across goroutines beyond
// the Release/PID accessors; one goroutine acquires, that goroutine
// (or the caller's chosen owner) releases. The underlying file
// descriptor is held exclusively by the handle and is closed by
// Release.
type LockHandle struct {
	// path is the absolute lockfile path used for error messages and
	// for stale-PID-lock reclamation cleanup.
	path string

	// f is the open lockfile. Closing f releases the OS-level lock on
	// every platform.
	f *os.File

	// pid is the PID written to the lockfile by [PIDLock]; 0 for
	// regular [Lock] / [LockShared] / [TryLock] / [LockTimeout].
	pid int

	// once guards Release for idempotency.
	once sync.Once

	// releaseErr captures the first Release's error so subsequent
	// callers can choose to inspect it (currently they get nil; this
	// field exists for future debug surfacing).
	releaseErr error
}

// Lock acquires an exclusive advisory lock on path, blocking until
// the lock is available. The lockfile is created if it does not yet
// exist. Returns a [*LockHandle] that the caller must Release.
//
// On POSIX, this is `flock(LOCK_EX)` via [syscall.Flock]. On Windows,
// it is `LockFileEx(LOCKFILE_EXCLUSIVE_LOCK)` via [syscall.Syscall6]
// against `kernel32.LockFileEx`. The lock is associated with the open
// file handle, not the path — closing the file (via Release) releases
// it.
//
// Advisory only: cooperating processes must agree to call Lock to see
// each other's locks. Direct file reads/writes ignore the lock.
func Lock(path string) (*LockHandle, error) {
	return acquireLock(path, lockKindExclusive, 0)
}

// LockShared acquires a shared (read) advisory lock on path. Multiple
// concurrent shared holders are allowed; shared and exclusive locks
// are mutually exclusive. Blocks until the lock is available.
func LockShared(path string) (*LockHandle, error) {
	return acquireLock(path, lockKindShared, 0)
}

// TryLock attempts to acquire an exclusive lock on path without
// blocking. Returns (handle, true, nil) on success and (nil, false,
// nil) when the lock is held by another holder. Any other error is
// returned as-is (handle nil, ok false).
func TryLock(path string) (*LockHandle, bool, error) {
	h, err := acquireLock(path, lockKindExclusiveNoBlock, 0)
	switch {
	case err == nil:
		return h, true, nil
	case errors.Is(err, errLockBusy):
		return nil, false, nil
	default:
		return nil, false, err
	}
}

// LockTimeout acquires an exclusive lock on path, blocking up to d.
// Returns [ErrLockTimeout] if the lock is not acquired before d
// elapses. A non-positive d behaves like [TryLock] (a single
// non-blocking attempt; busy → ErrLockTimeout).
//
// Implementation note: this is a poll-and-retry loop atop the
// non-blocking acquire; the wait granularity is 50 ms.
func LockTimeout(path string, d time.Duration) (*LockHandle, error) {
	deadline := time.Now().Add(d)
	for {
		h, err := acquireLock(path, lockKindExclusiveNoBlock, 0)
		if err == nil {
			return h, nil
		}
		if !errors.Is(err, errLockBusy) {
			return nil, err
		}
		if time.Now().After(deadline) {
			return nil, wrapPathError(opLock, path, ErrLockTimeout)
		}
		remaining := time.Until(deadline)
		wait := min(remaining, lockPollInterval)
		if wait <= 0 {
			return nil, wrapPathError(opLock, path, ErrLockTimeout)
		}
		time.Sleep(wait)
	}
}

// WithLock acquires an exclusive lock on path, runs fn, and releases
// the lock — including on panic from fn. The lock-acquire error is
// returned without invoking fn; any Release error joins fn's error
// via [errors.Join].
func WithLock(path string, fn func() error) (err error) {
	h, lerr := Lock(path)
	if lerr != nil {
		return lerr
	}
	defer func() {
		if rerr := h.Release(); rerr != nil {
			err = errors.Join(err, rerr)
		}
	}()
	return fn()
}

// IsLocked reports whether path currently has an exclusive lock held
// by another holder. Implementation: a non-blocking [TryLock] +
// immediate release; busy → true, available → false. False on any
// error (path missing, permission denied, etc.) so the predicate is
// safe to call from log statements and dashboards without needing
// error handling.
func IsLocked(path string) bool {
	h, ok, err := TryLock(path)
	if err != nil {
		return false
	}
	if !ok {
		return true
	}
	_ = h.Release() //nolint:errcheck // probe-only; release errors are not actionable here
	return false
}

// PIDLock writes the calling process's PID to path and acquires an
// exclusive lock on it. The lockfile content is the textual PID
// followed by a newline.
//
// Stale-lock detection: if path already exists, [PIDLock] reads the
// recorded PID. If the recorded PID is alive (probed via
// [os.FindProcess] + signal-0 ping on POSIX; OpenProcess on Windows)
// AND the lock is still held by it, [PIDLock] blocks waiting for the
// lock as a normal [Lock] would. If the recorded PID is dead — or no
// process exists at that PID — [PIDLock] truncates the file, writes
// the caller's PID, and returns the handle wrapped with
// [ErrStaleLock] (informational; the handle is fully valid).
//
// Returns the handle and a nil error on a clean acquisition. Returns
// the handle and a wrapped [ErrStaleLock] on a reclaimed stale lock.
// Returns nil and an error on any acquire failure.
func PIDLock(path string) (*LockHandle, error) {
	const myPID = -1 // sentinel meaning "use os.Getpid() at write time"
	staleReclaimed := false

	if existing, readErr := os.ReadFile(path); readErr == nil {
		recordedPID := parsePIDFile(existing)
		if recordedPID > 0 && pidAlive(recordedPID) {
			// Owner is alive; fall through to normal blocking acquire.
			// We deliberately do NOT short-circuit return-busy here:
			// callers asked PIDLock(path); they want to either acquire
			// it (if the holder finishes) or block until they can.
			_ = recordedPID
		} else if recordedPID > 0 {
			// Stale: the PID died without releasing the lock. POSIX
			// flock auto-releases on process exit, so the OS lock is
			// already gone — we just need to overwrite the stale PID
			// content. Windows likewise releases when the holder's
			// process handle is collected. Mark staleness for the
			// returned error.
			staleReclaimed = true
		}
	}

	h, err := acquireLock(path, lockKindExclusive, myPID)
	if err != nil {
		return nil, err
	}
	if staleReclaimed {
		return h, wrapPathError(opPIDLock, path, ErrStaleLock)
	}
	return h, nil
}

// Release releases the lock and closes the underlying file. The call
// is idempotent — repeated calls return nil after the first invocation
// (errors from the first call are not re-surfaced; check the first
// return). Release does NOT remove the lockfile from disk: it persists
// so other holders see the same path. For [PIDLock]-style
// "single-instance" lockfiles whose presence is itself meaningful,
// callers should call [os.Remove] after Release if they want the file
// gone — but the typical pattern is to leave it in place.
func (h *LockHandle) Release() error {
	if h == nil {
		return nil
	}
	h.once.Do(func() {
		h.releaseErr = h.releaseInternal()
	})
	return h.releaseErr
}

// PID returns the PID recorded by [PIDLock], or 0 for any other
// acquirer. Use this to log "instance owned by PID N" when failing to
// reach a single-instance constraint.
func (h *LockHandle) PID() int {
	if h == nil {
		return 0
	}
	return h.pid
}

// releaseInternal performs the actual unlock + close. On every
// platform, closing the file handle releases the OS-level lock, so we
// just need to ensure the close happens exactly once.
func (h *LockHandle) releaseInternal() error {
	if h.f == nil {
		return nil
	}
	if err := unlockFile(h.f); err != nil {
		_ = h.f.Close()
		h.f = nil
		return wrapPathError(opUnlock, h.path, err)
	}
	if err := h.f.Close(); err != nil {
		h.f = nil
		return wrapPathError(opUnlock, h.path, err)
	}
	h.f = nil
	return nil
}

// lockKind selects exclusive vs shared and blocking vs non-blocking.
type lockKind int

const (
	lockKindExclusive lockKind = iota
	lockKindExclusiveNoBlock
	lockKindShared
)

// errLockBusy signals that a non-blocking acquire (kind*NoBlock or
// the busy-poll inside LockTimeout) failed because the lock is held.
// Caller-visible APIs translate this to ok=false (TryLock) or
// ErrLockTimeout (LockTimeout). It does NOT escape this file.
var errLockBusy = errors.New("fs: lock: busy")

// errInvalidLockKind is returned by [osFlock] for an unknown
// [lockKind] value. Programmer-error guard; the caller-facing API
// never exposes this since the public functions hardcode the kind.
var errInvalidLockKind = errors.New("fs: lock: invalid lock kind")

// acquireLock opens (or creates) the lockfile at path and acquires
// the requested lock kind. If writePID > 0 OR writePID == -1 (sentinel
// meaning "use os.Getpid"), the file is truncated and the PID is
// written after acquisition (so the contents always reflect the
// current holder).
func acquireLock(path string, kind lockKind, writePID int) (*LockHandle, error) {
	if path == "" {
		return nil, wrapPathError(opLock, path, ErrInvalidPath)
	}

	flags := os.O_RDWR | os.O_CREATE
	f, err := os.OpenFile(path, flags, lockFileMode)
	if err != nil {
		return nil, wrapPathError(opLock, path, err)
	}

	if lerr := osFlock(f, kind); lerr != nil {
		_ = f.Close()
		if errors.Is(lerr, errLockBusy) {
			// Don't wrap busy — caller (TryLock / LockTimeout) checks
			// errors.Is and translates without surfacing it.
			return nil, errLockBusy
		}
		return nil, wrapPathError(opLock, path, lerr)
	}

	h := &LockHandle{path: path, f: f}

	if writePID != 0 {
		pid := writePID
		if pid == -1 {
			pid = os.Getpid()
		}
		if perr := writePIDContent(f, pid); perr != nil {
			_ = unlockFile(f) //nolint:errcheck // best-effort cleanup of an already-failed acquire
			_ = f.Close()
			return nil, wrapPathError(opPIDLock, path, perr)
		}
		h.pid = pid
	}

	return h, nil
}

// writePIDContent truncates f and writes "<pid>\n" starting at offset
// zero. Used by [PIDLock] to record the holder's PID.
func writePIDContent(f *os.File, pid int) error {
	if err := f.Truncate(0); err != nil {
		return err //nolint:wrapcheck // wrapped by caller
	}
	if _, err := f.Seek(0, 0); err != nil {
		return err //nolint:wrapcheck // wrapped by caller
	}
	content := strconv.Itoa(pid) + "\n"
	if _, err := f.WriteString(content); err != nil {
		return err //nolint:wrapcheck // wrapped by caller
	}
	return f.Sync() //nolint:wrapcheck // wrapped by caller
}

// parsePIDFile extracts the PID stored at the start of a PIDLock
// file. Returns 0 for any malformed input (empty, non-numeric,
// negative). Tolerates trailing whitespace / newlines / extra
// content.
func parsePIDFile(content []byte) int {
	s := strings.TrimSpace(string(content))
	if s == "" {
		return 0
	}
	// Take the first whitespace-delimited token so a future caller
	// that adds metadata after the PID doesn't break us.
	if idx := strings.IndexAny(s, " \t\r\n"); idx >= 0 {
		s = s[:idx]
	}
	pid, err := strconv.Atoi(s)
	if err != nil || pid <= 0 {
		return 0
	}
	return pid
}

// pidAlive reports whether a process with the given PID is currently
// alive. POSIX: [os.FindProcess] always succeeds, so we send signal 0
// and check for ESRCH (no such process). Windows: [os.FindProcess]
// only succeeds when the OS hands back a real process handle.
//
// The PID-recycling race (the kernel reused a dead PID between when
// we read the lockfile and when we probe) is fundamentally
// unresolvable without an OS-level "process token" we can name —
// neither POSIX nor Windows offers one for arbitrary cooperating
// processes. Callers that need stronger guarantees should add a
// "started-at" timestamp to the lockfile content and verify it
// matches /proc/PID/stat's start-time field (Linux) or the equivalent.
// The package opts for the simpler, more portable check.
func pidAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	return platformPIDAlive(pid)
}

// pidAliveSignal0 sends signal 0 to pid (POSIX). Returns true when
// the kill succeeds or fails with EPERM (which means the process
// exists but we lack permission to signal it — still alive). False
// on ESRCH (no such process) or any other error.
//
// Used by the POSIX platformPIDAlive implementation.
func pidAliveSignal0(pid int) bool {
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	err = proc.Signal(syscall.Signal(0))
	if err == nil {
		return true
	}
	return errors.Is(err, syscall.EPERM)
}

