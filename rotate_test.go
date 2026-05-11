package fs

import (
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestRotator_BasicWriteAppends(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "app.log")

	r, err := NewRotator(path)
	if err != nil {
		t.Fatalf("NewRotator: %v", err)
	}
	defer func() { _ = r.Close() }()

	for i := 0; i < 3; i++ {
		if _, err := r.Write([]byte(fmt.Sprintf("line%d\n", i))); err != nil {
			t.Fatalf("Write #%d: %v", i, err)
		}
	}

	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	want := "line0\nline1\nline2\n"
	if string(content) != want {
		t.Errorf("content = %q; want %q", string(content), want)
	}
}

func TestRotator_SizeRotation(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "app.log")

	// Cap at 12 bytes. Each Write is 10 bytes ("xxxxxxxx0\n").
	// First write: 0 to 10 bytes, no rotation.
	// Second write would push to 20 bytes (>12), rotate first.
	r, err := NewRotator(path, WithRotateMaxBytes(12))
	if err != nil {
		t.Fatalf("NewRotator: %v", err)
	}
	defer func() { _ = r.Close() }()

	if _, err := r.Write([]byte("xxxxxxxx0\n")); err != nil {
		t.Fatalf("Write 1: %v", err)
	}
	if _, err := r.Write([]byte("xxxxxxxx1\n")); err != nil {
		t.Fatalf("Write 2: %v", err)
	}

	// One rotated sibling should exist.
	rotated := listRotated(t, dir, "app.log")
	if len(rotated) != 1 {
		t.Errorf("rotated siblings = %v; want exactly 1", rotated)
	}

	// Current file holds the second write only.
	content, _ := os.ReadFile(path)
	if string(content) != "xxxxxxxx1\n" {
		t.Errorf("current = %q; want %q", string(content), "xxxxxxxx1\n")
	}
	// Rotated file holds the first write.
	rotatedContent, _ := os.ReadFile(filepath.Join(dir, rotated[0]))
	if string(rotatedContent) != "xxxxxxxx0\n" {
		t.Errorf("rotated = %q; want %q", string(rotatedContent), "xxxxxxxx0\n")
	}
}

func TestRotator_AgeRotation(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "app.log")

	clock := &mockClock{t: time.Now()}
	r, err := NewRotator(path,
		WithRotateMaxAge(100*time.Millisecond),
		WithRotateClock(clock.Now),
	)
	if err != nil {
		t.Fatalf("NewRotator: %v", err)
	}
	defer func() { _ = r.Close() }()

	if _, err := r.Write([]byte("first\n")); err != nil {
		t.Fatalf("Write 1: %v", err)
	}

	// Jump the clock past the age threshold.
	clock.mu.Lock()
	clock.t = clock.t.Add(200 * time.Millisecond)
	clock.mu.Unlock()

	if _, err := r.Write([]byte("second\n")); err != nil {
		t.Fatalf("Write 2: %v", err)
	}

	rotated := listRotated(t, dir, "app.log")
	if len(rotated) != 1 {
		t.Errorf("rotated siblings = %v; want exactly 1", rotated)
	}
}

func TestRotator_ManualRotate(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "app.log")

	clock := &mockClock{t: time.Now()}
	r, err := NewRotator(path, WithRotateClock(clock.Now))
	if err != nil {
		t.Fatalf("NewRotator: %v", err)
	}
	defer func() { _ = r.Close() }()

	if _, err := r.Write([]byte("before\n")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	// Advance clock so the rotated filename differs from any later
	// rotation; not strictly necessary here but disciplined.
	clock.mu.Lock()
	clock.t = clock.t.Add(time.Second)
	clock.mu.Unlock()

	if err := r.Rotate(); err != nil {
		t.Fatalf("Rotate: %v", err)
	}

	rotated := listRotated(t, dir, "app.log")
	if len(rotated) != 1 {
		t.Errorf("rotated siblings = %v; want 1", rotated)
	}

	// Live file is now empty (just-rotated).
	info, _ := os.Stat(path)
	if info.Size() != 0 {
		t.Errorf("live size = %d; want 0 after Rotate", info.Size())
	}
}

func TestRotator_KeepRetention(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "app.log")

	clock := &mockClock{t: time.Now()}
	r, err := NewRotator(path,
		WithRotateMaxBytes(5),
		WithRotateKeep(2),
		WithRotateClock(clock.Now),
	)
	if err != nil {
		t.Fatalf("NewRotator: %v", err)
	}
	defer func() { _ = r.Close() }()

	// Force 4 rotations with monotonically-advancing timestamps.
	for i := 0; i < 4; i++ {
		clock.mu.Lock()
		clock.t = clock.t.Add(time.Second)
		clock.mu.Unlock()
		if _, err := r.Write([]byte("aaaaaa\n")); err != nil {
			t.Fatalf("Write #%d: %v", i, err)
		}
	}

	rotated := listRotated(t, dir, "app.log")
	if len(rotated) != 2 {
		t.Errorf("rotated count = %d; want 2 (keep=2)", len(rotated))
	}
}

func TestRotator_Compression(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "app.log")

	r, err := NewRotator(path,
		WithRotateMaxBytes(5),
		WithRotateCompress(true),
	)
	if err != nil {
		t.Fatalf("NewRotator: %v", err)
	}
	defer func() { _ = r.Close() }()

	if _, err := r.Write([]byte("hello\n")); err != nil {
		t.Fatalf("Write 1: %v", err)
	}
	if _, err := r.Write([]byte("world\n")); err != nil {
		t.Fatalf("Write 2: %v", err)
	}

	rotated := listRotated(t, dir, "app.log")
	if len(rotated) != 1 {
		t.Fatalf("rotated count = %d; want 1", len(rotated))
	}
	if !strings.HasSuffix(rotated[0], ".gz") {
		t.Errorf("rotated name = %q; want .gz suffix", rotated[0])
	}

	gz, err := os.Open(filepath.Join(dir, rotated[0]))
	if err != nil {
		t.Fatalf("Open gz: %v", err)
	}
	defer func() { _ = gz.Close() }()
	gr, err := gzip.NewReader(gz)
	if err != nil {
		t.Fatalf("gzip.NewReader: %v", err)
	}
	defer func() { _ = gr.Close() }()
	body, err := io.ReadAll(gr)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if string(body) != "hello\n" {
		t.Errorf("decompressed = %q; want %q", string(body), "hello\n")
	}

	// Uncompressed sibling should not be left behind.
	uncompressed := strings.TrimSuffix(rotated[0], ".gz")
	if _, err := os.Stat(filepath.Join(dir, uncompressed)); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("uncompressed sibling still exists; stat err=%v", err)
	}
}

func TestRotator_EmptyPathRejected(t *testing.T) {
	t.Parallel()
	if _, err := NewRotator(""); !errors.Is(err, ErrInvalidPath) {
		t.Errorf("err = %v; want ErrInvalidPath", err)
	}
}

func TestRotator_CloseIdempotent(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "app.log")

	r, err := NewRotator(path)
	if err != nil {
		t.Fatalf("NewRotator: %v", err)
	}
	if err := r.Close(); err != nil {
		t.Errorf("first Close: %v", err)
	}
	if err := r.Close(); err != nil {
		t.Errorf("second Close: %v", err)
	}
	if _, err := r.Write([]byte("x")); !errors.Is(err, ErrRotatorClosed) {
		t.Errorf("Write after Close err = %v; want ErrRotatorClosed", err)
	}
}

func TestRotator_ConcurrentWrites(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "app.log")

	r, err := NewRotator(path)
	if err != nil {
		t.Fatalf("NewRotator: %v", err)
	}
	defer func() { _ = r.Close() }()

	const goroutines = 16
	const writesPerG = 32
	var wg sync.WaitGroup
	var errCount atomic.Int32
	wg.Add(goroutines)
	for g := 0; g < goroutines; g++ {
		go func(gid int) {
			defer wg.Done()
			for i := 0; i < writesPerG; i++ {
				if _, werr := r.Write([]byte(fmt.Sprintf("g%d-i%d\n", gid, i))); werr != nil {
					errCount.Add(1)
				}
			}
		}(g)
	}
	wg.Wait()

	if got := errCount.Load(); got != 0 {
		t.Errorf("Write errors: %d", got)
	}

	// All goroutines+ops should be present in the file (the rotator
	// is not configured to rotate, so every line lands in `path`).
	content, _ := os.ReadFile(path)
	gotLines := strings.Count(string(content), "\n")
	wantLines := goroutines * writesPerG
	if gotLines != wantLines {
		t.Errorf("line count = %d; want %d", gotLines, wantLines)
	}
}

func TestRotator_EmptyFileDoesNotRotate(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "app.log")

	// maxBytes=1, age=0 ns. Even a Write of 0-length data must not
	// rotate an empty file.
	clock := &mockClock{t: time.Now()}
	r, err := NewRotator(path,
		WithRotateMaxBytes(1),
		WithRotateMaxAge(time.Nanosecond),
		WithRotateClock(clock.Now),
	)
	if err != nil {
		t.Fatalf("NewRotator: %v", err)
	}
	defer func() { _ = r.Close() }()

	if _, err := r.Write([]byte("a")); err != nil {
		t.Fatalf("first Write: %v", err)
	}

	rotated := listRotated(t, dir, "app.log")
	if len(rotated) != 0 {
		t.Errorf("rotated before any threshold could meaningfully trigger: %v", rotated)
	}
}

// listRotated returns the names of rotated siblings of basename in
// dir. Sorted ascending for deterministic comparisons.
func listRotated(t *testing.T, dir, basename string) []string {
	t.Helper()
	dirents, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	prefix := basename + "."
	var out []string
	for _, e := range dirents {
		if e.IsDir() || e.Name() == basename {
			continue
		}
		if strings.HasPrefix(e.Name(), prefix) {
			out = append(out, e.Name())
		}
	}
	sort.Strings(out)
	return out
}
