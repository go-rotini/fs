package fs

import (
	"path/filepath"
	"sort"
	"testing"
)

func TestProjectType_Go(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "go.mod"), "module foo\n")
	got, err := ProjectType(root)
	if err != nil {
		t.Fatalf("ProjectType: %v", err)
	}
	if len(got) != 1 || got[0] != ProjectKindGo {
		t.Errorf("got = %v; want [go]", got)
	}
}

func TestProjectType_Monorepo(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "go.mod"), "module foo\n")
	mustWrite(t, filepath.Join(root, "package.json"), `{"name":"foo"}`)
	mustWrite(t, filepath.Join(root, "Cargo.toml"), "[package]\n")

	got, err := ProjectType(root)
	if err != nil {
		t.Fatalf("ProjectType: %v", err)
	}
	want := []ProjectKind{ProjectKindGo, ProjectKindNode, ProjectKindRust}
	sort.Slice(got, func(i, j int) bool { return string(got[i]) < string(got[j]) })
	if !equalProjectKinds(got, want) {
		t.Errorf("got = %v; want %v", got, want)
	}
}

func TestProjectType_Dotnet(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "app.csproj"), "<Project/>")
	got, err := ProjectType(root)
	if err != nil {
		t.Fatalf("ProjectType: %v", err)
	}
	if len(got) != 1 || got[0] != ProjectKindDotnet {
		t.Errorf("got = %v; want [dotnet]", got)
	}
}

func TestProjectType_Empty(t *testing.T) {
	t.Parallel()
	got, err := ProjectType(t.TempDir())
	if err != nil {
		t.Fatalf("ProjectType: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("got = %v; want empty", got)
	}
}

func equalProjectKinds(a, b []ProjectKind) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
