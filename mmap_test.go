package fs

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestMmap_RoundTrip(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "data.bin")
	body := []byte("hello mmap")
	if err := os.WriteFile(path, body, 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	m, err := Mmap(path)
	if err != nil {
		t.Fatalf("Mmap: %v", err)
	}
	defer func() { _ = m.Close() }()

	if got := m.Data(); string(got) != string(body) {
		t.Errorf("Data = %q; want %q", got, body)
	}
	if m.Len() != len(body) {
		t.Errorf("Len = %d; want %d", m.Len(), len(body))
	}
}

func TestMmap_EmptyFileRejected(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "empty")
	if err := os.WriteFile(path, nil, 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if _, err := Mmap(path); err == nil {
		t.Error("expected error mmap'ing empty file")
	}
}

func TestMmap_CloseIdempotent(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "d")
	if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	m, _ := Mmap(path)
	if err := m.Close(); err != nil {
		t.Errorf("first Close: %v", err)
	}
	if err := m.Close(); err != nil {
		t.Errorf("second Close: %v", err)
	}
}

func TestMmap_DirRejected(t *testing.T) {
	t.Parallel()
	if _, err := Mmap(t.TempDir()); !errors.Is(err, ErrIsDir) {
		t.Errorf("err = %v; want ErrIsDir", err)
	}
}
