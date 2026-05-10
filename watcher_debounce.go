package fs

import (
	"sync"
	"time"
)

// debouncer implements a per-key trailing-edge debouncer. Each call
// to In() resets a timer for the key; the timer's expiry pushes one
// merged event to Out().
//
// Used by [*Watcher] to coalesce rapid bursts (typical for editor
// saves that emit several kernel events in <10ms) into a single
// subscriber-visible event.
type debouncer struct {
	delay time.Duration

	mu     sync.Mutex
	timers map[string]*timerState
	out    chan rawWatchEvent
	closed bool
}

type timerState struct {
	timer *time.Timer
	op    WatchOp // accumulated ops since last fire
}

func newDebouncer(delay time.Duration) *debouncer {
	return &debouncer{
		delay:  delay,
		timers: make(map[string]*timerState),
		out:    make(chan rawWatchEvent, 64),
	}
}

// In ingests an event. If delay <= 0 the debouncer is a pass-through.
func (d *debouncer) In(ev rawWatchEvent) {
	if d.delay <= 0 {
		d.mu.Lock()
		closed := d.closed
		d.mu.Unlock()
		if closed {
			return
		}
		select {
		case d.out <- ev:
		default:
			// Drop on overflow; out has 64-slot headroom.
		}
		return
	}

	d.mu.Lock()
	defer d.mu.Unlock()
	if d.closed {
		return
	}
	if st, ok := d.timers[ev.path]; ok {
		st.op |= ev.op
		st.timer.Reset(d.delay)
		return
	}
	// Capture the path locally so the timer callback doesn't depend
	// on the loop variable's identity should this code ever move
	// inside a range loop. st is captured so the accumulated op
	// (which other Reset calls may OR into) is read at fire time.
	path := ev.path
	st := &timerState{op: ev.op}
	st.timer = time.AfterFunc(d.delay, func() {
		d.mu.Lock()
		out := rawWatchEvent{path: path, op: st.op}
		delete(d.timers, path)
		closed := d.closed
		d.mu.Unlock()
		if closed {
			return
		}
		select {
		case d.out <- out:
		default:
		}
	})
	d.timers[path] = st
}

// Out returns the channel of debounced events.
func (d *debouncer) Out() <-chan rawWatchEvent { return d.out }

// Close stops every pending timer and closes Out. Idempotent.
func (d *debouncer) Close() {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.closed {
		return
	}
	d.closed = true
	for _, st := range d.timers {
		st.timer.Stop()
	}
	d.timers = nil
	close(d.out)
}
