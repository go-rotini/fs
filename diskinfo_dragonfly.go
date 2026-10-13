package fs

import "golang.org/x/sys/unix"

func statfsDragonfly(path, op string) (*unix.Statfs_t, error) {
	var st unix.Statfs_t
	if err := unix.Statfs(path, &st); err != nil {
		return nil, wrapPathError(op, path, err)
	}
	return &st, nil
}

func diskUsageOf(path string) (DiskUsage, error) {
	st, err := statfsDragonfly(path, opDiskUsage)
	if err != nil {
		return DiskUsage{}, err
	}
	bsize := uint64(max(st.Bsize, 0))
	return DiskUsage{
		TotalBytes:     bsize * uint64(max(st.Blocks, 0)),
		FreeBytes:      bsize * uint64(max(st.Bfree, 0)),
		AvailableBytes: bsize * uint64(max(st.Bavail, 0)),
		UsedBytes:      bsize * uint64(max(st.Blocks-st.Bfree, 0)),
		InodesTotal:    uint64(max(st.Files, 0)),
		InodesFree:     uint64(max(st.Ffree, 0)),
	}, nil
}

func mountPointOf(path string) (string, error) {
	st, err := statfsDragonfly(path, opMountPoint)
	if err != nil {
		return "", err
	}
	return unix.ByteSliceToString(st.Mntonname[:]), nil
}

func filesystemTypeOf(path string) (string, error) {
	st, err := statfsDragonfly(path, opFilesystemType)
	if err != nil {
		return "", err
	}
	if t := unix.ByteSliceToString(st.Fstypename[:]); t != "" {
		return t, nil
	}
	return unknownLabel, nil
}
