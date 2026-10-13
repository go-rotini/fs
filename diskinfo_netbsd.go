//go:build netbsd

package fs

import (
	"bytes"

	"golang.org/x/sys/unix"
)

// NetBSD has no statfs(2); statvfs(2) carries the same facts. Block counts are in units of
// Frsize.
func statvfs(op, path string) (*unix.Statvfs_t, error) {
	var st unix.Statvfs_t
	if err := unix.Statvfs(path, &st); err != nil {
		return nil, wrapPathError(op, path, err)
	}
	return &st, nil
}

func diskUsageOf(path string) (DiskUsage, error) {
	st, err := statvfs(opDiskUsage, path)
	if err != nil {
		return DiskUsage{}, err
	}
	bsize := uint64(st.Frsize) //nolint:unconvert // uint32 on 32-bit NetBSD
	if bsize == 0 {
		bsize = uint64(st.Bsize) //nolint:unconvert // uint32 on 32-bit NetBSD
	}
	return DiskUsage{
		TotalBytes:     bsize * st.Blocks,
		FreeBytes:      bsize * st.Bfree,
		AvailableBytes: bsize * st.Bavail,
		UsedBytes:      bsize * (st.Blocks - st.Bfree),
		InodesTotal:    st.Files,
		InodesFree:     st.Ffree,
	}, nil
}

func mountPointOf(path string) (string, error) {
	st, err := statvfs(opMountPoint, path)
	if err != nil {
		return "", err
	}
	return cstr(st.Mntonname[:]), nil
}

func filesystemTypeOf(path string) (string, error) {
	st, err := statvfs(opFilesystemType, path)
	if err != nil {
		return "", err
	}
	if t := cstr(st.Fstypename[:]); t != "" {
		return t, nil
	}
	return unknownLabel, nil
}

// cstr converts a NUL-terminated byte buffer into a Go string.
func cstr(buf []byte) string {
	if i := bytes.IndexByte(buf, 0); i >= 0 {
		buf = buf[:i]
	}
	return string(buf)
}
