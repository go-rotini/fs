//go:build !unix && !windows

package fs

// Disk information is not available on this platform.

func diskUsageOf(path string) (DiskUsage, error) {
	return DiskUsage{}, wrapPathError(opDiskUsage, path, ErrNotSupported)
}

func mountPointOf(path string) (string, error) {
	return "", wrapPathError(opMountPoint, path, ErrNotSupported)
}

func filesystemTypeOf(path string) (string, error) {
	return "", wrapPathError(opFilesystemType, path, ErrNotSupported)
}
