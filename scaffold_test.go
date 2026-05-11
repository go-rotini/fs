package fs

import (
	"errors"
	stdfs "io/fs"
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"
)

// makeScaffoldSrc returns a small in-memory io/fs.FS suitable for
// driving scaffold tests. Uses testing/fstest from stdlib.
func makeScaffoldSrc() stdfs.FS {
	return fstest.MapFS{
		"README.md":             {Data: []byte("# {{.Name}}\n\nproject for {{.Owner}}\n")},
		"src/{{.Name}}.go":      {Data: []byte("package {{.Name}}\n")},
		"src/util/util.go":      {Data: []byte("package util\n")},
		"config.yaml":           {Data: []byte("name: {{.Name}}\nport: 8080\n")},
		"static/empty/.gitkeep": {Data: []byte("")},
	}
}

type scaffoldVars struct {
	Name  string
	Owner string
}

// --- ScaffoldPlan ---

func TestScaffoldPlan_AllCreate(t *testing.T) {
	t.Parallel()
	dst := t.TempDir()

	plan, err := ScaffoldPlan(makeScaffoldSrc(), dst, scaffoldVars{Name: "myapp", Owner: "alice"})
	if err != nil {
		t.Fatalf("ScaffoldPlan: %v", err)
	}
	if len(plan) == 0 {
		t.Fatal("empty plan")
	}
	for _, a := range plan {
		if a.Op != ScaffoldActionCreate {
			t.Errorf("entry %s: op=%s, want create", a.SrcPath, a.Op)
		}
	}
}

func TestScaffoldPlan_RendersPathTemplates(t *testing.T) {
	t.Parallel()
	dst := t.TempDir()

	plan, err := ScaffoldPlan(makeScaffoldSrc(), dst, scaffoldVars{Name: "myapp"})
	if err != nil {
		t.Fatalf("ScaffoldPlan: %v", err)
	}
	found := false
	for _, a := range plan {
		if a.SrcPath == "src/{{.Name}}.go" {
			if filepath.Base(a.DstPath) != "myapp.go" {
				t.Errorf("path template not rendered: dst=%s", a.DstPath)
			}
			found = true
		}
	}
	if !found {
		t.Error("template-named source entry not found in plan")
	}
}

// --- ScaffoldApply ---

func TestScaffoldApply_WritesRenderedFiles(t *testing.T) {
	t.Parallel()
	dst := t.TempDir()
	if err := ScaffoldApply(makeScaffoldSrc(), dst, scaffoldVars{Name: "myapp", Owner: "alice"}); err != nil {
		t.Fatalf("ScaffoldApply: %v", err)
	}

	// README.md content rendered.
	readme, err := os.ReadFile(filepath.Join(dst, "README.md"))
	if err != nil {
		t.Fatalf("README.md missing: %v", err)
	}
	if string(readme) != "# myapp\n\nproject for alice\n" {
		t.Errorf("README content not rendered: %q", readme)
	}

	// src/myapp.go (path template applied).
	myapp, err := os.ReadFile(filepath.Join(dst, "src", "myapp.go"))
	if err != nil {
		t.Fatalf("src/myapp.go missing: %v", err)
	}
	if string(myapp) != "package myapp\n" {
		t.Errorf("body not rendered: %q", myapp)
	}
}

func TestScaffoldApply_IdempotentSecondRun(t *testing.T) {
	t.Parallel()
	dst := t.TempDir()
	src := makeScaffoldSrc()
	vars := scaffoldVars{Name: "first", Owner: "alice"}

	if err := ScaffoldApply(src, dst, vars); err != nil {
		t.Fatalf("first apply: %v", err)
	}

	// Second run with DIFFERENT vars; default policy SkipExisting.
	// The README should remain the first-run content, not the second.
	if err := ScaffoldApply(src, dst, scaffoldVars{Name: "second", Owner: "bob"}); err != nil {
		t.Fatalf("second apply: %v", err)
	}
	readme, _ := os.ReadFile(filepath.Join(dst, "README.md"))
	if string(readme) != "# first\n\nproject for alice\n" {
		t.Errorf("default policy did not skip existing: %q", readme)
	}
}

func TestScaffoldApply_OverwriteAll(t *testing.T) {
	t.Parallel()
	dst := t.TempDir()
	src := makeScaffoldSrc()

	if err := ScaffoldApply(src, dst, scaffoldVars{Name: "first", Owner: "alice"}); err != nil {
		t.Fatalf("first apply: %v", err)
	}
	if err := ScaffoldApply(src, dst, scaffoldVars{Name: "second", Owner: "bob"},
		WithScaffoldOnConflict(ScaffoldOverwriteAll)); err != nil {
		t.Fatalf("overwrite apply: %v", err)
	}
	readme, _ := os.ReadFile(filepath.Join(dst, "README.md"))
	if string(readme) != "# second\n\nproject for bob\n" {
		t.Errorf("overwrite did not replace: %q", readme)
	}
}

func TestScaffoldApply_PromptInteractive(t *testing.T) {
	t.Parallel()
	dst := t.TempDir()
	src := makeScaffoldSrc()

	if err := ScaffoldApply(src, dst, scaffoldVars{Name: "first", Owner: "a"}); err != nil {
		t.Fatalf("first apply: %v", err)
	}

	prompts := 0
	prompt := func(_ string, _ ScaffoldAction) ScaffoldActionOp {
		prompts++
		return ScaffoldActionOverwrite
	}
	if err := ScaffoldApply(src, dst, scaffoldVars{Name: "second", Owner: "b"},
		WithScaffoldOnConflict(ScaffoldPromptInteractive),
		WithScaffoldPromptFunc(prompt),
	); err != nil {
		t.Fatalf("prompt apply: %v", err)
	}
	if prompts == 0 {
		t.Error("prompt callback never invoked")
	}
	readme, _ := os.ReadFile(filepath.Join(dst, "README.md"))
	if string(readme) != "# second\n\nproject for b\n" {
		t.Errorf("prompt-overwrite did not apply: %q", readme)
	}
}

func TestScaffoldApply_PromptRequired(t *testing.T) {
	t.Parallel()
	dst := t.TempDir()
	err := ScaffoldApply(makeScaffoldSrc(), dst, scaffoldVars{Name: "n"},
		WithScaffoldOnConflict(ScaffoldPromptInteractive))
	if !errors.Is(err, ErrScaffoldPromptRequired) {
		t.Errorf("got %v, want ErrScaffoldPromptRequired", err)
	}
}

func TestScaffoldApply_TemplateError(t *testing.T) {
	t.Parallel()
	src := fstest.MapFS{
		"bad.txt": {Data: []byte("{{.MissingField}}")},
	}
	dst := t.TempDir()
	err := ScaffoldApply(src, dst, struct{}{})
	if err == nil {
		t.Error("expected template error for missing field")
	}
}

// --- ScaffoldExtract ---

func TestScaffoldExtract_FirstRunWritesAndStampsMarker(t *testing.T) {
	t.Parallel()
	dst := t.TempDir()
	src := fstest.MapFS{
		"a.txt":   {Data: []byte("alpha")},
		"b/c.txt": {Data: []byte("gamma")},
	}

	if err := ScaffoldExtract(src, dst); err != nil {
		t.Fatalf("ScaffoldExtract: %v", err)
	}
	if got, _ := os.ReadFile(filepath.Join(dst, "a.txt")); string(got) != "alpha" {
		t.Errorf("a.txt content: %q", got)
	}
	if got, _ := os.ReadFile(filepath.Join(dst, "b", "c.txt")); string(got) != "gamma" {
		t.Errorf("b/c.txt content: %q", got)
	}
	marker := filepath.Join(dst, defaultScaffoldVersionMarker)
	if !Exists(marker) {
		t.Error("version marker not written")
	}
}

func TestScaffoldExtract_SecondRunSameVersionNoOp(t *testing.T) {
	t.Parallel()
	dst := t.TempDir()
	src := fstest.MapFS{
		"a.txt": {Data: []byte("alpha")},
	}
	if err := ScaffoldExtract(src, dst); err != nil {
		t.Fatalf("first extract: %v", err)
	}
	// Tamper with the file; second extract should NOT touch it
	// because the source version hasn't changed.
	if err := os.WriteFile(filepath.Join(dst, "a.txt"), []byte("user-edit"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if err := ScaffoldExtract(src, dst); err != nil {
		t.Fatalf("second extract: %v", err)
	}
	got, _ := os.ReadFile(filepath.Join(dst, "a.txt"))
	if string(got) != "user-edit" {
		t.Errorf("user edit overwritten on no-op extract: %q", got)
	}
}

func TestScaffoldExtract_VersionChangeReExtracts(t *testing.T) {
	t.Parallel()
	dst := t.TempDir()
	srcV1 := fstest.MapFS{"a.txt": {Data: []byte("v1")}}
	if err := ScaffoldExtract(srcV1, dst); err != nil {
		t.Fatalf("first extract: %v", err)
	}

	srcV2 := fstest.MapFS{"a.txt": {Data: []byte("v2-bumped")}}
	// Default policy (SkipExisting) means changed files are NOT
	// overwritten; force overwrite to verify version detection.
	if err := ScaffoldExtract(srcV2, dst, WithScaffoldOnConflict(ScaffoldOverwriteAll)); err != nil {
		t.Fatalf("re-extract: %v", err)
	}
	got, _ := os.ReadFile(filepath.Join(dst, "a.txt"))
	if string(got) != "v2-bumped" {
		t.Errorf("version-change re-extract did not replace contents: %q", got)
	}
}

func TestScaffoldExtract_CustomVersionMarker(t *testing.T) {
	t.Parallel()
	dst := t.TempDir()
	src := fstest.MapFS{"a.txt": {Data: []byte("x")}}
	if err := ScaffoldExtract(src, dst, WithScaffoldVersionMarker(".myapp.scaffold")); err != nil {
		t.Fatalf("ScaffoldExtract: %v", err)
	}
	if !Exists(filepath.Join(dst, ".myapp.scaffold")) {
		t.Error("custom version marker not written")
	}
	if Exists(filepath.Join(dst, defaultScaffoldVersionMarker)) {
		t.Error("default version marker should not exist when custom name provided")
	}
}

// --- ScaffoldExtract conflict policies ---

func TestScaffoldExtract_OverwriteAll(t *testing.T) {
	t.Parallel()
	dst := t.TempDir()
	srcV1 := fstest.MapFS{"a.txt": {Data: []byte("v1")}}
	if err := ScaffoldExtract(srcV1, dst); err != nil {
		t.Fatalf("first: %v", err)
	}

	// User edits, version bumps, OverwriteAll forces replacement.
	if err := os.WriteFile(filepath.Join(dst, "a.txt"), []byte("user"), 0o644); err != nil {
		t.Fatalf("user edit: %v", err)
	}
	srcV2 := fstest.MapFS{"a.txt": {Data: []byte("v2")}}
	if err := ScaffoldExtract(srcV2, dst, WithScaffoldOnConflict(ScaffoldOverwriteAll)); err != nil {
		t.Fatalf("v2: %v", err)
	}
	if got, _ := os.ReadFile(filepath.Join(dst, "a.txt")); string(got) != "v2" {
		t.Errorf("got %q, want v2", got)
	}
}

func TestScaffoldExtract_PromptInteractive(t *testing.T) {
	t.Parallel()
	dst := t.TempDir()
	srcV1 := fstest.MapFS{"a.txt": {Data: []byte("v1")}}
	if err := ScaffoldExtract(srcV1, dst); err != nil {
		t.Fatalf("first: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dst, "a.txt"), []byte("user-edit"), 0o644); err != nil {
		t.Fatalf("user edit: %v", err)
	}

	srcV2 := fstest.MapFS{"a.txt": {Data: []byte("v2")}}
	prompts := 0
	if err := ScaffoldExtract(srcV2, dst,
		WithScaffoldOnConflict(ScaffoldPromptInteractive),
		WithScaffoldPromptFunc(func(string, ScaffoldAction) ScaffoldActionOp {
			prompts++
			return ScaffoldActionSkip
		}),
	); err != nil {
		t.Fatalf("ScaffoldExtract: %v", err)
	}
	if prompts == 0 {
		t.Error("prompt callback never invoked")
	}
	// Skip means user-edit preserved.
	if got, _ := os.ReadFile(filepath.Join(dst, "a.txt")); string(got) != "user-edit" {
		t.Errorf("got %q, want user-edit (skip preserves)", got)
	}
}

func TestScaffoldExtract_PromptRequired(t *testing.T) {
	t.Parallel()
	dst := t.TempDir()
	srcV1 := fstest.MapFS{"a.txt": {Data: []byte("v1")}}
	if err := ScaffoldExtract(srcV1, dst); err != nil {
		t.Fatalf("first: %v", err)
	}
	srcV2 := fstest.MapFS{"a.txt": {Data: []byte("v2")}}
	err := ScaffoldExtract(srcV2, dst, WithScaffoldOnConflict(ScaffoldPromptInteractive))
	if !errors.Is(err, ErrScaffoldPromptRequired) {
		t.Errorf("got %v, want ErrScaffoldPromptRequired", err)
	}
}

// --- ScaffoldActionOp.String ---

func TestScaffoldActionOp_String(t *testing.T) {
	t.Parallel()
	cases := []struct {
		op   ScaffoldActionOp
		want string
	}{
		{ScaffoldActionCreate, "create"},
		{ScaffoldActionSkip, "skip"},
		{ScaffoldActionOverwrite, "overwrite"},
		{ScaffoldActionConflict, "conflict"},
	}
	for _, c := range cases {
		if got := c.op.String(); got != c.want {
			t.Errorf("got %q, want %q", got, c.want)
		}
	}
}

// --- Fault-injection: ScaffoldApply / ScaffoldExtract ---
//
// These tests swap package-level OS hooks (see fault_hooks.go) to
// exercise defensive error branches that real I/O can't easily
// provoke. None call t.Parallel; the hooks are package-global.

func TestFault_ScaffoldApply_MkdirError(t *testing.T) {
	h := newFaultyHooks(t)
	srcDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(srcDir, "sub"), 0o755); err != nil {
		t.Fatalf("setup: %v", err)
	}
	dst := t.TempDir()
	srcFS := os.DirFS(srcDir)
	h.failMkdirAllAlways()
	err := ScaffoldApply(srcFS, filepath.Join(dst, "out"), nil)
	if !errors.Is(err, errInjected) {
		t.Errorf("got %v, want errInjected", err)
	}
}

func TestFault_ScaffoldExtract_MkdirError(t *testing.T) {
	h := newFaultyHooks(t)
	srcDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(srcDir, "sub"), 0o755); err != nil {
		t.Fatalf("setup: %v", err)
	}
	dst := t.TempDir()
	srcFS := os.DirFS(srcDir)
	h.failMkdirAllAlways()
	err := ScaffoldExtract(srcFS, filepath.Join(dst, "out"))
	if !errors.Is(err, errInjected) {
		t.Errorf("got %v, want errInjected", err)
	}
}

func TestFault_ScaffoldExtract_MarkerWriteError(t *testing.T) {
	h := newFaultyHooks(t)
	srcDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(srcDir, "f"), []byte("v1"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}
	dst := t.TempDir()
	srcFS := os.DirFS(srcDir)

	// First osChmod (the temp file's chmod for the content) succeeds;
	// subsequent calls fail. By the time we get to the marker write
	// chmod, the injected error fires.
	calls := 0
	orig := osChmod
	osChmod = func(p string, m os.FileMode) error {
		calls++
		if calls == 1 {
			return orig(p, m)
		}
		return errInjected
	}
	_ = h

	err := ScaffoldExtract(srcFS, dst)
	if !errors.Is(err, errInjected) {
		t.Errorf("got %v, want errInjected", err)
	}
}

// --- ScaffoldExtract: prompt-driven skip ---

func TestScaffoldExtract_PromptSkip(t *testing.T) {
	t.Parallel()
	srcDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(srcDir, "f"), []byte("v1"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}
	dst := t.TempDir()
	if err := os.WriteFile(filepath.Join(dst, "f"), []byte("v0"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}
	srcFS := os.DirFS(srcDir)
	prompt := func(_ string, _ ScaffoldAction) ScaffoldActionOp {
		return ScaffoldActionSkip
	}
	err := ScaffoldExtract(srcFS, dst,
		WithScaffoldOnConflict(ScaffoldPromptInteractive),
		WithScaffoldPromptFunc(prompt))
	if err != nil {
		t.Fatalf("ScaffoldExtract: %v", err)
	}
	got, _ := os.ReadFile(filepath.Join(dst, "f"))
	if string(got) != "v0" {
		t.Errorf("got %q, want unchanged v0", got)
	}
}

// --- ScaffoldApply: prompt returns an unsupported action ---

func TestScaffoldApply_PromptUnsupportedAction(t *testing.T) {
	t.Parallel()
	srcDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(srcDir, "f"), []byte("v1"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}
	dst := t.TempDir()
	if err := os.WriteFile(filepath.Join(dst, "f"), []byte("v0"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}
	srcFS := os.DirFS(srcDir)
	prompt := func(_ string, _ ScaffoldAction) ScaffoldActionOp {
		return ScaffoldActionConflict // Conflict is invalid as a prompt response.
	}
	err := ScaffoldApply(srcFS, dst, nil,
		WithScaffoldOnConflict(ScaffoldPromptInteractive),
		WithScaffoldPromptFunc(prompt))
	if !errors.Is(err, ErrScaffoldPromptUnsupported) {
		t.Errorf("got %v, want ErrScaffoldPromptUnsupported", err)
	}
}

// --- ScaffoldActionOp.String fallback ---

func TestScaffoldActionOp_String_Unknown(t *testing.T) {
	t.Parallel()
	got := ScaffoldActionOp(99).String()
	if got == "" {
		t.Error("expected unknown label, got empty string")
	}
}
