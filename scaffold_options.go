package fs

// ScaffoldOption configures [ScaffoldApply], [ScaffoldPlan], and
// [ScaffoldExtract].
type ScaffoldOption func(*scaffoldOptions)

type scaffoldOptions struct {
	onConflict    ScaffoldOnConflict
	promptFunc    func(path string, plan ScaffoldAction) ScaffoldActionOp
	versionMarker string
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
// [ScaffoldActionSkip]). Required when onConflict is
// PromptInteractive — a missing prompt with that policy errors.
func WithScaffoldPromptFunc(fn func(path string, plan ScaffoldAction) ScaffoldActionOp) ScaffoldOption {
	return func(o *scaffoldOptions) { o.promptFunc = fn }
}

// WithScaffoldVersionMarker overrides the marker filename
// [ScaffoldExtract] writes under dst to record the source version.
// Default ".scaffold-version".
func WithScaffoldVersionMarker(name string) ScaffoldOption {
	return func(o *scaffoldOptions) {
		if name != "" {
			o.versionMarker = name
		}
	}
}
