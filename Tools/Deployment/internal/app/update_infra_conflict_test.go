package app_test

// update_infra_conflict_test.go verifies how the update-flow declaration refresh behaves when
// the orchestrator is locally modified (its bytes no longer match the manifest): overwrite,
// backup and skip decisions, and the TODO entry recorded when a skip leaves declarations stale.

import (
	"path/filepath"
	"strings"
	"testing"

	"mosaic-deploy/internal/app/interactiontest"
	"mosaic-deploy/internal/domain"
)

func modifiedStaleConfig(decision domain.ConflictDecision) (updateRefreshConfig, string) {
	files := refreshFiles(injSection(injSecurityBlock("1.0")))
	return updateRefreshConfig{
		files:           files,
		modified:        map[string]bool{injOrchestratorPath: true},
		conflictDefault: decision,
	}, files[injOrchestratorPath]
}

func TestUpdate_ModifiedStaleOrchestratorOverwrite_RefreshesDeclarations(t *testing.T) {
	cfg, _ := modifiedStaleConfig(domain.DecisionOverwrite)

	run := runUpdateRefresh(t, cfg)

	requireUpdateOK(t, run)
	if got, want := refreshInfraRegion(t, run, injOrchestratorPath), injSection(injSecurityBlock("1.1")); got != want {
		t.Errorf("region after overwrite =\n%s\nwant\n%s", got, want)
	}
	if n := countConflictQuestions(run.stub); n != 0 {
		t.Errorf("%d local-modification question(s) asked despite a conflict default", n)
	}
}

func TestUpdate_ModifiedStaleOrchestratorBackup_WritesBackupAndRefreshes(t *testing.T) {
	cfg, original := modifiedStaleConfig(domain.DecisionBackupThenOverwrite)

	run := runUpdateRefresh(t, cfg)

	requireUpdateOK(t, run)
	if got, want := refreshInfraRegion(t, run, injOrchestratorPath), injSection(injSecurityBlock("1.1")); got != want {
		t.Errorf("region after backup+overwrite =\n%s\nwant\n%s", got, want)
	}
	backups, err := filepath.Glob(filepath.Join(run.workspace, ".mosaic", "backups", "orchestrator.md.*.bak"))
	if err != nil || len(backups) != 1 {
		t.Fatalf("backups = %v (err %v), want exactly one", backups, err)
	}
	rel, _ := filepath.Rel(run.workspace, backups[0])
	if saved, _ := run.readFile(t, rel); saved != original {
		t.Error("the backup does not hold the original bytes")
	}
}

func TestUpdate_ModifiedStaleOrchestratorSkip_LeavesFileAndRecordsManualStep(t *testing.T) {
	cfg, original := modifiedStaleConfig(domain.DecisionSkip)

	run := runUpdateRefresh(t, cfg)

	requireUpdateOK(t, run)
	assertUpdateSkipRecorded(t, run, original)
}

func TestUpdate_ModifiedStaleOrchestratorInteractiveSkip_LeavesFileAndRecordsManualStep(t *testing.T) {
	cfg, original := modifiedStaleConfig("")
	cfg.stub = interactiontest.NewBuilder().
		AnswerSelectOne(domain.QLocalModification, injOrchestratorPath, string(domain.DecisionSkip)).
		AnswerReview(true).Build()

	run := runUpdateRefresh(t, cfg)

	requireUpdateOK(t, run)
	if n := run.stub.CountAsked(domain.QLocalModification, injOrchestratorPath); n != 1 {
		t.Errorf("local-modification question asked %d times, want 1", n)
	}
	assertUpdateSkipRecorded(t, run, original)
}

func TestUpdate_ModifiedStaleOrchestratorInteractiveOverwrite_Refreshes(t *testing.T) {
	cfg, _ := modifiedStaleConfig("")
	cfg.stub = interactiontest.NewBuilder().
		AnswerSelectOne(domain.QLocalModification, injOrchestratorPath, string(domain.DecisionOverwrite)).
		AnswerReview(true).Build()

	run := runUpdateRefresh(t, cfg)

	requireUpdateOK(t, run)
	if got, want := refreshInfraRegion(t, run, injOrchestratorPath), injSection(injSecurityBlock("1.1")); got != want {
		t.Errorf("region after interactive overwrite =\n%s\nwant\n%s", got, want)
	}
}

func TestUpdate_ModifiedOrchestratorWithCurrentDeclarationsSkip_RecordsNoDeclarationManualStep(t *testing.T) {
	files := refreshFiles(injSection(injSecurityBlock("1.1")))
	cfg := updateRefreshConfig{
		files:           files,
		modified:        map[string]bool{injOrchestratorPath: true},
		conflictDefault: domain.DecisionSkip,
	}

	run := runUpdateRefresh(t, cfg)

	requireUpdateOK(t, run)
	for _, g := range run.todo.gaps {
		if g.Kind == domain.GapManualStep && strings.Contains(g.Detail, injSecurityKey) {
			t.Errorf("manual step names a key whose declaration was already current: %q", g.Detail)
		}
	}
}

// assertUpdateSkipRecorded checks that the orchestrator is byte-identical and that a
// manual-step entry names the target path and the stale key.
func assertUpdateSkipRecorded(t *testing.T, run *injectionRun, original string) {
	t.Helper()
	if got, _ := run.readFile(t, injOrchestratorPath); got != original {
		t.Error("a skipped orchestrator was modified")
	}
	for _, g := range run.todo.gaps {
		if g.Kind != domain.GapManualStep {
			continue
		}
		for _, want := range []string{injOrchestratorPath, injSecurityKey} {
			if !strings.Contains(g.Detail, want) {
				t.Errorf("manual-step detail does not mention %q: %q", want, g.Detail)
			}
		}
		if !strings.Contains(g.Fragment, "InfrastructureAgent:"+injSecurityKey) {
			t.Errorf("manual-step fragment lacks the refreshed declaration: %q", g.Fragment)
		}
		return
	}
	t.Errorf("no manual-step TODO entry recorded for the skipped orchestrator; gaps = %+v", run.todo.gaps)
}
