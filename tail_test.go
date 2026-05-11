package fs

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
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

// awaitLines polls snap until len(snap()) >= want or the deadline
// elapses; reports failure with the most recent state.
func awaitLines(t *testing.T, snap func() ([]string, error), want int, timeout time.Duration) []string {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		got, err := snap()
		if err != nil {
			t.Fatalf("tail err: %v", err)
		}
		if len(got) >= want {
			return got
		}
		if time.Now().After(deadline) {
			t.Fatalf("timeout waiting for %d lines; got %d: %v", want, len(got), got)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestTail_AppendedLinesFromEOF(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "log")

	if err := os.WriteFile(path, []byte("preamble\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	ctx := t.Context()
	snap, stop := collectLines(t, ctx, path, WithTailPollInterval(10*time.Millisecond))
	defer stop()

	// Tail starts at EOF — the preamble must NOT be yielded.
	time.Sleep(50 * time.Millisecond)

	// Append two lines.
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatalf("Open append: %v", err)
	}
	if _, werr := f.WriteString("alpha\nbeta\n"); werr != nil {
		t.Fatalf("Write: %v", werr)
	}
	_ = f.Close()

	got := awaitLines(t, snap, 2, time.Second)
	want := []string{"alpha", "beta"}
	if !equalStringSlice(got, want) {
		t.Errorf("lines = %v; want %v", got, want)
	}
}

func TestTail_FromStartReadsExisting(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "log")

	if err := os.WriteFile(path, []byte("a\nb\nc\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	ctx := t.Context()
	snap, stop := collectLines(t, ctx, path, WithTailFromStart(), WithTailPollInterval(10*time.Millisecond))
	defer stop()

	got := awaitLines(t, snap, 3, time.Second)
	want := []string{"a", "b", "c"}
	if !equalStringSlice(got[:3], want) {
		t.Errorf("lines = %v; want %v", got, want)
	}
}

func TestTail_PartialLineWaitsForNewline(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "log")

	if err := os.WriteFile(path, []byte(""), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	ctx := t.Context()
	snap, stop := collectLines(t, ctx, path, WithTailFromStart(), WithTailPollInterval(10*time.Millisecond))
	defer stop()

	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatalf("Open append: %v", err)
	}
	defer func() { _ = f.Close() }()

	// Write a partial line. Tail must NOT yield it.
	if _, werr := f.WriteString("partial-without-newline"); werr != nil {
		t.Fatalf("Write partial: %v", werr)
	}
	time.Sleep(60 * time.Millisecond)
	got, _ := snap()
	if len(got) != 0 {
		t.Errorf("partial line was yielded prematurely: %v", got)
	}

	// Complete the line.
	if _, werr := f.WriteString("-completed\n"); werr != nil {
		t.Fatalf("Write completion: %v", werr)
	}
	got = awaitLines(t, snap, 1, time.Second)
	if got[0] != "partial-without-newline-completed" {
		t.Errorf("line = %q; want partial-without-newline-completed", got[0])
	}
}

func TestTail_CRLFStripped(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "log")

	if err := os.WriteFile(path, []byte("a\r\nb\r\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	ctx := t.Context()
	snap, stop := collectLines(t, ctx, path, WithTailFromStart(), WithTailPollInterval(10*time.Millisecond))
	defer stop()

	got := awaitLines(t, snap, 2, time.Second)
	if got[0] != "a" || got[1] != "b" {
		t.Errorf("lines = %v; want [a b]", got)
	}
}

func TestTail_DetectsRotationByRename(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "log")

	if err := os.WriteFile(path, []byte(""), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	ctx := t.Context()
	snap, stop := collectLines(t, ctx, path, WithTailPollInterval(10*time.Millisecond))
	defer stop()

	// Give Tail time to open the file and seek to EOF before the
	// writer appends — otherwise the seek-to-EOF would skip past the
	// appended bytes.
	time.Sleep(50 * time.Millisecond)

	// Write a line through the original inode.
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatalf("Open append: %v", err)
	}
	_, _ = f.WriteString("before-rotate\n")
	_ = f.Close()

	awaitLines(t, snap, 1, time.Second)

	// Simulate logrotate: rename current → .1, then create a fresh
	// file at the original path with new content.
	if err := os.Rename(path, path+".1"); err != nil {
		t.Fatalf("Rename: %v", err)
	}
	if err := os.WriteFile(path, []byte("after-rotate\n"), 0o644); err != nil {
		t.Fatalf("WriteFile new: %v", err)
	}

	awaitLines(t, snap, 2, 2*time.Second)
	got, _ := snap()
	if got[0] != "before-rotate" || got[1] != "after-rotate" {
		t.Errorf("lines = %v; want [before-rotate after-rotate]", got)
	}
}

func TestTail_DetectsInPlaceTruncation(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "log")

	if err := os.WriteFile(path, []byte("first\nsecond\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	ctx := t.Context()
	snap, stop := collectLines(t, ctx, path, WithTailFromStart(), WithTailPollInterval(10*time.Millisecond))
	defer stop()

	awaitLines(t, snap, 2, time.Second)

	// Truncate the file in place and append a new line.
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

	awaitLines(t, snap, 3, 2*time.Second)
	got, _ := snap()
	if got[2] != "after-trunc" {
		t.Errorf("got[2] = %q; want after-trunc", got[2])
	}
}

func TestTail_ContextCancelTerminates(t *testing.T) {
	t.Parallel()
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

	time.Sleep(30 * time.Millisecond)
	cancel()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Tail did not terminate after ctx cancel")
	}
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
	time.Sleep(20 * time.Millisecond)
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Tail did not exit after cancel under clamped options")
	}
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

