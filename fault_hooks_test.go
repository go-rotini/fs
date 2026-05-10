package fs

import (
	"errors"
	"os"
	"syscall"
	"testing"
	"time"
)

// Test scaffolding for fault-injection tests across the package.
//
// Production code in fault_hooks.go routes os.* calls through
// package-level function-typed vars so tests in this file (and the
// topical *_test.go files that compose with it) can swap in
// fault-injecting variants. The helpers here snapshot and restore
// those vars so tests don't bleed state between runs.
//
// Tests that use [newFaultyHooks] MUST NOT call t.Parallel — the
// hook vars are package-global, so concurrent swappers race against
// each other and against any other test that calls a hooked helper.

// errInjected is the sentinel returned by every fault helper. Tests
// assert via errors.Is(err, errInjected).
var errInjected = errors.New("fault injected")

// exdevError returns the platform's EXDEV (cross-device link) error.
// Used by tests to provoke the EXDEV branch in [Move] without
// needing a real cross-mount setup.
func exdevError() error { return syscall.EXDEV }

// faultyHooks captures the current values of every fault-prone hook
// and restores them via t.Cleanup.
type faultyHooks struct {
	t               *testing.T
	origOpenFile    func(string, int, os.FileMode) (*os.File, error)
	origOpen        func(string) (*os.File, error)
	origRename      func(string, string) error
	origChmod       func(string, os.FileMode) error
	origChtimes     func(string, time.Time, time.Time) error
	origStat        func(string) (os.FileInfo, error)
	origLstat       func(string) (os.FileInfo, error)
	origMkdirAll    func(string, os.FileMode) error
	origSymlink     func(string, string) error
	origLink        func(string, string) error
	origReadlink    func(string) (string, error)
	origRemove      func(string) error
	origFileSync    func(*os.File) error
	origFileClose   func(*os.File) error
	origFileWrite   func(*os.File, []byte) (int, error)
	origFileWriteAt func(*os.File, []byte, int64) (int, error)
}

// newFaultyHooks snapshots the current hook values and registers
// a t.Cleanup that restores them after the test. Returns a receiver
// whose methods install fault-injecting variants for individual hooks.
func newFaultyHooks(t *testing.T) *faultyHooks {
	t.Helper()
	h := &faultyHooks{
		t:               t,
		origOpenFile:    osOpenFile,
		origOpen:        osOpen,
		origRename:      osRename,
		origChmod:       osChmod,
		origChtimes:     osChtimes,
		origStat:        osStat,
		origLstat:       osLstat,
		origMkdirAll:    osMkdirAll,
		origSymlink:     osSymlink,
		origLink:        osLink,
		origReadlink:    osReadlink,
		origRemove:      osRemove,
		origFileSync:    fileSync,
		origFileClose:   fileClose,
		origFileWrite:   fileWrite,
		origFileWriteAt: fileWriteAt,
	}
	t.Cleanup(h.restore)
	return h
}

func (h *faultyHooks) restore() {
	osOpenFile = h.origOpenFile
	osOpen = h.origOpen
	osRename = h.origRename
	osChmod = h.origChmod
	osChtimes = h.origChtimes
	osStat = h.origStat
	osLstat = h.origLstat
	osMkdirAll = h.origMkdirAll
	osSymlink = h.origSymlink
	osLink = h.origLink
	osReadlink = h.origReadlink
	osRemove = h.origRemove
	fileSync = h.origFileSync
	fileClose = h.origFileClose
	fileWrite = h.origFileWrite
	fileWriteAt = h.origFileWriteAt
}

func (h *faultyHooks) failSyncAlways() {
	fileSync = func(*os.File) error { return errInjected }
}

// failCloseAlways replaces fileClose with a function that closes the
// underlying fd (so subsequent open calls have descriptors available)
// and reports errInjected to the caller.
func (h *faultyHooks) failCloseAlways() {
	orig := h.origFileClose
	fileClose = func(f *os.File) error {
		_ = orig(f)
		return errInjected
	}
}

func (h *faultyHooks) failWriteAlways() {
	fileWrite = func(*os.File, []byte) (int, error) { return 0, errInjected }
}

func (h *faultyHooks) failWriteAtAlways() {
	fileWriteAt = func(*os.File, []byte, int64) (int, error) { return 0, errInjected }
}

func (h *faultyHooks) failRenameAlways() {
	osRename = func(string, string) error { return errInjected }
}

func (h *faultyHooks) failChmodAlways() {
	osChmod = func(string, os.FileMode) error { return errInjected }
}

func (h *faultyHooks) failChtimesAlways() {
	osChtimes = func(string, time.Time, time.Time) error { return errInjected }
}

func (h *faultyHooks) failStatAlways() {
	osStat = func(string) (os.FileInfo, error) { return nil, errInjected }
}

func (h *faultyHooks) failLstatAlways() {
	osLstat = func(string) (os.FileInfo, error) { return nil, errInjected }
}

func (h *faultyHooks) failOpenAlways() {
	osOpen = func(string) (*os.File, error) { return nil, errInjected }
}

func (h *faultyHooks) failMkdirAllAlways() {
	osMkdirAll = func(string, os.FileMode) error { return errInjected }
}

func (h *faultyHooks) failSymlinkAlways() {
	osSymlink = func(string, string) error { return errInjected }
}

func (h *faultyHooks) failLinkAlways() {
	osLink = func(string, string) error { return errInjected }
}

func (h *faultyHooks) failReadlinkAlways() {
	osReadlink = func(string) (string, error) { return "", errInjected }
}

func (h *faultyHooks) failRemoveAlways() {
	osRemove = func(string) error { return errInjected }
}

func (h *faultyHooks) failOpenFileAlways() {
	osOpenFile = func(string, int, os.FileMode) (*os.File, error) { return nil, errInjected }
}
