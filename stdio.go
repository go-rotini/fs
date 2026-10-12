package fs

import (
	"bufio"
	"io"
	"iter"
	"os"
)

const (
	opReadStdin   = "readstdin"
	opWriteStdout = "writestdout"
	opWriteStderr = "writestderr"
)

// ReadStdin reads [os.Stdin] to EOF, capped by [WithMaxSize] (default
// [DefaultMaxReadSize]). Useful for `cat | mytool` patterns where
// the input has no path.
//
// A stdin stream exceeding the cap returns an error wrapping
// [ErrFileTooLarge].
func ReadStdin(opts ...ReadOption) ([]byte, error) {
	cfg := newReadOptions(opts)

	var reader io.Reader = os.Stdin
	if cfg.maxSize > 0 {
		reader = io.LimitReader(os.Stdin, cfg.maxSize+1)
	}
	data, err := io.ReadAll(reader)
	if err != nil {
		return nil, wrapPathError(opReadStdin, "stdin", err)
	}
	if cfg.maxSize > 0 && int64(len(data)) > cfg.maxSize {
		return nil, wrapPathError(opReadStdin, "stdin", ErrFileTooLarge)
	}
	return data, nil
}

// OpenStdinLines returns an iterator over [os.Stdin] lines, with each
// line's trailing LF or CRLF stripped. The iterator runs until EOF; a
// read error or a line longer than 64 KiB ends it silently.
//
// Stdin cannot be reopened, so the lines can be consumed only once.
// There is no cleanup function; the caller owns os.Stdin.
func OpenStdinLines() iter.Seq[string] {
	scanner := bufio.NewScanner(os.Stdin)
	return func(yield func(string) bool) {
		for scanner.Scan() {
			if !yield(scanner.Text()) {
				return
			}
		}
	}
}

// WriteStdout writes data to [os.Stdout]. A write that fails because
// the consumer closed the pipe early (EPIPE on POSIX, ERROR_NO_DATA
// or ERROR_BROKEN_PIPE on Windows; typical when piping to `head`) is
// reported as [ErrBrokenPipe], which callers conventionally treat as
// a quiet exit.
//
// On POSIX the Go runtime kills the process with SIGPIPE on such a
// write to stdout or stderr unless the program handles SIGPIPE (for
// example with [os/signal.Ignore]); only then does the error reach
// the caller.
func WriteStdout(data []byte) error {
	return writeFD(opWriteStdout, "stdout", os.Stdout, data)
}

// WriteStderr is the [os.Stderr] analog of [WriteStdout].
func WriteStderr(data []byte) error {
	return writeFD(opWriteStderr, "stderr", os.Stderr, data)
}

func writeFD(op, name string, w io.Writer, data []byte) error {
	if _, err := w.Write(data); err != nil {
		if isBrokenPipeErr(err) {
			return wrapPathError(op, name, ErrBrokenPipe)
		}
		return wrapPathError(op, name, err)
	}
	return nil
}

// IsTerminal reports whether f is connected to a terminal. Useful
// for callers deciding between human-readable and machine-readable
// output. Returns false for nil f.
func IsTerminal(f *os.File) bool {
	if f == nil {
		return false
	}
	return isTerminal(f)
}
