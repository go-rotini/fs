package fs

import (
	"context"
	"errors"
	stdfs "io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sync"
)

const opWalkParallel = "walkparallel"

// WalkParallelFunc is the per-entry callback for [WalkParallel].
// Returning an error aborts the walk and surfaces that error from
// the WalkParallel call. The callback is invoked from one of N
// worker goroutines, so it MUST be safe for concurrent use.
//
// Unlike [WalkFunc], the parallel variant does NOT honor
// [filepath.SkipDir] — directory traversal is interleaved with the
// fn calls, so a SkipDir return from one entry cannot prune work
// that's already been dispatched.
type WalkParallelFunc func(path string, e stdfs.DirEntry) error

// WalkParallel walks root concurrently using workers goroutines.
// Returns the first non-nil error returned by fn (other workers
// observe a sentinel cancel and exit promptly).
//
// workers <= 0 defaults to [runtime.NumCPU]. The walk is breadth-
// first within each worker, but global ordering across workers is
// unspecified — callers that need deterministic order should use
// [Walk] instead.
//
// Cancellation: cancel ctx to stop the walk early. Already-dispatched
// fn calls run to completion; new fn calls and new directory reads
// are skipped after cancel.
//
// Symlinks are NOT followed. The parallel walker re-implements only
// the most common case from [Walk]; callers needing symlink-follow,
// max-depth, or gitignore filtering should compose [Walk] (single-
// threaded) with their own work-pool.
//
// Internally the queue is an unbounded slice guarded by a [sync.Cond];
// workers pop the front, directory reads push the back. This avoids
// the bounded-channel deadlock that a fixed-size buffer suffers when
// any directory has more children than the buffer can hold.
//
//nolint:contextcheck // we derive stopCtx via context.WithCancel(ctx) on line ~70; contextcheck doesn't see the local-var chain
func WalkParallel(ctx context.Context, root string, fn WalkParallelFunc, workers int) error {
	if workers <= 0 {
		workers = runtime.NumCPU()
	}

	info, err := os.Lstat(root)
	if err != nil {
		return wrapPathError(opWalkParallel, root, err)
	}

	q := newParallelQueue()
	q.push(parallelJob{path: root, info: stdfs.FileInfoToDirEntry(info)})

	state := &parallelState{q: q}
	if ctx == nil {
		ctx = context.Background()
	}

	// Stop the workers when ctx is canceled.
	stopCtx, stopCancel := context.WithCancel(ctx)
	defer stopCancel()
	go func() {
		<-stopCtx.Done()
		q.shutdown()
	}()

	var wg sync.WaitGroup
	for range workers {
		wg.Go(func() {
			parallelWorker(state, fn)
		})
	}
	wg.Wait()

	state.mu.Lock()
	err = state.firstErr
	state.mu.Unlock()
	return err
}

// parallelJob is one entry in the WalkParallel queue.
type parallelJob struct {
	path string
	info stdfs.DirEntry
}

// parallelQueue is an unbounded FIFO of parallelJob with a sync.Cond
// signal so workers can park when empty. Tracks in-flight work via
// a counter so the queue knows when the walk is complete.
type parallelQueue struct {
	mu       sync.Mutex
	cond     *sync.Cond
	jobs     []parallelJob
	inflight int
	closed   bool
}

func newParallelQueue() *parallelQueue {
	q := &parallelQueue{}
	q.cond = sync.NewCond(&q.mu)
	return q
}

// push adds a job to the queue and bumps inflight. Wakes one
// waiting worker.
func (q *parallelQueue) push(j parallelJob) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.closed {
		return
	}
	q.jobs = append(q.jobs, j)
	q.inflight++
	q.cond.Signal()
}

// pop blocks until a job is available, the queue is closed, OR the
// walk has drained (inflight==0). Returns ok=false when the worker
// should exit.
func (q *parallelQueue) pop() (parallelJob, bool) {
	q.mu.Lock()
	defer q.mu.Unlock()
	for {
		if q.closed {
			return parallelJob{}, false
		}
		if len(q.jobs) > 0 {
			j := q.jobs[0]
			q.jobs = q.jobs[1:]
			return j, true
		}
		if q.inflight == 0 {
			// Walk has fully drained; wake every other parked worker
			// so they can exit too.
			q.cond.Broadcast()
			return parallelJob{}, false
		}
		q.cond.Wait()
	}
}

// done decrements inflight after a job is fully processed (including
// its children dispatch). Broadcasts when the walk completes.
func (q *parallelQueue) done() {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.inflight--
	if q.inflight == 0 {
		q.cond.Broadcast()
	}
}

// shutdown marks the queue closed and wakes every worker so they
// exit promptly. Called when ctx is canceled or the first error is
// observed.
func (q *parallelQueue) shutdown() {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.closed = true
	q.cond.Broadcast()
}

// parallelState carries the shared per-walk state between workers.
type parallelState struct {
	q *parallelQueue

	mu       sync.Mutex
	firstErr error
}

// setErr records the first error and signals the queue to shut down.
// Subsequent errors are dropped — first-error-wins.
func (s *parallelState) setErr(err error) {
	s.mu.Lock()
	if s.firstErr == nil {
		s.firstErr = err
	}
	s.mu.Unlock()
	s.q.shutdown()
}

// parallelWorker is the per-goroutine loop body.
func parallelWorker(s *parallelState, fn WalkParallelFunc) {
	for {
		j, ok := s.q.pop()
		if !ok {
			return
		}
		if ferr := fn(j.path, j.info); ferr != nil {
			s.setErr(ferr)
			s.q.done()
			return
		}
		if j.info.IsDir() {
			entries, rerr := os.ReadDir(j.path)
			if rerr != nil && !errors.Is(rerr, stdfs.ErrNotExist) {
				s.setErr(wrapPathError(opWalkParallel, j.path, rerr))
				s.q.done()
				return
			}
			for _, child := range entries {
				s.q.push(parallelJob{
					path: filepath.Join(j.path, child.Name()),
					info: child,
				})
			}
		}
		s.q.done()
	}
}
