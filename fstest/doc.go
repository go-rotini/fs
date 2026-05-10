// Package fstest provides test helpers for code that uses the
// [github.com/go-rotini/fs] package: a sandbox harness rooted at
// [testing.T.TempDir], an in-memory read-only [io/fs.FS], a
// process-env snapshot/restore helper, and `t.Cleanup`-registering
// temp-file wrappers.
//
// fstest lives in a subpackage so that production code importing
// [github.com/go-rotini/fs] does not pull the stdlib [testing]
// package into its binary. The main fs package is testing-free; the
// helpers here are for callers writing tests against filesystem-
// aware code.
//
// # Use from caller tests
//
//	import (
//	    "github.com/go-rotini/fs"
//	    "github.com/go-rotini/fs/fstest"
//	)
//
//	func TestMyApp(t *testing.T) {
//	    h := fstest.NewTestHarness(t)
//	    h.WriteString("config.yaml", "port: 8080\n")
//	    h.Mkdir("data/cache")
//
//	    // Run code under test against the harness root:
//	    cfg := h.Path("config.yaml")
//	    _ = cfg
//
//	    // Compare deterministic layout against a golden:
//	    if got := h.Snapshot(); got != want {
//	        t.Errorf("layout drift:\n%s", got)
//	    }
//	}
package fstest
