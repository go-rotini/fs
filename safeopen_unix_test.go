//go:build unix

package fs

import (
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/unix"
)

func TestSafeOpen_CloseOnExec(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	p := filepath.Join(dir, "f")
	if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	f, err := OpenNoFollow(p, os.O_RDONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	d, err := os.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	g, err := OpenAt(d, "f", os.O_RDONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer g.Close()
	h, err := Lock(filepath.Join(dir, "lock"))
	if err != nil {
		t.Fatal(err)
	}
	defer h.Release()
	for name, fd := range map[string]uintptr{"OpenNoFollow": f.Fd(), "OpenAt": g.Fd(), "Lock": h.f.Fd()} {
		flags, err := unix.FcntlInt(fd, unix.F_GETFD, 0)
		if err != nil {
			t.Fatal(err)
		}
		if flags&unix.FD_CLOEXEC == 0 {
			t.Errorf("%s: descriptor is inherited by child processes (no FD_CLOEXEC)", name)
		}
	}
}
