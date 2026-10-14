//go:build !windows

package fs

// isRemoteVolume reports false: outside Windows, [IsNetworkFS] judges
// by the filesystem type alone.
func isRemoteVolume(string) bool { return false }
