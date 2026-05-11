package fs

import (
	"bufio"
	"context"
	"errors"
	"io"
	stdfs "io/fs"
	"iter"
	"os"
	"strings"
	"time"
)

// ErrTailRotated is yielded by [Tail]'s iterator each time it
// detects a rotation (rename-style or in-place truncation) and
// reopens the underlying file — but only when the caller opts in
// via [WithTailNotifyRotation]. By default rotation is transparent.
//
// The yield is `(zero-value-string, ErrTailRotated)`; the iterator
// continues afterwards. Callers who pattern-match on this can log
// "log file rotated" or flush partial state. errors.Is recognizes
// it.
var ErrTailRotated = errors.New("fs: tail: file rotated")

const (
	opTail = "tail"

	// tailDefaultPollInterval is how long Tail sleeps between size
	// checks when sitting at EOF. 200 ms is a good balance between
	// responsiveness for interactive `cli logs -f` UX and not pinning
	// a CPU core.
	tailDefaultPollInterval = 200 * time.Millisecond

	// tailDefaultBufferSize sizes the underlying bufio.Reader. 4 KiB
	// matches the default page size on every supported platform and
	// is small enough to keep memory negligible across many tailed
	// files.
	tailDefaultBufferSize = 4096

	// tailMaxLineBytes caps the length of a single line. Lines longer
	// than this are truncated at the cap; the truncated chunk is
	// yielded immediately so we never accumulate unbounded state for
	// an adversarial input. 1 MiB is well above any realistic log
	// line and protects callers tailing untrusted sources.
	tailMaxLineBytes = 1 << 20
)

// TailOption configures a [Tail] call.
type TailOption func(*tailConfig)

// WithTailFromStart reads path from offset 0 on the first open
// instead of seeking to EOF. Subsequent rotation-triggered reopens
// always start at offset 0 regardless of this option.
func WithTailFromStart() TailOption {
	return func(c *tailConfig) {
		c.fromStart = true
	}
}

// WithTailPollInterval sets the cadence at which [Tail] checks for
// new content after hitting EOF. Default is 200ms. Values below 10ms
// are clamped to 10ms to avoid busy-loop polling.
func WithTailPollInterval(d time.Duration) TailOption {
	return func(c *tailConfig) {
		c.pollInterval = d
	}
}

// WithTailBufferSize sets the size of the read buffer in bytes.
// Default is 4096 bytes. Values <= 0 use the default.
func WithTailBufferSize(n int) TailOption {
	return func(c *tailConfig) {
		c.bufSize = n
	}
}

// WithTailNotifyRotation makes [Tail] yield ("", [ErrTailRotated])
// once each time it detects a rotation and reopens the file. Off
// by default — rotations are transparent unless the caller asks.
func WithTailNotifyRotation() TailOption {
	return func(c *tailConfig) {
		c.notifyRotation = true
	}
}

// tailConfig holds Tail's normalized options.
type tailConfig struct {
	fromStart      bool
	pollInterval   time.Duration
	bufSize        int
	notifyRotation bool
}

// Tail returns an iterator over lines appended to path. The iterator
// starts at EOF (or at offset 0 if [WithTailFromStart] is set) and
// blocks until new content is written, the file is rotated, or ctx
// is canceled.
//
// ctx must not be nil. Pass [context.Background] for an indefinite
// tail. Following stdlib convention, a nil ctx panics on first poll.
//
// Rotation handling: between reads, [Tail] stats path and compares
// the result against the held file descriptor via [os.SameFile]. If
// the path now resolves to a different inode (logrotate-style rename-
// and-create) or the held file's size has shrunk below the read
// offset (in-place truncation), [Tail] reopens path from offset 0
// and continues yielding.
//
// On ctx.Done(), the iterator terminates without yielding an error.
// On an unrecoverable IO error (path becomes permanently unreadable
// after rotation, etc.), the iterator yields (zero-value, err) once
// and terminates.
//
// Lines are yielded without their trailing newline; both LF and CRLF
// are stripped. The line length is capped at 1 MiB; lines longer
// than the cap are truncated and yielded as separate chunks.
//
// Usage:
//
//	for line, err := range fs.Tail(ctx, "/var/log/foo.log") {
//	    if err != nil {
//	        log.Printf("tail: %v", err)
//	        break
//	    }
//	    handle(line)
//	}
func Tail(ctx context.Context, path string, opts ...TailOption) iter.Seq2[string, error] {
	cfg := tailConfig{
		pollInterval: tailDefaultPollInterval,
		bufSize:      tailDefaultBufferSize,
	}
	for _, opt := range opts {
		opt(&cfg)
	}
	if cfg.pollInterval < 10*time.Millisecond {
		cfg.pollInterval = 10 * time.Millisecond
	}
	if cfg.bufSize <= 0 {
		cfg.bufSize = tailDefaultBufferSize
	}

	return func(yield func(string, error) bool) {
		runTail(ctx, path, cfg, yield)
	}
}

// runTail is the iterator body, split out so it isn't nested 5 deep
// inside Tail's closure.
func runTail(ctx context.Context, path string, cfg tailConfig, yield func(string, error) bool) {
	f, info, err := openTailFile(path, cfg.fromStart)
	if err != nil {
		yield("", err)
		return
	}
	defer func() { _ = f.Close() }()

	reader := bufio.NewReaderSize(f, cfg.bufSize)
	var partial strings.Builder

	// One reusable timer drives the poll loop. We never observe its
	// channel before resetting, so the standard NewTimer pattern
	// (stop+drain on early exit) keeps zero goroutines and timers
	// outstanding regardless of how the iterator terminates.
	timer := time.NewTimer(cfg.pollInterval)
	defer timer.Stop()

	for {
		// Drain whatever's currently readable. We stop at EOF inside
		// drainLines and fall through to the poll/rotation check.
		stop, derr := drainLines(reader, &partial, yield)
		if stop {
			return
		}
		if derr != nil {
			yield("", wrapPathError(opTail, path, derr))
			return
		}

		// Sleep until the next poll or ctx-cancel.
		if !timer.Stop() {
			select {
			case <-timer.C:
			default:
			}
		}
		timer.Reset(cfg.pollInterval)
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
		}

		// Detect rotation/truncation. If detected, swap f + reader and
		// flush any partial line accumulator (the truncation event
		// invalidates whatever was mid-line).
		rotated, newF, newInfo, rerr := checkRotation(path, f, info)
		if rerr != nil {
			yield("", wrapPathError(opTail, path, rerr))
			return
		}
		if rotated {
			_ = f.Close()
			f = newF
			info = newInfo
			reader = bufio.NewReaderSize(f, cfg.bufSize)
			partial.Reset()
			if cfg.notifyRotation && !yield("", ErrTailRotated) {
				return
			}
		}
	}
}

// openTailFile opens path and seeks to EOF unless fromStart is set.
// Returns the open file plus its stat snapshot for rotation
// comparisons.
func openTailFile(path string, fromStart bool) (*os.File, stdfs.FileInfo, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, nil, wrapPathError(opTail, path, err)
	}
	info, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return nil, nil, wrapPathError(opTail, path, err)
	}
	if !fromStart {
		if _, err := f.Seek(0, io.SeekEnd); err != nil {
			_ = f.Close()
			return nil, nil, wrapPathError(opTail, path, err)
		}
	}
	return f, info, nil
}

// drainLines reads from r until EOF. Completed lines are yielded
// (without trailing CR/LF); a trailing partial line accumulates in
// the partial buffer. Returns stop=true when the consumer broke out
// of the loop. A non-nil error is from the read itself (other than
// io.EOF).
func drainLines(r *bufio.Reader, partial *strings.Builder, yield func(string, error) bool) (stop bool, err error) {
	for {
		chunk, rerr := r.ReadString('\n')
		if chunk != "" {
			if strings.HasSuffix(chunk, "\n") {
				partial.WriteString(chunk[:len(chunk)-1])
				line := strings.TrimRight(partial.String(), "\r")
				partial.Reset()
				if !yield(line, nil) {
					return true, nil
				}
			} else {
				// EOF in the middle of a line — keep accumulating.
				partial.WriteString(chunk)
			}
		}

		// Cap the in-flight partial so an adversarial writer that
		// never emits a newline cannot blow memory. Yield a cap-sized
		// chunk and continue accumulating any overflow.
		for partial.Len() >= tailMaxLineBytes {
			s := partial.String()
			head := strings.TrimRight(s[:tailMaxLineBytes], "\r")
			rest := s[tailMaxLineBytes:]
			partial.Reset()
			partial.WriteString(rest)
			if !yield(head, nil) {
				return true, nil
			}
		}

		if errors.Is(rerr, io.EOF) {
			return false, nil
		}
		if rerr != nil {
			return false, rerr //nolint:wrapcheck // wrapped by caller via *PathError
		}
	}
}

// checkRotation reports whether path now resolves to a different
// inode than f, or whether f has been truncated to a shorter length.
// On rotation, returns the freshly-opened file + its stat.
func checkRotation(path string, f *os.File, prev stdfs.FileInfo) (rotated bool, newF *os.File, newInfo stdfs.FileInfo, err error) {
	pathInfo, statErr := os.Stat(path)
	if statErr != nil {
		// Path vanished or briefly unreadable. logrotate sometimes
		// renames-without-create; treat ErrNotExist as "wait for the
		// path to come back" rather than a fatal error.
		if errors.Is(statErr, stdfs.ErrNotExist) {
			return false, nil, nil, nil
		}
		return false, nil, nil, statErr //nolint:wrapcheck // wrapped by caller
	}

	// In-place truncation: same inode but smaller than the held f's
	// size. We compare against the held f's current end-of-file
	// position, not the original prev.Size(), since drainLines has
	// already advanced f.
	if os.SameFile(prev, pathInfo) {
		curSize, sErr := f.Seek(0, io.SeekCurrent)
		if sErr != nil {
			return false, nil, nil, sErr //nolint:wrapcheck // wrapped by caller
		}
		if pathInfo.Size() < curSize {
			fr, ri, rerr := openTailFile(path, true)
			if rerr != nil {
				return false, nil, nil, rerr
			}
			return true, fr, ri, nil
		}
		return false, nil, nil, nil
	}

	// Different inode → rotation. Reopen at the new path.
	fr, ri, rerr := openTailFile(path, true)
	if rerr != nil {
		return false, nil, nil, rerr
	}
	return true, fr, ri, nil
}
