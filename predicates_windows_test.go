//go:build windows

package fs

import (
	"errors"
	"os"
	"testing"
)

// Windows reports a cross-volume rename as ERROR_NOT_SAME_DEVICE, not EXDEV.
func TestIsCrossDevice_WindowsNotSameDevice(t *testing.T) {
	t.Parallel()
	err := &os.LinkError{Op: "rename", Old: `C:\a`, New: `D:\a`, Err: errNotSameDevice}
	if !isCrossDevice(err) {
		t.Error("isCrossDevice did not recognize ERROR_NOT_SAME_DEVICE")
	}
	if !errors.Is(wrapPathError(opMove, `D:\a`, err), ErrCrossDevice) {
		t.Error("wrapped ERROR_NOT_SAME_DEVICE should match ErrCrossDevice")
	}
}
