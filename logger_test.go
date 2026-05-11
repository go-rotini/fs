package fs

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
)

func TestSetLogger(t *testing.T) {
	// Not parallel: SetLogger mutates package-global state.

	original := pkgLogger.Load()
	t.Cleanup(func() {
		pkgLogger.Store(original)
	})

	var buf bytes.Buffer
	SetLogger(slog.New(slog.NewTextHandler(&buf, nil)))
	logger().Info("hello", "k", "v")
	if !strings.Contains(buf.String(), "hello") {
		t.Errorf("installed logger didn't see the record: %q", buf.String())
	}

	// Pass nil to restore the discard sink.
	SetLogger(nil)
	buf.Reset()
	logger().Info("dropped")
	if buf.Len() != 0 {
		t.Errorf("discard logger leaked output: %q", buf.String())
	}
}

func TestLoggerDefaultIsDiscard(t *testing.T) {
	// Discard logger must not panic or return nil.
	l := logger()
	if l == nil {
		t.Fatal("logger() returned nil")
	}
	l.Info("works", "k", 1)
}
