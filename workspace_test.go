package fs

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestWorkspaceRoots_GoWork(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	mustMkdir(t, filepath.Join(root, "svc-a"))
	mustMkdir(t, filepath.Join(root, "svc-b"))
	mustWrite(t, filepath.Join(root, "go.work"), `go 1.22

use (
    ./svc-a
    ./svc-b
)
`)
	got, err := WorkspaceRoots(root)
	if err != nil {
		t.Fatalf("WorkspaceRoots: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("len = %d; want 2: %+v", len(got), got)
	}
	for _, w := range got {
		if w.Kind != "go.work" {
			t.Errorf("Kind = %q; want go.work", w.Kind)
		}
		if !strings.HasPrefix(w.Path, root) {
			t.Errorf("Path %q not under root %q", w.Path, root)
		}
	}
}

func TestWorkspaceRoots_NpmArrayForm(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	mustMkdir(t, filepath.Join(root, "packages", "alpha"))
	mustMkdir(t, filepath.Join(root, "packages", "beta"))
	mustWrite(t, filepath.Join(root, "package.json"), `{"workspaces":["packages/*"]}`)

	got, err := WorkspaceRoots(root)
	if err != nil {
		t.Fatalf("WorkspaceRoots: %v", err)
	}
	if len(got) < 2 {
		t.Errorf("len = %d; want >= 2: %+v", len(got), got)
	}
}

func TestWorkspaceRoots_NpmObjectForm(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	mustMkdir(t, filepath.Join(root, "apps", "web"))
	mustWrite(t, filepath.Join(root, "package.json"), `{"workspaces":{"packages":["apps/web"]}}`)

	got, err := WorkspaceRoots(root)
	if err != nil {
		t.Fatalf("WorkspaceRoots: %v", err)
	}
	if len(got) != 1 || filepath.Base(got[0].Path) != "web" {
		t.Errorf("got = %+v; want one workspace named 'web'", got)
	}
}

func TestWorkspaceRoots_Pnpm(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	mustMkdir(t, filepath.Join(root, "packages", "x"))
	mustWrite(t, filepath.Join(root, "pnpm-workspace.yaml"), "packages:\n  - 'packages/*'\n")
	got, err := WorkspaceRoots(root)
	if err != nil {
		t.Fatalf("WorkspaceRoots: %v", err)
	}
	if len(got) != 1 || got[0].Kind != "pnpm-workspace.yaml" {
		t.Errorf("got = %+v; want one pnpm-workspace root", got)
	}
}

func TestWorkspaceRoots_None(t *testing.T) {
	t.Parallel()
	got, err := WorkspaceRoots(t.TempDir())
	if err != nil {
		t.Fatalf("WorkspaceRoots: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("got = %+v; want empty", got)
	}
}
