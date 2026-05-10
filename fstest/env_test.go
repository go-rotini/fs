package fstest

import (
	"os"
	"testing"
)

func TestWithTempEnv_RestoresOnCleanup(t *testing.T) {
	const key = "ROTINI_FS_TESTHARNESS_ENV"
	//nolint:usetesting // direct Setenv is the scenario WithTempEnv is meant to handle
	if err := os.Setenv(key, "outer"); err != nil {
		t.Fatalf("Setenv: %v", err)
	}
	t.Cleanup(func() { _ = os.Unsetenv(key) })

	t.Run("inner", func(t *testing.T) {
		WithTempEnv(t)
		// Mutate env inside the subtest via direct os.Setenv —
		// WithTempEnv's reason to exist.
		//nolint:usetesting // direct Setenv is the scenario under test
		if err := os.Setenv(key, "inner"); err != nil {
			t.Fatalf("Setenv: %v", err)
		}
		if got := os.Getenv(key); got != "inner" {
			t.Errorf("inside subtest got %q, want inner", got)
		}
	})

	// After the subtest's t.Cleanup runs, the variable should be back
	// to its outer value.
	if got := os.Getenv(key); got != "outer" {
		t.Errorf("after subtest got %q, want outer", got)
	}
}

func TestWithTempEnv_RestoresClearedVar(t *testing.T) {
	const key = "ROTINI_FS_TESTHARNESS_CLEAR"
	//nolint:usetesting // direct Setenv is the scenario WithTempEnv is meant to handle
	if err := os.Setenv(key, "set-by-outer"); err != nil {
		t.Fatalf("Setenv: %v", err)
	}
	t.Cleanup(func() { _ = os.Unsetenv(key) })

	t.Run("inner", func(t *testing.T) {
		WithTempEnv(t)
		if err := os.Unsetenv(key); err != nil {
			t.Fatalf("Unsetenv: %v", err)
		}
		if _, ok := os.LookupEnv(key); ok {
			t.Error("expected unset inside subtest")
		}
	})

	if got, ok := os.LookupEnv(key); !ok || got != "set-by-outer" {
		t.Errorf("after subtest got (%q, %v), want (set-by-outer, true)", got, ok)
	}
}
