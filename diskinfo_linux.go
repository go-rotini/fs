//go:build linux

package fs

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

func diskUsageOf(path string) (DiskUsage, error) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return DiskUsage{}, wrapPathError(opDiskUsage, path, err)
	}
	bsize := uint64(st.Bsize)
	used := st.Blocks - st.Bfree
	return DiskUsage{
		TotalBytes:     bsize * st.Blocks,
		FreeBytes:      bsize * st.Bfree,
		AvailableBytes: bsize * st.Bavail,
		UsedBytes:      bsize * used,
		InodesTotal:    st.Files,
		InodesFree:     st.Ffree,
	}, nil
}

// mountInfoEntry is the relevant subset of a /proc/self/mountinfo line.
type mountInfoEntry struct {
	mountPoint string
	fsType     string
}

// readMountInfo parses /proc/self/mountinfo. The kernel-documented
// format is:
//
//	mount_id parent_id maj:min root mount_point options - fs_type source super_options
//
// Field 5 (0-indexed: 4) is the mount point. The "-" separator
// before the fs_type lets us locate the type even when option
// fields contain whitespace.
func readMountInfo() ([]mountInfoEntry, error) {
	f, err := os.Open("/proc/self/mountinfo")
	if err != nil {
		return nil, err //nolint:wrapcheck // outer caller wraps
	}
	defer f.Close()

	var out []mountInfoEntry
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) < 7 {
			continue
		}
		mp := fields[4]
		sepIdx := -1
		for i, fld := range fields {
			if fld == "-" {
				sepIdx = i
				break
			}
		}
		if sepIdx < 0 || sepIdx+1 >= len(fields) {
			continue
		}
		out = append(out, mountInfoEntry{
			mountPoint: mp,
			fsType:     fields[sepIdx+1],
		})
	}
	return out, nil
}

// resolveMountInfo finds the longest-prefix mountInfoEntry covering
// path. Path is canonicalized via Abs+EvalSymlinks so the comparison
// is against the kernel-visible inode-resolved form.
func resolveMountInfo(path string) (mountInfoEntry, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return mountInfoEntry{}, err //nolint:wrapcheck // outer caller wraps
	}
	if resolved, eerr := filepath.EvalSymlinks(abs); eerr == nil {
		abs = resolved
	}

	entries, err := readMountInfo()
	if err != nil {
		return mountInfoEntry{}, err
	}

	var best mountInfoEntry
	bestLen := -1
	for _, e := range entries {
		if abs == e.mountPoint || strings.HasPrefix(abs, e.mountPoint+"/") || e.mountPoint == "/" {
			if len(e.mountPoint) > bestLen {
				best = e
				bestLen = len(e.mountPoint)
			}
		}
	}
	if bestLen < 0 {
		// Fall back to the synthetic root mount entry.
		return mountInfoEntry{mountPoint: "/", fsType: "unknown"}, nil
	}
	return best, nil
}

func mountPointOf(path string) (string, error) {
	e, err := resolveMountInfo(path)
	if err != nil {
		return "", wrapPathError(opMountPoint, path, err)
	}
	return e.mountPoint, nil
}

func filesystemTypeOf(path string) (string, error) {
	e, err := resolveMountInfo(path)
	if err != nil {
		return "", wrapPathError(opFilesystemType, path, err)
	}
	if e.fsType == "" {
		return "unknown", nil
	}
	return e.fsType, nil
}
