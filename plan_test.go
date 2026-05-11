package fs

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPlan_DiffEmpty(t *testing.T) {
	t.Parallel()
	if got := NewPlan().Diff(); !strings.Contains(got, "empty") {
		t.Errorf("Diff(empty) = %q; want to mention 'empty'", got)
	}
}

func TestPlan_DiffRendersOps(t *testing.T) {
	t.Parallel()
	p := NewPlan().
		Create("/a", []byte("x"), 0o644).
		Update("/b", []byte("yy"), 0o644).
		Delete("/c").
		Rename("/d", "/e")

	got := p.Diff()
	for _, want := range []string{"create /a", "update /b", "delete /c", "rename /d -> /e"} {
		if !strings.Contains(got, want) {
			t.Errorf("Diff missing %q\n%s", want, got)
		}
	}
}

func TestApply_CreatesFiles(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	target := filepath.Join(dir, "out", "hello.txt")
	jdir := filepath.Join(dir, "journal")

	p := NewPlan().Create(target, []byte("hi"), 0o644)
	if err := Apply(p, jdir); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	got, _ := os.ReadFile(target)
	if string(got) != "hi" {
		t.Errorf("content = %q; want hi", got)
	}
}

func TestApply_UpdateBacksUpOriginal(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	target := filepath.Join(dir, "data.txt")
	jdir := filepath.Join(dir, "journal")
	if err := os.WriteFile(target, []byte("v1"), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}

	p := NewPlan().Update(target, []byte("v2"), 0o644)
	if err := Apply(p, jdir); err != nil {
		t.Fatalf("Apply: %v", err)
	}

	got, _ := os.ReadFile(target)
	if string(got) != "v2" {
		t.Errorf("content = %q; want v2", got)
	}

	// The backup must be present in the journal so Rollback can use
	// it.
	backup := filepath.Join(jdir, journalBackupsDir, "step-0", "data.txt")
	bdata, err := os.ReadFile(backup)
	if err != nil {
		t.Fatalf("read backup: %v", err)
	}
	if string(bdata) != "v1" {
		t.Errorf("backup = %q; want v1", bdata)
	}
}

func TestApply_DeleteThenRollbackRestores(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	target := filepath.Join(dir, "byebye.txt")
	jdir := filepath.Join(dir, "journal")
	if err := os.WriteFile(target, []byte("original"), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}

	p := NewPlan().Delete(target)
	if err := Apply(p, jdir); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if Exists(target) {
		t.Fatal("Apply did not delete")
	}

	if err := Rollback(jdir); err != nil {
		t.Fatalf("Rollback: %v", err)
	}
	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("read after rollback: %v", err)
	}
	if string(got) != "original" {
		t.Errorf("after rollback content = %q; want original", got)
	}
}

func TestApply_RenameAndRollback(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	src := filepath.Join(dir, "src.txt")
	dst := filepath.Join(dir, "dst.txt")
	jdir := filepath.Join(dir, "journal")
	if err := os.WriteFile(src, []byte("moveme"), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}

	p := NewPlan().Rename(src, dst)
	if err := Apply(p, jdir); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if Exists(src) {
		t.Error("source still exists after rename")
	}
	if !Exists(dst) {
		t.Error("destination missing after rename")
	}

	if err := Rollback(jdir); err != nil {
		t.Fatalf("Rollback: %v", err)
	}
	if !Exists(src) {
		t.Error("source not restored after rollback")
	}
	if Exists(dst) {
		t.Error("destination not removed after rollback")
	}
}

func TestApply_CreateRollbackRemovesFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	target := filepath.Join(dir, "new.txt")
	jdir := filepath.Join(dir, "journal")

	p := NewPlan().Create(target, []byte("data"), 0o644)
	if err := Apply(p, jdir); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if err := Rollback(jdir); err != nil {
		t.Fatalf("Rollback: %v", err)
	}
	if Exists(target) {
		t.Error("rollback did not remove created file")
	}
}

func TestApply_MultipleOpsRollbackInReverse(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	a := filepath.Join(dir, "a.txt")
	b := filepath.Join(dir, "b.txt")
	c := filepath.Join(dir, "c.txt")
	jdir := filepath.Join(dir, "journal")
	if err := os.WriteFile(b, []byte("b-orig"), 0o644); err != nil {
		t.Fatalf("seed b: %v", err)
	}
	if err := os.WriteFile(c, []byte("c-orig"), 0o644); err != nil {
		t.Fatalf("seed c: %v", err)
	}

	p := NewPlan().
		Create(a, []byte("a-new"), 0o644).
		Update(b, []byte("b-new"), 0o644).
		Delete(c)
	if err := Apply(p, jdir); err != nil {
		t.Fatalf("Apply: %v", err)
	}

	if err := Rollback(jdir); err != nil {
		t.Fatalf("Rollback: %v", err)
	}
	if Exists(a) {
		t.Error("a should be removed by rollback")
	}
	gotB, _ := os.ReadFile(b)
	if string(gotB) != "b-orig" {
		t.Errorf("b after rollback = %q; want b-orig", gotB)
	}
	gotC, _ := os.ReadFile(c)
	if string(gotC) != "c-orig" {
		t.Errorf("c after rollback = %q; want c-orig", gotC)
	}
}

func TestResume_PicksUpFromInterruptedApply(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	target := filepath.Join(dir, "out.txt")
	jdir := filepath.Join(dir, "journal")

	p := NewPlan().Create(target, []byte("data"), 0o644)

	// Stage 1: pre-populate the journal with the plan but no
	// completed ops, simulating an apply that crashed before step 0.
	if err := MkdirAll(filepath.Join(jdir, journalBackupsDir), 0o755); err != nil {
		t.Fatalf("MkdirAll journal: %v", err)
	}
	j := journal{Plan: *p, Completed: 0}
	if err := savePlanFile(jdir, &j); err != nil {
		t.Fatalf("savePlanFile: %v", err)
	}
	if err := saveProgress(jdir, &j); err != nil {
		t.Fatalf("saveProgress: %v", err)
	}

	// Stage 2: Resume should complete the apply.
	if err := Resume(jdir); err != nil {
		t.Fatalf("Resume: %v", err)
	}
	got, _ := os.ReadFile(target)
	if string(got) != "data" {
		t.Errorf("after Resume = %q; want data", got)
	}

	// Resuming a completed journal is a no-op.
	if err := Resume(jdir); err != nil {
		t.Errorf("Resume on completed: %v", err)
	}
}

func TestApply_RejectsExistingJournal(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	jdir := filepath.Join(dir, "journal")

	p := NewPlan().Create(filepath.Join(dir, "a"), []byte("x"), 0o644)
	if err := Apply(p, jdir); err != nil {
		t.Fatalf("first Apply: %v", err)
	}
	// Same journal dir again must refuse.
	if err := Apply(p, jdir); !errors.Is(err, ErrAlreadyExists) {
		t.Errorf("second Apply err = %v; want ErrAlreadyExists", err)
	}
}

func TestApply_NilPlanRejected(t *testing.T) {
	t.Parallel()
	if err := Apply(nil, t.TempDir()); !errors.Is(err, ErrInvalidPath) {
		t.Errorf("err = %v; want ErrInvalidPath", err)
	}
}

func TestApplyTransient_RunsOpsWithoutJournal(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	target := filepath.Join(dir, "out", "hello.txt")

	p := NewPlan().Create(target, []byte("hi"), 0o644)
	if err := ApplyTransient(p); err != nil {
		t.Fatalf("ApplyTransient: %v", err)
	}
	got, _ := os.ReadFile(target)
	if string(got) != "hi" {
		t.Errorf("content = %q; want hi", got)
	}
	// No journal subdirs should be created anywhere under dir.
	dirents, _ := os.ReadDir(dir)
	for _, e := range dirents {
		if e.IsDir() && e.Name() == journalBackupsDir {
			t.Errorf("ApplyTransient leaked a %q dir", journalBackupsDir)
		}
	}
}

func TestApplyTransient_NilPlanRejected(t *testing.T) {
	t.Parallel()
	if err := ApplyTransient(nil); !errors.Is(err, ErrInvalidPath) {
		t.Errorf("err = %v; want ErrInvalidPath", err)
	}
}

func TestApplyTransient_StopsAtFirstError(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	pre := filepath.Join(dir, "pre.txt")
	if err := os.WriteFile(pre, []byte("here"), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}

	// Plan: create A (works), create pre (fails; exists), create B
	// (skipped). After the call, A exists, B does not.
	a := filepath.Join(dir, "a.txt")
	b := filepath.Join(dir, "b.txt")
	p := NewPlan().
		Create(a, []byte("a"), 0o644).
		Create(pre, []byte("nope"), 0o644).
		Create(b, []byte("b"), 0o644)

	err := ApplyTransient(p)
	if !errors.Is(err, ErrAlreadyExists) {
		t.Errorf("err = %v; want ErrAlreadyExists", err)
	}
	if !Exists(a) {
		t.Error("first op should have run")
	}
	if Exists(b) {
		t.Error("third op should not have run after the error")
	}
}

func TestApply_CreateRejectsExistingFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	target := filepath.Join(dir, "existing.txt")
	if err := os.WriteFile(target, []byte("here"), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	jdir := filepath.Join(dir, "journal")

	p := NewPlan().Create(target, []byte("new"), 0o644)
	err := Apply(p, jdir)
	if !errors.Is(err, ErrAlreadyExists) {
		t.Errorf("err = %v; want ErrAlreadyExists", err)
	}
}

func TestPlanAction_String(t *testing.T) {
	t.Parallel()
	cases := map[PlanAction]string{
		PlanActionCreate: "create",
		PlanActionUpdate: "update",
		PlanActionDelete: "delete",
		PlanActionRename: "rename",
		PlanAction(99):   unknownLabel,
	}
	for a, want := range cases {
		if got := a.String(); got != want {
			t.Errorf("PlanAction(%d).String() = %q; want %q", a, got, want)
		}
	}
}
