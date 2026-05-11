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

func TestApply_WithApplyNoMkdir_RejectsMissingDir(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	jdir := filepath.Join(dir, "absent-journal")

	p := NewPlan().Create(filepath.Join(dir, "out"), []byte("x"), 0o644)
	err := Apply(p, jdir, WithApplyNoMkdir())
	if !errors.Is(err, ErrNotDir) {
		t.Errorf("err=%v; want ErrNotDir", err)
	}
}

func TestApply_WithApplyNoMkdir_AcceptsExistingDir(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	jdir := filepath.Join(dir, "journal")
	if err := os.MkdirAll(filepath.Join(jdir, journalBackupsDir), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	p := NewPlan().Create(filepath.Join(dir, "out"), []byte("x"), 0o644)
	if err := Apply(p, jdir, WithApplyNoMkdir()); err != nil {
		t.Fatalf("Apply: %v", err)
	}
}

func TestApply_RollbackRestoresUpdateContents(t *testing.T) {
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
	if err := Rollback(jdir); err != nil {
		t.Fatalf("Rollback: %v", err)
	}
	got, _ := os.ReadFile(target)
	if string(got) != "v1" {
		t.Errorf("rollback content = %q; want v1", got)
	}
}

func TestRollback_NoJournalRejected(t *testing.T) {
	t.Parallel()
	if err := Rollback(filepath.Join(t.TempDir(), "missing")); err == nil {
		t.Error("expected error for missing journal")
	}
}

func TestResume_NoJournalRejected(t *testing.T) {
	t.Parallel()
	if err := Resume(filepath.Join(t.TempDir(), "missing")); err == nil {
		t.Error("expected error for missing journal")
	}
}

func TestApply_DeleteRollbackRestoresFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	target := filepath.Join(dir, "doomed.txt")
	jdir := filepath.Join(dir, "journal")
	if err := os.WriteFile(target, []byte("alive"), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}

	if err := Apply(NewPlan().Delete(target), jdir); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if Exists(target) {
		t.Fatal("Delete didn't remove")
	}
	if err := Rollback(jdir); err != nil {
		t.Fatalf("Rollback: %v", err)
	}
	got, _ := os.ReadFile(target)
	if string(got) != "alive" {
		t.Errorf("restored content = %q; want alive", got)
	}
}

func TestApply_DeleteMissingPathIsNoop(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	jdir := filepath.Join(dir, "journal")
	missing := filepath.Join(dir, "never-existed")

	if err := Apply(NewPlan().Delete(missing), jdir); err != nil {
		t.Errorf("Apply Delete on missing: %v", err)
	}
}

func TestApplyTransient_DeleteAndRename(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	a := filepath.Join(dir, "a.txt")
	b := filepath.Join(dir, "b.txt")
	if err := os.WriteFile(a, []byte("contents"), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}

	p := NewPlan().Rename(a, b).Delete(b)
	if err := ApplyTransient(p); err != nil {
		t.Fatalf("ApplyTransient: %v", err)
	}
	if Exists(a) || Exists(b) {
		t.Error("a or b still exists after Rename then Delete")
	}
}

func TestApply_RenameOverExistingDestination(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	src := filepath.Join(dir, "src.txt")
	dst := filepath.Join(dir, "dst.txt")
	jdir := filepath.Join(dir, "journal")
	if err := os.WriteFile(src, []byte("src"), 0o644); err != nil {
		t.Fatalf("seed src: %v", err)
	}
	if err := os.WriteFile(dst, []byte("dst"), 0o644); err != nil {
		t.Fatalf("seed dst: %v", err)
	}

	// Rename src → dst (dst already exists; rename overwrites and a
	// backup of dst's previous content is recorded).
	if err := Apply(NewPlan().Rename(src, dst), jdir); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	got, _ := os.ReadFile(dst)
	if string(got) != "src" {
		t.Errorf("dst after rename = %q; want src", got)
	}
	// Rollback should put both files back.
	if err := Rollback(jdir); err != nil {
		t.Fatalf("Rollback: %v", err)
	}
	gotSrc, _ := os.ReadFile(src)
	gotDst, _ := os.ReadFile(dst)
	if string(gotSrc) != "src" {
		t.Errorf("src after rollback = %q; want src", gotSrc)
	}
	if string(gotDst) != "dst" {
		t.Errorf("dst after rollback = %q; want dst", gotDst)
	}
}

func TestBackupBeforeWrite_DirectoryRejected(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	subdir := filepath.Join(dir, "subdir")
	if err := os.MkdirAll(subdir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := backupBeforeWrite(dir, subdir, 0); !errors.Is(err, ErrIsDir) {
		t.Errorf("err = %v; want ErrIsDir", err)
	}
}

func TestBackupBeforeWrite_EmptyJournalDirIsNoop(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	target := filepath.Join(dir, "file.txt")
	if err := os.WriteFile(target, []byte("x"), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := backupBeforeWrite("", target, 0); err != nil {
		t.Errorf("backupBeforeWrite('') = %v; want nil", err)
	}
}

func TestApply_RenameMissingSource(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	jdir := filepath.Join(dir, "journal")
	missing := filepath.Join(dir, "missing.txt")
	dst := filepath.Join(dir, "dst.txt")

	p := NewPlan().Rename(missing, dst)
	if err := Apply(p, jdir); err == nil {
		t.Error("expected error renaming missing source")
	}
}

func TestApply_UnknownActionRejected(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	jdir := filepath.Join(dir, "journal")

	// Construct a plan with an out-of-range action.
	p := &Plan{Ops: []PlanOp{{Action: PlanAction(99), Path: filepath.Join(dir, "x")}}}
	if err := Apply(p, jdir); !errors.Is(err, ErrInvalidPath) {
		t.Errorf("err = %v; want ErrInvalidPath", err)
	}
}

func TestRollback_UnknownActionRejected(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	jdir := filepath.Join(dir, "journal")
	if err := os.MkdirAll(filepath.Join(jdir, journalBackupsDir), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	// Seed the journal with one completed unknown-action op.
	j := &journal{
		Plan:      Plan{Ops: []PlanOp{{Action: PlanAction(99), Path: filepath.Join(dir, "x")}}},
		Completed: 1,
	}
	if err := savePlanFile(jdir, j); err != nil {
		t.Fatalf("savePlanFile: %v", err)
	}
	if err := saveProgress(jdir, j); err != nil {
		t.Fatalf("saveProgress: %v", err)
	}
	if err := Rollback(jdir); !errors.Is(err, ErrInvalidPath) {
		t.Errorf("err = %v; want ErrInvalidPath", err)
	}
}

func TestApplyTransient_DeleteMissingIsNoop(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	p := NewPlan().Delete(filepath.Join(dir, "missing.txt"))
	if err := ApplyTransient(p); err != nil {
		t.Errorf("ApplyTransient: %v", err)
	}
}

func TestApply_UpdateNoPriorContent(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	target := filepath.Join(dir, "new.txt")
	jdir := filepath.Join(dir, "journal")

	// Update on a non-existent file works (backup is a no-op because
	// the source doesn't exist).
	if err := Apply(NewPlan().Update(target, []byte("first"), 0o644), jdir); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	got, _ := os.ReadFile(target)
	if string(got) != "first" {
		t.Errorf("content = %q; want first", got)
	}
}

func TestApply_RollbackEmptyJournalIsNoop(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	jdir := filepath.Join(dir, "journal")

	// Apply an empty plan (no ops).
	if err := Apply(NewPlan(), jdir); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if err := Rollback(jdir); err != nil {
		t.Errorf("Rollback of empty journal: %v", err)
	}
}

func TestLoadJournal_MissingFiles(t *testing.T) {
	t.Parallel()
	// Empty journal dir: load fails because plan.json is missing.
	if _, err := loadJournal(t.TempDir()); err == nil {
		t.Error("expected error loading empty journal")
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
