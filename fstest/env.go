package fstest

import (
	"os"
	"strings"
	"testing"
)

// WithTempEnv snapshots `os.Environ` at call time and registers a
// `t.Cleanup` that restores it. Pair with `t.Setenv` for tests that
// modify many env vars: `WithTempEnv(t)` first, then any number of
// `t.Setenv` calls; every modification reverts when the test
// returns, regardless of how the test exits. It also reverts changes
// made with [os.Setenv] or [os.Unsetenv], which t.Setenv alone does
// not track.
func WithTempEnv(t *testing.T) {
	t.Helper()
	saved := append([]string(nil), os.Environ()...)
	t.Cleanup(func() {
		os.Clearenv()
		for _, kv := range saved {
			k, v, ok := strings.Cut(kv, "=")
			if !ok {
				continue
			}
			//nolint:usetesting // we're inside t.Cleanup; t.Setenv would re-register cleanups recursively
			_ = os.Setenv(k, v)
		}
	})
}
