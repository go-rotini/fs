package fs

import (
	"strings"
	"time"
)

// WatchEvent describes a single filesystem change reported by a
// [*Watcher] subscription. Path is the absolute path of the affected
// entry; Op is a bitmask of operations that occurred (a single
// kernel notification can carry multiple flags); Time is the
// wall-clock instant the cross-platform shell observed the event.
type WatchEvent struct {
	Path string
	Op   WatchOp
	Time time.Time
}

// WatchOp is a bitmask of filesystem operations. Multiple bits may
// be set when a single kernel event reports compound activity (e.g.,
// `WatchCreate|WatchWrite` for a freshly-written file).
//
// The constants are prefixed Watch* because the unprefixed names
// (Create, Write, Remove, Rename, Chmod) collide with existing fs
// package functions.
type WatchOp uint32

const (
	// WatchCreate fires when a path appears (was missing, now present).
	WatchCreate WatchOp = 1 << iota
	// WatchWrite fires when a path's contents are modified.
	WatchWrite
	// WatchRemove fires when a path is unlinked.
	WatchRemove
	// WatchRename fires when a path is moved (the path that vanished
	// gets WatchRename; the destination, if observed, gets WatchCreate).
	WatchRename
	// WatchChmod fires when a path's mode bits change.
	WatchChmod
)

// String renders the set bits in canonical order, joined by '+'.
// Returns "none" when no bits are set.
func (o WatchOp) String() string {
	if o == 0 {
		return "none"
	}
	var parts []string
	if o&WatchCreate != 0 {
		parts = append(parts, "create")
	}
	if o&WatchWrite != 0 {
		parts = append(parts, "write")
	}
	if o&WatchRemove != 0 {
		parts = append(parts, "remove")
	}
	if o&WatchRename != 0 {
		parts = append(parts, "rename")
	}
	if o&WatchChmod != 0 {
		parts = append(parts, "chmod")
	}
	return strings.Join(parts, "+")
}

// Has reports whether o includes target.
func (o WatchOp) Has(target WatchOp) bool { return o&target != 0 }
