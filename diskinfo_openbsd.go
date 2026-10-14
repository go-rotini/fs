//go:build openbsd

package fs

import "syscall"

func diskUsageOf(path string) (DiskUsage, error) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return DiskUsage{}, wrapPathError(opDiskUsage, path, err)
	}
	bsize := uint64(st.F_bsize)
	return DiskUsage{
		TotalBytes:     bsize * st.F_blocks,
		FreeBytes:      bsize * st.F_bfree,
		AvailableBytes: bsize * uint64(max(st.F_bavail, 0)),
		UsedBytes:      bsize * (st.F_blocks - st.F_bfree),
		InodesTotal:    st.F_files,
		InodesFree:     st.F_ffree,
	}, nil
}

func mountPointOf(path string) (string, error) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return "", wrapPathError(opMountPoint, path, err)
	}
	return cstrFromInt8(st.F_mntonname[:]), nil
}

func filesystemTypeOf(path string) (string, error) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return "", wrapPathError(opFilesystemType, path, err)
	}
	t := cstrFromInt8(st.F_fstypename[:])
	if t == "" {
		return unknownLabel, nil
	}
	return t, nil
}

// cstrFromInt8 converts a NUL-terminated int8 buffer, the type OpenBSD's Statfs_t uses for its
// string fields, into a Go string.
func cstrFromInt8(buf []int8) string {
	n := 0
	for n < len(buf) && buf[n] != 0 {
		n++
	}
	out := make([]byte, n)
	for i := range n {
		out[i] = byte(buf[i])
	}
	return string(out)
}
