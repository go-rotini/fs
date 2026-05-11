package fs

import (
	"errors"
	stdfs "io/fs"
	"path/filepath"
)

// ScaffoldAction describes one planned scaffold operation.
type ScaffoldAction struct {
	// Op classifies what the apply phase will do for this entry.
	Op ScaffoldActionOp
	// SrcPath is the path inside the source [io/fs.FS].
	SrcPath string
	// DstPath is the resolved on-disk destination path.
	DstPath string
	// Reason is a short human-readable explanation suitable for
	// dry-run output. Examples: "would create", "skipped: exists",
	// "would overwrite".
	Reason string
	// IsDir is true for directory entries.
	IsDir bool
}

// ScaffoldActionOp classifies a [ScaffoldAction]. Apply uses this to
// decide whether to touch the filesystem.
type ScaffoldActionOp int

const (
	// ScaffoldActionCreate creates a new entry at DstPath.
	ScaffoldActionCreate ScaffoldActionOp = iota
	// ScaffoldActionSkip leaves DstPath untouched.
	ScaffoldActionSkip
	// ScaffoldActionOverwrite replaces DstPath with rendered content.
	ScaffoldActionOverwrite
	// ScaffoldActionConflict means the conflict resolution failed
	// (typically a prompt callback returned an unsupported value);
	// apply aborts.
	ScaffoldActionConflict
)

// String renders the canonical lowercase op name.
func (o ScaffoldActionOp) String() string {
	switch o {
	case ScaffoldActionCreate:
		return planActionLabelCreate
	case ScaffoldActionSkip:
		return "skip"
	case ScaffoldActionOverwrite:
		return "overwrite"
	case ScaffoldActionConflict:
		return "conflict"
	default:
		return unknownLabel
	}
}

// ScaffoldOnConflict configures how [ScaffoldApply] handles an
// existing destination path.
type ScaffoldOnConflict int

const (
	// ScaffoldSkipExisting keeps the destination intact (default).
	ScaffoldSkipExisting ScaffoldOnConflict = iota
	// ScaffoldOverwriteAll replaces every existing destination.
	ScaffoldOverwriteAll
	// ScaffoldPromptInteractive calls the [WithScaffoldPromptFunc]
	// callback per conflict.
	ScaffoldPromptInteractive
)

const (
	opScaffoldApply   = "scaffoldapply"
	opScaffoldPlan    = "scaffoldplan"
	opScaffoldExtract = "scaffoldextract"
)

// ErrScaffoldPromptRequired is returned when [ScaffoldPromptInteractive]
// is selected without a prompt function provided via
// [WithScaffoldPromptFunc].
var ErrScaffoldPromptRequired = errors.New("fs: scaffold: prompt function required for PromptInteractive")

// ErrScaffoldPromptUnsupported is returned when [WithScaffoldPromptFunc]
// returns an action that isn't [ScaffoldActionSkip] /
// [ScaffoldActionOverwrite] / [ScaffoldActionCreate].
var ErrScaffoldPromptUnsupported = errors.New("fs: scaffold: prompt returned unsupported action")

// ErrScaffoldUnresolvedConflict is returned when a conflict makes
// it through to the apply phase without being resolved (typically
// only happens when the prompt callback is misconfigured).
var ErrScaffoldUnresolvedConflict = errors.New("fs: scaffold: unresolved conflict")

// ScaffoldPlan walks src, renders every path through [text/template]
// with vars (so a source named `{{.AppName}}.go` becomes
// `myapp.go`), and returns the actions [ScaffoldApply] would
// perform; without any filesystem writes. Useful for `--dry-run`
// flags.
//
// Template syntax errors in source paths or contents abort the
// plan.
func ScaffoldPlan(src stdfs.FS, dst string, vars any, opts ...ScaffoldOption) ([]ScaffoldAction, error) {
	cfg := newScaffoldOptions(opts)
	if cfg.onConflict == ScaffoldPromptInteractive && cfg.promptFunc == nil {
		return nil, wrapPathError(opScaffoldPlan, dst, ErrScaffoldPromptRequired)
	}
	return scaffoldWalk(src, dst, vars, cfg)
}

// ScaffoldApply walks src, renders templates with vars, and writes
// the rendered tree under dst. Conflict policy is configured via
// [WithScaffoldOnConflict]. By default existing destinations are
// kept (ScaffoldSkipExisting); a re-run on a previously-applied
// scaffold is therefore a no-op.
func ScaffoldApply(src stdfs.FS, dst string, vars any, opts ...ScaffoldOption) error {
	plan, err := ScaffoldPlan(src, dst, vars, opts...)
	if err != nil {
		return err
	}
	cfg := newScaffoldOptions(opts)
	return scaffoldExecute(src, vars, plan, cfg)
}

// scaffoldWalk traverses src and produces the action list.
func scaffoldWalk(src stdfs.FS, dst string, vars any, cfg scaffoldOptions) ([]ScaffoldAction, error) {
	var plan []ScaffoldAction
	err := stdfs.WalkDir(src, ".", func(path string, d stdfs.DirEntry, werr error) error {
		if werr != nil {
			return werr
		}
		if path == "." {
			return nil
		}
		renderedPath, terr := renderTemplate(path, vars)
		if terr != nil {
			return wrapPathError(opScaffoldPlan, path, terr)
		}
		dstPath := filepath.Join(dst, filepath.FromSlash(renderedPath))

		action := ScaffoldAction{
			SrcPath: path,
			DstPath: dstPath,
			IsDir:   d.IsDir(),
		}
		decideAction(&action, cfg)
		plan = append(plan, action)
		return nil
	})
	if err != nil {
		return plan, wrapPathError(opScaffoldPlan, dst, err)
	}
	return plan, nil
}

// decideAction sets action.Op and Reason based on the existence of
// dstPath and the conflict policy.
func decideAction(action *ScaffoldAction, cfg scaffoldOptions) {
	exists := Exists(action.DstPath)
	if !exists {
		action.Op = ScaffoldActionCreate
		action.Reason = "would create"
		return
	}
	if action.IsDir {
		// Directories are no-ops if they already exist.
		action.Op = ScaffoldActionSkip
		action.Reason = "directory exists"
		return
	}
	switch cfg.onConflict {
	case ScaffoldSkipExisting:
		action.Op = ScaffoldActionSkip
		action.Reason = "exists; skipped"
	case ScaffoldOverwriteAll:
		action.Op = ScaffoldActionOverwrite
		action.Reason = "would overwrite"
	case ScaffoldPromptInteractive:
		// Prompt resolution happens in apply; plan records a Conflict
		// so dry-run shows the user what will be prompted.
		action.Op = ScaffoldActionConflict
		action.Reason = "exists; will prompt"
	}
}

// scaffoldExecute applies the plan to disk.
func scaffoldExecute(src stdfs.FS, vars any, plan []ScaffoldAction, cfg scaffoldOptions) error {
	for _, action := range plan {
		op := action.Op
		// Resolve PromptInteractive conflicts now.
		if op == ScaffoldActionConflict && cfg.onConflict == ScaffoldPromptInteractive {
			op = cfg.promptFunc(action.DstPath, action)
			switch op {
			case ScaffoldActionSkip, ScaffoldActionOverwrite, ScaffoldActionCreate:
				// Allowed responses.
			default:
				return wrapPathError(opScaffoldApply, action.DstPath, ErrScaffoldPromptUnsupported)
			}
		}

		switch op {
		case ScaffoldActionSkip:
			continue
		case ScaffoldActionConflict:
			return wrapPathError(opScaffoldApply, action.DstPath, ErrScaffoldUnresolvedConflict)
		case ScaffoldActionCreate, ScaffoldActionOverwrite:
			if action.IsDir {
				if err := osMkdirAll(action.DstPath, Mode0755); err != nil {
					return wrapPathError(opScaffoldApply, action.DstPath, err)
				}
				continue
			}
			if err := scaffoldWriteFile(src, action, vars); err != nil {
				return err
			}
		}
	}
	return nil
}

// scaffoldWriteFile reads src/srcPath, renders its contents through
// text/template with vars, and writes to dstPath atomically. Parent
// directories are created with mode 0o755.
func scaffoldWriteFile(src stdfs.FS, action ScaffoldAction, vars any) error {
	if err := osMkdirAll(filepath.Dir(action.DstPath), Mode0755); err != nil {
		return wrapPathError(opScaffoldApply, action.DstPath, err)
	}
	data, err := stdfs.ReadFile(src, action.SrcPath)
	if err != nil {
		return wrapPathError(opScaffoldApply, action.SrcPath, err)
	}
	rendered, terr := renderTemplate(string(data), vars)
	if terr != nil {
		return wrapPathError(opScaffoldApply, action.SrcPath, terr)
	}
	if err := WriteFile(action.DstPath, []byte(rendered)); err != nil {
		return err
	}
	return nil
}
