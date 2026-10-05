package deploy_test

// format_change_executor_test.go specifies that the executor records a formatting change on
// the ActionRecord whenever a real write replaces an existing file whose BOM presence or
// line-ending style differs from the written bytes, and records nothing otherwise. The
// written bytes themselves stay exactly what the content function produced.

import (
	"bytes"
	"context"
	"path/filepath"
	"testing"

	"mosaic-deploy/internal/deploy"
	"mosaic-deploy/internal/domain"
	"mosaic-deploy/internal/manifest"
)

const fcTarget = "agents/runner.md"

// runFormatChange writes existing (when non-nil) at the target, executes item with content,
// and returns the single resulting action record plus the workspace.
func runFormatChange(t *testing.T, existing, content []byte, item domain.PlanItem,
	conflicts map[string]domain.ConflictDecision) (domain.ActionRecord, string) {
	t.Helper()
	workspace := t.TempDir()
	if existing != nil {
		writeExisting(t, workspace, fcTarget, existing)
	}
	req := deploy.ExecRequest{
		Plan:       newPlan(workspace, item),
		MosaicRoot: t.TempDir(),
		Content:    fixedContent(content),
		Conflicts:  conflicts,
	}

	ex := deploy.NewExecutor(manifest.NewStore(), newSpyLogger(), newSpyCollector())
	result, err := ex.Execute(context.Background(), req)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(result.Actions) != 1 {
		t.Fatalf("got %d action records; want 1", len(result.Actions))
	}
	return result.Actions[0], workspace
}

func updateItem() domain.PlanItem {
	return newAgentItem("runner", fcTarget, domain.ActionUpdate)
}

func overwriteDecision() map[string]domain.ConflictDecision {
	return map[string]domain.ConflictDecision{fcTarget: domain.DecisionOverwrite}
}

func backupDecision() map[string]domain.ConflictDecision {
	return map[string]domain.ConflictDecision{fcTarget: domain.DecisionBackupThenOverwrite}
}

// assertChange fails unless ar carries a FormatChange with the expected BOM and line-ending
// before/after values.
func assertChange(t *testing.T, ar domain.ActionRecord, bomBefore, bomAfter bool, before, after domain.LineEndingStyle) {
	t.Helper()
	fc := ar.FormatChange
	if fc == nil {
		t.Fatalf("ActionRecord.FormatChange = nil (Taken=%q); want a recorded change", ar.Taken)
	}
	if fc.BOMBefore != bomBefore || fc.BOMAfter != bomAfter {
		t.Errorf("BOM before/after = %v/%v; want %v/%v", fc.BOMBefore, fc.BOMAfter, bomBefore, bomAfter)
	}
	if fc.LineEndingsBefore != before || fc.LineEndingsAfter != after {
		t.Errorf("line endings before/after = %q/%q; want %q/%q",
			fc.LineEndingsBefore, fc.LineEndingsAfter, before, after)
	}
}

func TestExecutor_UpdateOverBOMCRLFFile_RecordsBothChanges(t *testing.T) {
	existing := withBOM("---\r\nname: a\r\n---\r\nbody\r\n")
	content := []byte("---\nname: a\n---\nbody\n")

	ar, ws := runFormatChange(t, existing, content, updateItem(), nil)

	assertChange(t, ar, true, false, domain.LineEndingCRLF, domain.LineEndingLF)
	if got := readFile(t, filepath.Join(ws, fcTarget)); !bytes.Equal(got, content) {
		t.Errorf("written bytes changed by reporting:\ngot:  %q\nwant: %q", got, content)
	}
}

func TestExecutor_UpdateBOMOnlyDifference_RecordsBOMChange(t *testing.T) {
	ar, _ := runFormatChange(t, withBOM("a\nb\n"), []byte("x\ny\n"), updateItem(), nil)

	assertChange(t, ar, true, false, domain.LineEndingLF, domain.LineEndingLF)
}

func TestExecutor_UpdateLineEndingOnlyDifference_RecordsLineEndingChange(t *testing.T) {
	ar, _ := runFormatChange(t, []byte("a\r\nb\r\n"), []byte("a\nb\n"), updateItem(), nil)

	assertChange(t, ar, false, false, domain.LineEndingCRLF, domain.LineEndingLF)
}

func TestExecutor_UpdateWithIdenticalFormatting_RecordsNothing(t *testing.T) {
	ar, _ := runFormatChange(t, []byte("old\nlines\n"), []byte("new\ncontent\n"), updateItem(), nil)

	if ar.FormatChange != nil {
		t.Errorf("FormatChange = %+v; want nil when formatting matches", *ar.FormatChange)
	}
}

func TestExecutor_CreateOnAbsentPath_RecordsNothing(t *testing.T) {
	item := newAgentItem("runner", fcTarget, domain.ActionCreate)

	ar, _ := runFormatChange(t, nil, withBOM("a\r\n"), item, nil)

	if ar.Taken != domain.TakenCreated {
		t.Fatalf("Taken = %q; want %q", ar.Taken, domain.TakenCreated)
	}
	if ar.FormatChange != nil {
		t.Errorf("FormatChange = %+v; want nil for a previously absent path", *ar.FormatChange)
	}
}

func TestExecutor_CreateLandingOnExistingFile_RecordsChange(t *testing.T) {
	item := newAgentItem("runner", fcTarget, domain.ActionCreate)

	ar, _ := runFormatChange(t, withBOM("a\r\n"), []byte("a\n"), item, nil)

	assertChange(t, ar, true, false, domain.LineEndingCRLF, domain.LineEndingLF)
}

func TestExecutor_ConflictOverwrite_RecordsChange(t *testing.T) {
	item := newConflictItem("runner", fcTarget)

	ar, _ := runFormatChange(t, withBOM("a\r\nb\r\n"), []byte("a\nb\n"), item, overwriteDecision())

	if ar.Taken != domain.TakenUpdated {
		t.Fatalf("Taken = %q; want %q", ar.Taken, domain.TakenUpdated)
	}
	assertChange(t, ar, true, false, domain.LineEndingCRLF, domain.LineEndingLF)
}

func TestExecutor_ConflictBackupThenOverwrite_RecordsChange(t *testing.T) {
	item := newConflictItem("runner", fcTarget)

	ar, _ := runFormatChange(t, withBOM("a\r\nb\r\n"), []byte("a\nb\n"), item, backupDecision())

	if ar.Taken != domain.TakenBackedUp {
		t.Fatalf("Taken = %q; want %q", ar.Taken, domain.TakenBackedUp)
	}
	assertChange(t, ar, true, false, domain.LineEndingCRLF, domain.LineEndingLF)
}

func TestExecutor_ConflictOverwriteWithIdenticalFormatting_RecordsNothing(t *testing.T) {
	item := newConflictItem("runner", fcTarget)

	ar, _ := runFormatChange(t, []byte("a\nb\n"), []byte("c\nd\n"), item, overwriteDecision())

	if ar.FormatChange != nil {
		t.Errorf("FormatChange = %+v; want nil when formatting matches", *ar.FormatChange)
	}
}

func TestExecutor_ConflictSkip_RecordsNothing(t *testing.T) {
	item := newConflictItem("runner", fcTarget)
	skip := map[string]domain.ConflictDecision{fcTarget: domain.DecisionSkip}

	ar, _ := runFormatChange(t, withBOM("a\r\n"), []byte("a\n"), item, skip)

	if ar.FormatChange != nil {
		t.Errorf("FormatChange = %+v; want nil when nothing was written", *ar.FormatChange)
	}
}
