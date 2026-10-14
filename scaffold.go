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
	// dry-run output, such as "would create", "exists; skipped" or
	// "would overwrite".
	Reason string
	// IsDir is true for directory entries.
	IsDir bool
}

// ScaffoldActionOp classifies a [ScaffoldAction]. [ScaffoldApply] uses
// it to decide whether to touch the filesystem.
type ScaffoldActionOp int

const (
	// ScaffoldActionCreate creates a new entry at DstPath.
	ScaffoldActionCreate ScaffoldActionOp = iota
	// ScaffoldActionSkip leaves DstPath untouched.
	ScaffoldActionSkip
	// ScaffoldActionOverwrite replaces DstPath with rendered content.
	ScaffoldActionOverwrite
	// ScaffoldActionConflict marks an existing file under
	// [ScaffoldPromptInteractive]; [ScaffoldApply] asks the prompt
	// function how to resolve it.
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

// ScaffoldOnConflict configures how [ScaffoldApply] and
// [ScaffoldExtract] handle an existing destination file. Existing
// directories are always reused.
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
// [WithScaffoldPromptFunc]. [ScaffoldPlan] and [ScaffoldApply] check
// up front; [ScaffoldExtract] fails at the first existing file.
var ErrScaffoldPromptRequired = errors.New("fs: scaffold: prompt function required for PromptInteractive")

// ErrScaffoldPromptUnsupported is returned by [ScaffoldApply] when the
// [WithScaffoldPromptFunc] callback returns an action other than
// [ScaffoldActionSkip], [ScaffoldActionOverwrite] or
// [ScaffoldActionCreate].
var ErrScaffoldPromptUnsupported = errors.New("fs: scaffold: prompt returned unsupported action")

// ErrScaffoldUnresolvedConflict is returned by [ScaffoldApply] if a
// planned conflict reaches the write phase without being resolved.
var ErrScaffoldUnresolvedConflict = errors.New("fs: scaffold: unresolved conflict")

// ScaffoldPlan walks src, renders every path through [text/template]
// with vars (so a source named `{{.AppName}}.go` becomes
// `myapp.go`), and returns the actions [ScaffoldApply] would
// perform, without any filesystem writes. Useful for `--dry-run`
// flags.
//
// Templates run with missingkey=error and no functions beyond the
// text/template builtins. A path template error aborts the plan; file
// contents are not rendered until [ScaffoldApply].
//
// Every rendered path must stay inside dst, both as written and as it
// resolves through the links already on disk (symlinks and Windows
// directory junctions); one that does not, such as a variable
// rendering to `../../x`, aborts the plan with an error wrapping
// [ErrEscapesRoot].
func ScaffoldPlan(src stdfs.FS, dst string, vars any, opts ...ScaffoldOption) ([]ScaffoldAction, error) {
	cfg := newScaffoldOptions(opts)
	if cfg.onConflict == ScaffoldPromptInteractive && cfg.promptFunc == nil {
		return nil, wrapPathError(opScaffoldPlan, dst, ErrScaffoldPromptRequired)
	}
	return scaffoldWalk(src, dst, vars, cfg)
}

// ScaffoldApply walks src, renders templates with vars, and writes
// the rendered tree under dst. Conflict policy is configured via
// [WithScaffoldOnConflict]. By default existing files are kept
// ([ScaffoldSkipExisting]), so re-running over an applied scaffold is
// a no-op.
//
// Paths are confined to dst as in [ScaffoldPlan]; an escaping path
// fails before anything is written. Every source file's contents are
// rendered as a template with vars, except files matching
// [WithScaffoldVerbatim], and written atomically: mode 0o644, plus the
// source's execute bits (so scripts stay executable); directories
// 0o755. A content template error stops the apply, leaving files
// already written in place.
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
	root, err := newExtractRoot(opScaffoldPlan, dst)
	if err != nil {
		return nil, err
	}
	var plan []ScaffoldAction
	err = stdfs.WalkDir(src, ".", func(path string, d stdfs.DirEntry, werr error) error {
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
		if err := root.confine(filepath.Join(root.dir, filepath.FromSlash(renderedPath))); err != nil {
			return err
		}

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
			if err := scaffoldWriteFile(src, action, vars, cfg); err != nil {
				return err
			}
		}
	}
	return nil
}

// scaffoldWriteFile reads src/srcPath, renders its contents through
// text/template with vars unless it is verbatim, and writes to dstPath
// atomically with the source's execute bits. Parent directories are
// created with mode 0o755.
func scaffoldWriteFile(src stdfs.FS, action ScaffoldAction, vars any, cfg scaffoldOptions) error {
	if err := osMkdirAll(filepath.Dir(action.DstPath), Mode0755); err != nil {
		return wrapPathError(opScaffoldApply, action.DstPath, err)
	}
	data, err := stdfs.ReadFile(src, action.SrcPath)
	if err != nil {
		return wrapPathError(opScaffoldApply, action.SrcPath, err)
	}
	if !cfg.isVerbatim(action.SrcPath) {
		rendered, terr := renderTemplate(string(data), vars)
		if terr != nil {
			return wrapPathError(opScaffoldApply, action.SrcPath, terr)
		}
		data = []byte(rendered)
	}
	return WriteFile(action.DstPath, data, scaffoldFileMode(src, action.SrcPath)...)
}

// scaffoldFileMode returns the write options that give a scaffolded
// file its source's execute bits: mode 0o644 plus those bits. Without
// execute bits it returns none, so new files get 0o644 and replaced
// files keep their mode. Other source bits are ignored: an
// [embed.FS] reports every file read-only.
func scaffoldFileMode(src stdfs.FS, name string) []WriteOption {
	info, err := stdfs.Stat(src, name)
	if err != nil || info.Mode().Perm()&0o111 == 0 {
		return nil
	}
	return []WriteOption{WithPerm(Mode0644 | info.Mode().Perm()&0o111)}
}
