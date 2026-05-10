//go:build !windows

package fs

import (
	"net"
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

func TestIsFIFO(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	fifo := filepath.Join(dir, "pipe")
	if err := syscall.Mkfifo(fifo, 0o600); err != nil {
		t.Fatalf("Mkfifo: %v", err)
	}
	if !IsFIFO(fifo) {
		t.Errorf("IsFIFO(fifo) = false")
	}

	regular := filepath.Join(dir, "f")
	if err := os.WriteFile(regular, []byte("x"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if IsFIFO(regular) {
		t.Errorf("IsFIFO(regular) = true")
	}
}

func TestIsSocket(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	socket := filepath.Join(dir, "s")
	l, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatalf("net.Listen unix: %v", err)
	}
	defer func() { _ = l.Close() }()
	if !IsSocket(socket) {
		t.Errorf("IsSocket(socket) = false")
	}
}

func TestIsCharDevice_DevNull(t *testing.T) {
	t.Parallel()
	if !IsCharDevice("/dev/null") {
		t.Error("IsCharDevice(/dev/null) = false")
	}
	dir := t.TempDir()
	regular := filepath.Join(dir, "f")
	if err := os.WriteFile(regular, []byte("x"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if IsCharDevice(regular) {
		t.Errorf("IsCharDevice(regular) = true")
	}
}
