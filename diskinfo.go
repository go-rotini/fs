package fs

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// DiskUsage describes the filesystem capacity of the volume
// containing a path. InodesTotal / InodesFree are zero on Windows
// (and on other filesystems that don't surface inode counts).
type DiskUsage struct {
	TotalBytes     uint64
	FreeBytes      uint64 // free for any user (root may use more)
	AvailableBytes uint64 // free for the calling user (after reserved blocks)
	UsedBytes      uint64
	InodesTotal    uint64
	InodesFree     uint64
}

// ErrInsufficientSpace is returned by [PreflightSpace] when a
// filesystem doesn't have enough free bytes for the requested
// reservation.
var ErrInsufficientSpace = errors.New("fs: insufficient free space")

const (
	opDiskUsage         = "diskusage"
	opMountPoint        = "mountpoint"
	opFilesystemType    = "filesystemtype"
	opPreflightSpace    = "preflightspace"
	opIsCaseInsensitive = "iscaseinsensitive"
)

// DiskUsageOf returns the [DiskUsage] for the filesystem containing
// path. POSIX uses `syscall.Statfs`; Windows uses
// `GetDiskFreeSpaceExW` via `syscall.Syscall6`. Inode fields are
// zero on Windows where the concept doesn't apply.
func DiskUsageOf(path string) (DiskUsage, error) { return diskUsageOf(path) }

// MountPoint returns the absolute mount point of the filesystem
// containing path. POSIX walks the kernel-supplied mount table
// (Linux: `/proc/self/mountinfo`; BSD/macOS: `getmntinfo` via
// `syscall.Statfs`). Windows uses `GetVolumePathName`.
func MountPoint(path string) (string, error) { return mountPointOf(path) }

// FilesystemType returns a best-effort string identification of the
// filesystem at path: "ext4", "apfs", "ntfs", "nfs4", etc. Returns
// "unknown" when the syscall succeeds but the type cannot be
// classified.
func FilesystemType(path string) (string, error) { return filesystemTypeOf(path) }

// IsNetworkFS reports whether path lives on a network filesystem
// (NFS, SMB, FUSE-network, AFP). Used to recommend
// [WithPolling]-based watching where kernel notifications are
// unreliable. A type-detection error is folded to false.
func IsNetworkFS(path string) bool {
	fsType, err := FilesystemType(path)
	if err != nil {
		return false
	}
	return isNetworkFSType(fsType)
}

func isNetworkFSType(fsType string) bool {
	t := strings.ToLower(fsType)
	switch t {
	case "nfs", "nfs4", "cifs", "smb", "smbfs", "afpfs", "webdav":
		return true
	}
	// fuse-mounted network filesystems advertise as "fuse" or "fuse.<name>"
	// where <name> identifies the helper (sshfs, davfs, gcsfuse, s3fs...).
	if t == "fuse" || strings.HasPrefix(t, "fuse.") {
		return true
	}
	return false
}

// IsCaseInsensitiveFS reports whether path lives on a case-insensitive
// volume. Probe-based: writes a temp file with a known case in the
// parent directory and checks whether the inverse case resolves to
// the same inode. Best-effort; a probe error returns
// (false, error).
func IsCaseInsensitiveFS(path string) (bool, error) {
	parent := path
	if info, err := os.Stat(path); err != nil || !info.IsDir() {
		parent = filepath.Dir(path)
	}
	if _, err := os.Stat(parent); err != nil {
		return false, wrapPathError(opIsCaseInsensitive, path, err)
	}

	probe := fmt.Sprintf("rotini-cis-AA-%d-%d", os.Getpid(), time.Now().UnixNano())
	upper := filepath.Join(parent, probe)
	lower := filepath.Join(parent, strings.ReplaceAll(probe, "AA", "aa"))

	f, err := os.Create(upper)
	if err != nil {
		return false, wrapPathError(opIsCaseInsensitive, parent, err)
	}
	_ = f.Close()
	defer os.Remove(upper)

	if _, err := os.Stat(lower); err == nil {
		return true, nil
	}
	return false, nil
}

// PreflightSpace returns nil if the filesystem containing path has
// at least requiredBytes available; otherwise [ErrInsufficientSpace].
// Use before large copies, archive extractions, downloads, etc.
//
// requiredBytes <= 0 is a no-op (returns nil).
func PreflightSpace(path string, requiredBytes int64) error {
	if requiredBytes <= 0 {
		return nil
	}
	u, err := DiskUsageOf(path)
	if err != nil {
		return err
	}
	if u.AvailableBytes < uint64(requiredBytes) {
		return wrapPathError(opPreflightSpace, path, ErrInsufficientSpace)
	}
	return nil
}
