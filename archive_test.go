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

// readBackTree reads every regular file under root into a relative-path → contents map.
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
