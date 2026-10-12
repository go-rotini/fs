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
// the WalkParallel call. fn is invoked from several worker goroutines
// and must be safe for concurrent use.
//
// Unlike [WalkFunc], [filepath.SkipDir] has no special meaning: it
// aborts the walk like any other error.
type WalkParallelFunc func(path string, e stdfs.DirEntry) error

// WalkParallel walks root, including root itself, calling fn from
// workers goroutines. It returns the first error from fn or from
// reading a directory (a directory that vanishes mid-walk is skipped);
// other workers then stop promptly.
//
// workers <= 0 defaults to [runtime.NumCPU]. Entries are dispatched
// roughly breadth-first from one shared queue, but the order of fn
// calls is unspecified; use [Walk] for a deterministic order.
//
// Cancel ctx to stop the walk early; fn calls already running
// complete, and WalkParallel returns nil rather than ctx.Err(). ctx
// must not be nil.
//
// Symlinks are not followed. For symlink following, max depth or
// gitignore filtering, use [Walk].
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

	stopCtx, stopCancel := context.WithCancel(ctx)
	defer stopCancel()

	// Watcher and workers ride separate WaitGroups. Workers exit when
	// the queue drains; the watcher exits when stopCtx fires. On the
	// happy path: wait for workers, then cancel stopCtx so the watcher
	// wakes, then wait for the watcher. This guarantees every
	// goroutine has exited before WalkParallel returns.
	var watcherWG sync.WaitGroup
	watcherWG.Go(func() {
		<-stopCtx.Done()
		q.shutdown()
	})

	var workersWG sync.WaitGroup
	for range workers {
		workersWG.Go(func() {
			parallelWorker(state, fn)
		})
	}
	workersWG.Wait()
	stopCancel()
	watcherWG.Wait()

	state.mu.Lock()
	err = state.firstErr
	state.mu.Unlock()
	return err
}

type parallelJob struct {
	path string
	info stdfs.DirEntry
}

// parallelQueue is an unbounded FIFO of parallelJob with a sync.Cond
// signal so workers can park when empty. inflight tracks work in
// progress so the queue knows when the walk has fully drained.
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

// pop blocks until a job is available, the queue is closed, or the
// walk has fully drained. Returns ok=false when the worker should
// exit.
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
			// Walk has drained; broadcast so every parked worker exits.
			q.cond.Broadcast()
			return parallelJob{}, false
		}
		q.cond.Wait()
	}
}

func (q *parallelQueue) done() {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.inflight--
	if q.inflight == 0 {
		q.cond.Broadcast()
	}
}

func (q *parallelQueue) shutdown() {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.closed = true
	q.cond.Broadcast()
}

type parallelState struct {
	q *parallelQueue

	mu       sync.Mutex
	firstErr error
}

// setErr records the first error and signals shutdown. Later errors
// are dropped.
func (s *parallelState) setErr(err error) {
	s.mu.Lock()
	if s.firstErr == nil {
		s.firstErr = err
	}
	s.mu.Unlock()
	s.q.shutdown()
}

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
