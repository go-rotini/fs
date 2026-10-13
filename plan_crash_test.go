package fs

import (
	"errors"
	"maps"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// crashHooks makes the n-th mutating OS call fail, and every call after
// it, as if the process had died at that point. A write that hits the
// crash point is torn: half of it lands. Syncs are counted but not
// performed, to keep the exhaustive loops fast.
type crashHooks struct {
	at      int
	calls   int
	crashed bool
}

func (c *crashHooks) hit() bool {
	if c.crashed {
		return true
	}
	c.calls++
	if c.calls == c.at {
		c.crashed = true
	}
	return c.crashed
}

// install routes the hooks through c; h restores them.
func (c *crashHooks) install(h *faultyHooks) {
	osRename = func(a, b string) error {
		if c.hit() {
			return errInjected
		}
		return h.origRename(a, b)
	}
	osRemove = func(p string) error {
		if c.hit() {
			return errInjected
		}
		return h.origRemove(p)
	}
	osRemoveAll = func(p string) error {
		if c.hit() {
			return errInjected
		}
		return h.origRemoveAll(p)
	}
	osMkdirAll = func(p string, m os.FileMode) error {
		if c.hit() {
			return errInjected
		}
		return h.origMkdirAll(p, m)
	}
	osChmod = func(p string, m os.FileMode) error {
		if c.hit() {
			return errInjected
		}
		return h.origChmod(p, m)
	}
	fileWrite = func(f *os.File, b []byte) (int, error) {
		if c.hit() {
			n, _ := h.origFileWrite(f, b[:len(b)/2])
			return n, errInjected
		}
		return h.origFileWrite(f, b)
	}
	fileSync = func(*os.File) error {
		if c.hit() {
			return errInjected
		}
		return nil
	}
	fileClose = func(f *os.File) error {
		err := h.origFileClose(f)
		if c.hit() {
			return errInjected
		}
		return err
	}
}

type planCrashFixture struct {
	work, journal string
	plan          *Plan
}

func newPlanCrashFixture(t *testing.T) planCrashFixture {
	t.Helper()
	root := t.TempDir()
	work := filepath.Join(root, "w")
	if err := os.MkdirAll(work, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, data := range planCrashBefore {
		if err := os.WriteFile(filepath.Join(work, name), []byte(data), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	p := NewPlan().
		Update(filepath.Join(work, "a"), []byte("a1"), 0o644).
		Create(filepath.Join(work, "b"), []byte("b1"), 0o644).
		Delete(filepath.Join(work, "c")).
		Rename(filepath.Join(work, "d"), filepath.Join(work, "e")).
		Update(filepath.Join(work, "f"), []byte("f1"), 0o644)
	return planCrashFixture{work: work, journal: filepath.Join(root, "j"), plan: p}
}

var (
	planCrashBefore = map[string]string{"a": "a0", "c": "c0", "d": "d0", "e": "e0"}
	planCrashAfter  = map[string]string{"a": "a1", "b": "b1", "e": "d0", "f": "f1"}
)

func (f planCrashFixture) state(t *testing.T) map[string]string {
	t.Helper()
	ents, err := os.ReadDir(f.work)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, e := range ents {
		if strings.Contains(e.Name(), ".tmp.") {
			continue // a killed process leaves an atomic write's temp file too
		}
		data, err := os.ReadFile(filepath.Join(f.work, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		got[e.Name()] = string(data)
	}
	return got
}

func (f planCrashFixture) expect(t *testing.T, label string, want map[string]string) {
	t.Helper()
	if got := f.state(t); !maps.Equal(got, want) {
		t.Fatalf("%s: files = %v; want %v", label, got, want)
	}
}

// crashRun runs fn with a crash injected at call n. It reports false
// when fn finished before reaching call n, which ends a sweep.
func crashRun(t *testing.T, h *faultyHooks, n int, fn func() error) bool {
	t.Helper()
	c := &crashHooks{at: n}
	c.install(h)
	err := fn()
	h.restore()
	if !c.crashed {
		if err != nil {
			t.Fatalf("run without a crash failed: %v", err)
		}
		return false
	}
	if err == nil {
		t.Fatalf("crash at call %d was not reported", n)
	}
	if !errors.Is(err, errInjected) {
		t.Fatalf("crash at call %d: err = %v; want the injected failure", n, err)
	}
	return true
}

// TestPlan_CrashAtEveryPoint interrupts Apply at every mutating call
// and checks that Resume finishes the plan and Rollback restores the
// original files, including when Resume or Rollback is itself
// interrupted at every point and then run again.
func TestPlan_CrashAtEveryPoint(t *testing.T) {
	// Skip real fsyncs (macOS makes each one a full flush); sync calls
	// still count as crash points.
	newFaultyHooks(t)
	fileSync = func(*os.File) error { return nil }
	h := newFaultyHooks(t)

	// crashedApply returns a fixture whose Apply crashed at call k (or,
	// with k == 0, completed). ok is false when Apply finished before
	// call k.
	crashedApply := func(k int) (planCrashFixture, bool) {
		f := newPlanCrashFixture(t)
		if k == 0 {
			if err := Apply(f.plan, f.journal); err != nil {
				t.Fatal(err)
			}
			return f, true
		}
		return f, crashRun(t, h, k, func() error { return Apply(f.plan, f.journal) })
	}

	points := 0
	for k := 1; ; k++ {
		f, ok := crashedApply(k)
		if !ok {
			f.expect(t, "uninterrupted apply", planCrashAfter)
			break
		}
		points++

		// A crash before plan.json is in place leaves no journal and no
		// changes; the caller runs Apply again.
		if !Exists(filepath.Join(f.journal, journalPlanFilename)) {
			f.expect(t, "crash before the journal exists", planCrashBefore)
			if err := Apply(f.plan, f.journal); err != nil {
				t.Fatalf("apply crash %d: Apply again: %v", k, err)
			}
			f.expect(t, "apply again", planCrashAfter)
			continue
		}

		if err := Resume(f.journal); err != nil {
			t.Fatalf("apply crash %d: Resume: %v", k, err)
		}
		f.expect(t, "resume", planCrashAfter)
		if err := Rollback(f.journal); err != nil {
			t.Fatalf("apply crash %d: Rollback after Resume: %v", k, err)
		}
		f.expect(t, "rollback after resume", planCrashBefore)

		g, _ := crashedApply(k)
		if err := Rollback(g.journal); err != nil {
			t.Fatalf("apply crash %d: Rollback: %v", k, err)
		}
		g.expect(t, "rollback", planCrashBefore)
		if err := Resume(g.journal); err != nil {
			t.Fatalf("apply crash %d: Resume after Rollback: %v", k, err)
		}
		g.expect(t, "resume after rollback", planCrashAfter)
	}
	if points < 20 {
		t.Fatalf("only %d crash points exercised; the hooks are not reaching the journal code", points)
	}

	// Interrupt Resume and Rollback themselves at every point, starting
	// from an apply that crashed part-way and from one that completed.
	for _, k := range []int{points / 2, 0} {
		for m := 1; ; m++ {
			f, _ := crashedApply(k)
			more := crashRun(t, h, m, func() error { return Resume(f.journal) })
			if err := Resume(f.journal); err != nil {
				t.Fatalf("apply crash %d, resume crash %d: Resume: %v", k, m, err)
			}
			f.expect(t, "resume after an interrupted resume", planCrashAfter)
			if !more {
				break
			}
		}
		for m := 1; ; m++ {
			f, _ := crashedApply(k)
			more := crashRun(t, h, m, func() error { return Rollback(f.journal) })
			if err := Rollback(f.journal); err != nil {
				t.Fatalf("apply crash %d, rollback crash %d: Rollback: %v", k, m, err)
			}
			f.expect(t, "rollback after an interrupted rollback", planCrashBefore)
			if err := Resume(f.journal); err != nil {
				t.Fatalf("apply crash %d, rollback crash %d: Resume: %v", k, m, err)
			}
			f.expect(t, "resume after an interrupted rollback", planCrashAfter)
			if !more {
				break
			}
		}
	}
}

func TestPlan_JournalIsOwnerOnly(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX modes")
	}
	t.Parallel()
	dir := t.TempDir()
	jdir := filepath.Join(dir, "j")
	target := filepath.Join(dir, "token")
	if err := os.WriteFile(target, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := Apply(NewPlan().Update(target, []byte("secret"), 0o600), jdir); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{jdir, filepath.Join(jdir, journalBackupsDir)} {
		info, err := os.Stat(p)
		if err != nil {
			t.Fatal(err)
		}
		if perm := info.Mode().Perm(); perm != journalDirPerm {
			t.Errorf("%s mode = %v; want %v", p, perm, journalDirPerm)
		}
	}
	for _, name := range []string{journalPlanFilename, journalProgressFilename} {
		info, err := os.Stat(filepath.Join(jdir, name))
		if err != nil {
			t.Fatal(err)
		}
		if perm := info.Mode().Perm(); perm != journalFilePerm {
			t.Errorf("%s mode = %v; want %v", name, perm, journalFilePerm)
		}
	}
	info, err := os.Stat(filepath.Join(snapshotDir(jdir, 0), "token"))
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("snapshot mode = %v; want 0600", perm)
	}
}

func TestPlan_JournalWritesAreSynced(t *testing.T) {
	h := newFaultyHooks(t)
	dir := t.TempDir()
	jdir := filepath.Join(dir, "j")
	target := filepath.Join(dir, "new")
	synced := map[string]bool{}
	fileSync = func(f *os.File) error {
		synced[filepath.Base(f.Name())] = true
		return h.origFileSync(f)
	}
	if err := Apply(NewPlan().Create(target, []byte("x"), 0o644), jdir); err != nil {
		t.Fatal(err)
	}
	h.restore()
	for _, want := range []string{"plan.json.tmp.", "progress.json.tmp.", "new.tmp."} {
		found := false
		for name := range synced {
			if len(name) >= len(want) && name[:len(want)] == want {
				found = true
			}
		}
		if !found {
			t.Errorf("no sync of %s* (synced: %v)", want, synced)
		}
	}
}

func TestPlan_ZeroPermWrites0644(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX modes")
	}
	t.Parallel()
	dir := t.TempDir()
	for _, journaled := range []bool{false, true} {
		target := filepath.Join(dir, "f")
		_ = os.Remove(target)
		p := &Plan{Ops: []PlanOp{{Action: PlanActionCreate, Path: target, Data: []byte("x")}}}
		var err error
		if journaled {
			err = Apply(p, filepath.Join(dir, "j"))
		} else {
			err = ApplyTransient(p)
		}
		if err != nil {
			t.Fatal(err)
		}
		info, err := os.Stat(target)
		if err != nil {
			t.Fatal(err)
		}
		if perm := info.Mode().Perm(); perm != 0o644 {
			t.Errorf("journaled=%v: mode = %v; want 0644", journaled, perm)
		}
	}
}

func TestPlan_ResumeAfterRollbackReapplies(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	target := filepath.Join(dir, "f")
	jdir := filepath.Join(dir, "j")
	if err := Apply(NewPlan().Create(target, []byte("x"), 0o644), jdir); err != nil {
		t.Fatal(err)
	}
	if err := Rollback(jdir); err != nil {
		t.Fatal(err)
	}
	if Exists(target) {
		t.Fatal("Rollback left the created file")
	}
	if err := Resume(jdir); err != nil {
		t.Fatal(err)
	}
	if !Exists(target) {
		t.Error("Resume after Rollback did not re-apply the plan")
	}
}
