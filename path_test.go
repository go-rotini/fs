package fs

import (
	"errors"
	"os"
	"os/user"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// --- Expand ---

func TestExpand_Empty(t *testing.T) {
	t.Parallel()
	got, err := Expand("")
	if err != nil || got != "" {
		t.Errorf("Expand(\"\") = (%q, %v)", got, err)
	}
}

func TestExpand_NoExpansion(t *testing.T) {
	t.Parallel()
	got, err := Expand("/a/b/c")
	if err != nil || got != "/a/b/c" {
		t.Errorf("got (%q, %v)", got, err)
	}
}

func TestExpand_TildeHome(t *testing.T) {
	t.Parallel()
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no home dir available")
	}
	got, err := Expand("~/foo/bar")
	if err != nil {
		t.Fatalf("Expand: %v", err)
	}
	want := home + string(filepath.Separator) + filepath.Join("foo", "bar")
	// Allow for either separator on Windows.
	if got != home+"/foo/bar" && got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestExpand_TildeMidPath(t *testing.T) {
	t.Parallel()
	// ~ only at start; mid-path is literal.
	got, err := Expand("/etc/~user/notes")
	if err != nil {
		t.Fatalf("Expand: %v", err)
	}
	if got != "/etc/~user/notes" {
		t.Errorf("got %q, want literal", got)
	}
}

func TestExpand_TildeUser_Missing(t *testing.T) {
	t.Parallel()
	_, err := Expand("~nosuchuser_xyz_zzz/foo")
	if err == nil {
		t.Error("expected error for unknown user")
	}
	var pe *PathError
	if !errors.As(err, &pe) {
		t.Errorf("expected *PathError; got %T", err)
	}
}

func TestExpand_DollarVar(t *testing.T) {
	// t.Setenv forbids t.Parallel.
	t.Setenv("FS_TEST_VAR", "hello")
	got, err := Expand("$FS_TEST_VAR/world")
	if err != nil || got != "hello/world" {
		t.Errorf("got (%q, %v)", got, err)
	}
}

func TestExpand_BracedVar(t *testing.T) {
	t.Setenv("FS_TEST_VAR", "hello")
	got, err := Expand("${FS_TEST_VAR}_suffix")
	if err != nil || got != "hello_suffix" {
		t.Errorf("got (%q, %v)", got, err)
	}
}

func TestExpand_UnsetVarLenient(t *testing.T) {
	t.Parallel()
	got, err := Expand("$FS_UNSET_TEST_VAR_xyz/path")
	if err != nil || got != "/path" {
		t.Errorf("got (%q, %v); unset to empty by default", got, err)
	}
}

func TestExpand_UnsetVarStrict(t *testing.T) {
	t.Parallel()
	_, err := Expand("$FS_UNSET_TEST_VAR_xyz", WithStrictExpansion())
	if err == nil {
		t.Error("expected error in strict mode")
	}
	if !errors.Is(err, errUnsetExpansion) {
		t.Errorf("err = %v, want errUnsetExpansion match", err)
	}
}

func TestExpand_LoneDollar(t *testing.T) {
	t.Parallel()
	got, err := Expand("$")
	if err != nil || got != "$" {
		t.Errorf("got (%q, %v); lone $ should be literal", got, err)
	}
}

func TestExpand_DollarNonIdent(t *testing.T) {
	t.Parallel()
	got, err := Expand("$.path")
	if err != nil || got != "$.path" {
		t.Errorf("got (%q, %v); $<non-ident> should be literal", got, err)
	}
}

func TestExpand_UnclosedBrace(t *testing.T) {
	t.Parallel()
	got, err := Expand("${UNCLOSED")
	if err != nil || got != "${UNCLOSED" {
		t.Errorf("got (%q, %v); unterminated ${ should be literal", got, err)
	}
}

func TestExpand_NULRejected(t *testing.T) {
	t.Parallel()
	_, err := Expand("path\x00with\x00nul")
	if !errors.Is(err, ErrInvalidPath) {
		t.Errorf("got %v, want ErrInvalidPath", err)
	}
}

// --- Abs ---

func TestAbs_RelativePath(t *testing.T) {
	t.Parallel()
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	got, err := Abs("foo/bar")
	if err != nil {
		t.Fatalf("Abs: %v", err)
	}
	want := filepath.Join(cwd, "foo/bar")
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestAbs_TildeExpansion(t *testing.T) {
	t.Parallel()
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no home")
	}
	got, err := Abs("~/foo")
	if err != nil {
		t.Fatalf("Abs: %v", err)
	}
	if !strings.Contains(got, home) {
		t.Errorf("got %q, want path containing %q", got, home)
	}
}

// --- Clean / Join / JoinSlash / Dir / Base / Ext / Stem / Split / IsAbs / Rel / ToSlash / FromSlash ---

func TestClean(t *testing.T) {
	t.Parallel()
	if got := Clean("a//b/../c/./"); got != filepath.Clean("a//b/../c/./") {
		t.Errorf("got %q", got)
	}
}

func TestJoin(t *testing.T) {
	t.Parallel()
	if got := Join("a", "b", "c"); got != filepath.Join("a", "b", "c") {
		t.Errorf("got %q", got)
	}
}

func TestJoinSlash(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in   []string
		want string
	}{
		{[]string{"a", "b", "c"}, "a/b/c"},
		{[]string{"a", "", "c"}, "a/c"},
		{[]string{}, ""},
		{[]string{"only"}, "only"},
	}
	for _, c := range cases {
		if got := JoinSlash(c.in...); got != c.want {
			t.Errorf("JoinSlash(%v) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestDir_Base_Ext(t *testing.T) {
	t.Parallel()
	const p = "/a/b/c.txt"
	if got := Dir(p); got != filepath.Dir(p) {
		t.Errorf("Dir = %q", got)
	}
	if got := Base(p); got != "c.txt" {
		t.Errorf("Base = %q", got)
	}
	if got := Ext(p); got != ".txt" {
		t.Errorf("Ext = %q", got)
	}
}

func TestStem(t *testing.T) {
	t.Parallel()
	cases := []struct {
		path string
		want string
	}{
		{"foo.txt", "foo"},
		{"foo/bar.tar.gz", "bar.tar"},
		{"README", "README"},
		{".env", ".env"},
		{".gitignore", ".gitignore"},
		{"path/to/file.json", "file"},
		{"", "."}, // filepath.Base("") == "."
		{".", "."},
		{"..", ".."},
	}
	for _, c := range cases {
		if got := Stem(c.path); got != c.want {
			t.Errorf("Stem(%q) = %q, want %q", c.path, got, c.want)
		}
	}
}

func TestIsAbs(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		if !IsAbs(`C:\foo`) {
			t.Error("Windows: C:\\foo should be absolute")
		}
		if IsAbs(`foo\bar`) {
			t.Error("Windows: foo\\bar should be relative")
		}
	} else {
		if !IsAbs("/abs") {
			t.Error("/abs should be absolute")
		}
		if IsAbs("rel") {
			t.Error("rel should not be absolute")
		}
	}
}

func TestRel(t *testing.T) {
	t.Parallel()
	got, err := Rel("/a/b", "/a/b/c/d")
	if err != nil {
		t.Fatalf("Rel: %v", err)
	}
	want := filepath.Join("c", "d")
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestRel_Error(t *testing.T) {
	t.Parallel()
	if runtime.GOOS != "windows" {
		// Cross-volume Rel doesn't error on POSIX; pick a case it
		// can't resolve: relative source against absolute target.
		_, err := Rel("rel/path", "/abs/target")
		if err == nil {
			t.Error("expected error")
		}
		var pe *PathError
		if !errors.As(err, &pe) {
			t.Errorf("expected *PathError; got %T", err)
		}
	}
}

func TestToSlash_FromSlash(t *testing.T) {
	t.Parallel()
	const ps = "a/b/c"
	if got := ToSlash(ps); got != "a/b/c" {
		t.Errorf("ToSlash = %q", got)
	}
	from := FromSlash(ps)
	want := filepath.FromSlash(ps)
	if from != want {
		t.Errorf("FromSlash = %q, want %q", from, want)
	}
}

// --- EqualPath ---

func TestEqualPath_Same(t *testing.T) {
	t.Parallel()
	if !EqualPath("/a/b", "/a/b") {
		t.Error("identical paths should be equal")
	}
}

func TestEqualPath_DifferentCleaning(t *testing.T) {
	t.Parallel()
	if !EqualPath("/a/b", "/a/./c/../b") {
		t.Error("paths cleaning to the same value should be equal")
	}
}

func TestSplit(t *testing.T) {
	t.Parallel()
	cases := []struct {
		path, dir, file string
	}{
		{"/a/b/c", "/a/b/", "c"},
		{"foo.txt", "", "foo.txt"},
		{"", "", ""},
	}
	for _, c := range cases {
		d, f := Split(c.path)
		if d != c.dir || f != c.file {
			t.Errorf("Split(%q) = (%q, %q); want (%q, %q)", c.path, d, f, c.dir, c.file)
		}
	}
}

func TestEqualPath_CaseSensitivity(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		if !EqualPath(`C:\Foo`, `c:\foo`) {
			t.Error("Windows: case-insensitive match expected")
		}
	} else {
		if EqualPath("/Foo", "/foo") {
			t.Error("POSIX: case-sensitive comparison expected")
		}
	}
}

// --- IsSubpath / MustBeChildOf ---

func TestIsSubpath_Inside(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	child := filepath.Join(dir, "sub", "file.txt")
	ok, err := IsSubpath(dir, child)
	if err != nil || !ok {
		t.Errorf("got (%v, %v); want (true, nil)", ok, err)
	}
}

func TestIsSubpath_OutsideViaDotDot(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	outside := filepath.Join(dir, "..", "elsewhere")
	ok, err := IsSubpath(dir, outside)
	if err != nil || ok {
		t.Errorf("got (%v, %v); want (false, nil)", ok, err)
	}
}

func TestIsSubpath_Identity(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	ok, err := IsSubpath(dir, dir)
	if err != nil || !ok {
		t.Errorf("a path is its own subpath; got (%v, %v)", ok, err)
	}
}

func TestMustBeChildOf_OK(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := MustBeChildOf(dir, filepath.Join(dir, "x")); err != nil {
		t.Errorf("got %v, want nil", err)
	}
}

func TestMustBeChildOf_Escapes(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	err := MustBeChildOf(dir, filepath.Join(dir, "..", "elsewhere"))
	if !errors.Is(err, ErrEscapesRoot) {
		t.Errorf("got %v, want ErrEscapesRoot", err)
	}
}

// --- EvalSymlinksWithin ---

func TestEvalSymlinksWithin_NoSymlink(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	target := filepath.Join(dir, "file")
	if err := os.WriteFile(target, []byte("x"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	got, err := EvalSymlinksWithin(dir, target)
	if err != nil {
		t.Fatalf("EvalSymlinksWithin: %v", err)
	}
	if !strings.HasSuffix(got, "file") {
		t.Errorf("got %q", got)
	}
}

func TestEvalSymlinksWithin_SymlinkInside(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("requires unprivileged symlinks; not always available on Windows CI")
	}
	t.Parallel()
	dir := t.TempDir()
	target := filepath.Join(dir, "real")
	if err := os.WriteFile(target, []byte("x"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	link := filepath.Join(dir, "link")
	if err := os.Symlink(target, link); err != nil {
		t.Fatalf("Symlink: %v", err)
	}
	got, err := EvalSymlinksWithin(dir, link)
	if err != nil {
		t.Fatalf("EvalSymlinksWithin: %v", err)
	}
	if !strings.HasSuffix(got, "real") {
		t.Errorf("got %q, want resolved", got)
	}
}

func TestEvalSymlinksWithin_SymlinkOutside(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("requires unprivileged symlinks")
	}
	t.Parallel()
	root := t.TempDir()
	outsideDir := t.TempDir()

	outside := filepath.Join(outsideDir, "secret")
	if err := os.WriteFile(outside, []byte("x"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	link := filepath.Join(root, "link")
	if err := os.Symlink(outside, link); err != nil {
		t.Fatalf("Symlink: %v", err)
	}

	_, err := EvalSymlinksWithin(root, link)
	if !errors.Is(err, ErrEscapesRoot) {
		t.Errorf("got %v, want ErrEscapesRoot", err)
	}
}

// --- Helper-internal: scanVarRef ---

func TestScanVarRef(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in       string
		wantName string
		wantN    int
	}{
		{"$FOO bar", "FOO", 4},
		{"${FOO}rest", "FOO", 6},
		{"$", "", 0},
		{"$.", "", 0},
		{"${UNCLOSED", "", 0},
		{"FOO", "", 0},
		{"$FOO_BAR123/x", "FOO_BAR123", 11},
	}
	for _, c := range cases {
		name, n := scanVarRef(c.in)
		if name != c.wantName || n != c.wantN {
			t.Errorf("scanVarRef(%q) = (%q, %d), want (%q, %d)",
				c.in, name, n, c.wantName, c.wantN)
		}
	}
}

// --- Expand / Abs / EvalSymlinksWithin extras ---

func TestEvalSymlinksWithin_Missing(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	_, err := EvalSymlinksWithin(dir, filepath.Join(dir, "missing"))
	if err == nil {
		t.Error("expected error for missing path")
	}
}

func TestStem_EdgeCases(t *testing.T) {
	t.Parallel()
	cases := []struct{ in, want string }{
		{"file.txt", "file"},
		{"file.tar.gz", "file.tar"},
		{"no-ext", "no-ext"},
		{".hidden", ".hidden"},
		{"/a/b/c.txt", "c"},
		{"", "."},
	}
	for _, c := range cases {
		if got := Stem(c.in); got != c.want {
			t.Errorf("Stem(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestAbs_ExpandError(t *testing.T) {
	t.Parallel()
	_, err := Abs("nul\x00here")
	if err == nil {
		t.Fatal("expected error from Abs on NUL-bearing path")
	}
}

func TestExpand_UnknownUser(t *testing.T) {
	t.Parallel()
	_, err := Expand("~__definitely_no_such_user_4f7a__/foo")
	if err == nil {
		t.Fatal("expected error from Expand on unknown ~user")
	}
}

func TestExpand_KnownUser(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == goosWindows {
		// os/user.Lookup of a domain-qualified name (DOMAIN\user, which
		// is what user.Current().Username returns on Windows) reports a
		// domain SID rather than a user SID and fails; ~user expansion
		// for an explicit name is therefore unreliable on Windows.
		t.Skip("os/user.Lookup of DOMAIN\\user is unreliable on Windows")
	}
	// Look up the current user; ~current should successfully resolve.
	u, err := user.Current()
	if err != nil || u.Username == "" {
		t.Skip("user.Current unavailable")
	}
	if _, err := Expand("~" + u.Username + "/x"); err != nil {
		t.Errorf("Expand(~%s/x): %v", u.Username, err)
	}
}
