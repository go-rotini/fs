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
	opPlanResume   = "plan.resume"
	opPlanRollback = "plan.rollback"
	opPlanJournal  = "plan.journal"

	// journalPlanFilename holds the immutable plan written at the
	// start of Apply. Read-only during the apply; consulted by Resume
	// + Rollback. Kept separate from the progress file so the
	// per-step rewrite cost stays O(progress-record-size) rather than
	// O(plan-size).
	journalPlanFilename = "plan.json"

	// journalProgressFilename holds the per-step progress counter
	// (and the Finalized flag). Tiny — typically <100 bytes — so the
	// rewrite cost is negligible regardless of plan size.
	journalProgressFilename = "progress.json"

	// journalLegacyFilename was the v0.1.0-dev combined plan +
	// progress file. Apply now rejects an existing journal containing
	// only this file as already-claimed; the rejection's error
	// message tells the caller to remove and start over.
	journalLegacyFilename = "journal.json"

	// journalSchemaVersion records the on-disk schema version so
	// future format changes can refuse to resume incompatible
	// journals. (Addresses R1.)
	journalSchemaVersion = 1

	// journalBackupsDir is the subdirectory holding pre-apply
	// snapshots of each modified path. Indexed by step number.
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

// planActionLabel* are the canonical short names rendered by
// [PlanAction.String] and [Plan.Diff]. Centralized so the labels
// stay consistent across both renderings.
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

// PlanOp is a single operation inside a [Plan]. JSON-serialized into
// the journal so an interrupted apply can resume on a later process
// start.
type PlanOp struct {
	Action PlanAction  `json:"action"`
	Path   string      `json:"path"`
	Source string      `json:"source,omitempty"` // PlanActionRename only
	Data   []byte      `json:"data,omitempty"`   // PlanActionCreate / PlanActionUpdate
	Perm   os.FileMode `json:"perm,omitempty"`   // PlanActionCreate / PlanActionUpdate
}

// Plan is a sequence of [PlanOp] executed in order by [Apply].
//
// A Plan is a value type — callers can build it up with the fluent
// helpers ([*Plan.Create], [*Plan.Update], [*Plan.Delete],
// [*Plan.Rename]) or by appending to Ops directly. Plans are
// JSON-serializable so they can be inspected, archived, or
// transported between processes.
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
// per op. Suitable for `--dry-run` previews; not a structured diff.
func (p *Plan) Diff() string {
	if p == nil || len(p.Ops) == 0 {
		return "(empty plan)\n"
	}
	var b strings.Builder
	for i, op := range p.Ops {
		switch op.Action {
		case PlanActionCreate:
			fmt.Fprintf(&b, "%d. %s %s (%d bytes, mode %v)\n", i+1, planActionLabelCreate, op.Path, len(op.Data), op.Perm)
		case PlanActionUpdate:
			fmt.Fprintf(&b, "%d. %s %s (%d bytes, mode %v)\n", i+1, planActionLabelUpdate, op.Path, len(op.Data), op.Perm)
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

// ApplyOption configures [Apply] / [Resume] / [Rollback].
type ApplyOption func(*applyConfig)

type applyConfig struct {
	// mkdirParents controls whether the journal directory and any
	// target-parent directories that don't yet exist are created.
	// On by default; pass [WithApplyNoMkdir] to disable.
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

// journal is the in-memory view of an in-progress or completed
// apply. Persisted across two files under journalDir:
//
//   - plan.json     — immutable plan, written once on Apply.
//   - progress.json — Completed + Finalized + Schema, rewritten per
//     step.
//
// Splitting the files keeps the per-step rewrite cost independent of
// plan size — a plan with megabyte-sized Data payloads doesn't
// re-serialize the payloads on every successful op.
//
// Wire format note: PlanOp.Data is a []byte, which Go's [encoding/json]
// serializes as base64-encoded strings — a ~33% inflation. For plans
// holding mostly text/config payloads the inflation is negligible.
// Plans whose ops carry many megabytes of binary data should expect
// the on-disk plan.json to be ~1.33× the in-memory payload size.
type journal struct {
	Plan      Plan `json:"-"`
	Completed int  `json:"completed"`
	Finalized bool `json:"finalized"`
	Schema    int  `json:"schema"`
}

// planFile is the on-disk shape of plan.json — just the plan.
type planFile struct {
	Plan   Plan `json:"plan"`
	Schema int  `json:"schema"`
}

// errSchemaMismatch is returned by [loadJournal] when the on-disk
// schema version doesn't match [journalSchemaVersion]. Wrapped in a
// [*PathError] at the call site so callers can errors.Is against
// either this sentinel or [stdfs.ErrNotExist] when distinguishing
// "stale journal" from "no journal".
var errSchemaMismatch = errors.New("fs: plan: journal schema mismatch")

// Apply runs the plan, writing a journal under journalDir so an
// interrupted apply can be resumed. journalDir must be an empty or
// nonexistent directory — Apply refuses to overwrite an existing
// journal to avoid corrupting in-progress applies.
//
// On any per-op failure, Apply returns the error without rolling
// back; the journal records progress so the caller can decide
// whether to [Rollback] or [Resume].
//
// Concurrency: Apply, [Resume], and [Rollback] perform no internal
// locking. Callers driving the same journalDir from multiple
// goroutines or processes must serialize externally (e.g., via
// [Lock] on a sibling lockfile). Independent journals are
// independent — running two Applies against unrelated journalDirs
// concurrently is safe.
func Apply(p *Plan, journalDir string, opts ...ApplyOption) error {
	if p == nil {
		return wrapPathError(opPlanApply, journalDir, ErrInvalidPath)
	}
	cfg := newApplyConfig(opts)

	planPath := filepath.Join(journalDir, journalPlanFilename)
	legacyPath := filepath.Join(journalDir, journalLegacyFilename)
	if Exists(planPath) || Exists(legacyPath) {
		return wrapPathError(opPlanApply, journalDir, ErrAlreadyExists)
	}

	if cfg.mkdirParents {
		if err := MkdirAll(filepath.Join(journalDir, journalBackupsDir), 0o755); err != nil {
			return err
		}
	} else if !IsDir(journalDir) {
		return wrapPathError(opPlanApply, journalDir, ErrNotDir)
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

// Resume continues a previously-interrupted apply from its journal.
// If the apply already completed, Resume is a no-op and returns nil.
// See [Apply] for the concurrency contract.
func Resume(journalDir string, opts ...ApplyOption) error {
	cfg := newApplyConfig(opts)

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
// place (callers can [os.RemoveAll] it explicitly).
// See [Apply] for the concurrency contract.
func Rollback(journalDir string, opts ...ApplyOption) error {
	_ = newApplyConfig(opts)

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

// resumeJournal is the shared body of Apply and Resume: iterate ops
// starting at j.Completed, perform each, persist progress.
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

// applyOp executes a single PlanOp and records any backup data the
// inverse will need.
func applyOp(journalDir string, op PlanOp, step int, cfg applyConfig) error {
	if cfg.mkdirParents && (op.Action == PlanActionCreate || op.Action == PlanActionUpdate || op.Action == PlanActionRename) {
		if err := MkdirAll(filepath.Dir(op.Path), 0o755); err != nil {
			return err
		}
	}

	switch op.Action {
	case PlanActionCreate:
		// Creating requires the file to not exist. No backup needed —
		// rollback removes it.
		if Exists(op.Path) {
			return wrapPathError(opPlanApply, op.Path, ErrAlreadyExists)
		}
		return WriteFile(op.Path, op.Data, WithPerm(op.Perm))

	case PlanActionUpdate:
		// Snapshot the prior contents (if any) so we can restore.
		if err := backupBeforeWrite(journalDir, op.Path, step); err != nil {
			return err
		}
		return WriteFile(op.Path, op.Data, WithPerm(op.Perm))

	case PlanActionDelete:
		if !Exists(op.Path) {
			return nil // idempotent — already gone
		}
		if err := backupBeforeWrite(journalDir, op.Path, step); err != nil {
			return err
		}
		if rerr := os.Remove(op.Path); rerr != nil {
			return wrapPathError(opPlanApply, op.Path, rerr)
		}
		return nil

	case PlanActionRename:
		// Snapshot the destination (if it exists) so rollback can put
		// it back. The source vanishes — its content is preserved at
		// the destination, so no extra snapshot needed.
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

// rollbackOp reverses a previously-applied op using the journal's
// backup snapshot.
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
		// Move back: dst → src.
		if rerr := os.Rename(op.Path, op.Source); rerr != nil {
			return wrapPathError(opPlanRollback, op.Path, rerr)
		}
		// If the destination originally existed (had a backup), put
		// it back where it was.
		return restoreFromBackup(journalDir, op.Path, step)

	default:
		return wrapPathError(opPlanRollback, op.Path, ErrInvalidPath)
	}
}

// backupBeforeWrite copies the existing file at path into the
// journal's backups/<step> dir. If path doesn't exist, it's a no-op.
// The backup is stored with the same basename so rollback can locate
// it.
func backupBeforeWrite(journalDir, path string, step int) error {
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
// snapshot exists (target was originally absent), the path is
// removed instead so the post-rollback state matches the pre-apply
// state.
func restoreFromBackup(journalDir, path string, step int) error {
	src := filepath.Join(journalDir, journalBackupsDir, fmt.Sprintf("step-%d", step), filepath.Base(path))
	data, err := os.ReadFile(src)
	if err != nil {
		if errors.Is(err, stdfs.ErrNotExist) {
			// No backup was created → path was originally absent.
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

// savePlanFile writes plan.json once at Apply start. The file is
// then immutable — Apply / Resume / Rollback only read it.
func savePlanFile(journalDir string, j *journal) error {
	pf := planFile{Plan: j.Plan, Schema: journalSchemaVersion}
	data, err := json.MarshalIndent(pf, "", "  ")
	if err != nil {
		return wrapPathError(opPlanJournal, journalDir, err)
	}
	target := filepath.Join(journalDir, journalPlanFilename)
	return WriteFile(target, data, WithPerm(0o644))
}

// saveProgress rewrites progress.json after each successful op (and
// once more on Finalize). Tiny payload — Completed + Finalized +
// Schema.
func saveProgress(journalDir string, j *journal) error {
	j.Schema = journalSchemaVersion
	data, err := json.MarshalIndent(j, "", "  ")
	if err != nil {
		return wrapPathError(opPlanJournal, journalDir, err)
	}
	target := filepath.Join(journalDir, journalProgressFilename)
	return WriteFile(target, data, WithPerm(0o644))
}

// loadJournal reads plan.json + progress.json from journalDir.
// Returns the merged journal. Refuses to resume on schema mismatch.
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

// newApplyConfig applies opts to a default-initialized config.
func newApplyConfig(opts []ApplyOption) applyConfig {
	cfg := applyConfig{mkdirParents: true}
	for _, opt := range opts {
		opt(&cfg)
	}
	return cfg
}
