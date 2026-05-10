//go:build windows

package fs

import (
	"path/filepath"
	"syscall"
	"unsafe"
)

var (
	kernel32                  = syscall.NewLazyDLL("kernel32.dll")
	procGetDiskFreeSpaceExW   = kernel32.NewProc("GetDiskFreeSpaceExW")
	procGetVolumePathNameW    = kernel32.NewProc("GetVolumePathNameW")
	procGetVolumeInformationW = kernel32.NewProc("GetVolumeInformationW")
)

func diskUsageOf(path string) (DiskUsage, error) {
	pathp, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return DiskUsage{}, wrapPathError(opDiskUsage, path, err)
	}
	var freeForUser, totalBytes, totalFree uint64
	r1, _, e1 := procGetDiskFreeSpaceExW.Call(
		uintptr(unsafe.Pointer(pathp)),
		uintptr(unsafe.Pointer(&freeForUser)),
		uintptr(unsafe.Pointer(&totalBytes)),
		uintptr(unsafe.Pointer(&totalFree)),
	)
	if r1 == 0 {
		return DiskUsage{}, wrapPathError(opDiskUsage, path, e1)
	}
	used := uint64(0)
	if totalBytes > totalFree {
		used = totalBytes - totalFree
	}
	return DiskUsage{
		TotalBytes:     totalBytes,
		FreeBytes:      totalFree,
		AvailableBytes: freeForUser,
		UsedBytes:      used,
		// Inode fields are zero — Windows doesn't surface inode counts.
	}, nil
}

func mountPointOf(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", wrapPathError(opMountPoint, path, err)
	}
	pathp, err := syscall.UTF16PtrFromString(abs)
	if err != nil {
		return "", wrapPathError(opMountPoint, path, err)
	}

	const bufLen = 512
	buf := make([]uint16, bufLen)
	r1, _, e1 := procGetVolumePathNameW.Call(
		uintptr(unsafe.Pointer(pathp)),
		uintptr(unsafe.Pointer(&buf[0])),
		uintptr(bufLen),
	)
	if r1 == 0 {
		return "", wrapPathError(opMountPoint, path, e1)
	}
	return syscall.UTF16ToString(buf), nil
}

func filesystemTypeOf(path string) (string, error) {
	mp, err := mountPointOf(path)
	if err != nil {
		return "", wrapPathError(opFilesystemType, path, err)
	}
	mpp, err := syscall.UTF16PtrFromString(mp)
	if err != nil {
		return "", wrapPathError(opFilesystemType, path, err)
	}

	var (
		volumeName   [256]uint16
		volSerial    uint32
		maxComponent uint32
		fsFlags      uint32
		fsName       [256]uint16
	)
	r1, _, e1 := procGetVolumeInformationW.Call(
		uintptr(unsafe.Pointer(mpp)),
		uintptr(unsafe.Pointer(&volumeName[0])),
		uintptr(len(volumeName)),
		uintptr(unsafe.Pointer(&volSerial)),
		uintptr(unsafe.Pointer(&maxComponent)),
		uintptr(unsafe.Pointer(&fsFlags)),
		uintptr(unsafe.Pointer(&fsName[0])),
		uintptr(len(fsName)),
	)
	if r1 == 0 {
		return "", wrapPathError(opFilesystemType, path, e1)
	}
	t := syscall.UTF16ToString(fsName[:])
	if t == "" {
		return "unknown", nil
	}
	return t, nil
}
