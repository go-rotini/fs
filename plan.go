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

	// journalLegacyFilename is the pre-split combined file. Apply
	// rejects journals containing it as already-claimed.
	journalLegacyFilename = "journal.json"

	// journalSchemaVersion guards against resuming journals written
	// by an incompatible package version.
	journalSchemaVersion = 1

	// journalBackupsDir holds pre-apply snapshots indexed by step.
	journalBackupsDir = "backups"
)

// PlanAction enumerates the operation kinds a [Plan] can hold.
type PlanAction int

const (
	// PlanActionCreate writes a new file. Errors if the path already
	// exists at apply time.
	PlanActionCreate PlanAction = iota + 1

	// PlanActionUpdate overwrites an existing file's contents. The
	// prior contents are backed up so the op is reversible.
	PlanActionUpdate

	// PlanActionDelete removes a file. The file is backed up before
	// removal so Rollback can restore it.
	PlanActionDelete

	// PlanActionRename moves Source to Path. Reversible.
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
	Action PlanAction  `json:"action"`
	Path   string      `json:"path"`
	Source string      `json:"source,omitempty"` // PlanActionRename only
	Data   []byte      `json:"data,omitempty"`   // PlanActionCreate / PlanActionUpdate
	Perm   os.FileMode `json:"perm,omitempty"`   // PlanActionCreate / PlanActionUpdate
}

// Plan is a sequence of [PlanOp] executed in order by [Apply]. Build
// it with the fluent helpers ([*Plan.Create], [*Plan.Update],
// [*Plan.Delete], [*Plan.Rename]) or by appending to Ops directly.
// Plans are JSON-serializable.
type Plan struct {
	Ops []PlanOp `json:"ops"`
}

// NewPlan returns an empty [*Plan].
func NewPlan() *Plan {
	return &Plan{}
}

// Create appends a [PlanActionCreate] op. Returns p for chaining.
func (p *Plan) Create(path string, data []byte, perm os.FileMode) *Plan {
	if perm == 0 {
		perm = 0o644
	}
	p.Ops = append(p.Ops, PlanOp{Action: PlanActionCreate, Path: path, Data: data, Perm: perm})
	return p
}

// Update appends a [PlanActionUpdate] op. Returns p for chaining.
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

// ApplyOption configures [Apply], [Resume], and [Rollback].
type ApplyOption func(*applyConfig)

type applyConfig struct {
	mkdirParents bool
}

// WithApplyNoMkdir disables automatic creation of missing parent
// directories for both the journal and target paths. Default behavior
// creates them with mode 0o755.
func WithApplyNoMkdir() ApplyOption {
	return func(c *applyConfig) {
		c.mkdirParents = false
	}
}

// journal is the in-memory view of an in-progress or completed apply.
// Persisted across two files under journalDir: plan.json (immutable,
// written once on Apply) and progress.json (rewritten per step).
//
// PlanOp.Data is JSON-serialized as base64, inflating binary payloads
// by ~33% on disk.
type journal struct {
	Plan      Plan `json:"-"`
	Completed int  `json:"completed"`
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
// interrupted apply can be resumed. journalDir must be an empty or
// nonexistent directory; Apply refuses to overwrite an existing
// journal.
//
// On any per-op failure, Apply returns the error without rolling
// back. The journal records progress so the caller can decide
// whether to [Rollback] or [Resume].
//
// Apply, [Resume], and [Rollback] acquire an advisory [Lock] on
// <journalDir>/.lock for the duration of their work, so concurrent
// same-journal callers serialize automatically. Independent
// journals are independent.
func Apply(p *Plan, journalDir string, opts ...ApplyOption) error {
	if p == nil {
		return wrapPathError(opPlanApply, journalDir, ErrInvalidPath)
	}
	cfg := newApplyConfig(opts)

	if cfg.mkdirParents {
		if err := MkdirAll(filepath.Join(journalDir, journalBackupsDir), 0o755); err != nil {
			return err
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
	if err := savePlanFile(journalDir, &j); err != nil {
		return err
	}
	if err := saveProgress(journalDir, &j); err != nil {
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

// Resume continues a previously-interrupted apply from its journal.
// If the apply already completed, Resume is a no-op and returns nil.
// See [Apply] for the concurrency contract.
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
// order. Each op's stored backup is used to restore the original
// state. After successful rollback, the journal directory is left in
// place. See [Apply] for the concurrency contract.
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

	for i := j.Completed - 1; i >= 0; i-- {
		if rerr := rollbackOp(journalDir, j.Plan.Ops[i], i); rerr != nil {
			return rerr
		}
		j.Completed = i
		if serr := saveProgress(journalDir, j); serr != nil {
			return serr
		}
	}
	return nil
}

func resumeJournal(journalDir string, j *journal, cfg applyConfig) error {
	log := logger()
	for i := j.Completed; i < len(j.Plan.Ops); i++ {
		op := j.Plan.Ops[i]
		log.Debug("fs.plan: applying op", "step", i, "action", op.Action.String(), "path", op.Path)
		if err := applyOp(journalDir, op, i, cfg); err != nil {
			log.Debug("fs.plan: op failed", "step", i, "error", err)
			return err
		}
		j.Completed = i + 1
		if err := saveProgress(journalDir, j); err != nil {
			return err
		}
	}
	j.Finalized = true
	log.Debug("fs.plan: finalized", "ops", len(j.Plan.Ops), "journal", journalDir)
	return saveProgress(journalDir, j)
}

// ApplyTransient runs the plan in-memory, with no on-disk journal,
// no backup snapshots, and no resume/rollback support. The first
// op's failure surfaces as the return; subsequent ops are skipped.
//
// Use this when sequenced execution is wanted without the journal
// overhead. For executions that must be resumable or reversible, use
// [Apply] against a journal directory.
//
// ApplyTransient has no internal locking. Concurrent calls touching
// the same on-disk files race exactly as the underlying [WriteFile]
// / [os.Remove] / [os.Rename] would.
func ApplyTransient(p *Plan, opts ...ApplyOption) error {
	if p == nil {
		return wrapPathError(opPlanApply, "", ErrInvalidPath)
	}
	cfg := newApplyConfig(opts)
	for i, op := range p.Ops {
		if err := applyOp("", op, i, cfg); err != nil {
			return err
		}
	}
	return nil
}

// applyOp executes a single PlanOp and records any backup data the
// inverse will need. When journalDir is empty (the [ApplyTransient]
// path), backup recording is skipped.
func applyOp(journalDir string, op PlanOp, step int, cfg applyConfig) error {
	if cfg.mkdirParents && (op.Action == PlanActionCreate || op.Action == PlanActionUpdate || op.Action == PlanActionRename) {
		if err := MkdirAll(filepath.Dir(op.Path), 0o755); err != nil {
			return err
		}
	}

	switch op.Action {
	case PlanActionCreate:
		if Exists(op.Path) {
			return wrapPathError(opPlanApply, op.Path, ErrAlreadyExists)
		}
		return WriteFile(op.Path, op.Data, WithPerm(op.Perm))

	case PlanActionUpdate:
		if err := backupBeforeWrite(journalDir, op.Path, step); err != nil {
			return err
		}
		return WriteFile(op.Path, op.Data, WithPerm(op.Perm))

	case PlanActionDelete:
		if !Exists(op.Path) {
			return nil
		}
		if err := backupBeforeWrite(journalDir, op.Path, step); err != nil {
			return err
		}
		if rerr := os.Remove(op.Path); rerr != nil {
			return wrapPathError(opPlanApply, op.Path, rerr)
		}
		return nil

	case PlanActionRename:
		if Exists(op.Path) {
			if err := backupBeforeWrite(journalDir, op.Path, step); err != nil {
				return err
			}
		}
		if rerr := os.Rename(op.Source, op.Path); rerr != nil {
			return wrapPathError(opPlanApply, op.Source, rerr)
		}
		return nil

	default:
		return wrapPathError(opPlanApply, op.Path, ErrInvalidPath)
	}
}

func rollbackOp(journalDir string, op PlanOp, step int) error {
	switch op.Action {
	case PlanActionCreate:
		if rerr := os.Remove(op.Path); rerr != nil && !errors.Is(rerr, stdfs.ErrNotExist) {
			return wrapPathError(opPlanRollback, op.Path, rerr)
		}
		return nil

	case PlanActionUpdate:
		return restoreFromBackup(journalDir, op.Path, step)

	case PlanActionDelete:
		return restoreFromBackup(journalDir, op.Path, step)

	case PlanActionRename:
		if rerr := os.Rename(op.Path, op.Source); rerr != nil {
			return wrapPathError(opPlanRollback, op.Path, rerr)
		}
		return restoreFromBackup(journalDir, op.Path, step)

	default:
		return wrapPathError(opPlanRollback, op.Path, ErrInvalidPath)
	}
}

// backupBeforeWrite copies the existing file at path into the
// journal's backups/<step> dir. No-op when path doesn't exist or
// when journalDir is empty.
func backupBeforeWrite(journalDir, path string, step int) error {
	if journalDir == "" {
		return nil
	}
	info, err := os.Stat(path)
	if err != nil {
		if errors.Is(err, stdfs.ErrNotExist) {
			return nil
		}
		return wrapPathError(opPlanJournal, path, err)
	}
	if info.IsDir() {
		return wrapPathError(opPlanJournal, path, ErrIsDir)
	}

	bdir := filepath.Join(journalDir, journalBackupsDir, fmt.Sprintf("step-%d", step))
	if err := MkdirAll(bdir, 0o755); err != nil {
		return err
	}
	dst := filepath.Join(bdir, filepath.Base(path))

	data, err := os.ReadFile(path)
	if err != nil {
		return wrapPathError(opPlanJournal, path, err)
	}
	if err := WriteFile(dst, data, WithPerm(info.Mode().Perm())); err != nil {
		return err
	}
	return nil
}

// restoreFromBackup writes the journal snapshot back to path. If no
// snapshot exists, the path is removed instead.
func restoreFromBackup(journalDir, path string, step int) error {
	src := filepath.Join(journalDir, journalBackupsDir, fmt.Sprintf("step-%d", step), filepath.Base(path))
	data, err := os.ReadFile(src)
	if err != nil {
		if errors.Is(err, stdfs.ErrNotExist) {
			if rerr := os.Remove(path); rerr != nil && !errors.Is(rerr, stdfs.ErrNotExist) {
				return wrapPathError(opPlanRollback, path, rerr)
			}
			return nil
		}
		return wrapPathError(opPlanRollback, src, err)
	}
	info, statErr := os.Stat(src)
	perm := os.FileMode(0o644)
	if statErr == nil {
		perm = info.Mode().Perm()
	}
	if err := WriteFile(path, data, WithPerm(perm)); err != nil {
		return err
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
	return WriteFile(target, data, WithPerm(0o644))
}

func saveProgress(journalDir string, j *journal) error {
	j.Schema = journalSchemaVersion
	data, err := json.MarshalIndent(j, "", "  ")
	if err != nil {
		return wrapPathError(opPlanJournal, journalDir, err)
	}
	target := filepath.Join(journalDir, journalProgressFilename)
	return WriteFile(target, data, WithPerm(0o644))
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
