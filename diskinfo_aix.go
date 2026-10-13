package fs

import "golang.org/x/sys/unix"

func diskUsageOf(path string) (DiskUsage, error) {
	var st unix.Statfs_t
	if err := unix.Statfs(path, &st); err != nil {
		return DiskUsage{}, wrapPathError(opDiskUsage, path, err)
	}
	return DiskUsage{
		TotalBytes:     st.Bsize * st.Blocks,
		FreeBytes:      st.Bsize * st.Bfree,
		AvailableBytes: st.Bsize * st.Bavail,
		UsedBytes:      st.Bsize * (st.Blocks - st.Bfree),
		InodesTotal:    st.Files,
		InodesFree:     st.Ffree,
	}, nil
}

// mountPointOf returns [ErrNotSupported] on AIX.
func mountPointOf(path string) (string, error) {
	return "", wrapPathError(opMountPoint, path, ErrNotSupported)
}

// filesystemTypeOf returns [ErrNotSupported] on AIX.
func filesystemTypeOf(path string) (string, error) {
	return "", wrapPathError(opFilesystemType, path, ErrNotSupported)
}
