package fs

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/unix"
)

func diskUsageOf(path string) (DiskUsage, error) {
	var st unix.Statvfs_t
	if err := unix.Statvfs(path, &st); err != nil {
		return DiskUsage{}, wrapPathError(opDiskUsage, path, err)
	}
	return DiskUsage{
		TotalBytes:     st.Frsize * st.Blocks,
		FreeBytes:      st.Frsize * st.Bfree,
		AvailableBytes: st.Frsize * st.Bavail,
		UsedBytes:      st.Frsize * (st.Blocks - st.Bfree),
		InodesTotal:    st.Files,
		InodesFree:     st.Ffree,
	}, nil
}

// mountPointOf finds the longest mount point in /etc/mnttab that
// contains path (symlinks resolved).
func mountPointOf(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", wrapPathError(opMountPoint, path, err)
	}
	if resolved, eerr := filepath.EvalSymlinks(abs); eerr == nil {
		abs = resolved
	}
	f, err := os.Open("/etc/mnttab")
	if err != nil {
		return "", wrapPathError(opMountPoint, path, err)
	}
	defer f.Close()
	best := "/"
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		fields := strings.Split(sc.Text(), "\t")
		if len(fields) < 2 {
			continue
		}
		mp := fields[1]
		if (abs == mp || strings.HasPrefix(abs, strings.TrimSuffix(mp, "/")+"/")) && len(mp) > len(best) {
			best = mp
		}
	}
	if err := sc.Err(); err != nil {
		return "", wrapPathError(opMountPoint, path, err)
	}
	return best, nil
}

func filesystemTypeOf(path string) (string, error) {
	var st unix.Statvfs_t
	if err := unix.Statvfs(path, &st); err != nil {
		return "", wrapPathError(opFilesystemType, path, err)
	}
	b := make([]byte, 0, len(st.Basetype))
	for _, c := range st.Basetype {
		if c == 0 {
			break
		}
		b = append(b, byte(c))
	}
	if len(b) == 0 {
		return unknownLabel, nil
	}
	return string(b), nil
}
