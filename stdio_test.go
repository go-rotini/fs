package fs

import (
	"errors"
	"io"
	"os"
	"strings"
	"testing"
)

// withStdin replaces os.Stdin with the given reader-side of a pipe
// for the duration of fn, then restores. Pipe-mutation tests must
// not be run in parallel with each other.
func withStdin(t *testing.T, content string, fn func()) {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}

	orig := os.Stdin
	os.Stdin = r
	defer func() { os.Stdin = orig }()

	go func() {
		defer w.Close()
		_, _ = io.WriteString(w, content)
	}()

	fn()
	_ = r.Close()
}

// withStdout replaces os.Stdout with the writer-side of a pipe and
// returns whatever was written, plus restores after fn.
func withStdout(t *testing.T, fn func() error) (string, error) {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}

	orig := os.Stdout
	os.Stdout = w
	defer func() { os.Stdout = orig }()

	done := make(chan struct {
		out []byte
		err error
	})
	go func() {
		out, rerr := io.ReadAll(r)
		done <- struct {
			out []byte
			err error
		}{out, rerr}
	}()

	werr := fn()
	_ = w.Close()
	res := <-done
	if res.err != nil && werr == nil {
		t.Fatalf("ReadAll: %v", res.err)
	}
	return string(res.out), werr
}

// --- ReadStdin ---

func TestReadStdin_Basic(t *testing.T) {
	want := "hello stdin\n"
	withStdin(t, want, func() {
		got, err := ReadStdin()
		if err != nil {
			t.Fatalf("ReadStdin: %v", err)
		}
		if string(got) != want {
			t.Errorf("got %q, want %q", got, want)
		}
	})
}

func TestReadStdin_BoundedByMaxSize(t *testing.T) {
	// 1KiB of data, cap at 256 bytes; must error.
	withStdin(t, strings.Repeat("x", 1024), func() {
		_, err := ReadStdin(WithMaxSize(256))
		if !errors.Is(err, ErrFileTooLarge) {
			t.Errorf("got %v, want ErrFileTooLarge", err)
		}
	})
}

// --- OpenStdinLines ---

func TestOpenStdinLines(t *testing.T) {
	withStdin(t, "alpha\nbeta\r\ngamma\n", func() {
		seq := OpenStdinLines()
		var got []string
		for line := range seq {
			got = append(got, line)
		}
		want := []string{"alpha", "beta", "gamma"}
		if len(got) != len(want) {
			t.Fatalf("got %d lines, want 3: %v", len(got), got)
		}
		for i := range want {
			if got[i] != want[i] {
				t.Errorf("line[%d] = %q, want %q", i, got[i], want[i])
			}
		}
	})
}

// --- WriteStdout ---

func TestWriteStdout_Basic(t *testing.T) {
	got, err := withStdout(t, func() error {
		return WriteStdout([]byte("hello\n"))
	})
	if err != nil {
		t.Fatalf("WriteStdout: %v", err)
	}
	if got != "hello\n" {
		t.Errorf("captured = %q, want %q", got, "hello\n")
	}
}

func TestWriteStdout_BrokenPipe(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	// Close the read side first so the write side will EPIPE on next write.
	_ = r.Close()

	orig := os.Stdout
	os.Stdout = w
	defer func() { os.Stdout = orig; _ = w.Close() }()

	werr := WriteStdout([]byte("payload"))
	if !errors.Is(werr, ErrBrokenPipe) {
		t.Errorf("got %v, want ErrBrokenPipe", werr)
	}
}

// --- WriteStderr ---

func TestWriteStderr_Basic(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	orig := os.Stderr
	os.Stderr = w

	done := make(chan []byte)
	go func() {
		buf, _ := io.ReadAll(r)
		done <- buf
	}()

	werr := WriteStderr([]byte("err\n"))
	os.Stderr = orig
	_ = w.Close()
	got := <-done

	if werr != nil {
		t.Fatalf("WriteStderr: %v", werr)
	}
	if string(got) != "err\n" {
		t.Errorf("got %q, want %q", got, "err\n")
	}
}

// --- IsTerminal ---

func TestIsTerminal_FalseForRegularFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := dir + "/f"
	if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer f.Close()
	if IsTerminal(f) {
		t.Errorf("regular file reported as terminal: %s", path)
	}
}

func TestIsTerminal_FalseForPipe(t *testing.T) {
	t.Parallel()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	defer r.Close()
	defer w.Close()
	if IsTerminal(r) || IsTerminal(w) {
		t.Error("pipe reported as terminal")
	}
}

func TestIsTerminal_NilSafe(t *testing.T) {
	t.Parallel()
	if IsTerminal(nil) {
		t.Error("nil should not be reported as terminal")
	}
}
