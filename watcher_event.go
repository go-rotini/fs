package fs

import (
	"strings"
	"time"
)

// WatchEvent describes a single filesystem change reported by a
// [*Watcher] subscription.
type WatchEvent struct {
	// Path is the absolute path of the affected entry.
	Path string
	// Op holds every operation seen for Path since its last event;
	// several bits may be set.
	Op WatchOp
	// Time is when the event was delivered, after debouncing.
	Time time.Time
}

// WatchOp is a bitmask of filesystem operations. Multiple bits may be
// set when the debouncer merges several changes to one path (e.g.,
// `WatchCreate|WatchWrite` for a freshly-written file).
type WatchOp uint32

const (
	// WatchCreate fires when a path appears (was missing, now present).
	WatchCreate WatchOp = 1 << iota
	// WatchWrite fires when a path's size or modification time
	// changes.
	WatchWrite
	// WatchRemove fires when a path is unlinked.
	WatchRemove
	// WatchRename is reserved for a moved path. The polling watcher
	// never sets it: a rename shows up as [WatchRemove] on the old
	// path and [WatchCreate] on the new one.
	WatchRename
	// WatchChmod fires when a path's permission bits change.
	WatchChmod
	// WatchOverflow reports that events were dropped because a buffer
	// was full, most often a subscriber's (see [WithBufferSize]). It is
	// sent alone, with Path set to the watched path, once there is
	// room; the subscriber should rescan what it watches.
	WatchOverflow
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
	if o&WatchOverflow != 0 {
		parts = append(parts, "overflow")
	}
	return strings.Join(parts, "+")
}

// Has reports whether o includes target.
func (o WatchOp) Has(target WatchOp) bool { return o&target != 0 }
