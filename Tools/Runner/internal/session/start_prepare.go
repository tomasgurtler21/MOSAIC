package session

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	commonharness "mosaic-common/harness"
	"mosaic-run/internal/agentresolve"
	"mosaic-run/internal/compat"
	"mosaic-run/internal/domain"
	"mosaic-run/internal/orchfile"
	"mosaic-run/internal/seed"
	"mosaic-run/internal/snapshot/lockprotocol"
	"mosaic-run/internal/workflow"
)

// runStartCtx holds all state built up during the run-start sequence
// (Steps 1-8c) and consumed by the dispatch loop. The config field is
// mutable: step 6d overwrites RunSettings and step 7.9 overwrites
// CommitBranch, so callers must use rs.config rather than the original
// domain.RunConfig value after this struct is created.
type runStartCtx struct {
	// Run configuration; mutable throughout the start sequence.
	config domain.RunConfig
	// Workflow context resolved at steps 1-2.
	region   domain.WorkflowRegion
	table    domain.RoutingTable
	admitted domain.AdmittedWorkflow
	orchDir  string
	// Artifact state from the store (set at step 3 for resume, created at step 8).
	existingState domain.ArtifactState
	// Agent resolution results (set at step 5; updated at step 5b for path-based harnesses).
	agents      map[string]domain.AgentReference
	identifiers []string
	// Infrastructure agents (set at step 6b; filtered at step 6b2).
	declaredInfraAgents []domain.DeclaredInfraAgent
	// Stage set (nil if no Plan.md present; updated on resume at step 6
	// and on new-run at step 8a2).
	stages   *domain.StageSet
	planPath string
	// Run artifact state (created/resumed at step 8; updated by dispatch loop).
	state domain.ArtifactState
	seq   int
	// Dispatch loop shared mutable state.
	lastResponse        *domain.ProtocolResponse
	prevWorkflowStep    *domain.CompletedStep
	refreshedStages     *domain.StageSet
	lastOutputArtifacts []string
	antiLoop            antiLoopState
	// Pre-consultation result (step 8c).
	preConsultAdvice domain.PreConsultationAdvice
	// Seed inputs applied at step 8 (new-run only).
	seedPlan seed.Plan
	// Commit setup record held in memory until artifact is created (step 7.9).
	commitSetup *commitSetupRecord
	// Stage source descriptor for engine error messages.
	stageSource domain.StageSource
}

// prepareRouting handles steps 1 through 2c of the run-start sequence:
// load the orchestrator file, parse the routing table, resolve the
// orchestrator agent reference, bind the run context to all consultants,
// and refuse if the approval reader cannot handle a HITL workflow.
//
// On a refuse-or-error result, done is true and outcome/err carry the
// values to return from Start. On success, done is false and the returned
// *runStartCtx is ready for the next step.
func (s *sessionImpl) prepareRouting(ctx context.Context, config domain.RunConfig) (*runStartCtx, domain.RunOutcome, bool, error) {
	// Step 1: load orchestrator file and get the selected workflow region.
	if !config.IsNewRun && config.WorkflowID == "" {
		return nil, s.resumeRecordedNoWorkflowRefusal(config.RunID), true, nil
	}
	region, err := orchfile.GetWorkflow(config.OrchestratorFilePath, string(config.WorkflowID))
	if err != nil {
		if !config.IsNewRun && isWorkflowNotFound(err, string(config.WorkflowID)) {
			return nil, s.resumeWorkflowGoneRefusal(config.RunID, string(config.WorkflowID)), true, nil
		}
		return nil, s.refusal(err.Error()), true, nil
	}

	// Step 2: parse the routing table.
	table, err := workflow.Parse(region.Content, region.Info)
	if err != nil {
		return nil, s.refusal(err.Error()), true, nil
	}

	// Step 2a: resolve the orchestrator agent reference.
	orchRef, err := agentresolve.ResolveOrchestrator(config.OrchestratorFilePath)
	if err != nil {
		return nil, s.refusal(err.Error()), true, nil
	}
	s.orchRef = orchRef

	// Step 2b: bind the run context to all consultants.
	rc := domain.RunContext{Orchestrator: orchRef, Table: table}
	bindRunContext(s.deps.Routing, rc)
	bindRunContext(s.deps.Manual, rc)
	bindRunContext(s.deps.PreConsult, rc)

	// Step 2c: refuse if the approval reader cannot handle a HITL workflow.
	if ac, ok := s.deps.Approvals.(domain.ApprovalCapability); ok && !ac.ApprovalsReadable() {
		for _, row := range table.Rows {
			if row.HITL {
				return nil, s.refusal(fmt.Sprintf(
					"cannot start run: this surface cannot read approvals "+
						"but workflow %q declares human-review rows; "+
						"supply an approval reader to run a HITL workflow",
					config.WorkflowID,
				)), true, nil
			}
		}
	}

	rs := &runStartCtx{
		config: config,
		region: region,
		table:  table,
	}
	rs.antiLoop = antiLoopState{rowIndex: -1}
	return rs, domain.RunOutcome{}, false, nil
}

// readArtifact handles step 3 of the run-start sequence: read the existing
// artifact, enforce the IsNewRun contract, and perform the FR-7b version check.
func (s *sessionImpl) readArtifact(ctx context.Context, rs *runStartCtx) (domain.RunOutcome, bool, error) {
	existingState, readErr := s.deps.Store.Read(ctx)
	if readErr != nil {
		var refErr *domain.RefusalError
		if errors.As(readErr, &refErr) {
			return s.refusal(refErr.Error()), true, nil
		}
		if !errors.Is(readErr, os.ErrNotExist) {
			return domain.RunOutcome{Status: domain.RunFailed, Message: readErr.Error()}, true, readErr
		}
	}

	// IsNewRun contract.
	if rs.config.IsNewRun {
		if readErr == nil {
			refusalMsg := "run folder already contains an artifact; cannot create a new run here"
			if rs.config.RunFolder != "" {
				refusalMsg = "run folder already contains an artifact at " +
					filepath.Join(rs.config.RunFolder, "Orchestration.md") +
					"; cannot create a new run here"
			}
			return s.refusal(refusalMsg), true, nil
		}
	} else {
		if errors.Is(readErr, os.ErrNotExist) {
			return s.refusal("no artifact found at the resolved run folder; cannot resume"), true, nil
		}
	}

	// FR-7b: version check for resume mode.
	if !rs.config.IsNewRun && !rs.config.AllowVersionDrift {
		if existingState.WorkflowVersion != rs.region.Info.Version {
			return s.refusal(fmt.Sprintf(
				"workflow version mismatch: artifact has %q, selected workflow has %q",
				existingState.WorkflowVersion, rs.region.Info.Version,
			)), true, nil
		}
	}

	rs.existingState = existingState
	return domain.RunOutcome{}, false, nil
}

// admitAndResolveAgents handles steps 4 through 5 of the run-start sequence:
// admit the workflow (FR-18a compat checks), run the recovery check for CLI
// harnesses, and resolve all agent identifiers to definition files.
func (s *sessionImpl) admitAndResolveAgents(ctx context.Context, rs *runStartCtx) (domain.RunOutcome, bool, error) {
	// Step 4: admit the workflow.
	admitted, err := compat.Admit(rs.table)
	if err != nil {
		return s.refusal(err.Error()), true, nil
	}
	rs.orchDir = filepath.Dir(rs.config.OrchestratorFilePath)
	rs.admitted = admitted

	// Step 4b: recovery check (CLI harnesses only).
	if commonharness.IsCLIHarness(rs.config.HarnessID) {
		if rcErr := lockprotocol.RecoveryCheck(rs.orchDir, s.deps.Debug); rcErr != nil {
			return s.refusal(rcErr.Error()), true, nil
		}
	}

	// Step 5: resolve all agent identifiers.
	identifiers := uniqueAgentIdentifiers(rs.table)
	agents, err := agentresolve.ResolveAll(rs.orchDir, identifiers)
	if err != nil {
		return s.refusal(err.Error()), true, nil
	}
	rs.identifiers = identifiers
	rs.agents = agents
	return domain.RunOutcome{}, false, nil
}
