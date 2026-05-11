package fs

import (
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
// Symlinks are NOT followed. The parallel walker re-implements only
// the most common case from [Walk]; callers needing symlink-follow,
// max-depth, or gitignore filtering should compose [Walk] (single-
// threaded) with their own work-pool.
//
//nolint:gocognit // worker-pool orchestration: complexity reflects the dispatch + completion-tracking needed
func WalkParallel(root string, fn WalkParallelFunc, workers int) error {
	if workers <= 0 {
		workers = runtime.NumCPU()
	}

	info, err := os.Lstat(root)
	if err != nil {
		return wrapPathError(opWalkParallel, root, err)
	}

	type job struct {
		path string
		info stdfs.DirEntry
	}

	jobs := make(chan job, workers*8)
	errs := make(chan error, workers)
	var (
		wg       sync.WaitGroup
		pending  sync.WaitGroup
		stopOnce sync.Once
		stopCh   = make(chan struct{})
	)

	stop := func() {
		stopOnce.Do(func() {
			close(stopCh)
		})
	}

	dispatch := func(path string, e stdfs.DirEntry) {
		pending.Add(1)
		select {
		case <-stopCh:
			pending.Done()
		case jobs <- job{path: path, info: e}:
		}
	}

	// Seed the root entry.
	dispatch(root, stdfs.FileInfoToDirEntry(info))

	for range workers {
		wg.Go(func() {
			for {
				select {
				case <-stopCh:
					return
				case j, ok := <-jobs:
					if !ok {
						return
					}
					if ferr := fn(j.path, j.info); ferr != nil {
						errs <- ferr
						stop()
						pending.Done()
						return
					}
					if j.info.IsDir() {
						entries, rerr := os.ReadDir(j.path)
						if rerr != nil && !errors.Is(rerr, stdfs.ErrNotExist) {
							errs <- wrapPathError(opWalkParallel, j.path, rerr)
							stop()
							pending.Done()
							return
						}
						for _, child := range entries {
							dispatch(filepath.Join(j.path, child.Name()), child)
						}
					}
					pending.Done()
				}
			}
		})
	}

	// Close jobs when all dispatched work has been processed.
	go func() {
		pending.Wait()
		close(jobs)
	}()
	wg.Wait()
	close(errs)
	for e := range errs {
		if e != nil {
			return e
		}
	}
	return nil
}
