package fstest

import (
	"os"
	"strings"
	"testing"
)

// WithTempEnv snapshots `os.Environ` at call time and registers a
// `t.Cleanup` that restores it. Pair with `t.Setenv` for tests that
// modify many env vars: `WithTempEnv(t)` first, then any number of
// `t.Setenv` calls — every modification reverts when the test
// returns, regardless of how the test exits.
//
// `t.Setenv` alone restores the specific variable it sets, but
// tests that mutate env via direct `os.Setenv` (or that need to
// guarantee no leakage from earlier test goroutines) benefit from
// the bulk snapshot/restore.
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
