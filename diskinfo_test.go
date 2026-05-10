package fs

import (
	"errors"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// --- DiskUsageOf ---

func TestDiskUsageOf_Basics(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	u, err := DiskUsageOf(dir)
	if err != nil {
		t.Fatalf("DiskUsageOf: %v", err)
	}
	if u.TotalBytes == 0 {
		t.Error("TotalBytes = 0; expected >0 on a real volume")
	}
	if u.UsedBytes+u.FreeBytes < u.TotalBytes/2 {
		// Sanity: used + free should approximately equal total. Allow
		// slack for reserved blocks; this is just a smell test.
		t.Logf("used=%d free=%d total=%d", u.UsedBytes, u.FreeBytes, u.TotalBytes)
	}
	if u.AvailableBytes > u.FreeBytes {
		t.Errorf("AvailableBytes=%d > FreeBytes=%d (impossible)", u.AvailableBytes, u.FreeBytes)
	}
}

func TestDiskUsageOf_MissingPath(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	_, err := DiskUsageOf(filepath.Join(dir, "missing"))
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("got %v, want ErrNotFound", err)
	}
}

// --- MountPoint ---

func TestMountPoint_NonEmpty(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	mp, err := MountPoint(dir)
	if err != nil {
		t.Fatalf("MountPoint: %v", err)
	}
	if mp == "" {
		t.Error("empty mount point")
	}
	if !filepath.IsAbs(mp) {
		t.Errorf("not absolute: %s", mp)
	}
}

// --- FilesystemType ---

func TestFilesystemType_NonEmpty(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	fsType, err := FilesystemType(dir)
	if err != nil {
		t.Fatalf("FilesystemType: %v", err)
	}
	if fsType == "" {
		t.Error("empty filesystem type")
	}
	t.Logf("filesystem type: %q", fsType)
}

// --- IsNetworkFS classifier ---

func TestIsNetworkFSType_Classifier(t *testing.T) {
	t.Parallel()
	cases := []struct {
		fs   string
		want bool
	}{
		{"nfs", true},
		{"nfs4", true},
		{"NFS", true},
		{"cifs", true},
		{"smb", true},
		{"smbfs", true},
		{"afpfs", true},
		{"webdav", true},
		{"fuse", true},
		{"fuse.sshfs", true},
		{"fuse.gcsfuse", true},
		{"ext4", false},
		{"apfs", false},
		{"ntfs", false},
		{"tmpfs", false},
		{"unknown", false},
		{"", false},
	}
	for _, c := range cases {
		if got := isNetworkFSType(c.fs); got != c.want {
			t.Errorf("isNetworkFSType(%q) = %v, want %v", c.fs, got, c.want)
		}
	}
}

func TestIsNetworkFS_LocalPath(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if IsNetworkFS(dir) {
		t.Error("local temp dir reported as network filesystem")
	}
}

// --- IsCaseInsensitiveFS ---

func TestIsCaseInsensitiveFS_Reports(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	got, err := IsCaseInsensitiveFS(dir)
	if err != nil {
		t.Fatalf("IsCaseInsensitiveFS: %v", err)
	}
	// We don't assert true/false because the answer depends on the
	// volume the temp dir lives on. Just verify the probe ran.
	t.Logf("case-insensitive(%s) = %v (runtime.GOOS=%s)", dir, got, runtime.GOOS)
}

func TestIsCaseInsensitiveFS_MissingParent(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	// Pass a path whose grandparent doesn't exist.
	_, err := IsCaseInsensitiveFS(filepath.Join(dir, "doesnt-exist", "child"))
	// Either succeeds (using dir itself) or errors — both are tolerated;
	// the test verifies the probe doesn't panic.
	if err != nil {
		t.Logf("probe error (acceptable): %v", err)
	}
}

// --- PreflightSpace ---

func TestPreflightSpace_Sufficient(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	// 1 KiB should be available on any sane mount.
	if err := PreflightSpace(dir, 1024); err != nil {
		t.Errorf("PreflightSpace(1KiB): %v", err)
	}
}

func TestPreflightSpace_ZeroIsNoop(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := PreflightSpace(dir, 0); err != nil {
		t.Errorf("PreflightSpace(0) = %v, want nil", err)
	}
	if err := PreflightSpace(dir, -1); err != nil {
		t.Errorf("PreflightSpace(-1) = %v, want nil", err)
	}
}

func TestPreflightSpace_ExhaustsCap(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	u, err := DiskUsageOf(dir)
	if err != nil {
		t.Fatalf("DiskUsageOf: %v", err)
	}
	// Request a value greater than total (and thus available) capacity.
	// Use uint64→int64 with safe clamping; if total exceeds int64 max,
	// skip — extremely unlikely in real test environments.
	if u.TotalBytes > 1<<62 {
		t.Skip("disk too large to construct an over-cap test value")
	}
	overCap := int64(u.AvailableBytes) + int64(1<<30)
	err = PreflightSpace(dir, overCap)
	if !errors.Is(err, ErrInsufficientSpace) {
		t.Errorf("got %v, want ErrInsufficientSpace", err)
	}
	if err != nil && !strings.Contains(err.Error(), "preflight") {
		t.Errorf("error %q should mention op", err)
	}
}

func TestPreflightSpace_MissingPath(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	err := PreflightSpace(filepath.Join(dir, "missing"), 1024)
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("got %v, want ErrNotFound", err)
	}
}
