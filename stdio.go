package fs

import (
	"bufio"
	"errors"
	"io"
	"iter"
	"os"
	"syscall"
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
// A stdin stream exceeding the cap returns [ErrFileTooLarge].
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

// OpenStdinLines returns an iterator over [os.Stdin] lines.
// Trailing line endings (LF / CRLF / CR) are stripped per line, per
// [bufio.Scanner.Text]. The iterator continues until EOF.
//
// Stdin can't be reopened, so the returned iterator can only be
// consumed once per process. There is no cleanup function; the
// caller's lifecycle owns os.Stdin.
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

// WriteStdout writes data to [os.Stdout]. A write that fails with
// [syscall.EPIPE] (typical when the consumer is `head` or another
// tool that closes its input early) is reported as [ErrBrokenPipe],
// which callers conventionally translate to a silent process exit
// (matching `head`-friendly Unix idiom).
func WriteStdout(data []byte) error {
	return writeFD(opWriteStdout, "stdout", os.Stdout, data)
}

// WriteStderr is the [os.Stderr] analog of [WriteStdout].
func WriteStderr(data []byte) error {
	return writeFD(opWriteStderr, "stderr", os.Stderr, data)
}

func writeFD(op, name string, w io.Writer, data []byte) error {
	if _, err := w.Write(data); err != nil {
		if errors.Is(err, syscall.EPIPE) {
			return wrapPathError(op, name, ErrBrokenPipe)
		}
		return wrapPathError(op, name, err)
	}
	return nil
}

// IsTerminal reports whether f is connected to a terminal. Useful
// for callers deciding between human-readable and machine-readable
// output. The fs package exposes only this primitive; pager
// invocation, color decisions, and width queries belong in the
// future rotini/cli framework.
//
// Returns false for nil f.
func IsTerminal(f *os.File) bool {
	if f == nil {
		return false
	}
	return isTerminal(f)
}
