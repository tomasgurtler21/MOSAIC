package app_test

// deployagents_infra_conflict_test.go verifies how declaration injection into an
// orchestrator-role file behaves when that file is a conflict (no manifest record, so the
// tool cannot confirm it wrote the file): overwrite, backup and skip decisions from
// ConflictDefault and from the interactive question, and the TODO entry recorded on skip.

import (
	"path/filepath"
	"strings"
	"testing"

	"mosaic-deploy/internal/app/interactiontest"
	"mosaic-deploy/internal/domain"
)

// conflictConfig seeds one orchestrator with no manifest, forcing a conflict, and selects
// both infrastructure agents.
func conflictConfig(original string, decision domain.ConflictDecision) injectionConfig {
	return injectionConfig{
		files:           map[string]string{injOrchestratorPath: original},
		infraIDs:        []string{injSecurityKey, injCheckpointKey},
		conflictDefault: decision,
	}
}

func TestDeployAgents_ModifiedOrchestratorOverwriteDefault_WritesDeclarationsAndKeepsWorkflows(t *testing.T) {
	original := injDeployedOrchestrator("", injWorkflowInner)

	run := runInfraInjection(t, conflictConfig(original, domain.DecisionOverwrite))

	requireRunOK(t, run)
	got, _ := run.readFile(t, injOrchestratorPath)
	region := injRegion(t, got, "InfrastructureAgents")
	for _, block := range []string{injSection(injSecurityBlock("1.1")), injSection(injCheckpointBlock("1.0"))} {
		if !strings.Contains(region, block) {
			t.Errorf("declaration missing after overwrite:\n%s", block)
		}
	}
	if w := injRegion(t, got, "AvailableWorkflows"); w != injWorkflowInner {
		t.Errorf("workflows changed on overwrite:\n%s", w)
	}
	if n := countConflictQuestions(run.stub); n != 0 {
		t.Errorf("%d local-modification question(s) asked despite a conflict default", n)
	}
}

func TestDeployAgents_ModifiedOrchestratorBackupDefault_WritesBackupAndDeclarations(t *testing.T) {
	original := injDeployedOrchestrator("", injWorkflowInner)

	run := runInfraInjection(t, conflictConfig(original, domain.DecisionBackupThenOverwrite))

	requireRunOK(t, run)
	got, _ := run.readFile(t, injOrchestratorPath)
	if !strings.Contains(injRegion(t, got, "InfrastructureAgents"), injSection(injSecurityBlock("1.1"))) {
		t.Error("declarations were not written after backup")
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

func TestDeployAgents_ModifiedOrchestratorSkipDefault_LeavesFileAndRecordsManualStep(t *testing.T) {
	original := injDeployedOrchestrator("", injWorkflowInner)

	run := runInfraInjection(t, conflictConfig(original, domain.DecisionSkip))

	requireRunOK(t, run)
	assertSkippedWithManualStep(t, run, original)
}

func TestDeployAgents_ModifiedOrchestratorInteractiveOverwrite_AsksOnceAndWritesDeclarations(t *testing.T) {
	original := injDeployedOrchestrator("", injWorkflowInner)
	cfg := conflictConfig(original, "")
	cfg.stub = interactiontest.NewBuilder().
		AnswerSelectOne(domain.QLocalModification, injOrchestratorPath, string(domain.DecisionOverwrite)).
		AnswerReview(true).Build()

	run := runInfraInjection(t, cfg)

	requireRunOK(t, run)
	if n := run.stub.CountAsked(domain.QLocalModification, injOrchestratorPath); n != 1 {
		t.Errorf("local-modification question asked %d times for the orchestrator, want 1", n)
	}
	got, _ := run.readFile(t, injOrchestratorPath)
	if !strings.Contains(injRegion(t, got, "InfrastructureAgents"), injSection(injSecurityBlock("1.1"))) {
		t.Error("declarations were not written after an interactive overwrite")
	}
}

func TestDeployAgents_ModifiedOrchestratorInteractiveSkip_LeavesFileAndRecordsManualStep(t *testing.T) {
	original := injDeployedOrchestrator("", injWorkflowInner)
	cfg := conflictConfig(original, "")
	cfg.stub = interactiontest.NewBuilder().
		AnswerSelectOne(domain.QLocalModification, injOrchestratorPath, string(domain.DecisionSkip)).
		AnswerReview(true).Build()

	run := runInfraInjection(t, cfg)

	requireRunOK(t, run)
	assertSkippedWithManualStep(t, run, original)
}

func TestDeployAgents_OrchestratorAlreadyCurrent_NoConflictQuestionEvenWithoutManifest(t *testing.T) {
	// Without a manifest the file would classify as a conflict, but with nothing to declare it
	// must not be admitted, so the user is never asked about it.
	original := injDeployedOrchestrator(
		injSection(injSecurityBlock("1.1"))+injSection(injCheckpointBlock("1.0")), injWorkflowInner)

	run := runInfraInjection(t, conflictConfig(original, ""))

	requireRunOK(t, run)
	if run.stub.WasAsked(domain.QLocalModification, injOrchestratorPath) {
		t.Error("the user was asked about an orchestrator whose declarations are already current")
	}
	if got, _ := run.readFile(t, injOrchestratorPath); got != original {
		t.Error("orchestrator.md was rewritten although its declarations were already current")
	}
}

// assertSkippedWithManualStep checks that the orchestrator is byte-identical and that a
// manual-step entry names the target path and every infrastructure key.
func assertSkippedWithManualStep(t *testing.T, run *injectionRun, original string) {
	t.Helper()
	if got, _ := run.readFile(t, injOrchestratorPath); got != original {
		t.Error("a skipped orchestrator was modified")
	}
	for _, g := range run.todo.gaps {
		if g.Kind != domain.GapManualStep {
			continue
		}
		for _, want := range []string{injOrchestratorPath, injSecurityKey, injCheckpointKey} {
			if !strings.Contains(g.Detail, want) {
				t.Errorf("manual-step detail does not mention %q: %q", want, g.Detail)
			}
		}
		if !strings.Contains(g.Fragment, "InfrastructureAgent:"+injSecurityKey) {
			t.Errorf("manual-step fragment lacks the declaration to paste: %q", g.Fragment)
		}
		return
	}
	t.Errorf("no manual-step TODO entry recorded for the skipped orchestrator; gaps = %+v", run.todo.gaps)
}
