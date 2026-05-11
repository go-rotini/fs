package fs

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"
)

// collectLines drains seq into a slice in a separate goroutine and
// returns a stop function that cancels ctx and waits for the
// goroutine to exit. lines and lastErr are mutex-guarded for the
// duration of the test.
func collectLines(t *testing.T, ctx context.Context, path string, opts ...TailOption) (snapshot func() ([]string, error), stop func()) {
	t.Helper()

	ctx, cancel := context.WithCancel(ctx)
	var (
		mu      sync.Mutex
		lines   []string
		lastErr error
		done    = make(chan struct{})
	)

	go func() {
		defer close(done)
		for line, err := range Tail(ctx, path, opts...) {
			mu.Lock()
			if err != nil {
				lastErr = err
				mu.Unlock()
				return
			}
			lines = append(lines, line)
			mu.Unlock()
		}
	}()

	snapshot = func() ([]string, error) {
		mu.Lock()
		defer mu.Unlock()
		out := make([]string, len(lines))
		copy(out, lines)
		return out, lastErr
	}

	stop = func() {
		cancel()
		<-done
	}
	return snapshot, stop
}

// settleAndSnap advances the synctest bubble's fake clock by enough
// poll intervals for Tail to drain the file, then returns the
// captured lines. synctest.Wait blocks until every goroutine in the
// bubble (including Tail's poller) is durably blocked; at which
// point any newly-yielded lines have landed in the slice.
func settleAndSnap(t *testing.T, snap func() ([]string, error), pollInterval time.Duration, polls int) []string {
	t.Helper()
	for range polls {
		time.Sleep(pollInterval)
		synctest.Wait()
	}
	got, err := snap()
	if err != nil {
		t.Fatalf("tail err: %v", err)
	}
	return got
}

func TestTail_AppendedLinesFromEOF(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "log")

		if err := os.WriteFile(path, []byte("preamble\n"), 0o644); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}

		const poll = 10 * time.Millisecond
		snap, stop := collectLines(t, t.Context(), path, WithTailPollInterval(poll))
		defer stop()

		// Let Tail open the file and seek to EOF. synctest.Wait blocks
		// until Tail is durably parked on its poll timer; only then is
		// it safe to append (otherwise the seek-to-EOF could overshoot
		// the appended bytes).
		synctest.Wait()

		f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
		if err != nil {
			t.Fatalf("Open append: %v", err)
		}
		if _, werr := f.WriteString("alpha\nbeta\n"); werr != nil {
			t.Fatalf("Write: %v", werr)
		}
		_ = f.Close()

		got := settleAndSnap(t, snap, poll, 2)
		want := []string{"alpha", "beta"}
		if !equalStringSlice(got, want) {
			t.Errorf("lines = %v; want %v", got, want)
		}
	})
}

func TestTail_FromStartReadsExisting(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "log")

		if err := os.WriteFile(path, []byte("a\nb\nc\n"), 0o644); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}

		const poll = 10 * time.Millisecond
		snap, stop := collectLines(t, t.Context(), path, WithTailFromStart(), WithTailPollInterval(poll))
		defer stop()

		got := settleAndSnap(t, snap, poll, 2)
		want := []string{"a", "b", "c"}
		if len(got) < 3 || !equalStringSlice(got[:3], want) {
			t.Errorf("lines = %v; want %v", got, want)
		}
	})
}

func TestTail_PartialLineWaitsForNewline(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "log")

		if err := os.WriteFile(path, []byte(""), 0o644); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}

		const poll = 10 * time.Millisecond
		snap, stop := collectLines(t, t.Context(), path, WithTailFromStart(), WithTailPollInterval(poll))
		defer stop()

		f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
		if err != nil {
			t.Fatalf("Open append: %v", err)
		}
		defer func() { _ = f.Close() }()

		if _, werr := f.WriteString("partial-without-newline"); werr != nil {
			t.Fatalf("Write partial: %v", werr)
		}

		// Advance the bubble several polls; Tail must NOT yield the
		// partial line.
		got := settleAndSnap(t, snap, poll, 3)
		if len(got) != 0 {
			t.Errorf("partial line was yielded prematurely: %v", got)
		}

		if _, werr := f.WriteString("-completed\n"); werr != nil {
			t.Fatalf("Write completion: %v", werr)
		}

		got = settleAndSnap(t, snap, poll, 2)
		if len(got) != 1 || got[0] != "partial-without-newline-completed" {
			t.Errorf("lines = %v; want one line 'partial-without-newline-completed'", got)
		}
	})
}

func TestTail_CRLFStripped(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "log")

		if err := os.WriteFile(path, []byte("a\r\nb\r\n"), 0o644); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}

		const poll = 10 * time.Millisecond
		snap, stop := collectLines(t, t.Context(), path, WithTailFromStart(), WithTailPollInterval(poll))
		defer stop()

		got := settleAndSnap(t, snap, poll, 2)
		if len(got) < 2 || got[0] != "a" || got[1] != "b" {
			t.Errorf("lines = %v; want [a b]", got)
		}
	})
}

func TestTail_DetectsRotationByRename(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "log")

		if err := os.WriteFile(path, []byte(""), 0o644); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}

		const poll = 10 * time.Millisecond
		snap, stop := collectLines(t, t.Context(), path, WithTailPollInterval(poll))
		defer stop()

		// Tail opens file + seeks to EOF before we append.
		synctest.Wait()

		f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
		if err != nil {
			t.Fatalf("Open append: %v", err)
		}
		_, _ = f.WriteString("before-rotate\n")
		_ = f.Close()

		got := settleAndSnap(t, snap, poll, 2)
		if len(got) != 1 || got[0] != "before-rotate" {
			t.Fatalf("pre-rotate lines = %v; want [before-rotate]", got)
		}

		// Simulate logrotate: rename current to .1, then create fresh.
		if err := os.Rename(path, path+".1"); err != nil {
			t.Fatalf("Rename: %v", err)
		}
		if err := os.WriteFile(path, []byte("after-rotate\n"), 0o644); err != nil {
			t.Fatalf("WriteFile new: %v", err)
		}

		got = settleAndSnap(t, snap, poll, 4)
		if len(got) != 2 || got[0] != "before-rotate" || got[1] != "after-rotate" {
			t.Errorf("lines = %v; want [before-rotate after-rotate]", got)
		}
	})
}

func TestTail_DetectsInPlaceTruncation(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "log")

		if err := os.WriteFile(path, []byte("first\nsecond\n"), 0o644); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}

		const poll = 10 * time.Millisecond
		snap, stop := collectLines(t, t.Context(), path, WithTailFromStart(), WithTailPollInterval(poll))
		defer stop()

		got := settleAndSnap(t, snap, poll, 2)
		if len(got) < 2 {
			t.Fatalf("pre-trunc lines = %v; want at least 2", got)
		}

		if err := os.Truncate(path, 0); err != nil {
			t.Fatalf("Truncate: %v", err)
		}
		f, err := os.OpenFile(path, os.O_WRONLY, 0)
		if err != nil {
			t.Fatalf("Open: %v", err)
		}
		if _, werr := f.WriteString("after-trunc\n"); werr != nil {
			t.Fatalf("Write: %v", werr)
		}
		_ = f.Close()

		got = settleAndSnap(t, snap, poll, 4)
		if len(got) < 3 || got[2] != "after-trunc" {
			t.Errorf("got = %v; want [..., after-trunc]", got)
		}
	})
}

func TestTail_ContextCancelTerminates(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "log")
		if err := os.WriteFile(path, []byte(""), 0o644); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}

		ctx, cancel := context.WithCancel(t.Context())
		done := make(chan struct{})
		go func() {
			for line, err := range Tail(ctx, path, WithTailPollInterval(10*time.Millisecond)) {
				_ = line
				_ = err
			}
			close(done)
		}()

		// Let Tail park on its poll timer, then cancel. With fake time
		// and durable-blocking semantics, the cancel + goroutine exit
		// is observable instantly.
		synctest.Wait()
		cancel()

		select {
		case <-done:
		case <-time.After(time.Second):
			t.Fatal("Tail did not terminate after ctx cancel")
		}
	})
}

func TestTail_MissingPathYieldsError(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "missing.log")

	var gotErr error
	for _, err := range Tail(t.Context(), path) {
		if err != nil {
			gotErr = err
			break
		}
	}
	if !errors.Is(gotErr, ErrNotFound) {
		t.Errorf("err = %v; want ErrNotFound", gotErr)
	}
}

func TestTail_LineLongerThanCapIsTruncated(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "log")

	// Write > tailMaxLineBytes without a newline.
	huge := strings.Repeat("x", tailMaxLineBytes+128)
	if err := os.WriteFile(path, []byte(huge), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()

	var firstLineLen int
	for line, err := range Tail(ctx, path, WithTailFromStart(), WithTailPollInterval(10*time.Millisecond)) {
		if err != nil {
			t.Fatalf("err: %v", err)
		}
		firstLineLen = len(line)
		break
	}
	if firstLineLen == 0 {
		t.Fatal("never yielded any chunk")
	}
	if firstLineLen > tailMaxLineBytes {
		t.Errorf("first chunk len = %d; expected <= %d", firstLineLen, tailMaxLineBytes)
	}
}

func TestTail_BufferSizeAndPollClamping(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "log")
		if err := os.WriteFile(path, []byte(""), 0o644); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}

		// Sub-millisecond poll + zero buffer; both should be clamped /
		// defaulted without misbehaving.
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		done := make(chan struct{})
		go func() {
			for range Tail(ctx, path, WithTailPollInterval(time.Microsecond), WithTailBufferSize(0)) {
			}
			close(done)
		}()
		synctest.Wait()
		cancel()
		select {
		case <-done:
		case <-time.After(time.Second):
			t.Fatal("Tail did not exit after cancel under clamped options")
		}
	})
}

// Lightweight assertion helper to avoid importing google/go-cmp into
// the package's zero-dep test surface.
func equalStringSlice(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
