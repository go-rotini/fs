package fs

import "path"

// ScaffoldOption configures [ScaffoldApply], [ScaffoldPlan] and
// [ScaffoldExtract].
type ScaffoldOption func(*scaffoldOptions)

type scaffoldOptions struct {
	onConflict    ScaffoldOnConflict
	promptFunc    func(path string, plan ScaffoldAction) ScaffoldActionOp
	versionMarker string
	verbatim      []string
}

// isVerbatim reports whether the source file at p (slash-separated)
// matches a [WithScaffoldVerbatim] pattern.
func (o scaffoldOptions) isVerbatim(p string) bool {
	for _, pat := range o.verbatim {
		if ok, _ := path.Match(pat, p); ok { //nolint:errcheck // a malformed pattern matches nothing
			return true
		}
		if ok, _ := path.Match(pat, path.Base(p)); ok { //nolint:errcheck // a malformed pattern matches nothing
			return true
		}
	}
	return false
}

// WithScaffoldVerbatim makes [ScaffoldApply] copy the contents of
// source files matching any of patterns as they are, without
// rendering them as templates; their paths are still rendered. Use it
// for files that contain `{{` of their own, such as GitHub Actions
// workflows, Helm charts or Go templates. A pattern uses [path.Match]
// syntax and is matched against both the file's slash-separated path
// in the source FS and its base name, so "*.yml" matches at any depth
// and ".github/workflows/*" matches one directory.
func WithScaffoldVerbatim(patterns ...string) ScaffoldOption {
	return func(o *scaffoldOptions) {
		o.verbatim = append(o.verbatim, patterns...)
	}
}

const defaultScaffoldVersionMarker = ".scaffold-version"

func newScaffoldOptions(opts []ScaffoldOption) scaffoldOptions {
	cfg := scaffoldOptions{
		onConflict:    ScaffoldSkipExisting,
		versionMarker: defaultScaffoldVersionMarker,
	}
	for _, o := range opts {
		o(&cfg)
	}
	return cfg
}

// WithScaffoldOnConflict sets the policy used when an output path
// already exists. Default [ScaffoldSkipExisting] (don't overwrite).
func WithScaffoldOnConflict(c ScaffoldOnConflict) ScaffoldOption {
	return func(o *scaffoldOptions) { o.onConflict = c }
}

// WithScaffoldPromptFunc registers the per-conflict prompt called
// under [ScaffoldPromptInteractive]. The function receives the
// destination path and the planned action, and returns the action
// to execute (typically [ScaffoldActionOverwrite] or
// [ScaffoldActionSkip]; under [ScaffoldExtract] anything but
// [ScaffoldActionSkip] writes the file). Required with
// [ScaffoldPromptInteractive]; see [ErrScaffoldPromptRequired].
func WithScaffoldPromptFunc(fn func(path string, plan ScaffoldAction) ScaffoldActionOp) ScaffoldOption {
	return func(o *scaffoldOptions) { o.promptFunc = fn }
}

// WithScaffoldVersionMarker overrides the marker filename
// [ScaffoldExtract] writes under dst to record the source version.
// Default ".scaffold-version". The name must be a single file name;
// one containing a path separator, or "." or "..", makes
// ScaffoldExtract fail with an error wrapping [ErrInvalidPath].
func WithScaffoldVersionMarker(name string) ScaffoldOption {
	return func(o *scaffoldOptions) {
		if name != "" {
			o.versionMarker = name
		}
	}
}
