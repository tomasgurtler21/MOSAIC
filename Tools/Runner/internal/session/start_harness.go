package session

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	commonharness "mosaic-common/harness"
	"mosaic-run/internal/agentresolve"
	"mosaic-run/internal/domain"
	"mosaic-run/internal/engine"
	"mosaic-run/internal/orchfile"
	"mosaic-run/internal/planstages"
	"mosaic-run/internal/seed"
	"mosaic-run/internal/snapshot"
	"mosaic-run/internal/snapshot/lockprotocol"
	"mosaic-run/internal/snapshot/transform"
)

// setupSnapshotOrBackup handles step 5b of the run-start sequence: select
// and execute the snapshot or backup-and-transform strategy for CLI harnesses.
// Non-CLI harnesses skip this step entirely.
//
// On success, s.snapshotDir and/or s.backupState are set (as applicable).
// The caller (Start) is responsible for registering the corresponding defers
// immediately after this call returns without done=true.
func (s *sessionImpl) setupSnapshotOrBackup(ctx context.Context, rs *runStartCtx) (domain.RunOutcome, bool, error) {
	entry, ok := commonharness.LookupCLIHarness(rs.config.HarnessID)
	if !ok {
		return domain.RunOutcome{}, false, nil
	}

	switch entry.LoadingMechanism {
	case commonharness.LoadingMechanismPath:
		return s.setupPathSnapshot(ctx, rs)
	case commonharness.LoadingMechanismName:
		return s.setupNameBackup(ctx, rs)
	default:
		return s.refusal(fmt.Sprintf(
			"harness %q has unset or unrecognized loading mechanism (%s); catalog misconfiguration",
			rs.config.HarnessID, entry.LoadingMechanism,
		)), true, nil
	}
}

// setupPathSnapshot implements the copy-and-invoke strategy for
// LoadingMechanismPath harnesses (e.g. claude-code).
func (s *sessionImpl) setupPathSnapshot(ctx context.Context, rs *runStartCtx) (domain.RunOutcome, bool, error) {
	snapshotDir := filepath.Join(filepath.Dir(rs.orchDir), filepath.Base(rs.orchDir)+"-runner-"+rs.config.RunID)
	rules := transform.TransformationsFor(rs.config.HarnessID)
	if err := snapshot.NewSnapshot(rs.orchDir, snapshotDir, rules); err != nil {
		return s.refusal(err.Error()), true, nil
	}
	s.snapshotDir = snapshotDir

	// Re-resolve all agents against the snapshot directory.
	agents, err := agentresolve.ResolveAll(snapshotDir, rs.identifiers)
	if err != nil {
		return s.refusal(err.Error()), true, nil
	}
	rs.agents = agents

	// Re-resolve the orchestrator against the snapshot directory.
	snapshotOrchPath := filepath.Join(snapshotDir, filepath.Base(rs.config.OrchestratorFilePath))
	orchRef, err := agentresolve.ResolveOrchestrator(snapshotOrchPath)
	if err != nil {
		return s.refusal(err.Error()), true, nil
	}
	s.orchRef = orchRef

	// Re-bind consultants with the snapshot-resolved orchestrator reference.
	rc := domain.RunContext{Orchestrator: orchRef, Table: rs.table}
	bindRunContext(s.deps.Routing, rc)
	bindRunContext(s.deps.Manual, rc)
	bindRunContext(s.deps.PreConsult, rc)
	return domain.RunOutcome{}, false, nil
}

// setupNameBackup implements the backup-and-transform strategy for
// LoadingMechanismName harnesses (e.g. opencode, ghcp-cli).
func (s *sessionImpl) setupNameBackup(ctx context.Context, rs *runStartCtx) (domain.RunOutcome, bool, error) {
	rules := transform.TransformationsFor(rs.config.HarnessID)
	bs, bsErr := lockprotocol.SetupBackupAndTransform(rs.orchDir, rs.config.RunID, rules, s.deps.Debug)
	if bsErr != nil {
		return s.refusal(bsErr.Error()), true, nil
	}
	if bs != nil {
		s.backupState = bs
		if s.deps.BackupStateHook != nil {
			s.deps.BackupStateHook(bs)
		}
	}
	return domain.RunOutcome{}, false, nil
}

// setupInfraAndStages handles steps 6 through 7.5b of the run-start sequence:
// read the stage set (resumed runs), enumerate and filter declared infrastructure
// agents, validate class selections, read run settings from the artifact on
// resume, and validate checkpoints, mode, and commits.
func (s *sessionImpl) setupInfraAndStages(ctx context.Context, rs *runStartCtx) (domain.RunOutcome, bool, error) {
	// Step 6: read stage set (resumed runs; new runs read after Store.Create).
	rs.planPath = filepath.Join(rs.config.RunFolder, "Plan.md")
	if !rs.config.IsNewRun {
		if _, statErr := os.Stat(rs.planPath); statErr == nil {
			ss, stageErr := planstages.ReadStages(rs.planPath, rs.admitted.GroupsDeclared)
			if stageErr != nil {
				return s.refusal(stageErr.Error()), true, nil
			}
			rs.stages = &ss
		}
	}

	// Step 6b: enumerate declared infrastructure agents.
	declaredInfraAgents, err := orchfile.EnumerateInfrastructureAgents(rs.config.OrchestratorFilePath)
	if err != nil {
		return s.refusal(err.Error()), true, nil
	}

	// Step 6b2: apply infrastructure filter.
	if rs.config.InfrastructureFilter != nil {
		declaredInfraAgents = s.applyInfraFilter(rs.config.InfrastructureFilter, declaredInfraAgents)
	}

	// Step 6c: on resume, settle the run settings against the artifact. Values
	// the frontend supplied are compared with the recorded ones; a native-created
	// artifact without runner settings takes the supplied ones (recorded below).
	adoptRunnerSettings := false
	if !rs.config.IsNewRun {
		effective, adopt, err := domain.ReconcileResumeSettings(rs.existingState.RunSettings, rs.config.RunSettings, rs.config.Supplied)
		if err != nil {
			return s.refusal(err.Error()), true, nil
		}
		rs.config.RunSettings = effective
		adoptRunnerSettings = adopt
	}

	// Step 6d: validate per-class agent selection against the effective map
	// (the caller's for a new run, the persisted one for a resume).
	if err := domain.ValidateInfraSelections(declaredInfraAgents, rs.config.InfraClassSelections); err != nil {
		return s.refusal(err.Error()), true, nil
	}

	// Step 7: settle checkpoints.
	if rs.config.Checkpoints && !hasCheckpointClassAgent(declaredInfraAgents) {
		return s.refusal("checkpoints requested but no checkpoint provider is available"), true, nil
	}

	// Step 7.5a: mode is required.
	if rs.config.Mode == domain.ExecutionModeUnset {
		return s.refusal("mode is required; valid values: orchestrated, auto, auto-review"), true, nil
	}

	// Step 7.5b: commits validation.
	if rs.config.Commits {
		if !hasCommitClassAgent(declaredInfraAgents) {
			return s.refusal("commits enabled but no commit-class infrastructure agent is declared"), true, nil
		}
	}

	// Record adopted runner settings once, before any dispatch.
	if adoptRunnerSettings {
		adopted, err := s.deps.Store.AdoptRunnerSettings(ctx, rs.config.Mode, rs.config.PreConsultation, rs.config.ManualResolution, s.deps.Clock.Now())
		if err != nil {
			return domain.RunOutcome{Status: domain.RunFailed, Message: err.Error()}, true, err
		}
		rs.existingState = adopted
	}

	rs.declaredInfraAgents = declaredInfraAgents
	return domain.RunOutcome{}, false, nil
}

// applyInfraFilter applies the InfrastructureFilter from config to the
// declared infrastructure agents and returns the filtered slice. A debug
// event is emitted for each filter key that does not match any declared agent.
func (s *sessionImpl) applyInfraFilter(filter []string, declared []domain.DeclaredInfraAgent) []domain.DeclaredInfraAgent {
	filterSet := make(map[string]bool, len(filter))
	for _, key := range filter {
		filterSet[key] = true
	}
	for _, key := range filter {
		matched := false
		for _, agent := range declared {
			if agent.Name == key {
				matched = true
				break
			}
		}
		if !matched {
			s.deps.Debug.Log(domain.EventSessionFilterUnmatched,
				"infrastructure filter key does not match any declared agent",
				domain.F("key", key),
			)
		}
	}
	filtered := make([]domain.DeclaredInfraAgent, 0, len(declared))
	for _, agent := range declared {
		if filterSet[agent.Name] {
			filtered = append(filtered, agent)
		}
	}
	return filtered
}

// createOrResumeArtifact handles steps 7a through 8 of the run-start
// sequence: build and validate the seed plan, create or resume the run
// artifact, and dispatch commit setup when commits are enabled and no
// commit_branch is recorded yet.
//
// The artifact always exists before commit setup is dispatched, so a failed
// setup leaves the artifact and the setup row behind and a resume retries it.
func (s *sessionImpl) createOrResumeArtifact(ctx context.Context, rs *runStartCtx) (domain.RunOutcome, bool, error) {
	// Step 7a: build and validate the seed plan.
	if rs.config.IsNewRun && len(rs.config.SeedInputs) > 0 {
		p, seedErr := seed.NewPlan(rs.config.SeedInputs)
		if seedErr != nil {
			return s.refusal(seedErr.Error()), true, nil
		}
		rs.seedPlan = p
	}

	// Step 7.9: resolve the commit setup agent before anything is written, so
	// an unresolvable agent is a refusal that leaves no artifact.
	var setupAgent domain.AgentReference
	if rs.config.Commits && (rs.config.IsNewRun || needsCommitSetup(rs.existingState)) {
		ref, refusalMsg := resolveCommitSetupAgent(rs.declaredInfraAgents, rs.orchDir)
		if refusalMsg != "" {
			return s.refusal(refusalMsg), true, nil
		}
		setupAgent = ref
	}

	if rs.config.IsNewRun {
		if outcome, done, err := s.createNewRunArtifact(ctx, rs); done {
			return outcome, done, err
		}
	} else {
		rs.state = rs.existingState
	}

	// Step 8b: commit setup (new runs with commits enabled, and resumed runs
	// with commits enabled and no commit_branch).
	if rs.config.Commits && needsCommitSetup(rs.state) {
		state, outcome, done, err := s.runCommitSetup(ctx, setupAgent, rs.state, rs.config.RunID)
		rs.state = state
		if done {
			return outcome, true, err
		}
		rs.config.RunSettings.CommitBranch = state.CommitBranch
	}

	if rs.config.IsNewRun {
		rs.seq = rs.state.GlobalSequence
		return domain.RunOutcome{}, false, nil
	}
	rs.existingState = rs.state
	return s.resumeRunArtifact(ctx, rs)
}

// createNewRunArtifact creates the run artifact and applies seed inputs.
// Called by createOrResumeArtifact on the new-run path.
func (s *sessionImpl) createNewRunArtifact(ctx context.Context, rs *runStartCtx) (domain.RunOutcome, bool, error) {
	state, err := s.deps.Store.Create(ctx, rs.region.Info, rs.config.Task, rs.config.RunSettings, s.deps.Clock.Now(), rs.config.RunID)
	if err != nil {
		return domain.RunOutcome{Status: domain.RunFailed, Message: err.Error()}, true, err
	}

	// Apply the seed plan. On failure, remove the run folder so no trace remains.
	if applyErr := seed.Apply(rs.seedPlan, rs.config.RunFolder); applyErr != nil {
		if rmErr := os.RemoveAll(rs.config.RunFolder); rmErr != nil {
			return s.refusal(fmt.Sprintf("%s; additionally, removing the run folder %s failed: %v",
				applyErr.Error(), rs.config.RunFolder, rmErr)), true, nil
		}
		return s.refusal(applyErr.Error()), true, nil
	}

	// Step 8a2: read stage set now that the run folder and seed inputs are in place.
	if _, statErr := os.Stat(rs.planPath); statErr == nil {
		ss, stageErr := planstages.ReadStages(rs.planPath, rs.admitted.GroupsDeclared)
		if stageErr != nil {
			if rmErr := os.RemoveAll(rs.config.RunFolder); rmErr != nil {
				return s.refusal(fmt.Sprintf("%s; additionally, removing the run folder %s failed: %v",
					stageErr.Error(), rs.config.RunFolder, rmErr)), true, nil
			}
			return s.refusal(stageErr.Error()), true, nil
		}
		rs.stages = &ss
	}

	rs.state = state
	return domain.RunOutcome{}, false, nil
}

// resumeRunArtifact resumes a run from its existing artifact (rs.existingState,
// which already includes any retried commit setup row).
// Called by createOrResumeArtifact on the resume path.
func (s *sessionImpl) resumeRunArtifact(ctx context.Context, rs *runStartCtx) (domain.RunOutcome, bool, error) {
	state := rs.existingState
	resume, resumeErr := engine.ResumePoint(rs.admitted, rs.stages, state, domain.NewInfraAgentSet(rs.declaredInfraAgents, s.orchRef.Identifier))
	if resumeErr != nil {
		return domain.RunOutcome{Status: domain.RunFailed, Message: resumeErr.Error()}, true, resumeErr
	}
	// Sequences already allocated to infrastructure rows (a commit setup row
	// from an earlier attempt) are never reused, even when no workflow step
	// has run yet and the resume point reports a fresh start.
	rs.seq = max(resume.Seq, state.GlobalSequence)

	if resume.RerunLast {
		state = rewindStateForRerun(rs.admitted.Table, domain.NewInfraAgentSet(rs.declaredInfraAgents, s.orchRef.Identifier), state)
	}
	rs.state = state
	return domain.RunOutcome{}, false, nil
}

// applyOverridesAndPreConsult handles steps 8b and 8c of the run-start
// sequence: validate and apply infrastructure overrides, build the stage
// source descriptor, and perform the optional pre-consultation.
func (s *sessionImpl) applyOverridesAndPreConsult(ctx context.Context, rs *runStartCtx) (domain.RunOutcome, bool, error) {
	// Step 8b: validate and apply infrastructure overrides.
	updated, err := validateAndApplyOverrides(rs.state.InfrastructureOverrides, rs.declaredInfraAgents)
	if err != nil {
		return s.refusal(err.Error()), true, nil
	}
	rs.declaredInfraAgents = updated

	// Build the stage source descriptor.
	rs.stageSource = domain.StageSource{
		Path:   rs.planPath,
		Seeded: rs.seedPlan.HasDest("Plan.md"),
	}

	// Step 8c: pre-consultation (auto and auto-review modes only, when enabled).
	if !rs.config.PreConsultation {
		return domain.RunOutcome{}, false, nil
	}
	if rs.config.Mode != domain.ExecutionModeAuto && rs.config.Mode != domain.ExecutionModeAutoReview {
		return domain.RunOutcome{}, false, nil
	}
	advice, pcErr := s.deps.PreConsult.PreConsult(ctx, domain.ConsultationRequest{
		Context:               domain.ConsultContextPreConsultation,
		OrchestrationArtifact: filepath.Join(rs.config.RunFolder, "Orchestration.md"),
	})
	if pcErr != nil {
		// The run folder, artifact and any setup row are kept; pre-consultation
		// writes nothing to the artifact, and a resume retries it.
		return s.startFailed("pre-consultation failed: "+pcErr.Error(), pcErr), true, nil
	}
	rs.preConsultAdvice = advice
	return domain.RunOutcome{}, false, nil
}
