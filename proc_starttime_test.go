package fs

import (
	"os"
	"strings"
	"testing"
)

func TestProcessStartTime_Self(t *testing.T) {
	t.Parallel()
	got, err := ProcessStartTime(os.Getpid())
	if err != nil {
		t.Fatalf("ProcessStartTime(self): %v", err)
	}
	if strings.TrimSpace(got) == "" {
		t.Error("ProcessStartTime(self) returned empty string")
	}
}

func TestProcessStartTime_Stable(t *testing.T) {
	t.Parallel()
	first, err := ProcessStartTime(os.Getpid())
	if err != nil {
		t.Fatalf("first call: %v", err)
	}
	second, err := ProcessStartTime(os.Getpid())
	if err != nil {
		t.Fatalf("second call: %v", err)
	}
	if first != second {
		t.Errorf("ProcessStartTime not stable across calls: %q vs %q", first, second)
	}
}

func TestProcessStartTime_InvalidPIDRejected(t *testing.T) {
	t.Parallel()
	if _, err := ProcessStartTime(0); err == nil {
		t.Error("expected error for pid=0")
	}
	if _, err := ProcessStartTime(-1); err == nil {
		t.Error("expected error for pid=-1")
	}
}

// The start time doesn't depend on the caller's time zone or locale, so two processes with
// different environments agree on it.
func TestProcessStartTime_IndependentOfEnv(t *testing.T) {
	want, err := ProcessStartTime(os.Getpid())
	if err != nil {
		t.Fatal(err)
	}
	for _, env := range [][2]string{{"TZ", "Asia/Tokyo"}, {"TZ", "America/Los_Angeles"}, {"LC_ALL", "fr_FR.UTF-8"}} {
		t.Setenv(env[0], env[1])
		got, err := ProcessStartTime(os.Getpid())
		if err != nil {
			t.Fatal(err)
		}
		if got != want {
			t.Errorf("with %s=%s: %q, want %q", env[0], env[1], got, want)
		}
	}
}
