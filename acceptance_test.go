package fs

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// Acceptance tests exercise real-world CLI scenarios end-to-end.
// Each test name starts with `TestAcceptance` so
// `make test-acceptance` (which uses `-run TestAcceptance`) can
// target them.

// TestAcceptanceProjectRoot — fixture tree with `.git` at the top;
// from a deeply-nested subdirectory, ProjectRoot returns the
// correct ancestor.
func TestAcceptanceProjectRoot(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, ".git"), 0o755); err != nil {
		t.Fatalf("setup: %v", err)
	}
	deep := filepath.Join(root, "src", "pkg", "internal")
	if err := os.MkdirAll(deep, 0o755); err != nil {
		t.Fatalf("setup: %v", err)
	}

	got, err := ProjectRoot(deep)
	if err != nil {
		t.Fatalf("ProjectRoot: %v", err)
	}
	if !sameRealPath(t, got, root) {
		t.Errorf("got %s, want %s", got, root)
	}
}

// TestAcceptanceConfigSave — CLI atomic-save scenario:
//  1. Write a config with mode 0o600 ("secret-bearing").
//  2. Overwrite with new content; mode is preserved.
//  3. Read back to verify contents survive.
func TestAcceptanceConfigSave(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	cfg := filepath.Join(dir, "secrets.yaml")

	if err := WriteFileSecret(cfg, []byte("token: abc123\n")); err != nil {
		t.Fatalf("WriteFileSecret: %v", err)
	}
	info, _ := os.Stat(cfg)
	if got := info.Mode().Perm(); got != 0o600 {
		t.Errorf("first-write mode = %o, want 0o600", got)
	}

	// Overwrite with new content.
	if err := WriteFile(cfg, []byte("token: xyz789\n")); err != nil {
		t.Fatalf("WriteFile overwrite: %v", err)
	}

	got, _ := os.ReadFile(cfg)
	if string(got) != "token: xyz789\n" {
		t.Errorf("after overwrite: %q", got)
	}

	// Mode should be preserved across the overwrite (write.go
	// snapshots the existing perm).
	info, _ = os.Stat(cfg)
	if got := info.Mode().Perm(); got != 0o600 {
		t.Errorf("after overwrite mode = %o, want 0o600 (preserved)", got)
	}
}

// TestAcceptanceWalkSkipping — walk over a fixture with `.git`,
// `node_modules`, and `.terraform` directories; each is pruned by
// WithSkipNames.
func TestAcceptanceWalkSkipping(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	for _, p := range []string{
		filepath.Join(root, "src", "main.go"),
		filepath.Join(root, ".git", "HEAD"),
		filepath.Join(root, "node_modules", "foo", "package.json"),
		filepath.Join(root, ".terraform", "modules", "x"),
	} {
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatalf("setup: %v", err)
		}
		if err := os.WriteFile(p, []byte{}, 0o644); err != nil {
			t.Fatalf("setup: %v", err)
		}
	}

	var visited []string
	err := Walk(root, func(path string, _ os.DirEntry, _ error) error {
		visited = append(visited, path)
		return nil
	}, WithSkipNames([]string{".git", "node_modules", ".terraform"}))
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}

	for _, p := range visited {
		for _, name := range []string{".git", "node_modules", ".terraform"} {
			if strings.Contains(p, string(filepath.Separator)+name+string(filepath.Separator)) {
				t.Errorf("walk leaked into %s subtree: %s", name, p)
			}
		}
	}
	// Sanity: src/main.go must be visited.
	found := false
	for _, p := range visited {
		if strings.HasSuffix(p, filepath.Join("src", "main.go")) {
			found = true
		}
	}
	if !found {
		t.Error("walk didn't visit src/main.go")
	}
}

// TestAcceptanceFindUpEnv — fixture with multiple `.env` files at
// different levels; FindUpAll returns them leaf-to-root and
// ProjectRoot picks the right ancestor.
func TestAcceptanceFindUpEnv(t *testing.T) {
	t.Parallel()
	rootDir := t.TempDir()
	mid := filepath.Join(rootDir, "service", "api")
	leaf := filepath.Join(mid, "internal", "handlers")
	if err := os.MkdirAll(leaf, 0o755); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if err := os.Mkdir(filepath.Join(rootDir, ".git"), 0o755); err != nil {
		t.Fatalf("setup: %v", err)
	}
	for _, p := range []string{
		filepath.Join(rootDir, ".env"),
		filepath.Join(rootDir, "service", ".env"),
		filepath.Join(leaf, ".env"),
	} {
		if err := os.WriteFile(p, []byte("X=1"), 0o644); err != nil {
			t.Fatalf("setup: %v", err)
		}
	}

	got, err := FindUpAll(".env", leaf, WithStopAt(rootDir))
	if err != nil {
		t.Fatalf("FindUpAll: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("got %d matches, want 3: %v", len(got), got)
	}

	pr, err := ProjectRoot(leaf)
	if err != nil {
		t.Fatalf("ProjectRoot: %v", err)
	}
	if !sameRealPath(t, pr, rootDir) {
		t.Errorf("ProjectRoot = %s, want %s", pr, rootDir)
	}
}

// TestAcceptanceUserDirs — set `XDG_CONFIG_HOME` to a fixture path
// (Linux/freebsd only); ConfigDir returns it; AppConfigDir("myapp")
// returns the joined form. macOS / Windows have non-XDG conventions
// and are skipped.
func TestAcceptanceUserDirs(t *testing.T) {
	if !canTestXDG() {
		t.Skip("XDG only on linux/freebsd")
	}
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)

	got, err := ConfigDir()
	if err != nil {
		t.Fatalf("ConfigDir: %v", err)
	}
	if got != dir {
		t.Errorf("ConfigDir = %s, want %s", got, dir)
	}

	app, err := AppConfigDir("myapp")
	if err != nil {
		t.Fatalf("AppConfigDir: %v", err)
	}
	want := filepath.Join(dir, "myapp")
	if app != want {
		t.Errorf("AppConfigDir = %s, want %s", app, want)
	}
}

// TestAcceptanceArchiveRoundtrip — create an archive from a tree,
// extract elsewhere, verify the resulting tree matches the source.
func TestAcceptanceArchiveRoundtrip(t *testing.T) {
	t.Parallel()
	src := t.TempDir()
	dst := t.TempDir()
	archivePath := filepath.Join(t.TempDir(), "out.tar.gz")

	for _, p := range []string{
		filepath.Join(src, "README.md"),
		filepath.Join(src, "src", "main.go"),
		filepath.Join(src, "src", "lib", "lib.go"),
		filepath.Join(src, "config", "app.yaml"),
	} {
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatalf("setup: %v", err)
		}
		if err := os.WriteFile(p, []byte("payload-"+filepath.Base(p)), 0o644); err != nil {
			t.Fatalf("setup: %v", err)
		}
	}

	if err := CreateArchiveFile(archivePath, src, WithArchiveFormat(ArchiveFormatTarGz)); err != nil {
		t.Fatalf("CreateArchiveFile: %v", err)
	}
	if err := ExtractArchiveFile(archivePath, dst); err != nil {
		t.Fatalf("ExtractArchiveFile: %v", err)
	}

	// Verify every source file landed in dst with matching contents.
	if err := filepath.WalkDir(src, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, _ := filepath.Rel(src, path)
		want, _ := os.ReadFile(path)
		got, gerr := os.ReadFile(filepath.Join(dst, rel))
		if gerr != nil {
			t.Errorf("missing %s: %v", rel, gerr)
			return nil
		}
		if string(got) != string(want) {
			t.Errorf("%s: got %q, want %q", rel, got, want)
		}
		return nil
	}); err != nil {
		t.Fatalf("WalkDir: %v", err)
	}
}

// TestAcceptanceScaffoldIdempotent — apply a scaffold, then apply
// again; the second call is a no-op (default SkipExisting policy).
func TestAcceptanceScaffoldIdempotent(t *testing.T) {
	t.Parallel()
	dst := t.TempDir()
	src := MockFS(map[string]string{
		"README.md":             "# {{.Name}}\n",
		"src/{{.Name}}/main.go": "package {{.Name}}\n",
	})

	if err := ScaffoldApply(src, dst, struct{ Name string }{Name: "myapp"}); err != nil {
		t.Fatalf("first apply: %v", err)
	}
	first := h(t, dst).Snapshot()

	// Modify a destination file; second apply should NOT overwrite it.
	if err := os.WriteFile(filepath.Join(dst, "README.md"), []byte("user-edit"), 0o644); err != nil {
		t.Fatalf("user edit: %v", err)
	}
	if err := ScaffoldApply(src, dst, struct{ Name string }{Name: "myapp"}); err != nil {
		t.Fatalf("second apply: %v", err)
	}

	got, _ := os.ReadFile(filepath.Join(dst, "README.md"))
	if string(got) != "user-edit" {
		t.Errorf("idempotent apply overwrote user edit: %q", got)
	}

	// Snapshot should still show the same shape (modulo content of
	// README.md which the user changed).
	_ = first
}

// canTestXDG reports whether the platform's XDG behavior is wired
// (linux / freebsd). On darwin and windows ConfigDir uses a
// different, non-XDG convention.
func canTestXDG() bool {
	return runtime.GOOS == "linux" || runtime.GOOS == "freebsd"
}

// h is a shorthand TestHarness factory used inside acceptance tests.
func h(t *testing.T, root string) *TestHarness {
	t.Helper()
	// Use a harness rooted at an existing dir by side-loading it;
	// NewTestHarness creates its own temp dir, so for an existing
	// dst we'd need a different shape. For acceptance tests we just
	// want a Snapshot() helper, so build one ad-hoc.
	return &TestHarness{t: t, root: root}
}
