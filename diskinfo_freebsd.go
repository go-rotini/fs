//go:build freebsd

package fs

import "syscall"

func diskUsageOf(path string) (DiskUsage, error) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return DiskUsage{}, wrapPathError(opDiskUsage, path, err)
	}
	bsize := st.Bsize
	used := st.Blocks - st.Bfree

	// Bavail is signed on FreeBSD; clamp negatives to 0 for the
	// unsigned DiskUsage fields.
	avail := max(st.Bavail, 0)
	ffree := max(st.Ffree, 0)

	return DiskUsage{
		TotalBytes:     bsize * st.Blocks,
		FreeBytes:      bsize * st.Bfree,
		AvailableBytes: bsize * uint64(avail),
		UsedBytes:      bsize * used,
		InodesTotal:    st.Files,
		InodesFree:     uint64(ffree),
	}, nil
}

func mountPointOf(path string) (string, error) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return "", wrapPathError(opMountPoint, path, err)
	}
	return cstrFromInt8(st.Mntonname[:]), nil
}

func filesystemTypeOf(path string) (string, error) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return "", wrapPathError(opFilesystemType, path, err)
	}
	t := cstrFromInt8(st.Fstypename[:])
	if t == "" {
		return unknownLabel, nil
	}
	return t, nil
}

// cstrFromInt8 converts a NUL-terminated [N]int8 buffer (the type
// freebsd's stdlib uses for Statfs_t string fields) into a Go string.
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
