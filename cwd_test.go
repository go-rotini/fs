package fs

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// cwd-touching tests run serially; the process cwd is global state.

func TestCwd(t *testing.T) {
	got, err := Cwd()
	if err != nil {
		t.Fatalf("Cwd: %v", err)
	}
	want, err := os.Getwd()
	if err != nil {
		t.Fatalf("os.Getwd: %v", err)
	}
	if got != want {
		t.Errorf("Cwd = %s, want %s", got, want)
	}
}

func TestChdir_AndRestore(t *testing.T) {
	orig, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}

	dir := t.TempDir()
	// t.Chdir is called after t.TempDir so its cwd-restore cleanup is
	// registered last and therefore runs first (LIFO). On Windows a
	// directory that is the process cwd cannot be removed, so the
	// restore must happen before t.TempDir's RemoveAll cleanup.
	t.Chdir(orig)

	resolvedDir, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatalf("EvalSymlinks: %v", err)
	}

	if err := Chdir(dir); err != nil {
		t.Fatalf("Chdir: %v", err)
	}
	got, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	resolvedGot, err := filepath.EvalSymlinks(got)
	if err != nil {
		t.Fatalf("EvalSymlinks: %v", err)
	}
	if resolvedGot != resolvedDir {
		t.Errorf("after Chdir, cwd = %s, want %s", resolvedGot, resolvedDir)
	}
}

func TestChdir_Missing(t *testing.T) {
	dir := t.TempDir()
	err := Chdir(filepath.Join(dir, "missing"))
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("got %v, want ErrNotFound", err)
	}
}

func TestWithDir_RestoresOnSuccess(t *testing.T) {
	orig, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	dir := t.TempDir()
	resolvedDir, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatalf("EvalSymlinks: %v", err)
	}

	var inside string
	werr := WithDir(dir, func() error {
		got, gerr := os.Getwd()
		if gerr != nil {
			return gerr
		}
		inside = got
		return nil
	})
	if werr != nil {
		t.Fatalf("WithDir: %v", werr)
	}
	resolvedInside, err := filepath.EvalSymlinks(inside)
	if err != nil {
		t.Fatalf("EvalSymlinks: %v", err)
	}
	if resolvedInside != resolvedDir {
		t.Errorf("inside fn, cwd = %s, want %s", resolvedInside, resolvedDir)
	}
	after, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	if after != orig {
		t.Errorf("after WithDir, cwd = %s, want %s", after, orig)
	}
}

func TestWithDir_RestoresOnError(t *testing.T) {
	orig, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	sentinel := errors.New("fn error")

	werr := WithDir(t.TempDir(), func() error { return sentinel })
	if !errors.Is(werr, sentinel) {
		t.Errorf("got %v, want sentinel", werr)
	}
	after, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	if after != orig {
		t.Errorf("after WithDir(error), cwd = %s, want %s", after, orig)
	}
}

func TestWithDir_RestoresOnPanic(t *testing.T) {
	orig, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	dir := t.TempDir()

	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic to propagate")
		}
		after, gerr := os.Getwd()
		if gerr != nil {
			t.Fatalf("Getwd: %v", gerr)
		}
		if after != orig {
			t.Errorf("after panic, cwd = %s, want %s", after, orig)
		}
	}()

	_ = WithDir(dir, func() error {
		panic("boom")
	})
}

func TestWithDir_BadPath(t *testing.T) {
	orig, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	dir := t.TempDir()
	werr := WithDir(filepath.Join(dir, "missing"), func() error { return nil })
	if !errors.Is(werr, ErrNotFound) {
		t.Errorf("got %v, want ErrNotFound", werr)
	}
	after, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	if after != orig {
		t.Errorf("cwd should be unchanged when target is bad: got %s, want %s", after, orig)
	}
}
