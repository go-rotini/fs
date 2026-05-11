package fs

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// makeTreeFixture writes a small tree for create-archive round-trip tests.
//
//	root/
//	  a.txt        ("alpha")
//	  sub/
//	    b.txt      ("beta")
//	    deep/
//	      c.txt    ("gamma")
func makeTreeFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	for _, p := range []string{
		filepath.Join(root, "sub", "deep"),
	} {
		if err := os.MkdirAll(p, 0o755); err != nil {
			t.Fatalf("MkdirAll: %v", err)
		}
	}
	files := map[string]string{
		filepath.Join(root, "a.txt"):                "alpha",
		filepath.Join(root, "sub", "b.txt"):         "beta",
		filepath.Join(root, "sub", "deep", "c.txt"): "gamma",
	}
	for p, c := range files {
		if err := os.WriteFile(p, []byte(c), 0o644); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}
	}
	return root
}

// readBackTree reads every regular file under root into a relative-path to contents map.
func readBackTree(t *testing.T, root string) map[string]string {
	t.Helper()
	out := map[string]string{}
	if err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		data, rerr := os.ReadFile(path)
		if rerr != nil {
			return rerr
		}
		out[filepath.ToSlash(rel)] = string(data)
		return nil
	}); err != nil {
		t.Fatalf("WalkDir: %v", err)
	}
	return out
}

// --- Format detection ---

func TestDetectArchiveFormat(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		buf  []byte
		want ArchiveFormat
	}{
		{"gzip", []byte{0x1F, 0x8B, 0x08, 0x00}, ArchiveFormatTarGz},
		{"zip", []byte{0x50, 0x4B, 0x03, 0x04}, ArchiveFormatZip},
		{"tar", append(make([]byte, 257), append([]byte("ustar\x00"), make([]byte, 6)...)...), ArchiveFormatTar},
		{"random", []byte{0x00, 0x01, 0x02, 0x03}, ArchiveFormatUnknown},
		{"short", []byte{0x1F}, ArchiveFormatUnknown},
	}
	for _, c := range cases {
		if got := detectArchiveFormat(c.buf); got != c.want {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
}

// --- Round-trip ---

func TestCreateExtract_TarRoundTrip(t *testing.T) {
	t.Parallel()
	src := makeTreeFixture(t)
	dst := t.TempDir()
	archivePath := filepath.Join(t.TempDir(), "out.tar")

	if err := CreateArchiveFile(archivePath, src, WithArchiveFormat(ArchiveFormatTar)); err != nil {
		t.Fatalf("CreateArchiveFile: %v", err)
	}
	if err := ExtractArchiveFile(archivePath, dst); err != nil {
		t.Fatalf("ExtractArchiveFile: %v", err)
	}

	got := readBackTree(t, dst)
	want := map[string]string{
		"a.txt":          "alpha",
		"sub/b.txt":      "beta",
		"sub/deep/c.txt": "gamma",
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s: got %q, want %q", k, got[k], v)
		}
	}
}

func TestCreateExtract_TarGzRoundTrip(t *testing.T) {
	t.Parallel()
	src := makeTreeFixture(t)
	dst := t.TempDir()
	archivePath := filepath.Join(t.TempDir(), "out.tar.gz")

	if err := CreateArchiveFile(archivePath, src, WithArchiveFormat(ArchiveFormatTarGz)); err != nil {
		t.Fatalf("CreateArchiveFile: %v", err)
	}
	if err := ExtractArchiveFile(archivePath, dst); err != nil {
		t.Fatalf("ExtractArchiveFile: %v", err)
	}

	got := readBackTree(t, dst)
	if got["a.txt"] != "alpha" || got["sub/deep/c.txt"] != "gamma" {
		t.Errorf("round-trip lost contents: %v", got)
	}
}

func TestCreateExtract_ZipRoundTrip(t *testing.T) {
	t.Parallel()
	src := makeTreeFixture(t)
	dst := t.TempDir()
	archivePath := filepath.Join(t.TempDir(), "out.zip")

	if err := CreateArchiveFile(archivePath, src, WithArchiveFormat(ArchiveFormatZip)); err != nil {
		t.Fatalf("CreateArchiveFile: %v", err)
	}
	if err := ExtractArchiveFile(archivePath, dst); err != nil {
		t.Fatalf("ExtractArchiveFile: %v", err)
	}

	got := readBackTree(t, dst)
	if got["a.txt"] != "alpha" || got["sub/b.txt"] != "beta" {
		t.Errorf("zip round-trip lost contents: %v", got)
	}
}

// --- Path-confinement / zip-slip / tar-slip defenses ---

func makeMaliciousTar(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	hdr := &tar.Header{
		Name:    "../escape.txt",
		Mode:    0o644,
		Size:    int64(len("escaped")),
		ModTime: time.Now(),
	}
	if err := tw.WriteHeader(hdr); err != nil {
		t.Fatalf("WriteHeader: %v", err)
	}
	if _, err := tw.Write([]byte("escaped")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if err := tw.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	return buf.Bytes()
}

func TestExtractArchive_TarSlipRefused(t *testing.T) {
	t.Parallel()
	dst := t.TempDir()
	tarBytes := makeMaliciousTar(t)

	err := ExtractArchive(bytes.NewReader(tarBytes), dst)
	if !errors.Is(err, ErrEscapesRoot) {
		t.Errorf("got %v, want ErrEscapesRoot", err)
	}
	// Verify the malicious entry didn't land outside dst.
	if _, statErr := os.Stat(filepath.Join(filepath.Dir(dst), "escape.txt")); statErr == nil {
		t.Error("tar-slip wrote a file outside dst")
	}
}

func makeMaliciousZip(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, err := zw.Create("../escape.txt")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := w.Write([]byte("zip-slipped")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	return buf.Bytes()
}

func TestExtractArchive_ZipSlipRefused(t *testing.T) {
	t.Parallel()
	dst := t.TempDir()
	zipBytes := makeMaliciousZip(t)

	err := ExtractArchive(bytes.NewReader(zipBytes), dst)
	if !errors.Is(err, ErrEscapesRoot) {
		t.Errorf("got %v, want ErrEscapesRoot", err)
	}
}

func TestExtractArchive_SymlinkEscapeRefused(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation typically requires elevation on Windows")
	}
	t.Parallel()
	dst := t.TempDir()

	// Build a tar with a symlink that points outside dst.
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	hdr := &tar.Header{
		Name:     "lnk",
		Mode:     0o777,
		Typeflag: tar.TypeSymlink,
		Linkname: "/etc/passwd",
		ModTime:  time.Now(),
	}
	if err := tw.WriteHeader(hdr); err != nil {
		t.Fatalf("WriteHeader: %v", err)
	}
	if err := tw.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	err := ExtractArchive(&buf, dst)
	if !errors.Is(err, ErrEscapesRoot) {
		t.Errorf("got %v, want ErrEscapesRoot", err)
	}
}

// --- Mode masking ---

func TestExtractArchive_ModeMaskedByDefault(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX mode bits only")
	}
	t.Parallel()

	// Build a tar with a too-permissive mode.
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	hdr := &tar.Header{
		Name:    "f",
		Mode:    0o777,
		Size:    1,
		ModTime: time.Now(),
	}
	if err := tw.WriteHeader(hdr); err != nil {
		t.Fatalf("WriteHeader: %v", err)
	}
	if _, err := tw.Write([]byte("x")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if err := tw.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	dst := t.TempDir()
	if err := ExtractArchive(&buf, dst); err != nil {
		t.Fatalf("ExtractArchive: %v", err)
	}
	info, _ := os.Stat(filepath.Join(dst, "f"))
	if got := info.Mode().Perm(); got != 0o644 {
		t.Errorf("default mode = %o, want 0644", got)
	}
}

func TestExtractArchive_PreserveMode(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX mode bits only")
	}
	t.Parallel()

	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	hdr := &tar.Header{
		Name:    "f",
		Mode:    0o600,
		Size:    1,
		ModTime: time.Now(),
	}
	if err := tw.WriteHeader(hdr); err != nil {
		t.Fatalf("WriteHeader: %v", err)
	}
	if _, err := tw.Write([]byte("x")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if err := tw.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	dst := t.TempDir()
	if err := ExtractArchive(&buf, dst, WithPreserveMode(true)); err != nil {
		t.Fatalf("ExtractArchive: %v", err)
	}
	info, _ := os.Stat(filepath.Join(dst, "f"))
	if got := info.Mode().Perm(); got != 0o600 {
		t.Errorf("preserved mode = %o, want 0600", got)
	}
}

// --- WithArchiveMaxBytes ---

func TestExtractArchive_MaxBytes(t *testing.T) {
	t.Parallel()
	src := makeTreeFixture(t)
	archiveBuf := bytes.Buffer{}
	if err := CreateArchive(&archiveBuf, src, WithArchiveFormat(ArchiveFormatTar)); err != nil {
		t.Fatalf("CreateArchive: %v", err)
	}

	dst := t.TempDir()
	// 5 bytes is far below the 5 + 4 + 5 = 14 bytes total content.
	err := ExtractArchive(&archiveBuf, dst, WithArchiveMaxBytes(5))
	if !errors.Is(err, ErrArchiveTooLarge) {
		t.Errorf("got %v, want ErrArchiveTooLarge", err)
	}
}

// --- WithArchiveFilter ---

func TestExtractArchive_Filter(t *testing.T) {
	t.Parallel()
	src := makeTreeFixture(t)
	archivePath := filepath.Join(t.TempDir(), "out.tar")
	if err := CreateArchiveFile(archivePath, src); err != nil {
		t.Fatalf("CreateArchiveFile: %v", err)
	}

	dst := t.TempDir()
	if err := ExtractArchiveFile(archivePath, dst, WithArchiveFilter(func(h ArchiveHeader) bool {
		return !strings.HasSuffix(h.Name, "b.txt")
	})); err != nil {
		t.Fatalf("ExtractArchiveFile: %v", err)
	}

	if Exists(filepath.Join(dst, "sub", "b.txt")) {
		t.Error("filter did not exclude b.txt")
	}
	if !Exists(filepath.Join(dst, "a.txt")) {
		t.Error("filter incorrectly excluded a.txt")
	}
}

// --- OpenAutoArchive ---

func TestOpenAutoArchive_Plain(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "f")
	if err := os.WriteFile(path, []byte("plain"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	rc, err := OpenAutoArchive(path)
	if err != nil {
		t.Fatalf("OpenAutoArchive: %v", err)
	}
	defer rc.Close()
	got, _ := io.ReadAll(rc)
	if string(got) != "plain" {
		t.Errorf("got %q, want plain", got)
	}
}

func TestOpenAutoArchive_Gzip(t *testing.T) {
	t.Parallel()
	src := makeTreeFixture(t)
	archivePath := filepath.Join(t.TempDir(), "out.tar.gz")
	if err := CreateArchiveFile(archivePath, src, WithArchiveFormat(ArchiveFormatTarGz)); err != nil {
		t.Fatalf("CreateArchiveFile: %v", err)
	}

	rc, err := OpenAutoArchive(archivePath)
	if err != nil {
		t.Fatalf("OpenAutoArchive: %v", err)
	}
	defer rc.Close()

	// The decompressed stream should be a tar; the first 5 bytes
	// after the leading tar header padding contain the first
	// filename. Just verify we can read >0 bytes and they don't look
	// like raw gzip.
	got, _ := io.ReadAll(rc)
	if len(got) == 0 {
		t.Error("OpenAutoArchive returned empty stream from gzip-tar archive")
	}
	if got[0] == 0x1F && len(got) > 1 && got[1] == 0x8B {
		t.Error("OpenAutoArchive did not transparently decompress gzip")
	}
}

// --- cappedWriter partial-write path ---

// TestCappedWriter_PartialWrite exercises the path where the caller
// writes a chunk that crosses the cap. The writer should commit the
// partial bytes that fit and then surface ErrArchiveTooLarge.
func TestCappedWriter_PartialWrite(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	used := int64(0)
	w := limitWrap(&buf, &used, 5)

	// First write: 3 bytes, fits.
	if n, err := w.Write([]byte("abc")); err != nil || n != 3 {
		t.Fatalf("first write: n=%d err=%v", n, err)
	}
	// Second write: 5 bytes, but only 2 remaining to partial commit + cap error.
	n, err := w.Write([]byte("12345"))
	if !errors.Is(err, ErrArchiveTooLarge) {
		t.Errorf("got %v, want ErrArchiveTooLarge", err)
	}
	if n != 2 {
		t.Errorf("partial bytes = %d, want 2", n)
	}
	if buf.String() != "abc12" {
		t.Errorf("buffered = %q, want abc12", buf.String())
	}
	// Third write: any byte to cap error.
	n, err = w.Write([]byte("z"))
	if !errors.Is(err, ErrArchiveTooLarge) {
		t.Errorf("post-cap got %v, want ErrArchiveTooLarge", err)
	}
	if n != 0 {
		t.Errorf("post-cap n = %d, want 0", n)
	}
}

func TestCappedWriter_DisabledCap(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	used := int64(0)
	// limit <= 0 disables the cap.
	w := limitWrap(&buf, &used, 0)
	// Writing more than any plausible cap should never error.
	if _, err := w.Write([]byte(strings.Repeat("x", 1024))); err != nil {
		t.Errorf("disabled-cap write errored: %v", err)
	}
}

// --- Create-side filter ---

func TestCreateArchive_CreateFilterSkipsRegularFile(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == goosWindows {
		t.Skip("test exercises a POSIX symlink in the source tree")
	}
	src := makeTreeFixture(t)

	// Add a symlink so tarWalkHandler exercises its readlink path.
	if err := os.Symlink("a.txt", filepath.Join(src, "alias")); err != nil {
		t.Fatalf("Symlink: %v", err)
	}

	archivePath := filepath.Join(t.TempDir(), "out.tar")
	// Exclude only the regular file a.txt; this hits the
	// `return nil` (non-dir skip) branch in tarWalkHandler.
	if err := CreateArchiveFile(archivePath, src,
		WithArchiveCreateFilter(func(path string, _ os.FileInfo) bool {
			return filepath.Base(path) != "a.txt"
		}),
	); err != nil {
		t.Fatalf("CreateArchiveFile: %v", err)
	}

	dst := t.TempDir()
	if err := ExtractArchiveFile(archivePath, dst); err != nil {
		t.Fatalf("ExtractArchiveFile: %v", err)
	}
	if Exists(filepath.Join(dst, "a.txt")) {
		t.Error("filtered regular file appeared in archive")
	}
	// Symlink should survive.
	info, err := os.Lstat(filepath.Join(dst, "alias"))
	if err != nil {
		t.Fatalf("Lstat alias: %v", err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		t.Error("alias did not round-trip as a symlink")
	}
}

func TestCreateArchive_CreateFilter(t *testing.T) {
	t.Parallel()
	src := makeTreeFixture(t)
	archivePath := filepath.Join(t.TempDir(), "out.tar")

	// Skip everything under sub/.
	if err := CreateArchiveFile(archivePath, src,
		WithArchiveCreateFilter(func(path string, _ os.FileInfo) bool {
			return !strings.Contains(path, string(filepath.Separator)+"sub")
		}),
	); err != nil {
		t.Fatalf("CreateArchiveFile: %v", err)
	}

	dst := t.TempDir()
	if err := ExtractArchiveFile(archivePath, dst); err != nil {
		t.Fatalf("ExtractArchiveFile: %v", err)
	}
	if Exists(filepath.Join(dst, "sub")) {
		t.Error("create filter did not exclude sub/ subtree")
	}
	if !Exists(filepath.Join(dst, "a.txt")) {
		t.Error("non-filtered entry missing")
	}
}

// --- Tar hardlinks ---

func TestExtractArchive_TarHardlink(t *testing.T) {
	t.Parallel()
	dst := t.TempDir()

	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	// First write the regular file.
	if err := tw.WriteHeader(&tar.Header{
		Name: "real.txt", Mode: 0o644, Size: 5, ModTime: time.Now(),
	}); err != nil {
		t.Fatalf("WriteHeader: %v", err)
	}
	if _, err := tw.Write([]byte("hello")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	// Then a hard link to it.
	if err := tw.WriteHeader(&tar.Header{
		Name: "hard.txt", Mode: 0o644, Typeflag: tar.TypeLink, Linkname: "real.txt", ModTime: time.Now(),
	}); err != nil {
		t.Fatalf("WriteHeader link: %v", err)
	}
	if err := tw.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	if err := ExtractArchive(&buf, dst); err != nil {
		t.Fatalf("ExtractArchive: %v", err)
	}
	a, _ := os.ReadFile(filepath.Join(dst, "real.txt"))
	b, _ := os.ReadFile(filepath.Join(dst, "hard.txt"))
	if string(a) != "hello" || string(b) != "hello" {
		t.Errorf("hard link not extracted: real=%q link=%q", a, b)
	}
	same, err := SameFile(filepath.Join(dst, "real.txt"), filepath.Join(dst, "hard.txt"))
	if err != nil {
		t.Fatalf("SameFile: %v", err)
	}
	if !same {
		t.Error("hard link did not produce shared inode")
	}
}

func TestExtractArchive_TarHardlinkEscape(t *testing.T) {
	t.Parallel()
	dst := t.TempDir()

	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	if err := tw.WriteHeader(&tar.Header{
		Name: "hard", Mode: 0o644, Typeflag: tar.TypeLink, Linkname: "../escape", ModTime: time.Now(),
	}); err != nil {
		t.Fatalf("WriteHeader: %v", err)
	}
	if err := tw.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	err := ExtractArchive(&buf, dst)
	if !errors.Is(err, ErrEscapesRoot) {
		t.Errorf("got %v, want ErrEscapesRoot", err)
	}
}

// --- Symlink (POSIX): non-escape + Readlink ---

func TestExtractArchive_TarSymlinkInside(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation typically requires elevation on Windows")
	}
	t.Parallel()
	dst := t.TempDir()

	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	if err := tw.WriteHeader(&tar.Header{
		Name: "real.txt", Mode: 0o644, Size: 1, ModTime: time.Now(),
	}); err != nil {
		t.Fatalf("WriteHeader: %v", err)
	}
	if _, err := tw.Write([]byte("x")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if err := tw.WriteHeader(&tar.Header{
		Name: "lnk", Mode: 0o777, Typeflag: tar.TypeSymlink, Linkname: "real.txt", ModTime: time.Now(),
	}); err != nil {
		t.Fatalf("WriteHeader link: %v", err)
	}
	if err := tw.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	if err := ExtractArchive(&buf, dst); err != nil {
		t.Fatalf("ExtractArchive: %v", err)
	}
	link := filepath.Join(dst, "lnk")
	got, err := os.Readlink(link)
	if err != nil {
		t.Fatalf("Readlink: %v", err)
	}
	if got != "real.txt" {
		t.Errorf("link target = %q, want real.txt", got)
	}
}

// --- Zip filter exercises zipFileToArchiveHeader ---

func TestExtractArchive_ZipFilter(t *testing.T) {
	t.Parallel()
	src := makeTreeFixture(t)
	archivePath := filepath.Join(t.TempDir(), "out.zip")
	if err := CreateArchiveFile(archivePath, src, WithArchiveFormat(ArchiveFormatZip)); err != nil {
		t.Fatalf("CreateArchiveFile: %v", err)
	}

	dst := t.TempDir()
	// Filter receives an ArchiveHeader from zipFileToArchiveHeader.
	// The filter callback being invoked at all proves the helper ran.
	called := false
	if err := ExtractArchiveFile(archivePath, dst,
		WithArchiveFilter(func(h ArchiveHeader) bool {
			called = true
			// Drop b.txt; keep everything else.
			return !strings.HasSuffix(h.Name, "b.txt")
		}),
	); err != nil {
		t.Fatalf("ExtractArchiveFile: %v", err)
	}
	if !called {
		t.Error("filter callback never invoked")
	}
	if Exists(filepath.Join(dst, "sub", "b.txt")) {
		t.Error("filter did not exclude sub/b.txt")
	}
	if !Exists(filepath.Join(dst, "a.txt")) {
		t.Error("non-filtered entry missing")
	}
}

// --- ArchiveFormat.String ---

func TestArchiveFormat_String(t *testing.T) {
	t.Parallel()
	cases := []struct {
		f    ArchiveFormat
		want string
	}{
		{ArchiveFormatTar, "tar"},
		{ArchiveFormatTarGz, "tar.gz"},
		{ArchiveFormatZip, "zip"},
		{ArchiveFormatUnknown, "unknown"},
	}
	for _, c := range cases {
		if got := c.f.String(); got != c.want {
			t.Errorf("%v.String() = %q, want %q", c.f, got, c.want)
		}
	}
}

// --- Fault-injection: archive create ---
//
// These tests swap package-level OS hooks (see fault_hooks.go) to
// exercise defensive error branches that real I/O can't easily
// provoke. None call t.Parallel; the hooks are package-global.

func TestFault_CreateArchive_Tar_ReadError(t *testing.T) {
	h := newFaultyHooks(t)
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if err := os.WriteFile(filepath.Join(src, "f"), []byte("xy"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}
	orig := fileRead
	t.Cleanup(func() { fileRead = orig })
	fileRead = func(*os.File, []byte) (int, error) { return 0, errInjected }
	_ = h

	out := filepath.Join(dir, "out.tar")
	err := CreateArchiveFile(out, src, WithArchiveFormat(ArchiveFormatTar))
	if !errors.Is(err, errInjected) {
		t.Errorf("got %v, want errInjected", err)
	}
}

func TestFault_CreateArchive_Zip_ReadError(t *testing.T) {
	h := newFaultyHooks(t)
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if err := os.WriteFile(filepath.Join(src, "f"), []byte("xy"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}
	orig := fileRead
	t.Cleanup(func() { fileRead = orig })
	fileRead = func(*os.File, []byte) (int, error) { return 0, errInjected }
	_ = h

	out := filepath.Join(dir, "out.zip")
	err := CreateArchiveFile(out, src, WithArchiveFormat(ArchiveFormatZip))
	if !errors.Is(err, errInjected) {
		t.Errorf("got %v, want errInjected", err)
	}
}

func TestFault_CreateArchive_Tar_OpenError(t *testing.T) {
	h := newFaultyHooks(t)
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if err := os.WriteFile(filepath.Join(src, "f"), []byte("xy"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}
	h.failOpenAlways()

	out := filepath.Join(dir, "out.tar")
	err := CreateArchiveFile(out, src, WithArchiveFormat(ArchiveFormatTar))
	if !errors.Is(err, errInjected) {
		t.Errorf("got %v, want errInjected", err)
	}
}

func TestFault_CreateArchive_Zip_OpenError(t *testing.T) {
	h := newFaultyHooks(t)
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if err := os.WriteFile(filepath.Join(src, "f"), []byte("xy"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}
	h.failOpenAlways()

	out := filepath.Join(dir, "out.zip")
	err := CreateArchiveFile(out, src, WithArchiveFormat(ArchiveFormatZip))
	if !errors.Is(err, errInjected) {
		t.Errorf("got %v, want errInjected", err)
	}
}

func TestFault_CreateArchive_Tar_ReadlinkError(t *testing.T) {
	h := newFaultyHooks(t)
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if err := os.Symlink("target", filepath.Join(src, "lnk")); err != nil {
		t.Skipf("symlinks not supported: %v", err)
	}
	h.failReadlinkAlways()

	out := filepath.Join(dir, "out.tar")
	err := CreateArchiveFile(out, src, WithArchiveFormat(ArchiveFormatTar))
	if !errors.Is(err, errInjected) {
		t.Errorf("got %v, want errInjected", err)
	}
}

// --- Fault-injection: archive extract ---

func TestFault_ExtractArchive_RootMkdirError(t *testing.T) {
	h := newFaultyHooks(t)
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if err := os.WriteFile(filepath.Join(src, "f"), []byte("x"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}
	var buf bytes.Buffer
	if err := CreateArchive(&buf, src, WithArchiveFormat(ArchiveFormatTar)); err != nil {
		t.Fatalf("CreateArchive: %v", err)
	}
	h.failMkdirAllAlways()
	err := ExtractArchive(&buf, filepath.Join(dir, "extract"))
	if !errors.Is(err, errInjected) {
		t.Errorf("got %v, want errInjected", err)
	}
}

func TestFault_ExtractTar_FinalCloseError(t *testing.T) {
	h := newFaultyHooks(t)
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if err := os.WriteFile(filepath.Join(src, "f"), []byte("xy"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}
	archive := filepath.Join(dir, "archive.tar")
	if err := CreateArchiveFile(archive, src, WithArchiveFormat(ArchiveFormatTar)); err != nil {
		t.Fatalf("create: %v", err)
	}
	h.failCloseAlways()
	err := ExtractArchiveFile(archive, filepath.Join(dir, "extract"))
	if !errors.Is(err, errInjected) {
		t.Errorf("got %v, want errInjected", err)
	}
}

func TestFault_ExtractTar_MkdirError(t *testing.T) {
	h := newFaultyHooks(t)
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	if err := os.MkdirAll(filepath.Join(src, "sub"), 0o755); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if err := os.WriteFile(filepath.Join(src, "sub", "f"), []byte("xy"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}
	archive := filepath.Join(dir, "archive.tar")
	if err := CreateArchiveFile(archive, src, WithArchiveFormat(ArchiveFormatTar)); err != nil {
		t.Fatalf("create: %v", err)
	}

	h.failMkdirAllAlways()
	err := ExtractArchiveFile(archive, filepath.Join(dir, "extract"))
	if !errors.Is(err, errInjected) {
		t.Errorf("got %v, want errInjected", err)
	}
}

func TestFault_ExtractTar_OpenFileError(t *testing.T) {
	h := newFaultyHooks(t)
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if err := os.WriteFile(filepath.Join(src, "f"), []byte("xy"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}
	archive := filepath.Join(dir, "archive.tar")
	if err := CreateArchiveFile(archive, src, WithArchiveFormat(ArchiveFormatTar)); err != nil {
		t.Fatalf("create: %v", err)
	}

	h.failOpenFileAlways()
	err := ExtractArchiveFile(archive, filepath.Join(dir, "extract"))
	if !errors.Is(err, errInjected) {
		t.Errorf("got %v, want errInjected", err)
	}
}

func TestFault_ExtractTar_SymlinkError(t *testing.T) {
	h := newFaultyHooks(t)
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if err := os.Symlink("target", filepath.Join(src, "lnk")); err != nil {
		t.Skipf("symlinks not supported: %v", err)
	}
	archive := filepath.Join(dir, "archive.tar")
	if err := CreateArchiveFile(archive, src, WithArchiveFormat(ArchiveFormatTar)); err != nil {
		t.Fatalf("create: %v", err)
	}

	h.failSymlinkAlways()
	err := ExtractArchiveFile(archive, filepath.Join(dir, "extract"))
	if !errors.Is(err, errInjected) {
		t.Errorf("got %v, want errInjected", err)
	}
}

func TestFault_ExtractTar_HardlinkError(t *testing.T) {
	h := newFaultyHooks(t)
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatalf("setup: %v", err)
	}
	target := filepath.Join(src, "real")
	if err := os.WriteFile(target, []byte("xy"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if err := os.Link(target, filepath.Join(src, "hard")); err != nil {
		t.Skipf("hardlinks not supported: %v", err)
	}

	// CreateArchive doesn't emit hardlink entries; build one manually.
	archive := filepath.Join(dir, "archive.tar")
	tarFile, err := os.Create(archive)
	if err != nil {
		t.Fatalf("create tar: %v", err)
	}
	tw := tar.NewWriter(tarFile)
	if err := tw.WriteHeader(&tar.Header{Name: "real", Mode: 0o644, Size: 2, Typeflag: tar.TypeReg}); err != nil {
		t.Fatalf("WriteHeader real: %v", err)
	}
	if _, err := tw.Write([]byte("xy")); err != nil {
		t.Fatalf("Write real: %v", err)
	}
	if err := tw.WriteHeader(&tar.Header{Name: "hard", Mode: 0o644, Linkname: "real", Typeflag: tar.TypeLink}); err != nil {
		t.Fatalf("WriteHeader hard: %v", err)
	}
	if err := tw.Close(); err != nil {
		t.Fatalf("tw close: %v", err)
	}
	if err := tarFile.Close(); err != nil {
		t.Fatalf("file close: %v", err)
	}

	h.failLinkAlways()
	err = ExtractArchiveFile(archive, filepath.Join(dir, "extract"))
	if !errors.Is(err, errInjected) {
		t.Errorf("got %v, want errInjected", err)
	}
}

func TestFault_ExtractZip_MkdirError(t *testing.T) {
	h := newFaultyHooks(t)
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	if err := os.MkdirAll(filepath.Join(src, "sub"), 0o755); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if err := os.WriteFile(filepath.Join(src, "sub", "f"), []byte("xy"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}
	archive := filepath.Join(dir, "archive.zip")
	if err := CreateArchiveFile(archive, src, WithArchiveFormat(ArchiveFormatZip)); err != nil {
		t.Fatalf("create: %v", err)
	}

	h.failMkdirAllAlways()
	err := ExtractArchiveFile(archive, filepath.Join(dir, "extract"))
	if !errors.Is(err, errInjected) {
		t.Errorf("got %v, want errInjected", err)
	}
}

func TestFault_ExtractZip_OpenFileError(t *testing.T) {
	h := newFaultyHooks(t)
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if err := os.WriteFile(filepath.Join(src, "f"), []byte("xy"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}
	archive := filepath.Join(dir, "archive.zip")
	if err := CreateArchiveFile(archive, src, WithArchiveFormat(ArchiveFormatZip)); err != nil {
		t.Fatalf("create: %v", err)
	}

	h.failOpenFileAlways()
	err := ExtractArchiveFile(archive, filepath.Join(dir, "extract"))
	if !errors.Is(err, errInjected) {
		t.Errorf("got %v, want errInjected", err)
	}
}

// --- ExtractArchive size cap ---

func TestExtractArchive_ZipMaxBytes(t *testing.T) {
	t.Parallel()
	src := t.TempDir()
	if err := os.WriteFile(filepath.Join(src, "big"), bytes.Repeat([]byte("X"), 4096), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}
	archivePath := filepath.Join(t.TempDir(), "out.zip")
	if err := CreateArchiveFile(archivePath, src, WithArchiveFormat(ArchiveFormatZip)); err != nil {
		t.Fatalf("CreateArchiveFile: %v", err)
	}

	dst := t.TempDir()
	// 16-byte cap on a 4KB zip: must error.
	err := ExtractArchiveFile(archivePath, dst, WithArchiveMaxBytes(16))
	if !errors.Is(err, ErrArchiveTooLarge) {
		t.Errorf("got %v, want ErrArchiveTooLarge", err)
	}
}

// --- Archive format dispatch & error paths ---

func TestCreateArchive_UnknownFormat(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	dir := t.TempDir()
	err := CreateArchive(&buf, dir, WithArchiveFormat(ArchiveFormat(99)))
	if !errors.Is(err, ErrArchiveFormatUnknown) {
		t.Errorf("got %v, want ErrArchiveFormatUnknown", err)
	}
}

func TestExtractArchive_UnknownFormat(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	// 16 bytes of garbage that doesn't match any sniffer.
	r := bytes.NewReader([]byte("not-an-archive!!"))
	err := ExtractArchive(r, dir)
	if !errors.Is(err, ErrArchiveFormatUnknown) {
		t.Errorf("got %v, want ErrArchiveFormatUnknown", err)
	}
}

func TestExtractArchiveFile_OpenError(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	err := ExtractArchiveFile(filepath.Join(dir, "nonexistent.tar"), filepath.Join(dir, "out"))
	if err == nil {
		t.Fatal("expected error for missing archive")
	}
}

func TestCreateArchiveFile_CreateError(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatalf("setup: %v", err)
	}
	// Try to create archive at a path whose parent doesn't exist.
	err := CreateArchiveFile(filepath.Join(dir, "missing", "out.tar"), src, WithArchiveFormat(ArchiveFormatTar))
	if err == nil {
		t.Fatal("expected error for unwritable archive path")
	}
}

func TestOpenAutoArchive_OpenError(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	_, err := OpenAutoArchive(filepath.Join(dir, "nonexistent"))
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestOpenAutoArchive_BadGzip(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.gz")
	// gzip magic prefix but truncated body; gzip.NewReader will fail.
	if err := os.WriteFile(path, []byte{0x1f, 0x8b}, 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}
	_, err := OpenAutoArchive(path)
	if err == nil {
		t.Fatal("expected error from gzip.NewReader on truncated input")
	}
}

// --- ExtractArchive zip stream paths ---

func TestExtractArchive_ZipStream_NoCap(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if err := os.WriteFile(filepath.Join(src, "f"), []byte("payload"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}
	var buf bytes.Buffer
	if err := CreateArchive(&buf, src, WithArchiveFormat(ArchiveFormatZip)); err != nil {
		t.Fatalf("CreateArchive: %v", err)
	}
	// maxBytes=0 disables the cap, exercising the io.Copy-without-LimitReader branch.
	if err := ExtractArchive(&buf, filepath.Join(dir, "extract"), WithArchiveMaxBytes(0)); err != nil {
		t.Fatalf("ExtractArchive: %v", err)
	}
}

func TestExtractArchive_ZipStream_CorruptZip(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	// "PK\x05\x06" is the zip end-of-central-directory marker; without
	// the trailing bytes it parses as a corrupt zip.
	r := bytes.NewReader([]byte("PK\x05\x06\x00\x00\x00\x00garbage"))
	err := ExtractArchive(r, filepath.Join(dir, "extract"))
	if err == nil {
		t.Fatal("expected error from corrupt zip stream")
	}
}

func TestExtractArchive_ZipStream_TooLarge(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if err := os.WriteFile(filepath.Join(src, "f"), bytes.Repeat([]byte("X"), 4096), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}
	var buf bytes.Buffer
	if err := CreateArchive(&buf, src, WithArchiveFormat(ArchiveFormatZip)); err != nil {
		t.Fatalf("CreateArchive: %v", err)
	}

	// 100 bytes is well below the zip's actual size, so the streaming
	// buffer trips the cap.
	err := ExtractArchive(&buf, filepath.Join(dir, "extract"), WithArchiveMaxBytes(100))
	if !errors.Is(err, ErrArchiveTooLarge) {
		t.Errorf("got %v, want ErrArchiveTooLarge", err)
	}
}

// --- ExtractArchive tar.gz round-trip via sniff path ---

func TestExtractArchive_TarGzStream(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if err := os.WriteFile(filepath.Join(src, "f"), []byte("payload"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}
	var buf bytes.Buffer
	if err := CreateArchive(&buf, src, WithArchiveFormat(ArchiveFormatTarGz)); err != nil {
		t.Fatalf("CreateArchive(tgz): %v", err)
	}
	if err := ExtractArchive(&buf, filepath.Join(dir, "extract")); err != nil {
		t.Fatalf("ExtractArchive(tgz): %v", err)
	}
	if !Exists(filepath.Join(dir, "extract", "f")) {
		t.Error("extracted file missing")
	}
}

// --- CreateArchive filter/symlink behavior ---

func TestCreateArchive_Zip_SkipsSymlinks(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if err := os.WriteFile(filepath.Join(src, "f.txt"), []byte("payload"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if err := os.Symlink("f.txt", filepath.Join(src, "lnk")); err != nil {
		t.Skipf("symlinks not supported: %v", err)
	}
	out := filepath.Join(dir, "out.zip")
	if err := CreateArchiveFile(out, src, WithArchiveFormat(ArchiveFormatZip)); err != nil {
		t.Fatalf("CreateArchiveFile: %v", err)
	}
	// Verify the symlink was skipped: extract and confirm only f.txt is present.
	dst := filepath.Join(dir, "extract")
	if err := ExtractArchiveFile(out, dst); err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if Exists(filepath.Join(dst, "lnk")) {
		t.Error("symlink unexpectedly present in zip extract")
	}
}

func TestCreateArchive_Zip_FilterFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if err := os.WriteFile(filepath.Join(src, "skip.tmp"), []byte("x"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if err := os.WriteFile(filepath.Join(src, "keep.txt"), []byte("y"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}
	out := filepath.Join(dir, "out.zip")
	err := CreateArchiveFile(out, src,
		WithArchiveFormat(ArchiveFormatZip),
		WithArchiveCreateFilter(func(p string, _ os.FileInfo) bool {
			return !strings.HasSuffix(p, ".tmp")
		}))
	if err != nil {
		t.Fatalf("CreateArchiveFile: %v", err)
	}
}

func TestCreateArchive_TarGz_FilterFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if err := os.WriteFile(filepath.Join(src, "skip.tmp"), []byte("x"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if err := os.WriteFile(filepath.Join(src, "keep.txt"), []byte("y"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}
	out := filepath.Join(dir, "out.tar.gz")
	err := CreateArchiveFile(out, src,
		WithArchiveFormat(ArchiveFormatTarGz),
		WithArchiveCreateFilter(func(p string, _ os.FileInfo) bool {
			return !strings.HasSuffix(p, ".tmp")
		}))
	if err != nil {
		t.Fatalf("CreateArchiveFile: %v", err)
	}
}
