//go:build !(unix || js || wasip1)

package fs

import "os"

// fileID is empty here: [fileIdentity] never has one to report.
type fileID struct{}

// fileIdentity reports ok=false: on this platform the information
// [os.ReadDir] and [os.Lstat] return carries no file identity, and
// fetching one costs an extra open per file.
func fileIdentity(os.FileInfo) (fileID, bool) {
	return fileID{}, false
}
