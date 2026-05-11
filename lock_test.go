package fs

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// lockPath returns a fresh per-test lockfile path inside t.TempDir.
func lockPath(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "test.lock")
}

func TestLock_BasicAcquireRelease(t *testing.T) {
	t.Parallel()
	p := lockPath(t)

	h, err := Lock(p)
	if err != nil {
		t.Fatalf("Lock: %v", err)
	}
	if h == nil {
		t.Fatal("nil handle")
	}
	if !Exists(p) {
		t.Errorf("lockfile not created at %s", p)
	}
	if err := h.Release(); err != nil {
		t.Errorf("Release: %v", err)
	}
}

func TestLock_ReleaseIsIdempotent(t *testing.T) {
	t.Parallel()
	p := lockPath(t)

	h, err := Lock(p)
	if err != nil {
		t.Fatalf("Lock: %v", err)
	}
	if err := h.Release(); err != nil {
		t.Errorf("first Release: %v", err)
	}
	for i := 0; i < 3; i++ {
		if err := h.Release(); err != nil {
			t.Errorf("Release #%d: %v", i+2, err)
		}
	}
}

func TestTryLock_BusySecondCaller(t *testing.T) {
	t.Parallel()
	p := lockPath(t)

	first, err := Lock(p)
	if err != nil {
		t.Fatalf("first Lock: %v", err)
	}
	defer func() { _ = first.Release() }()

	h2, ok, err := TryLock(p)
	if err != nil {
		t.Fatalf("TryLock returned error: %v", err)
	}
	if ok {
		t.Fatal("TryLock unexpectedly succeeded while first holder held the lock")
	}
	if h2 != nil {
		t.Fatal("TryLock returned non-nil handle with ok=false")
	}
}

func TestTryLock_SuccessAfterRelease(t *testing.T) {
	t.Parallel()
	p := lockPath(t)

	first, err := Lock(p)
	if err != nil {
		t.Fatalf("first Lock: %v", err)
	}
	if err := first.Release(); err != nil {
		t.Fatalf("first Release: %v", err)
	}

	h2, ok, err := TryLock(p)
	if err != nil {
		t.Fatalf("TryLock: %v", err)
	}
	if !ok {
		t.Fatal("TryLock did not succeed after release")
	}
	defer func() { _ = h2.Release() }()
}

func TestLockTimeout_ExpiresWhenBusy(t *testing.T) {
	t.Parallel()
	p := lockPath(t)

	first, err := Lock(p)
	if err != nil {
		t.Fatalf("Lock: %v", err)
	}
	defer func() { _ = first.Release() }()

	start := time.Now()
	h, err := LockTimeout(p, 150*time.Millisecond)
	elapsed := time.Since(start)
	if h != nil {
		_ = h.Release()
		t.Fatal("LockTimeout unexpectedly acquired lock")
	}
	if !errors.Is(err, ErrLockTimeout) {
		t.Errorf("err = %v; want ErrLockTimeout", err)
	}
	// Should wait roughly the full timeout, +/- one poll interval.
	if elapsed < 100*time.Millisecond {
		t.Errorf("returned in %v; expected >= 100ms", elapsed)
	}
	if elapsed > 500*time.Millisecond {
		t.Errorf("returned in %v; expected <= 500ms", elapsed)
	}
}

func TestLockTimeout_SucceedsAfterPeerReleases(t *testing.T) {
	t.Parallel()
	p := lockPath(t)

	first, err := Lock(p)
	if err != nil {
		t.Fatalf("Lock: %v", err)
	}

	// Release the lock after a short delay so the timeout-waiter
	// observes it.
	go func() {
		time.Sleep(80 * time.Millisecond)
		_ = first.Release()
	}()

	h, err := LockTimeout(p, 2*time.Second)
	if err != nil {
		t.Fatalf("LockTimeout: %v", err)
	}
	if h == nil {
		t.Fatal("nil handle")
	}
	_ = h.Release()
}

func TestWithLock_RunsFunctionUnderLock(t *testing.T) {
	t.Parallel()
	p := lockPath(t)

	var ran bool
	err := WithLock(p, func() error {
		ran = true
		// Verify the lock is actually held: a non-blocking acquire
		// from the same process MUST fail (same OFD).
		h2, ok, err := TryLock(p)
		if err != nil {
			return fmt.Errorf("inner TryLock returned err: %w", err)
		}
		if ok {
			_ = h2.Release()
			return errors.New("inner TryLock succeeded while WithLock body held the lock")
		}
		return nil
	})
	if err != nil {
		t.Fatalf("WithLock: %v", err)
	}
	if !ran {
		t.Fatal("function did not run")
	}
}

func TestWithLock_ReleasesOnPanic(t *testing.T) {
	t.Parallel()
	p := lockPath(t)

	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic to propagate")
		}
		// After the panic, the lock should be released.
		h, ok, err := TryLock(p)
		if err != nil {
			t.Errorf("post-panic TryLock returned err: %v", err)
		}
		if !ok {
			t.Error("lock was not released after panic")
		}
		if h != nil {
			_ = h.Release()
		}
	}()

	_ = WithLock(p, func() error {
		panic("boom")
	})
}

func TestWithLock_PropagatesFunctionError(t *testing.T) {
	t.Parallel()
	p := lockPath(t)

	want := errors.New("fn-error")
	err := WithLock(p, func() error { return want })
	if !errors.Is(err, want) {
		t.Errorf("err = %v; want %v", err, want)
	}
}

func TestIsLocked(t *testing.T) {
	t.Parallel()
	p := lockPath(t)

	if IsLocked(p) {
		t.Fatal("IsLocked returned true for a fresh path")
	}

	h, err := Lock(p)
	if err != nil {
		t.Fatalf("Lock: %v", err)
	}

	// IsLocked from the same process uses a separate OFD, so flock
	// returns EWOULDBLOCK and IsLocked correctly reports true.
	if !IsLocked(p) {
		t.Error("IsLocked returned false while lock was held")
	}

	if err := h.Release(); err != nil {
		t.Fatalf("Release: %v", err)
	}
	if IsLocked(p) {
		t.Error("IsLocked returned true after release")
	}
}

func TestLockShared_AllowsMultipleHolders(t *testing.T) {
	t.Parallel()
	p := lockPath(t)

	h1, err := LockShared(p)
	if err != nil {
		t.Fatalf("first LockShared: %v", err)
	}
	defer func() { _ = h1.Release() }()

	h2, err := LockShared(p)
	if err != nil {
		t.Fatalf("second LockShared: %v", err)
	}
	defer func() { _ = h2.Release() }()
}

func TestPIDLock_RecordsPID(t *testing.T) {
	t.Parallel()
	p := lockPath(t)

	h, err := PIDLock(p)
	if err != nil {
		t.Fatalf("PIDLock: %v", err)
	}
	defer func() { _ = h.Release() }()

	if h.PID() != os.Getpid() {
		t.Errorf("PID() = %d; want %d", h.PID(), os.Getpid())
	}

	content, rerr := os.ReadFile(p)
	if rerr != nil {
		t.Fatalf("ReadFile(lockfile): %v", rerr)
	}
	got := strings.TrimSpace(string(content))
	want := fmt.Sprintf("%d", os.Getpid())
	if got != want {
		t.Errorf("lockfile content = %q; want %q", got, want)
	}
}

func TestPIDLock_ReclaimsStale(t *testing.T) {
	t.Parallel()
	p := lockPath(t)

	// Write a lockfile with a guaranteed-dead PID. Pid 1 is init,
	// guaranteed alive on every supported platform, so we use a PID
	// that's astronomically unlikely to be live: the max representable
	// PID on most OSes is in the millions; 2^31-2 is the largest
	// non-sentinel value (-1 is reserved). On Linux the pid_max
	// default is 4194304, so 2^31-2 is comfortably above any real
	// allocator.
	const deadPID = 2147483646
	if err := os.WriteFile(p, []byte(fmt.Sprintf("%d\n", deadPID)), 0o644); err != nil {
		t.Fatalf("WriteFile(seed lockfile): %v", err)
	}

	h, err := PIDLock(p)
	if h == nil {
		t.Fatalf("PIDLock returned nil handle; err=%v", err)
	}
	defer func() { _ = h.Release() }()

	if !errors.Is(err, ErrStaleLock) {
		t.Errorf("err = %v; want ErrStaleLock", err)
	}
	if h.PID() != os.Getpid() {
		t.Errorf("PID() = %d; want %d (caller's pid after reclaim)", h.PID(), os.Getpid())
	}
}

func TestLock_RegularHandlePIDIsZero(t *testing.T) {
	t.Parallel()
	p := lockPath(t)
	h, err := Lock(p)
	if err != nil {
		t.Fatalf("Lock: %v", err)
	}
	defer func() { _ = h.Release() }()

	if h.PID() != 0 {
		t.Errorf("Lock handle PID = %d; want 0", h.PID())
	}
}

func TestLock_EmptyPathRejected(t *testing.T) {
	t.Parallel()
	if _, err := Lock(""); !errors.Is(err, ErrInvalidPath) {
		t.Errorf("err = %v; want ErrInvalidPath", err)
	}
}

func TestLock_ConcurrentTryLockExactlyOneWins(t *testing.T) {
	t.Parallel()
	p := lockPath(t)

	const goroutines = 16
	var (
		wg       sync.WaitGroup
		winners  atomic.Int32
		start    = make(chan struct{})
		handlers = make([]*LockHandle, goroutines)
	)
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func(slot int) {
			defer wg.Done()
			<-start
			h, ok, err := TryLock(p)
			if err != nil {
				t.Errorf("TryLock: %v", err)
				return
			}
			if ok {
				winners.Add(1)
				handlers[slot] = h
			}
		}(i)
	}
	close(start)
	wg.Wait()

	if got := winners.Load(); got != 1 {
		t.Errorf("winners = %d; want exactly 1", got)
	}
	// Release the one winner.
	for _, h := range handlers {
		if h != nil {
			_ = h.Release()
		}
	}
}

func TestParsePIDFile(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in              string
		wantPID         int
		wantFingerprint string
	}{
		{"123", 123, ""},
		{"123\n", 123, ""},
		{"  456  \n", 456, ""},
		{"789 fp-abc", 789, "fp-abc"},
		{"789 fp-abc\n", 789, "fp-abc"},
		{"789 multi word fingerprint", 789, "multi word fingerprint"},
		{"", 0, ""},
		{"   ", 0, ""},
		{"abc", 0, ""},
		{"-1", 0, ""},
		{"0", 0, ""},
	}
	for _, c := range cases {
		gotPID, gotFP := parsePIDFile([]byte(c.in))
		if gotPID != c.wantPID || gotFP != c.wantFingerprint {
			t.Errorf("parsePIDFile(%q) = (%d, %q); want (%d, %q)", c.in, gotPID, gotFP, c.wantPID, c.wantFingerprint)
		}
	}
}

func TestPIDLock_FingerprintRejectsRecycledPID(t *testing.T) {
	t.Parallel()
	p := lockPath(t)

	// Seed: write a lockfile that records the current PID with a
	// fingerprint of "original". The new acquire is configured with
	// a fingerprint function that returns "changed"; simulating a
	// PID-recycle scenario where the PID is alive (true, we are
	// alive) but is a *different* process from the one recorded.
	if err := os.WriteFile(p, fmt.Appendf(nil, "%d original\n", os.Getpid()), 0o644); err != nil {
		t.Fatalf("WriteFile seed: %v", err)
	}

	h, err := PIDLock(p, WithPIDLockFingerprint(func(_ int) string { return "changed" }))
	if h == nil {
		t.Fatalf("PIDLock returned nil handle; err=%v", err)
	}
	defer func() { _ = h.Release() }()

	if !errors.Is(err, ErrStaleLock) {
		t.Errorf("err = %v; want ErrStaleLock (fingerprint mismatch should reclaim)", err)
	}
}
