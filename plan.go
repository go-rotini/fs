package fs

import (
	"encoding/json"
	"errors"
	"fmt"
	stdfs "io/fs"
	"os"
	"path/filepath"
	"strings"
)

const (
	opPlanApply    = "plan.apply"
	opPlanRollback = "plan.rollback"
	opPlanJournal  = "plan.journal"

	// journalLockFilename is the per-journal advisory lockfile. Apply,
	// Resume, and Rollback acquire it for the duration of their work.
	journalLockFilename = ".lock"

	// journalPlanFilename holds the immutable plan written at the start
	// of Apply. Split from the progress file so per-step rewrite cost
	// is independent of plan size.
	journalPlanFilename = "plan.json"

	// journalProgressFilename holds the per-step progress counter and
	// the finalized flag. Rewritten after every successful op.
	journalProgressFilename = "progress.json"

	// journalLegacyFilename is the single-file journal format. Apply
	// treats a directory containing it as an existing journal.
	journalLegacyFilename = "journal.json"

	// journalSchemaVersion guards against resuming journals written
	// by an incompatible package version.
	journalSchemaVersion = 1

	// journalBackupsDir holds pre-apply snapshots indexed by step.
	journalBackupsDir = "backups"

	// The journal copies plan data and overwritten files, so it is
	// owner-only.
	journalDirPerm  os.FileMode = 0o700
	journalFilePerm os.FileMode = 0o600
)

// PlanAction enumerates the operation kinds a [Plan] can hold.
type PlanAction int

const (
	// PlanActionCreate writes a new file. It fails with an error
	// wrapping [ErrAlreadyExists] if the path exists at apply time.
	PlanActionCreate PlanAction = iota + 1

	// PlanActionUpdate writes a file, creating it if missing. Existing
	// contents are backed up so the op is reversible.
	PlanActionUpdate

	// PlanActionDelete removes a file; a missing file is not an error.
	// The file is backed up before removal so Rollback can restore it.
	PlanActionDelete

	// PlanActionRename moves Source to Path, backing up and replacing
	// any existing file at Path. Reversible.
	PlanActionRename
)

const (
	planActionLabelCreate = "create"
	planActionLabelUpdate = "update"
	planActionLabelDelete = "delete"
	planActionLabelRename = "rename"
)

// String returns the action's lowercase name.
func (a PlanAction) String() string {
	switch a {
	case PlanActionCreate:
		return planActionLabelCreate
	case PlanActionUpdate:
		return planActionLabelUpdate
	case PlanActionDelete:
		return planActionLabelDelete
	case PlanActionRename:
		return planActionLabelRename
	default:
		return unknownLabel
	}
}

// PlanOp is a single operation inside a [Plan].
type PlanOp struct {
	// Action is the operation kind.
	Action PlanAction `json:"action"`
	// Path is the file the op writes or deletes, or the rename
	// destination.
	Path string `json:"path"`
	// Source is the rename source ([PlanActionRename] only).
	Source string `json:"source,omitempty"`
	// Data is the file content ([PlanActionCreate] and
	// [PlanActionUpdate]).
	Data []byte `json:"data,omitempty"`
	// Perm is the file mode ([PlanActionCreate] and
	// [PlanActionUpdate]).
	Perm os.FileMode `json:"perm,omitempty"`
}

// Plan is a sequence of [PlanOp] executed in order by [Apply]. Build
// it with the fluent helpers ([Plan.Create], [Plan.Update],
// [Plan.Delete], [Plan.Rename]) or by appending to Ops directly.
// Plans are JSON-serializable.
type Plan struct {
	// Ops are the operations, in execution order.
	Ops []PlanOp `json:"ops"`
}

// NewPlan returns an empty [*Plan].
func NewPlan() *Plan {
	return &Plan{}
}

// Create appends a [PlanActionCreate] op; a zero perm means 0o644.
// Returns p for chaining.
func (p *Plan) Create(path string, data []byte, perm os.FileMode) *Plan {
	if perm == 0 {
		perm = 0o644
	}
	p.Ops = append(p.Ops, PlanOp{Action: PlanActionCreate, Path: path, Data: data, Perm: perm})
	return p
}

// Update appends a [PlanActionUpdate] op; a zero perm means 0o644.
// Returns p for chaining.
func (p *Plan) Update(path string, data []byte, perm os.FileMode) *Plan {
	if perm == 0 {
		perm = 0o644
	}
	p.Ops = append(p.Ops, PlanOp{Action: PlanActionUpdate, Path: path, Data: data, Perm: perm})
	return p
}

// Delete appends a [PlanActionDelete] op. Returns p for chaining.
func (p *Plan) Delete(path string) *Plan {
	p.Ops = append(p.Ops, PlanOp{Action: PlanActionDelete, Path: path})
	return p
}

// Rename appends a [PlanActionRename] op moving src to dst. Returns
// p for chaining.
func (p *Plan) Rename(src, dst string) *Plan {
	p.Ops = append(p.Ops, PlanOp{Action: PlanActionRename, Path: dst, Source: src})
	return p
}

// Diff renders the plan as a short human-readable summary, one line
// per op. Suitable for --dry-run previews; not a structured diff.
func (p *Plan) Diff() string {
	if p == nil || len(p.Ops) == 0 {
		return "(empty plan)\n"
	}
	var b strings.Builder
	for i, op := range p.Ops {
		switch op.Action {
		case PlanActionCreate:
			fmt.Fprintf(&b, "%d. %s %s (%d bytes, mode %#o)\n", i+1, planActionLabelCreate, op.Path, len(op.Data), uint32(op.Perm.Perm()))
		case PlanActionUpdate:
			fmt.Fprintf(&b, "%d. %s %s (%d bytes, mode %#o)\n", i+1, planActionLabelUpdate, op.Path, len(op.Data), uint32(op.Perm.Perm()))
		case PlanActionDelete:
			fmt.Fprintf(&b, "%d. %s %s\n", i+1, planActionLabelDelete, op.Path)
		case PlanActionRename:
			fmt.Fprintf(&b, "%d. %s %s -> %s\n", i+1, planActionLabelRename, op.Source, op.Path)
		default:
			fmt.Fprintf(&b, "%d. %s %s\n", i+1, unknownLabel, op.Path)
		}
	}
	return b.String()
}

// ApplyOption configures [Apply], [Resume] and [ApplyTransient].
// [Rollback] accepts but ignores them.
type ApplyOption func(*applyConfig)

type applyConfig struct {
	mkdirParents bool
}

// WithApplyNoMkdir disables automatic creation of missing directories:
// the journal directory must then exist, and so must the parent of
// every created, updated or renamed-to path. By default they are
// created, the journal directory with mode 0o700 and the others with
// mode 0o755.
func WithApplyNoMkdir() ApplyOption {
	return func(c *applyConfig) {
		c.mkdirParents = false
	}
}

// journal is the in-memory view of an in-progress or completed apply.
// Persisted across two files under journalDir: plan.json (immutable,
// written once on Apply) and progress.json (rewritten per step).
//
// Started records that the op at index Completed may have begun. It
// is saved before the op touches anything, so after a crash Resume
// and Rollback know which step to finish or undo.
//
// PlanOp.Data is JSON-serialized as base64, inflating binary payloads
// by ~33% on disk.
type journal struct {
	Plan      Plan `json:"-"`
	Completed int  `json:"completed"`
	Started   bool `json:"started,omitempty"`
	Finalized bool `json:"finalized"`
	Schema    int  `json:"schema"`
}

// planFile is the on-disk shape of plan.json.
type planFile struct {
	Plan   Plan `json:"plan"`
	Schema int  `json:"schema"`
}

// errSchemaMismatch is returned by [loadJournal] when the on-disk
// schema version doesn't match [journalSchemaVersion].
var errSchemaMismatch = errors.New("fs: plan: journal schema mismatch")

// Apply runs the plan, writing a journal under journalDir so an
// interrupted apply can be resumed. If journalDir already holds a
// journal, Apply fails with an error wrapping [ErrAlreadyExists]. A
// nil p returns an error wrapping [ErrInvalidPath]. An op with a zero
// Perm writes mode 0o644.
//
// On any per-op failure, Apply returns the error without rolling
// back. The journal records progress so the caller can decide
// whether to [Rollback] or [Resume]. Each step is recorded before it
// runs and the original file is snapshotted first, with every journal
// write, snapshot and op flushed to disk, so a crash at any point
// leaves a journal that Resume can finish and Rollback can undo.
//
// The journal holds a copy of every op's data and of every file the
// plan overwrites or deletes. It is created owner-only (directories
// 0o700, journal files 0o600; snapshots keep their file's mode), but
// it is never removed automatically: delete journalDir once the
// result is no longer needed.
//
// Apply, [Resume], and [Rollback] acquire an advisory [Lock] on
// <journalDir>/.lock for the duration of their work, so concurrent
// same-journal callers serialize automatically. Independent
// journals are independent. Where [Lock] returns [ErrNotSupported]
// (js/wasm, wasip1, Plan 9), so do they; on AIX the lock does not
// serialize callers within one process (see [Lock]).
func Apply(p *Plan, journalDir string, opts ...ApplyOption) error {
	if p == nil {
		return wrapPathError(opPlanApply, journalDir, ErrInvalidPath)
	}
	cfg := newApplyConfig(opts)

	if cfg.mkdirParents {
		existed := IsDir(journalDir)
		if err := MkdirAll(filepath.Join(journalDir, journalBackupsDir), journalDirPerm); err != nil {
			return err
		}
		if !existed {
			if err := syncDir(filepath.Dir(filepath.Clean(journalDir))); err != nil {
				return wrapPathError(opPlanJournal, journalDir, err)
			}
		}
	} else if !IsDir(journalDir) {
		return wrapPathError(opPlanApply, journalDir, ErrNotDir)
	}

	release, err := acquireJournalLock(journalDir)
	if err != nil {
		return err
	}
	defer release()

	planPath := filepath.Join(journalDir, journalPlanFilename)
	legacyPath := filepath.Join(journalDir, journalLegacyFilename)
	if Exists(planPath) || Exists(legacyPath) {
		return wrapPathError(opPlanApply, journalDir, ErrAlreadyExists)
	}

	j := journal{Plan: *p}
	if err := saveProgress(journalDir, &j); err != nil {
		return err
	}
	if err := savePlanFile(journalDir, &j); err != nil {
		return err
	}
	return resumeJournal(journalDir, &j, cfg)
}

func acquireJournalLock(journalDir string) (func(), error) {
	lockPath := filepath.Join(journalDir, journalLockFilename)
	h, err := Lock(lockPath)
	if err != nil {
		return nil, err
	}
	return func() { _ = h.Release() }, nil //nolint:errcheck // probe-only; release errors are not actionable here
}

// Resume continues a previously-interrupted apply from its journal,
// finishing the step that was running when it stopped. If the apply
// already completed, Resume is a no-op and returns nil. After a
// [Rollback], Resume applies the plan again from the start. See
// [Apply] for the concurrency contract.
func Resume(journalDir string, opts ...ApplyOption) error {
	cfg := newApplyConfig(opts)

	release, err := acquireJournalLock(journalDir)
	if err != nil {
		return err
	}
	defer release()

	j, err := loadJournal(journalDir)
	if err != nil {
		return err
	}
	if j.Finalized {
		return nil
	}
	return resumeJournal(journalDir, j, cfg)
}

// Rollback reverts the operations recorded in the journal in reverse
// order, including a step that was interrupted part-way. Each op's
// snapshot is used to restore the original state. Rollback can itself
// be interrupted and run again. After successful rollback, the
// journal directory is left in place. See [Apply] for the concurrency
// contract.
func Rollback(journalDir string, opts ...ApplyOption) error {
	_ = newApplyConfig(opts)

	release, err := acquireJournalLock(journalDir)
	if err != nil {
		return err
	}
	defer release()

	j, err := loadJournal(journalDir)
	if err != nil {
		return err
	}
	if j.Finalized {
		j.Finalized = false
		if err := saveProgress(journalDir, j); err != nil {
			return err
		}
	}

	if j.Started && j.Completed < len(j.Plan.Ops) {
		if err := rollbackOp(journalDir, j.Plan.Ops[j.Completed], j.Completed, false); err != nil {
			return err
		}
		j.Started = false
		if err := saveProgress(journalDir, j); err != nil {
			return err
		}
		if err := removeSnapshot(journalDir, j.Completed); err != nil {
			return err
		}
	}
	for i := j.Completed - 1; i >= 0; i-- {
		if err := rollbackOp(journalDir, j.Plan.Ops[i], i, true); err != nil {
			return err
		}
		j.Completed = i
		if err := saveProgress(journalDir, j); err != nil {
			return err
		}
		if err := removeSnapshot(journalDir, i); err != nil {
			return err
		}
	}
	return nil
}

// resumeJournal runs the ops from j.Completed on. Every step follows
// the same order, so each crash point is recoverable:
//
//  1. progress records the step as started (a step not yet started
//     first drops any snapshot left over from an earlier rollback);
//  2. the target's prior state is snapshotted, once per step;
//  3. the op runs, idempotently, and is flushed to disk;
//  4. progress records the step as completed.
func resumeJournal(journalDir string, j *journal, cfg applyConfig) error {
	log := logger()
	for i := j.Completed; i < len(j.Plan.Ops); i++ {
		op := j.Plan.Ops[i]
		log.Debug("fs.plan: applying op", "step", i, "action", op.Action.String(), "path", op.Path)
		if !j.Started {
			if err := removeSnapshot(journalDir, i); err != nil {
				return err
			}
			j.Started = true
			if err := saveProgress(journalDir, j); err != nil {
				return err
			}
		}
		if err := applyJournaledOp(journalDir, op, i, cfg); err != nil {
			log.Debug("fs.plan: op failed", "step", i, "error", err)
			return err
		}
		j.Completed = i + 1
		j.Started = false
		if err := saveProgress(journalDir, j); err != nil {
			return err
		}
	}
	j.Finalized = true
	log.Debug("fs.plan: finalized", "ops", len(j.Plan.Ops), "journal", journalDir)
	return saveProgress(journalDir, j)
}

// ApplyTransient runs the plan with no journal, no backups and no
// resume or rollback support. It stops at the first failing op and
// returns its error. Use [Apply] when the run must be resumable or
// reversible.
//
// ApplyTransient takes no lock; concurrent runs touching the same
// files race.
func ApplyTransient(p *Plan, opts ...ApplyOption) error {
	if p == nil {
		return wrapPathError(opPlanApply, "", ErrInvalidPath)
	}
	cfg := newApplyConfig(opts)
	for _, op := range p.Ops {
		if err := applyTransientOp(op, cfg); err != nil {
			return err
		}
	}
	return nil
}

// opPerm is the mode an op writes with: Perm, or 0o644 when it is zero.
func opPerm(op PlanOp) os.FileMode {
	if op.Perm.Perm() == 0 {
		return 0o644
	}
	return op.Perm.Perm()
}

func mkdirOpParent(op PlanOp, cfg applyConfig) error {
	if cfg.mkdirParents && (op.Action == PlanActionCreate || op.Action == PlanActionUpdate || op.Action == PlanActionRename) {
		return MkdirAll(filepath.Dir(op.Path), 0o755)
	}
	return nil
}

// applyTransientOp executes a single PlanOp without a journal.
func applyTransientOp(op PlanOp, cfg applyConfig) error {
	if err := mkdirOpParent(op, cfg); err != nil {
		return err
	}
	switch op.Action {
	case PlanActionCreate:
		if Exists(op.Path) {
			return wrapPathError(opPlanApply, op.Path, ErrAlreadyExists)
		}
		return WriteFile(op.Path, op.Data, WithPerm(opPerm(op)))
	case PlanActionUpdate:
		return WriteFile(op.Path, op.Data, WithPerm(opPerm(op)))
	case PlanActionDelete:
		if rerr := osRemove(op.Path); rerr != nil && !errors.Is(rerr, stdfs.ErrNotExist) {
			return wrapPathError(opPlanApply, op.Path, rerr)
		}
		return nil
	case PlanActionRename:
		if rerr := osRename(op.Source, op.Path); rerr != nil {
			return wrapPathError(opPlanApply, op.Source, rerr)
		}
		return nil
	default:
		return wrapPathError(opPlanApply, op.Path, ErrInvalidPath)
	}
}

// applyJournaledOp snapshots the op's target (once per step) and then
// runs the op. It is safe to call again after a crash at any point:
// a write is simply repeated, a delete of a missing file succeeds, and
// a rename whose source is gone and whose destination exists has
// already happened. Everything it changes is flushed before it
// returns.
func applyJournaledOp(journalDir string, op PlanOp, step int, cfg applyConfig) error {
	if err := mkdirOpParent(op, cfg); err != nil {
		return err
	}
	switch op.Action {
	case PlanActionCreate, PlanActionUpdate, PlanActionDelete, PlanActionRename:
	default:
		return wrapPathError(opPlanApply, op.Path, ErrInvalidPath)
	}

	taken, err := hasSnapshot(journalDir, step)
	if err != nil {
		return err
	}
	if !taken {
		switch op.Action {
		case PlanActionCreate:
			if _, serr := osLstat(op.Path); serr == nil {
				return wrapPathError(opPlanApply, op.Path, ErrAlreadyExists)
			}
		case PlanActionRename:
			if _, serr := osLstat(op.Source); serr != nil {
				return wrapPathError(opPlanApply, op.Source, serr)
			}
		}
		if err := takeSnapshot(journalDir, op.Path, step); err != nil {
			return err
		}
	}

	switch op.Action {
	case PlanActionCreate, PlanActionUpdate:
		return WriteFile(op.Path, op.Data, WithPerm(opPerm(op)), WithSync(true))

	case PlanActionDelete:
		if rerr := osRemove(op.Path); rerr != nil && !errors.Is(rerr, stdfs.ErrNotExist) {
			return wrapPathError(opPlanApply, op.Path, rerr)
		}
		return syncParent(opPlanApply, op.Path)

	default: // PlanActionRename
		_, srcErr := osLstat(op.Source)
		_, dstErr := osLstat(op.Path)
		if errors.Is(srcErr, stdfs.ErrNotExist) && dstErr == nil {
			return nil // renamed before an interruption
		}
		if rerr := osRename(op.Source, op.Path); rerr != nil {
			return wrapPathError(opPlanApply, op.Source, rerr)
		}
		if err := syncParent(opPlanApply, op.Path); err != nil {
			return err
		}
		if filepath.Dir(op.Source) != filepath.Dir(op.Path) {
			return syncParent(opPlanApply, op.Source)
		}
		return nil
	}
}

// rollbackOp undoes one step. completed reports whether progress
// recorded the step as finished; for a step that was only started, a
// missing snapshot means the op never ran, so there is nothing to
// undo. Every branch is safe to repeat.
func rollbackOp(journalDir string, op PlanOp, step int, completed bool) error {
	switch op.Action {
	case PlanActionCreate, PlanActionUpdate, PlanActionDelete, PlanActionRename:
	default:
		return wrapPathError(opPlanRollback, op.Path, ErrInvalidPath)
	}
	if !completed {
		taken, err := hasSnapshot(journalDir, step)
		if err != nil || !taken {
			return err
		}
	}

	switch op.Action {
	case PlanActionCreate:
		if rerr := osRemove(op.Path); rerr != nil && !errors.Is(rerr, stdfs.ErrNotExist) {
			return wrapPathError(opPlanRollback, op.Path, rerr)
		}
		return syncParent(opPlanRollback, op.Path)

	case PlanActionUpdate, PlanActionDelete:
		return restoreFromBackup(journalDir, op.Path, step)

	default: // PlanActionRename
		if _, serr := osLstat(op.Source); errors.Is(serr, stdfs.ErrNotExist) {
			if rerr := osRename(op.Path, op.Source); rerr != nil {
				return wrapPathError(opPlanRollback, op.Path, rerr)
			}
			if err := syncParent(opPlanRollback, op.Source); err != nil {
				return err
			}
		}
		return restoreFromBackup(journalDir, op.Path, step)
	}
}

// snapshotDir is where step's snapshot of its target lives.
func snapshotDir(journalDir string, step int) string {
	return filepath.Join(journalDir, journalBackupsDir, fmt.Sprintf("step-%d", step))
}

func hasSnapshot(journalDir string, step int) (bool, error) {
	_, err := osLstat(snapshotDir(journalDir, step))
	switch {
	case err == nil:
		return true, nil
	case errors.Is(err, stdfs.ErrNotExist):
		return false, nil
	default:
		return false, wrapPathError(opPlanJournal, snapshotDir(journalDir, step), err)
	}
}

// takeSnapshot records path's current state for step: a copy of the
// file (mode and modification time kept), the target text of a
// symlink, or an empty snapshot directory when path does not exist.
// The snapshot is built in a temporary directory and renamed into
// place, so it either exists complete and flushed or not at all.
func takeSnapshot(journalDir, path string, step int) error {
	dst := snapshotDir(journalDir, step)
	tmp := dst + ".tmp"
	if err := osRemoveAll(tmp); err != nil {
		return wrapPathError(opPlanJournal, tmp, err)
	}
	if err := osMkdirAll(tmp, journalDirPerm); err != nil {
		return wrapPathError(opPlanJournal, tmp, err)
	}

	info, err := osLstat(path)
	switch {
	case err == nil:
		if err := snapshotEntry(tmp, path, info); err != nil {
			return err
		}
	case errors.Is(err, stdfs.ErrNotExist):
	default:
		return wrapPathError(opPlanJournal, path, err)
	}

	if err := osRename(tmp, dst); err != nil {
		return wrapPathError(opPlanJournal, dst, err)
	}
	return syncParent(opPlanJournal, dst)
}

// snapshotEntry copies the existing entry at path, described by info,
// into the snapshot directory dir.
func snapshotEntry(dir, path string, info os.FileInfo) error {
	base := filepath.Base(path)
	switch {
	case info.IsDir():
		return wrapPathError(opPlanJournal, path, ErrIsDir)
	case info.Mode()&os.ModeSymlink != 0:
		target, err := osReadlink(path)
		if err != nil {
			return wrapPathError(opPlanJournal, path, err)
		}
		return WriteFile(filepath.Join(dir, base+snapshotLinkExt), []byte(target), WithPerm(journalFilePerm), WithSync(true))
	default:
		return withOp(opPlanJournal, copyFileInternal(path, filepath.Join(dir, base), snapshotCopyOptions))
	}
}

// snapshotLinkExt marks a snapshot of a symlink: the file
// "<base>.symlink" holds the link's target text. A snapshot of a
// regular file is named "<base>", so the two never collide.
const snapshotLinkExt = ".symlink"

// snapshotCopyOptions copies a file into or out of a snapshot: the
// same bytes, mode and modification time, flushed to disk.
var snapshotCopyOptions = copyOptions{overwrite: true, sync: true, preserveMtime: true}

// removeSnapshot deletes step's snapshot, if any.
func removeSnapshot(journalDir string, step int) error {
	dir := snapshotDir(journalDir, step)
	for _, p := range []string{dir, dir + ".tmp"} {
		if err := osRemoveAll(p); err != nil {
			return wrapPathError(opPlanJournal, p, err)
		}
	}
	return nil
}

// restoreFromBackup puts the step's snapshot back at path: the file,
// or the symlink, that was there. If the snapshot holds neither (path
// did not exist before the step, or a journal written without per-step
// snapshots has none), path is removed instead.
func restoreFromBackup(journalDir, path string, step int) error {
	dir := snapshotDir(journalDir, step)
	base := filepath.Base(path)
	target, err := os.ReadFile(filepath.Join(dir, base+snapshotLinkExt))
	switch {
	case err == nil:
		return restoreSymlink(path, string(target))
	case !errors.Is(err, stdfs.ErrNotExist):
		return wrapPathError(opPlanRollback, filepath.Join(dir, base+snapshotLinkExt), err)
	}

	src := filepath.Join(dir, base)
	if _, err := osLstat(src); err != nil {
		if errors.Is(err, stdfs.ErrNotExist) {
			if rerr := osRemove(path); rerr != nil && !errors.Is(rerr, stdfs.ErrNotExist) {
				return wrapPathError(opPlanRollback, path, rerr)
			}
			return syncParent(opPlanRollback, path)
		}
		return wrapPathError(opPlanRollback, src, err)
	}
	return withOp(opPlanRollback, copyFileInternal(src, path, snapshotCopyOptions))
}

// restoreSymlink makes path a symlink to target, replacing whatever is
// there in one rename.
func restoreSymlink(path, target string) error {
	tmp := filepath.Join(filepath.Dir(path), strings.TrimSuffix(tempPatternFor(path), "*")+"link")
	if err := osRemove(tmp); err != nil && !errors.Is(err, stdfs.ErrNotExist) {
		return wrapPathError(opPlanRollback, tmp, err)
	}
	if err := os.Symlink(target, tmp); err != nil {
		return wrapPathError(opPlanRollback, path, err)
	}
	if err := osRename(tmp, path); err != nil {
		_ = osRemove(tmp) //nolint:errcheck // best-effort cleanup; the rename error is what matters
		return wrapPathError(opPlanRollback, path, err)
	}
	return syncParent(opPlanRollback, path)
}

// syncParent flushes the directory holding path, so a create, rename
// or removal of path survives a power loss.
func syncParent(op, path string) error {
	if err := syncDir(filepath.Dir(path)); err != nil {
		return wrapPathError(op, path, err)
	}
	return nil
}

func savePlanFile(journalDir string, j *journal) error {
	pf := planFile{Plan: j.Plan, Schema: journalSchemaVersion}
	data, err := json.MarshalIndent(pf, "", "  ")
	if err != nil {
		return wrapPathError(opPlanJournal, journalDir, err)
	}
	target := filepath.Join(journalDir, journalPlanFilename)
	return WriteFile(target, data, WithPerm(journalFilePerm), WithSync(true))
}

func saveProgress(journalDir string, j *journal) error {
	j.Schema = journalSchemaVersion
	data, err := json.MarshalIndent(j, "", "  ")
	if err != nil {
		return wrapPathError(opPlanJournal, journalDir, err)
	}
	target := filepath.Join(journalDir, journalProgressFilename)
	return WriteFile(target, data, WithPerm(journalFilePerm), WithSync(true))
}

func loadJournal(journalDir string) (*journal, error) {
	planPath := filepath.Join(journalDir, journalPlanFilename)
	planData, err := os.ReadFile(planPath)
	if err != nil {
		return nil, wrapPathError(opPlanJournal, planPath, err)
	}
	var pf planFile
	if uerr := json.Unmarshal(planData, &pf); uerr != nil {
		return nil, wrapPathError(opPlanJournal, planPath, uerr)
	}
	if pf.Schema != journalSchemaVersion {
		return nil, wrapPathError(opPlanJournal, planPath, fmt.Errorf("%w: file=%d package=%d", errSchemaMismatch, pf.Schema, journalSchemaVersion))
	}

	progressPath := filepath.Join(journalDir, journalProgressFilename)
	progressData, err := os.ReadFile(progressPath)
	if err != nil {
		return nil, wrapPathError(opPlanJournal, progressPath, err)
	}
	var j journal
	if uerr := json.Unmarshal(progressData, &j); uerr != nil {
		return nil, wrapPathError(opPlanJournal, progressPath, uerr)
	}
	if j.Schema != journalSchemaVersion {
		return nil, wrapPathError(opPlanJournal, progressPath, fmt.Errorf("%w: file=%d package=%d", errSchemaMismatch, j.Schema, journalSchemaVersion))
	}
	if j.Completed < 0 || j.Completed > len(pf.Plan.Ops) {
		return nil, wrapPathError(opPlanJournal, progressPath, ErrInvalidPath)
	}
	j.Plan = pf.Plan
	return &j, nil
}

func newApplyConfig(opts []ApplyOption) applyConfig {
	cfg := applyConfig{mkdirParents: true}
	for _, opt := range opts {
		opt(&cfg)
	}
	return cfg
}
