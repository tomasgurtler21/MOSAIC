// Package session implements the use-case loop that connects the pure engine
// to the outside world. It drives the complete run lifecycle:
//
//  1. Run-start sequence (in fixed order):
//     a. Load orchestrator file and enumerate workflow regions (orchfile)
//     b. Parse the selected workflow routing table (workflow)
//     c. Read existing artifact or detect new run (artifact store)
//     d. Admit the workflow (compat)
//     e. Resolve every agent identifier (agentresolve)
//     f. Read stage set if a staged phase is present (planstages)
//     g. Settle checkpoint value (FR-9 refusal if no provider)
//     h. Create or resume the artifact
//     i. Pre-consultation (auto and auto-review modes, when enabled)
//
//  2. Dispatch loop: ask engine → dispatch via harness → apply to artifact → repeat.
//
//  3. Special cases in the dispatch loop:
//     - Mode-driven routing: in ExecutionModeOrchestrated, every routing choice
//       is delegated to the RoutingConsultant (engine returns Consult). In auto
//       and auto-review modes, the engine decides; the consultant is only invoked
//       on Deviation decisions or HITL escalations.
//     - On Findings auto-routing: engine returns Dispatch for COMPLETED_NEEDS_ACTION
//       with an unambiguous OnFindings hint; session treats it identically to any
//       other Dispatch (harness → artifact update). No consultation is needed.
//     - Stage-* output re-derivation: after a completed row's output artifacts
//       include a Stage-* pattern, session re-reads the plan artifact via
//       planstages to obtain a refreshed stage set for the engine's next call.
//     - Deviation: engine returns Deviation; session routes through the
//       RoutingConsultant (consultRoute). If no consultant is wired, the run
//       terminates with RunDeviationUnresolved.
//     - Harness error: a harness-level failure is treated as a Deviation with
//       kind DeviationHarnessError ("never a crash" per port contract). Routed
//       through the RoutingConsultant the same way as engine-originated deviations.
//     - HITL verification: after each auto-routed SUCCESS, output artifacts are
//       checked for human_approved. A non-compliant result triggers one HITL
//       redispatch; if the redispatch is also non-compliant, the deviation is
//       escalated to the RoutingConsultant.
//     - Consultation recording: every RoutingConsultant invocation is recorded
//       as an infrastructure-flagged CompletedStep under "{orchestrator-stem}#{seq}"
//       (where orchestrator-stem is the file stem of the orchestrator file supplied
//       to the run), consuming global_sequence without moving current_state.
//     - Graceful stop: session records current state and returns RunStopped.
//     - Infrastructure-agent trigger: a named no-op hook is called after each
//       harness invocation (FR-40).
//
// Both frontends (TUI and CLI) drive the same Session surface.
package session

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"mosaic-common/interaction"
	commonharness "mosaic-common/harness"
	"mosaic-run/internal/agentresolve"
	"mosaic-run/internal/compat"
	"mosaic-run/internal/domain"
	"mosaic-run/internal/engine"
	"mosaic-run/internal/orchfile"
	"mosaic-run/internal/planstages"
	"mosaic-run/internal/seed"
	"mosaic-run/internal/snapshot"
	"mosaic-run/internal/workflow"
)

// Session is the use-case layer that drives the execution loop.
// Both frontends (TUI and CLI) drive it through this surface.
type Session interface {
	// Start begins or resumes a run. It performs the full run-start sequence
	// and then enters the dispatch loop.
	//
	// Progress is reported through the Interaction port's Progress and Notify
	// methods.
	//
	// Returns the run outcome when the run completes, stops, or is refused.
	// Refusals (pre-invocation failures) are returned as RunOutcome{Status:
	// RunRefused} with a nil error. Unexpected infrastructure failures return
	// a non-nil error.
	Start(ctx context.Context, config domain.RunConfig) (domain.RunOutcome, error)
}

// Deps collects all port dependencies required by the session.
// Every field is a port (interface); no concrete types are used.
type Deps struct {
	// Harness dispatches subagent invocations.
	Harness domain.HarnessAdapter
	// Store reads and writes the Orchestration.md artifact.
	Store domain.ArtifactStore
	// Clock provides deterministic timestamps.
	Clock domain.Clock
	// Interact provides the user-interaction channel (progress events, etc.).
	Interact domain.Interaction
	// PreConsult performs the one-shot run-start pre-consultation (auto and
	// auto-review modes only). Nil is permitted only when the run's
	// PreConsultation setting is disabled; calling it when nil panics.
	PreConsult domain.PreConsultant
	// OnInfrastructureTrigger is an optional hook called after each harness
	// invocation (FR-40). If nil, no action is taken. In production this is the
	// named no-op; tests inject a counter function to verify the hook is invoked
	// exactly once per dispatch cycle (AC8.7).
	OnInfrastructureTrigger func()

	// Debug records dispatch events to the run's debug log, including run-start
	// refusals and unresolved deviations that never reach Store.Apply and so
	// leave no trace in Orchestration.md.
	//
	// Optional: nil is normalised to domain.NopDebugLogger in New. Behaviour is
	// otherwise identical with and without a logger.
	Debug domain.DebugLogger

	// DispatchLog records the full ProtocolRequest/ProtocolResponse pairs for
	// every subagent invocation to a dedicated JSONL log file.
	//
	// Optional: nil is normalised to domain.NopDispatchLogger in New, so the
	// dispatch loop never nil-checks it.
	DispatchLog domain.DispatchLogger

	// Routing is the configured routing decision-maker consulted whenever the
	// engine yields a Consult or Deviation decision. In production this is the
	// OrchestratorConsultant; a run started with manual resolution still uses
	// it first and falls back to Manual on a consultation failure.
	//
	// Nil is permitted only when the run's mode does not require routing
	// consultations (i.e. auto/auto-review with no expected deviations in
	// tests). The session must not call it when nil.
	Routing domain.RoutingConsultant

	// Manual is the fallback resolver used when a consultation fails and the
	// run's ManualResolution setting is enabled. Nil when manual resolution is
	// disabled; the session must not call it in that case.
	Manual domain.RoutingConsultant

	// Approvals reads human_approved from dispatched output artifacts for HITL
	// compliance verification. Nil is normalised in New to a reader that
	// reports ApprovalUnreadable, so the session never nil-checks it.
	Approvals domain.ApprovalReader

	// StopRequested reports whether a graceful stop has been confirmed. The
	// dispatch loop polls it at safe boundaries (immediately before each
	// invokeAndLog call, never mid-invocation) and never inspects ctx for
	// this purpose -- ctx cancellation remains the separate, unchanged
	// hard-cancel (ctrl+c) path.
	//
	// Optional: nil is normalised in New to a function that always returns
	// false, so the dispatch loop never nil-checks it.
	StopRequested func() bool
}

// New creates a new Session with the given port dependencies.
//
// A nil Deps.Debug is replaced with domain.NopDebugLogger before the session
// is returned, so the dispatch loop never nil-checks the logger.
//
// The returned session uses the following fixed-path dependencies from the
// runner's package set: orchfile, workflow, compat, agentresolve, planstages,
// and engine. These are not behind ports because they are pure functions or
// read-only loaders that impose no testability burden of their own.
func New(deps Deps) Session {
	if deps.Debug == nil {
		deps.Debug = domain.NopDebugLogger{}
	}
	if deps.DispatchLog == nil {
		deps.DispatchLog = domain.NopDispatchLogger{}
	}
	if deps.Approvals == nil {
		deps.Approvals = unreadableApprovalReader{}
	}
	if deps.StopRequested == nil {
		deps.StopRequested = func() bool { return false }
	}
	return &sessionImpl{deps: deps}
}

// unreadableApprovalReader is the zero-value ApprovalReader used when
// Deps.Approvals is nil. It reports every artifact as ApprovalUnreadable, so
// the session never nil-checks the reader and a missing reader never silently
// passes the HITL gate.
//
// It also implements domain.ApprovalCapability, returning false, so the
// run-start refusal check can distinguish this stand-in from a real reader.
// maxConsecutiveSameAgentDispatches is the upper bound on consecutive
// dispatches of the same agent for the same workflow step. When the count
// reaches this value, the next would-be dispatch is blocked and the session
// escalates to the routing consultant instead.
const maxConsecutiveSameAgentDispatches = 4

// antiLoopState tracks consecutive same-agent dispatches for the current
// workflow step. All fields are updated by recordDispatch. The zero value is
// safe to use: rowIndex 0 is a valid row index so callers must initialize
// rowIndex to -1 to indicate "no step tracked yet".
type antiLoopState struct {
	rowIndex  int    // -1 = no step tracked yet
	count     int    // consecutive same-agent dispatch count for current step
	lastAgent string // identifier of the last dispatched agent
}

// recordDispatch updates the anti-loop state for a dispatch of agentID at
// rowIndex and reports whether the dispatch is allowed. A dispatch is blocked
// (returns false) only when the same agent at the same row has already been
// dispatched maxConsecutiveSameAgentDispatches times. Any change in row index
// or agent identifier resets the counter and always permits the dispatch.
func (a *antiLoopState) recordDispatch(rowIndex int, agentID string) bool {
	if rowIndex != a.rowIndex || agentID != a.lastAgent {
		// New step or different agent: reset counter.
		a.rowIndex = rowIndex
		a.lastAgent = agentID
		a.count = 1
		return true
	}
	if a.count >= maxConsecutiveSameAgentDispatches {
		return false // 5th (or more) consecutive same-agent dispatch for this step
	}
	a.count++
	return true
}

type unreadableApprovalReader struct{}

func (unreadableApprovalReader) ReadApproval(_ context.Context, _ string) domain.HumanApproval {
	return domain.ApprovalUnreadable
}

func (unreadableApprovalReader) ApprovalsReadable() bool { return false }

// sessionImpl is the concrete implementation of Session.
type sessionImpl struct {
	deps Deps
	// orchRef is the resolved orchestrator agent reference. It is assigned at
	// step 2a of Start and used when recording each consultation log row so
	// that the row's AgentInstance carries the real orchestrator identifier
	// (derived from the orchestrator file stem) rather than any hardcoded value.
	orchRef domain.AgentReference
	// manualDispatchPending tracks the one-shot ManualDispatch signal from the
	// stop-screen recovery action. Set to true at the start of each Start call
	// when config.ManualDispatch is true; cleared after the first consultRoute
	// call so that subsequent routing decisions use the configured consultant.
	manualDispatchPending bool
	// snapshotDir is the absolute path to the run-scoped agent snapshot
	// directory created at step 5a. Empty until step 5a succeeds. Used by
	// cleanup to know what to delete on terminal completion.
	snapshotDir string
}

// invokeAndLog wraps s.deps.Harness.Invoke with dispatch logging. It logs the
// full request immediately before the invocation and the full response (or a
// harness-level error) immediately after. The response and error are returned
// unchanged so callers need only change the method name.
func (s *sessionImpl) invokeAndLog(ctx context.Context, agentRef domain.AgentReference, request domain.ProtocolRequest) (domain.ProtocolResponse, error) {
	s.deps.DispatchLog.LogRequest(request)
	resp, err := s.deps.Harness.Invoke(ctx, agentRef, request)
	if err != nil {
		s.deps.DispatchLog.LogError(request.AgentInstanceID, err.Error())
	} else {
		s.deps.DispatchLog.LogResponse(resp)
	}
	return resp, err
}

// Start implements Session.
func (s *sessionImpl) Start(ctx context.Context, config domain.RunConfig) (outcome domain.RunOutcome, err error) {
	// =========================================================================
	// Run-start sequence (fixed order)
	// =========================================================================

	// Step 1: Load orchestrator file and get the selected workflow region.
	//
	// A resumed run is never asked for its workflow again, so an absent or
	// undeclared one is a condition to name rather than a selection to redo.
	// Both are refused by name, because they send the user to different
	// places: a workflow that has gone missing to the orchestrator file, a run
	// that carries none to its own artifact.
	if !config.IsNewRun && config.WorkflowID == "" {
		return s.resumeRecordedNoWorkflowRefusal(config.RunID), nil
	}
	region, err := orchfile.GetWorkflow(config.OrchestratorFilePath, string(config.WorkflowID))
	if err != nil {
		if !config.IsNewRun && isWorkflowNotFound(err, string(config.WorkflowID)) {
			return s.resumeWorkflowGoneRefusal(config.RunID, string(config.WorkflowID)), nil
		}
		return s.refusal(err.Error()), nil
	}

	// Step 2: Parse the routing table.
	table, err := workflow.Parse(region.Content, region.Info)
	if err != nil {
		return s.refusal(err.Error()), nil
	}

	// Step 2a: Resolve the orchestrator agent reference. This must happen before
	// artifact creation so an unresolvable orchestrator refuses the run without
	// leaving a run folder behind. The returned reference carries
	// InvocationOrchestrator so every consultation retains native subagent-spawning.
	orchRef, err := agentresolve.ResolveOrchestrator(config.OrchestratorFilePath)
	if err != nil {
		return s.refusal(err.Error()), nil
	}
	s.orchRef = orchRef

	// Step 2b: Bind the resolved orchestrator reference and routing table to every
	// consultant that accepts them. The binding happens before artifact creation and
	// before the first consultation, so no consultant is ever consulted with an empty
	// table. Binding is idempotent and protected by a type assertion and nil check
	// on each dep, so consultants that do not implement RunContextBinder are skipped.
	rc := domain.RunContext{Orchestrator: orchRef, Table: table}
	bindRunContext(s.deps.Routing, rc)
	bindRunContext(s.deps.Manual, rc)
	bindRunContext(s.deps.PreConsult, rc)

	// Step 2c: Refuse the run if the approval reader cannot read approvals and
	// the workflow declares at least one human-review row. Detecting this at
	// run start avoids spending a re-dispatch on every HITL row only to have
	// it fail closed, and surfaces a clear diagnosis rather than a
	// per-row deviation.
	if ac, ok := s.deps.Approvals.(domain.ApprovalCapability); ok && !ac.ApprovalsReadable() {
		for _, row := range table.Rows {
			if row.HITL {
				return s.refusal(fmt.Sprintf(
					"cannot start run: this surface cannot read approvals "+
						"but workflow %q declares human-review rows; "+
						"supply an approval reader to run a HITL workflow",
					config.WorkflowID,
				)), nil
			}
		}
	}

	// Step 3: Read existing artifact (FR-7a: refuse non-canonical; ErrNotExist = no artifact).
	existingState, readErr := s.deps.Store.Read(ctx)
	if readErr != nil {
		var refErr *domain.RefusalError
		if errors.As(readErr, &refErr) {
			// Non-canonical format: always refuse regardless of IsNewRun.
			return s.refusal(refErr.Error()), nil
		}
		if !errors.Is(readErr, os.ErrNotExist) {
			// Unexpected infrastructure error.
			return domain.RunOutcome{Status: domain.RunFailed, Message: readErr.Error()}, readErr
		}
		// os.ErrNotExist: no artifact at this location (expected for a new run).
	}

	// Apply IsNewRun contract: the CLI/TUI layer resolves run identity before calling
	// session, so the session only needs to know "create" vs "resume".
	if config.IsNewRun {
		// New run: refuse if an artifact already exists (race condition guard).
		if readErr == nil {
			refusalMsg := "run folder already contains an artifact; cannot create a new run here"
			if config.RunFolder != "" {
				refusalMsg = "run folder already contains an artifact at " +
					filepath.Join(config.RunFolder, "Orchestration.md") +
					"; cannot create a new run here"
			}
			return s.refusal(refusalMsg), nil
		}
		// Expected: os.ErrNotExist — proceed to create below.
	} else {
		// Resume: refuse if no artifact exists (stale scan guard).
		if errors.Is(readErr, os.ErrNotExist) {
			return s.refusal("no artifact found at the resolved run folder; cannot resume"), nil
		}
		// Artifact exists — proceed to resume below.
	}

	// FR-7b: version check for resume mode.
	if !config.IsNewRun && !config.AllowVersionDrift {
		if existingState.WorkflowVersion != region.Info.Version {
			return s.refusal(fmt.Sprintf(
				"workflow version mismatch: artifact has %q, selected workflow has %q",
				existingState.WorkflowVersion, region.Info.Version,
			)), nil
		}
	}

	// Step 4: Admit the workflow (FR-18a compat checks and group resolution).
	admitted, err := compat.Admit(table)
	if err != nil {
		return s.refusal(err.Error()), nil
	}

	// Step 5: Resolve every agent identifier to a definition file.
	orchDir := filepath.Dir(config.OrchestratorFilePath)
	identifiers := uniqueAgentIdentifiers(table)
	agents, err := agentresolve.ResolveAll(orchDir, identifiers)
	if err != nil {
		return s.refusal(err.Error()), nil
	}

	// Step 5a: Create a run-scoped agent snapshot (CLI harnesses only).
	// Skip for non-CLI harnesses (e.g. the "fake" test double) which have
	// no agents directory convention. For CLI harnesses, copy the agents
	// directory to a sibling snapshot directory, re-resolve all agents against
	// the snapshot, and re-bind consultants with the snapshot-resolved
	// orchestrator reference. On failure, refuse the run.
	if commonharness.IsCLIHarness(config.HarnessID) {
		// The snapshot directory is a sibling of the agents directory, named
		// by appending "-runner-{runID}" to the agents directory base name.
		snapshotDir := filepath.Join(filepath.Dir(orchDir), filepath.Base(orchDir)+"-runner-"+config.RunID)
		rules := snapshot.TransformationsFor(config.HarnessID)
		if err := snapshot.CreateSnapshot(orchDir, snapshotDir, rules); err != nil {
			return s.refusal(err.Error()), nil
		}
		s.snapshotDir = snapshotDir

		// Defer cleanup: delete the snapshot directory on terminal completion
		// (RunCompleted or RunStopped). Non-terminal outcomes leave the snapshot
		// in place so the run can be resumed with access to the original agent files.
		// The named return variable 'outcome' is read by the deferred function after
		// all return statements have set it.
		defer func() {
			if s.snapshotDir == "" {
				return
			}
			if outcome.Status == domain.RunCompleted || outcome.Status == domain.RunStopped {
				if rmErr := os.RemoveAll(s.snapshotDir); rmErr != nil {
					s.deps.Debug.Log(domain.EventSnapshotCleanupFailed,
						fmt.Sprintf("failed to remove snapshot directory %s: %v", s.snapshotDir, rmErr))
				}
			}
		}()

		// Re-resolve all agents against the snapshot directory so every dispatch
		// uses the transformed snapshot copies, not the originals.
		agents, err = agentresolve.ResolveAll(snapshotDir, identifiers)
		if err != nil {
			return s.refusal(err.Error()), nil
		}

		// Re-resolve the orchestrator against the snapshot directory.
		snapshotOrchPath := filepath.Join(snapshotDir, filepath.Base(config.OrchestratorFilePath))
		orchRef, err = agentresolve.ResolveOrchestrator(snapshotOrchPath)
		if err != nil {
			return s.refusal(err.Error()), nil
		}
		s.orchRef = orchRef

		// Re-bind consultants with the snapshot-resolved orchestrator reference
		// so consultation dispatches the transformed copy of the orchestrator
		// script rather than the original.
		rc = domain.RunContext{Orchestrator: orchRef, Table: table}
		bindRunContext(s.deps.Routing, rc)
		bindRunContext(s.deps.Manual, rc)
		bindRunContext(s.deps.PreConsult, rc)
	}

	// Step 6: Read the stage set from the run folder, if a plan file is
	// present there. Absence is a normal state for any run that has not yet
	// reached the EXECUTION phase (new or resumed) and is never a run-start
	// refusal; the engine enforces the stage set precondition at the point it
	// is actually required (routing an EXECUTION row). A plan file that
	// exists but cannot be parsed remains a refusal.
	//
	// Ordering: a resumed run's folder already exists, so the read happens
	// here, in its original position. A new run's folder does not exist yet
	// at this point -- seeded inputs (Plan.md among them) are not applied
	// until Step 8 -- so on the new-run path the read is deferred until after
	// Store.Create and seed.Apply have both run (Step 8a2 below). Reading it
	// here on the new-run path would observe the run before its seeded
	// inputs are in place, which is the defect this ordering fixes.
	var stages *domain.StageSet
	planPath := filepath.Join(config.RunFolder, "Plan.md")
	if !config.IsNewRun {
		if _, statErr := os.Stat(planPath); statErr == nil {
			ss, stageErr := planstages.ReadStages(planPath, admitted.GroupsDeclared)
			if stageErr != nil {
				return s.refusal(stageErr.Error()), nil
			}
			stages = &ss
		}
	}

	// Step 6b: Enumerate declared infrastructure agents from the orchestrator file.
	// This must happen before Step 7 (checkpoint refusal) so we can check for a
	// checkpoint-class agent. An empty slice is valid — no infrastructure agents deployed.
	declaredInfraAgents, err := orchfile.EnumerateInfrastructureAgents(config.OrchestratorFilePath)
	if err != nil {
		return s.refusal(err.Error()), nil
	}

	// Step 6c: Validate per-class agent selection. When multiple agents of the
	// same gated class are declared and no selection is provided in RunConfig,
	// refuse at run start (non-interactive CLI runs must supply --infra-class).
	if err := validateClassSelections(declaredInfraAgents, config.InfraClassSelections); err != nil {
		return s.refusal(err.Error()), nil
	}

	// Step 6d: On resume, read all RunSettings from the existing artifact before
	// any validation that depends on those settings. This ensures that a resumed
	// run is governed by exactly the settings that were recorded when the run was
	// created, regardless of what the caller supplied in RunConfig.
	if !config.IsNewRun {
		config.RunSettings = existingState.RunSettings
	}

	// Step 7: Settle checkpoints (FR-9). Refuse only when checkpoints are enabled
	// AND no checkpoint-class infrastructure agent is declared for this run.
	if config.Checkpoints && !hasCheckpointClassAgent(declaredInfraAgents) {
		return s.refusal("checkpoints requested but no checkpoint provider is available"), nil
	}

	// Step 7.5a: Mode is required. The caller must supply a valid mode; the
	// runner has no default and refuses rather than guessing.
	if config.Mode == domain.ExecutionModeUnset {
		return s.refusal("mode is required; valid values: orchestrated, auto, auto-review"), nil
	}

	// Step 7.5b: Commits validation. If commits are enabled but no commit-class
	// infrastructure agent is declared, refuse. If commits are enabled and a
	// commit-class agent exists, proceed. If no commit-class agent is declared
	// at all, silently disable commits (the caller may have set Commits=true
	// speculatively, e.g. from a flag that applies to all runs).
	if config.Commits {
		if !hasCommitClassAgent(declaredInfraAgents) {
			return s.refusal("commits enabled but no commit-class infrastructure agent is declared"), nil
		}
	}

	// Step 7a: Build and validate the seed plan. New runs only: a resumed run
	// ignores SeedInputs entirely. Validation precedes Store.Create so an invalid
	// seed set leaves no run folder behind.
	var seedPlan seed.Plan
	if config.IsNewRun && len(config.SeedInputs) > 0 {
		p, seedErr := seed.BuildPlan(config.SeedInputs)
		if seedErr != nil {
			return s.refusal(seedErr.Error()), nil
		}
		seedPlan = p
	}

	// Step 8: Create or resume the artifact.
	var state domain.ArtifactState
	var seq int
	var lastResponse *domain.ProtocolResponse

	// Step 7.9: Commit setup dispatch (new runs only, when commits are enabled).
	// This occurs before Store.Create so that a failed setup leaves no artifact
	// trace. On success, the branch name is recorded in RunSettings so it flows
	// into the artifact via Store.Create. The dispatch record is held in memory
	// and applied retroactively as the first Execution Log row (Seq=1) after the
	// artifact exists.
	var commitSetup *commitSetupRecord
	if config.IsNewRun && config.Commits {
		cs, refusalMsg := s.doCommitSetupDispatch(ctx, declaredInfraAgents, config, orchDir)
		if refusalMsg != "" {
			return s.refusal(refusalMsg), nil
		}
		config.RunSettings.CommitBranch = cs.branchName
		commitSetup = cs
	}

	if config.IsNewRun {
		// New run: create a fresh artifact.
		settings := config.RunSettings
		state, err = s.deps.Store.Create(ctx, region.Info, config.Task, settings, s.deps.Clock.Now(), config.RunID)
		if err != nil {
			return domain.RunOutcome{Status: domain.RunFailed, Message: err.Error()}, err
		}
		// Apply the seed plan into the run folder. If any copy fails, remove the
		// entire run folder (including Orchestration.md and already-copied files)
		// so no trace of the failed attempt remains. Only reachable on the new-run
		// path, after a Create this attempt performed.
		if applyErr := seed.Apply(seedPlan, config.RunFolder); applyErr != nil {
			if rmErr := os.RemoveAll(config.RunFolder); rmErr != nil {
				return s.refusal(fmt.Sprintf("%s; additionally, removing the run folder %s failed: %v",
					applyErr.Error(), config.RunFolder, rmErr)), nil
			}
			return s.refusal(applyErr.Error()), nil
		}

		// Step 8a2 (moved from Step 6 for the new-run path): the run folder
		// exists and any seeded inputs have been applied, so this is the
		// first point at which a read of the stage table observes the run as
		// fully constituted. Absence is still normal. A plan file that exists
		// but cannot be parsed is a refusal; the run folder created above is
		// removed so a refused run leaves no trace, matching the seed-apply
		// failure above.
		if _, statErr := os.Stat(planPath); statErr == nil {
			ss, stageErr := planstages.ReadStages(planPath, admitted.GroupsDeclared)
			if stageErr != nil {
				if rmErr := os.RemoveAll(config.RunFolder); rmErr != nil {
					return s.refusal(fmt.Sprintf("%s; additionally, removing the run folder %s failed: %v",
						stageErr.Error(), config.RunFolder, rmErr)), nil
				}
				return s.refusal(stageErr.Error()), nil
			}
			stages = &ss
		}

		seq = 0

		// Apply the commit setup row as the first Execution Log entry (Seq=1).
		// This must happen after Store.Create (so the artifact exists) and after
		// seed.Apply (so the plan context is in place), but before the dispatch
		// loop. If the Apply fails, remove the run folder so no trace remains.
		if commitSetup != nil {
			commitStep := domain.CompletedStep{
				Seq:              state.GlobalSequence + 1, // always 1 on a fresh artifact
				AgentInstance:    commitSetup.agentInstance,
				Status:           commitSetup.status,
				Summary:          commitSetup.summary,
				Timestamp:        commitSetup.completedAt,
				IsInfrastructure: true,
			}
			state, err = s.deps.Store.Apply(ctx, state, commitStep)
			if err != nil {
				_ = os.RemoveAll(config.RunFolder)
				return s.refusal("commit setup row apply failed: " + err.Error()), nil
			}
			seq = 1
		}
	} else {
		// Resume: use the existing artifact (already validated above).
		state = existingState
		resume, resumeErr := engine.ResumePoint(admitted, stages, state, domain.NewInfraAgentSet(declaredInfraAgents, s.orchRef.Identifier))
		if resumeErr != nil {
			return domain.RunOutcome{Status: domain.RunFailed, Message: resumeErr.Error()}, resumeErr
		}
		seq = resume.Seq

		// FR-33: if the last step was interrupted, rewind CurrentState so that
		// engine.Next re-dispatches the interrupted row.
		if resume.RerunLast {
			state = rewindStateForRerun(admitted.Table, domain.NewInfraAgentSet(declaredInfraAgents, s.orchRef.Identifier), state)
		}
	}

	// Step 8b: Validate and apply infrastructure_overrides from the artifact state.
	// Each override must name a declared infrastructure agent; unknown names are
	// a run-start refusal. Trigger restrictions are also validated per agent class.
	// The returned slice has replacement trigger lists applied (override semantics).
	declaredInfraAgents, err = validateAndApplyOverrides(state.InfrastructureOverrides, declaredInfraAgents)
	if err != nil {
		return s.refusal(err.Error()), nil
	}

	// Step 8c: Pre-consultation (auto and auto-review modes only, when enabled).
	// The advice strings are retained for the entire dispatch loop; the runner
	// appends them mechanically to every auto-routed dispatch task description.
	// On failure, the run folder is removed so no trace of the failed attempt
	// remains (matching the seed-apply and plan-parse failure contracts above).
	var preConsultAdvice domain.PreConsultationAdvice
	if config.PreConsultation &&
		(config.Mode == domain.ExecutionModeAuto || config.Mode == domain.ExecutionModeAutoReview) {
		advice, pcErr := s.deps.PreConsult.PreConsult(ctx, domain.ConsultationRequest{
			Context:               domain.ConsultContextPreConsultation,
			OrchestrationArtifact: filepath.Join(config.RunFolder, "Orchestration.md"),
		})
		if pcErr != nil {
			// Remove the run folder only on a new run — the "no trace remains"
			// contract. On a resumed run the folder predates this attempt and
			// contains existing run history; removing it would silently destroy
			// the user's artifact and break the override-restart's preservation
			// guarantee.
			if config.IsNewRun && config.RunFolder != "" {
				_ = os.RemoveAll(config.RunFolder)
			}
			return s.refusalCaused("pre-consultation failed: "+pcErr.Error(), pcErr), nil
		}
		preConsultAdvice = advice
	}

	// stageSource describes where the stage table was looked for, so the
	// engine's stop message can name the cause rather than the symptom when
	// a staged row is reached with no stage set (I5.3). Seeded reflects
	// whether the seed plan targets Plan.md; on a resumed run seedPlan is
	// always the zero value (seeded inputs are ignored there), so Seeded is
	// always false on that path.
	stageSource := domain.StageSource{
		Path:   planPath,
		Seeded: seedPlan.HasDest("Plan.md"),
	}

	// =========================================================================
	// Dispatch loop
	// =========================================================================

	// Arm the one-shot ManualDispatch signal so the first consultRoute call
	// uses the ManualResolver instead of the configured RoutingConsultant.
	// The flag is cleared inside consultRoute after the first use.
	s.manualDispatchPending = config.ManualDispatch

	var refreshedStages *domain.StageSet
	// lastOutputArtifacts holds the output artifacts of the most recently
	// dispatched step. Passed to engine.Next so that COMPLETED_NEEDS_ACTION
	// auto-review artifact injection receives the correct input artifact list.
	var lastOutputArtifacts []string
	// prevWorkflowStep tracks the most recently completed workflow step for
	// retrospective STAGE_END / PHASE_END trigger evaluation. Nil until the
	// first workflow step completes. Updated only for workflow steps, not for
	// infrastructure agent completions (no-cascades rule).
	var prevWorkflowStep *domain.CompletedStep
	// antiLoop tracks consecutive same-agent dispatches for the current step
	// and enforces the anti-loop guard. rowIndex starts at -1 to signal that
	// no step has been tracked yet; recordDispatch resets the counter whenever
	// the row index or agent identifier changes.
	antiLoop := antiLoopState{rowIndex: -1}

	for {
		decision := engine.Next(engine.NextInput{
			Workflow:             admitted,
			Stages:               stages,
			State:                state,
			LastResponse:         lastResponse,
			Agents:               agents,
			Seq:                  seq,
			Now:                  s.deps.Clock.Now(),
			RefreshedStages:      refreshedStages,
			StageSource:          stageSource,
			Mode:                 state.Mode,
			LastOutputArtifacts:  lastOutputArtifacts,
		})
		// Consume the refreshed stages once (they are only for one engine.Next call).
		refreshedStages = nil

		if decision.Dispatch != nil {
			if len(decision.Dispatch.Steps) == 0 {
				return domain.RunOutcome{Status: domain.RunFailed, Message: "engine returned empty dispatch"}, nil
			}
			step := decision.Dispatch.Steps[0]

			// Auto-routed dispatch: the engine never sets a task description;
			// the session assigns the generic baseline so agents always receive
			// a non-empty task description. Pre-consultation advice is appended
			// only to auto-routed dispatches, never to consultant-routed ones.
			if step.Request.TaskDescription == "" {
				step.Request.TaskDescription = domain.GenericTaskDescription
			}
			if preConsultAdvice.TaskDescription != "" {
				step.Request.TaskDescription += "\n\n" + preConsultAdvice.TaskDescription
			}
			if preConsultAdvice.Constraints != "" {
				if step.Request.Constraints != "" {
					step.Request.Constraints += "\n\n" + preConsultAdvice.Constraints
				} else {
					step.Request.Constraints = preConsultAdvice.Constraints
				}
			}

			// Populate RunID from the artifact state and resolve artifact paths to
			// run-scoped form. The session derives RunID from state (not from
			// RunConfig) so that resumed runs carry the RunID minted at creation.
			step.Request.RunID = state.RunID
			if state.RunID != "" {
				folder := domain.RunScopedFolder(state.RunID) + "/"
				step.Request.InputArtifacts = resolveToRunScoped(step.Request.InputArtifacts, folder)
				step.Request.OutputArtifacts = resolveToRunScoped(step.Request.OutputArtifacts, folder)
			}

			// Log dispatch start so every invocation has a trace, including
			// steps that fail or end unresolved and never reach Store.Apply.
			s.deps.Debug.Log(domain.EventSessionDispatchStart, "dispatching step",
				domain.F("agent", step.Request.AgentInstanceID),
				domain.F("phase", step.Phase),
				domain.F("stage", step.Stage),
				domain.F("row", strconv.Itoa(step.RowIndex)),
			)

			// Notify the interaction port that a step is starting.
			s.deps.Interact.Notify(ctx, interaction.Notice{
				Level:   interaction.NoticeInfo,
				Title:   step.Request.AgentInstanceID,
				Message: fmt.Sprintf("phase=%s stage=%q status=running", step.Phase, step.Stage),
			})

			// Register this auto-routed dispatch with the anti-loop guard. An
			// engine-dispatched step always starts a new step context (new row),
			// so recordDispatch resets the counter here and always returns true.
			antiLoop.recordDispatch(step.RowIndex, step.Agent.Identifier)

			// Graceful-stop checkpoint: independent of ctx cancellation. Checked
			// immediately before the invocation so an already-recorded step is
			// never lost and this dispatch never starts once a stop is confirmed.
			if s.deps.StopRequested() {
				return domain.RunOutcome{Status: domain.RunStopped, Message: "run stopped: graceful stop confirmed"}, nil
			}

			// Invoke the harness.
			response, invokeErr := s.invokeAndLog(ctx, step.Agent, step.Request)
			if invokeErr != nil {
				if ctx.Err() != nil {
					return domain.RunOutcome{Status: domain.RunStopped, Message: "run stopped: context cancelled"}, nil
				}
				s.deps.Debug.Log(domain.EventSessionHarnessError, invokeErr.Error(),
					domain.F("agent", step.Request.AgentInstanceID),
				)
				deviationInfo := domain.DeviationInfo{
					Kind: domain.DeviationHarnessError,
					Response: domain.ProtocolResponse{
						AgentInstanceID: step.Request.AgentInstanceID,
						StatusCode:      domain.StatusBLOCKED,
						StatusMessage:   invokeErr.Error(),
					},
					CurrentRow:    step.RowIndex,
					CurrentPhase:  step.Phase,
					CurrentStage:  step.Stage,
					ArtifactState: state,
				}
				if s.deps.Routing == nil {
					msg := fmt.Sprintf("harness error: no routing consultant configured: %s", invokeErr.Error())
					s.deps.Debug.Log(domain.EventSessionDeviationUnresolved, msg)
					return domain.RunOutcome{Status: domain.RunDeviationUnresolved, Message: msg}, nil
				}
				// Persist a record of the failed dispatch attempt so the execution
				// log captures every attempt, including those that fail before
				// returning a response. This follows the HITL-rejected-attempt
				// pattern: IsInfrastructure=true leaves current_state unchanged so
				// resume detection works correctly, and OutputArtifacts is omitted
				// to avoid polluting the artifact registry. Only persisted when a
				// routing consultant is configured (i.e., when the deviation will be
				// handled rather than left unresolved with no artifact trace).
				failedAttemptStep := domain.CompletedStep{
					Seq:              state.GlobalSequence + 1,
					AgentInstance:    step.Request.AgentInstanceID,
					Phase:            step.Phase,
					Stage:            step.Stage,
					Status:           domain.StatusBLOCKED,
					Summary:          invokeErr.Error(),
					Timestamp:        s.deps.Clock.Now(),
					Inputs:           formatInputs(step.Request.InputArtifacts),
					IsInfrastructure: true,
				}
				state, err = s.deps.Store.Apply(ctx, state, failedAttemptStep)
				if err != nil {
					s.deps.Debug.Log(domain.EventSessionApplyFailed, err.Error())
					return domain.RunOutcome{Status: domain.RunFailed, Message: err.Error()}, err
				}
				seq = state.GlobalSequence
				done, outcome, outErr := s.consultRoute(ctx, &deviationInfo, &state, &seq,
					&lastResponse, &prevWorkflowStep, &refreshedStages, &stages,
					table, agents, config, declaredInfraAgents, admitted, &antiLoop)
				if done {
					return outcome, outErr
				}
				continue
			}

			// HITL compliance verification for auto-routed dispatches. The loop
			// runs before the final Store.Apply so that a non-compliant SUCCESS
			// is never written to the execution log as an accepted step.
			// Each rejected attempt is persisted immediately with HITLRejected=true
			// and IsInfrastructure=true before the redispatch or escalation, so the
			// execution log has a complete record of every dispatch attempt.
			//
			// hitlAttemptSeq tracks the sequence number assigned to each attempt.
			// It starts at state.GlobalSequence+1 (the next available slot) so
			// that the auto-routed step Seq always derives from the same counter
			// that consultRoute uses, preventing duplicate Seq values when a
			// preceding consultRoute call consumed additional sequence slots via
			// HITL redispatches or infrastructure records.
			hitlAttemptSeq := state.GlobalSequence + 1
			hitlRedispatchUsed := false
			hitlStep := step
			hitlResponse := response
			hitlAccepted := false

			// Pre-HITL-check stage-set re-derivation for self-referential rows.
			// When this step's output contains Stage-* and no stage set has been
			// established yet (the current row is both the Stage-* producer and
			// the HITL subject, with no prior row having populated stages), derive
			// the stage set from Plan.md now so that expandStageGlobs receives a
			// non-nil stage set and produces expanded paths for approval reads.
			// The guard prevents redundant disk reads when a prior row already
			// established a stage set. The existing post-loop re-derivation block
			// below remains in place for rows where this guard does not apply.
			if stages == nil && hasStageStarArtifact(hitlStep.Request.OutputArtifacts) {
				earlyPlanPath := filepath.Join(config.RunFolder, "Plan.md")
				ss, ssErr := planstages.ReadStages(earlyPlanPath, admitted.GroupsDeclared)
				if ssErr != nil {
					s.deps.Interact.Notify(ctx, interaction.Notice{
						Level:   interaction.NoticeWarning,
						Message: fmt.Sprintf("failed to re-read stage set after Stage-* output: %v", ssErr),
					})
				} else {
					refreshedStages = &ss
					stages = &ss
				}
			}

		hitlCheckLoop:
			for {
				var approvals []domain.ArtifactApproval
				expandedPaths := expandStageGlobs(hitlStep.Request.OutputArtifacts, stages)
				for _, path := range expandedPaths {
					approvals = append(approvals, domain.ArtifactApproval{
						Path:     path,
						Approval: s.deps.Approvals.ReadApproval(ctx, path),
					})
				}
				hitlDec := domain.DecideHITLCompliance(domain.HITLComplianceInput{
					EffectiveHITL:  hitlStep.EffectiveHITL,
					Status:         hitlResponse.StatusCode,
					Approvals:      approvals,
					RedispatchUsed: hitlRedispatchUsed,
				})
				switch hitlDec.Outcome {
				case domain.HITLAccept:
					hitlAccepted = true
					break hitlCheckLoop

				case domain.HITLRedispatch:
					hitlRedispatchUsed = true
					// Persist the rejected attempt before redispatching so the execution
					// log has a complete record of every dispatch. IsInfrastructure=true
					// leaves current_state.LastAgent unchanged, keeping ResumePoint's
					// interruption detection correct. OutputArtifacts=nil avoids
					// registering non-compliant paths in the artifact registry.
					rejStep := domain.CompletedStep{
						Seq:              hitlAttemptSeq,
						AgentInstance:    hitlStep.Request.AgentInstanceID,
						Phase:            hitlStep.Phase,
						Stage:            hitlStep.Stage,
						Status:           hitlResponse.StatusCode,
						ErrorCode:        hitlResponse.ErrorCode,
						Summary:          hitlResponse.StatusMessage,
						Timestamp:        s.deps.Clock.Now(),
						Inputs:           formatInputs(hitlStep.Request.InputArtifacts),
						IsInfrastructure: true,
						HITLRejected:     true,
					}
					state, err = s.deps.Store.Apply(ctx, state, rejStep)
					if err != nil {
						s.deps.Debug.Log(domain.EventSessionApplyFailed, err.Error())
						return domain.RunOutcome{Status: domain.RunFailed, Message: err.Error()}, err
					}
					seq = state.GlobalSequence
					hitlAttemptSeq++
					// Anti-loop guard: check before performing the HITL redispatch. The
					// rejected step was already persisted above; if the guard fires we
					// escalate via the consultant rather than redispatching.
					if !antiLoop.recordDispatch(hitlStep.RowIndex, hitlStep.Agent.Identifier) {
						s.deps.Debug.Log(domain.EventSessionHITLEscalate, "anti-loop guard triggered in HITL redispatch; escalating",
							domain.F("agent", hitlStep.Agent.Identifier),
						)
						if s.deps.Routing == nil {
							alrdMsg := "anti-loop guard: no routing consultant configured"
							s.deps.Debug.Log(domain.EventSessionDeviationUnresolved, alrdMsg)
							return domain.RunOutcome{Status: domain.RunDeviationUnresolved, Message: alrdMsg}, nil
						}
						alrdDevInfo := domain.DeviationInfo{
							Kind:          domain.DeviationNonSuccess,
							Response:      hitlResponse,
							CurrentRow:    hitlStep.RowIndex,
							CurrentPhase:  hitlStep.Phase,
							CurrentStage:  hitlStep.Stage,
							ArtifactState: state,
						}
						alrdDone, alrdOutcome, alrdErr := s.consultRoute(ctx, &alrdDevInfo, &state, &seq,
							&lastResponse, &prevWorkflowStep, &refreshedStages, &stages,
							table, agents, config, declaredInfraAgents, admitted, &antiLoop)
						if alrdDone {
							return alrdOutcome, alrdErr
						}
						break hitlCheckLoop
					}
					s.deps.Debug.Log(domain.EventSessionHITLRedispatch, "HITL non-compliant; redispatching same agent",
						domain.F("agent", hitlStep.Agent.Identifier),
					)
					rdReq := hitlStep.Request
					rdReq.AgentInstanceID = fmt.Sprintf("%s#%d", hitlStep.Agent.Identifier, hitlAttemptSeq)
					hitlStep.Request = rdReq
					// Graceful-stop checkpoint: the rejected attempt above is already
					// persisted, so it is safe to stop before the redispatch call.
					if s.deps.StopRequested() {
						return domain.RunOutcome{Status: domain.RunStopped, Message: "run stopped: graceful stop confirmed"}, nil
					}
					rdResp, rdErr := s.invokeAndLog(ctx, hitlStep.Agent, hitlStep.Request)
					if rdErr != nil {
						if ctx.Err() != nil {
							return domain.RunOutcome{Status: domain.RunStopped, Message: "run stopped: context cancelled"}, nil
						}
						s.deps.Debug.Log(domain.EventSessionHarnessError, rdErr.Error(),
							domain.F("agent", hitlStep.Request.AgentInstanceID),
						)
						rdDevInfo := domain.DeviationInfo{
							Kind: domain.DeviationHarnessError,
							Response: domain.ProtocolResponse{
								AgentInstanceID: hitlStep.Request.AgentInstanceID,
								StatusCode:      domain.StatusBLOCKED,
								StatusMessage:   rdErr.Error(),
							},
							CurrentRow:    hitlStep.RowIndex,
							CurrentPhase:  hitlStep.Phase,
							CurrentStage:  hitlStep.Stage,
							ArtifactState: state,
						}
						if s.deps.Routing == nil {
							rdMsg := fmt.Sprintf("HITL redispatch harness error: no routing consultant configured: %s", rdErr.Error())
							s.deps.Debug.Log(domain.EventSessionDeviationUnresolved, rdMsg)
							return domain.RunOutcome{Status: domain.RunDeviationUnresolved, Message: rdMsg}, nil
						}
						rdDone, rdOutcome, rdOutErr := s.consultRoute(ctx, &rdDevInfo, &state, &seq,
							&lastResponse, &prevWorkflowStep, &refreshedStages, &stages,
							table, agents, config, declaredInfraAgents, admitted, &antiLoop)
						if rdDone {
							return rdOutcome, rdOutErr
						}
						break hitlCheckLoop
					}
					hitlResponse = rdResp
					// Loop to re-check HITL with RedispatchUsed=true.

				case domain.HITLEscalate:
					// Persist the final rejected attempt (the redispatch that also
					// failed compliance) before escalating. IsInfrastructure=true and
					// OutputArtifacts=nil follow the same contract as the HITLRedispatch
					// rejected-step Apply above.
					escRejStep := domain.CompletedStep{
						Seq:              hitlAttemptSeq,
						AgentInstance:    hitlStep.Request.AgentInstanceID,
						Phase:            hitlStep.Phase,
						Stage:            hitlStep.Stage,
						Status:           hitlResponse.StatusCode,
						ErrorCode:        hitlResponse.ErrorCode,
						Summary:          hitlResponse.StatusMessage,
						Timestamp:        s.deps.Clock.Now(),
						Inputs:           formatInputs(hitlStep.Request.InputArtifacts),
						IsInfrastructure: true,
						HITLRejected:     true,
					}
					state, err = s.deps.Store.Apply(ctx, state, escRejStep)
					if err != nil {
						s.deps.Debug.Log(domain.EventSessionApplyFailed, err.Error())
						return domain.RunOutcome{Status: domain.RunFailed, Message: err.Error()}, err
					}
					seq = state.GlobalSequence
					s.deps.Debug.Log(domain.EventSessionHITLEscalate, "HITL redispatch exhausted; escalating to deviation",
						domain.F("agent", hitlStep.Agent.Identifier),
					)
					escDevInfo := domain.DeviationInfo{
						Kind:          domain.DeviationNonSuccess,
						Response:      hitlResponse,
						CurrentRow:    hitlStep.RowIndex,
						CurrentPhase:  hitlStep.Phase,
						CurrentStage:  hitlStep.Stage,
						ArtifactState: state,
					}
					if s.deps.Routing == nil {
						escMsg := "HITL escalation: no routing consultant configured"
						s.deps.Debug.Log(domain.EventSessionDeviationUnresolved, escMsg)
						return domain.RunOutcome{Status: domain.RunDeviationUnresolved, Message: escMsg}, nil
					}
					escDone, escOutcome, escErr := s.consultRoute(ctx, &escDevInfo, &state, &seq,
						&lastResponse, &prevWorkflowStep, &refreshedStages, &stages,
						table, agents, config, declaredInfraAgents, admitted, &antiLoop)
					if escDone {
						return escOutcome, escErr
					}
					break hitlCheckLoop
				}
			}

			// If the HITL loop ended via consultRoute (harness error or escalation),
			// that path has already applied its own steps to the store; skip the
			// Apply/trigger/stage-refresh block and continue the engine loop.
			if !hitlAccepted {
				continue
			}

			// Apply the HITL-accepted response to the artifact. Each rejected
			// attempt was already applied with HITLRejected=true above; this
			// call records the final accepted outcome. Because rejected-step
			// rows carry IsInfrastructure=true, current_state still points at
			// the prior workflow step, so a resume after an interrupted run
			// correctly detects the mismatch and re-dispatches the step.
			completedStep := domain.CompletedStep{
				Seq:             hitlAttemptSeq,
				AgentInstance:   hitlStep.Request.AgentInstanceID,
				Phase:           hitlStep.Phase,
				Stage:           hitlStep.Stage,
				Status:          hitlResponse.StatusCode,
				ErrorCode:       hitlResponse.ErrorCode,
				Summary:         hitlResponse.StatusMessage,
				Timestamp:       s.deps.Clock.Now(),
				Inputs:          formatInputs(hitlStep.Request.InputArtifacts),
				OutputArtifacts: hitlStep.Request.OutputArtifacts,
			}
			state, err = s.deps.Store.Apply(ctx, state, completedStep)
			if err != nil {
				s.deps.Debug.Log(domain.EventSessionApplyFailed, err.Error())
				return domain.RunOutcome{Status: domain.RunFailed, Message: err.Error()}, err
			}
			s.deps.Debug.Log(domain.EventSessionStepDone, "step applied to artifact",
				domain.F("agent", completedStep.AgentInstance),
				domain.F("status", string(completedStep.Status)),
				domain.F("error_code", string(completedStep.ErrorCode)),
			)
			seq = hitlAttemptSeq
			lastResponse = &hitlResponse
			lastOutputArtifacts = hitlStep.Request.OutputArtifacts

			s.deps.Interact.Notify(ctx, interaction.Notice{
				Level:   interaction.NoticeInfo,
				Title:   completedStep.AgentInstance,
				Message: fmt.Sprintf("phase=%s stage=%q status=%s", completedStep.Phase, completedStep.Stage, string(completedStep.Status)),
			})

			// Infrastructure-agent trigger evaluation (FR-40).
			if !completedStep.IsInfrastructure {
				halt, trigStopped, trigErr := s.evaluateTriggers(
					ctx, &state, &seq, completedStep, prevWorkflowStep,
					declaredInfraAgents, config,
					buildActiveAgentsFilter(declaredInfraAgents, config.InfraClassSelections),
					orchDir,
				)
				if trigErr != nil {
					if ctx.Err() != nil {
						return domain.RunOutcome{Status: domain.RunStopped, Message: "run stopped: context cancelled"}, nil
					}
					return domain.RunOutcome{Status: domain.RunFailed, Message: trigErr.Error()}, trigErr
				}
				if trigStopped {
					return domain.RunOutcome{Status: domain.RunStopped, Message: "run stopped: graceful stop confirmed"}, nil
				}
				if halt {
					return domain.RunOutcome{Status: domain.RunStopped, Message: "infrastructure agent halted the run"}, nil
				}
				cp := completedStep
				prevWorkflowStep = &cp
				onInfrastructureAgentTrigger()
				if s.deps.OnInfrastructureTrigger != nil {
					s.deps.OnInfrastructureTrigger()
				}
			}

			// Stage-* output re-derivation.
			if hasStageStarArtifact(hitlStep.Request.OutputArtifacts) {
				rederivePlanPath := filepath.Join(config.RunFolder, "Plan.md")
				ss, ssErr := planstages.ReadStages(rederivePlanPath, admitted.GroupsDeclared)
				if ssErr != nil {
					s.deps.Interact.Notify(ctx, interaction.Notice{
						Level:   interaction.NoticeWarning,
						Message: fmt.Sprintf("failed to re-read stage set after Stage-* output: %v", ssErr),
					})
				} else {
					refreshedStages = &ss
					stages = &ss
				}
			}

			continue
		}

		if decision.Complete != nil {
			return domain.RunOutcome{
				Status:  domain.RunCompleted,
				Message: "run completed successfully",
			}, nil
		}

		// Consult decision: the engine defers every routing choice to the
		// consultant (ExecutionModeOrchestrated). ConsultRoute handles the full
		// consultation-record-reread-dispatch cycle and updates all loop state.
		if decision.Consult != nil {
			if s.deps.Routing == nil && !(s.manualDispatchPending && s.deps.Manual != nil) {
				return domain.RunOutcome{
					Status:  domain.RunFailed,
					Message: "orchestrated mode requires a RoutingConsultant but none was wired",
				}, nil
			}
			done, outcome, outErr := s.consultRoute(ctx, nil, &state, &seq,
				&lastResponse, &prevWorkflowStep, &refreshedStages, &stages,
				table, agents, config, declaredInfraAgents, admitted, &antiLoop)
			if done {
				return outcome, outErr
			}
			continue
		}

		if decision.Deviation != nil {
			s.deps.Debug.Log(domain.EventSessionDeviation, "engine returned deviation",
				domain.F("kind", string(decision.Deviation.Info.Kind)),
			)
			if s.deps.Routing == nil && !(s.manualDispatchPending && s.deps.Manual != nil) {
				msg := fmt.Sprintf("deviation: no routing consultant configured (kind=%s)", decision.Deviation.Info.Kind)
				s.deps.Debug.Log(domain.EventSessionDeviationUnresolved, msg)
				return domain.RunOutcome{Status: domain.RunDeviationUnresolved, Message: msg}, nil
			}
			done, outcome, outErr := s.consultRoute(ctx, &decision.Deviation.Info, &state, &seq,
				&lastResponse, &prevWorkflowStep, &refreshedStages, &stages,
				table, agents, config, declaredInfraAgents, admitted, &antiLoop)
			if done {
				return outcome, outErr
			}
			continue
		}

		if decision.Stop != nil {
			return domain.RunOutcome{
				Status:  domain.RunStopped,
				Message: "engine stop: " + decision.Stop.Reason,
			}, nil
		}

		return domain.RunOutcome{Status: domain.RunFailed, Message: "engine returned nil decision"}, nil
	}
}

// rowAtIndex returns the routing table row at the given zero-based index, or
// false if no row with that index exists.
func rowAtIndex(table domain.RoutingTable, idx int) (domain.RoutingRow, bool) {
	for _, row := range table.Rows {
		if row.Index == idx {
			return row, true
		}
	}
	return domain.RoutingRow{}, false
}

// extractAgentIdentifier extracts the agent identifier from an agent instance
// ID (e.g. "agent-a#1" → "agent-a"). If the input contains no "#", it is
// returned unchanged.
func extractAgentIdentifier(instanceID string) string {
	if idx := strings.LastIndex(instanceID, "#"); idx >= 0 {
		return instanceID[:idx]
	}
	return instanceID
}

// rewindStateForRerun adjusts the artifact CurrentState for a mid-invocation
// interruption (FR-33): the last logged step was interrupted before Apply
// completed, so the session must re-dispatch it. Rewinding CurrentState to
// the last WORKFLOW log entry before the interrupted one (or empty) causes
// engine.Next to route to the interrupted row again.
//
// Trailing infrastructure entries between the interrupted step and the
// workflow step that precedes it are skipped: current_state must reflect the
// last completed WORKFLOW step, never an infrastructure invocation, so the
// engine's row-lookup stays correct when infrastructure activity interleaves
// with the interruption.
//
// An entry is recognised as infrastructure by the same two-condition
// derivation used elsewhere: absent from the routing table and present in
// infra. infra may be nil, meaning no infrastructure agents are declared.
func rewindStateForRerun(table domain.RoutingTable, infra domain.InfraAgentSet, state domain.ArtifactState) domain.ArtifactState {
	for i := len(state.ExecutionLog) - 2; i >= 0; i-- {
		entry := state.ExecutionLog[i]
		agentID := extractAgentIdentifier(entry.Agent)
		if !isRoutingTableAgent(table, agentID) {
			if infra != nil && infra.IsInfrastructureAgent(entry.Agent) {
				continue // recognised infrastructure entry: keep looking
			}
			// Not a routing table participant and not recognised
			// infrastructure either. Unreachable in practice -- ResumePoint
			// would already have stopped on this entry with
			// PositionUnresolvedError before this function is ever called --
			// but applying the same two-condition recognition rule used
			// elsewhere (rather than "absent from the routing table" alone)
			// keeps this function from silently masking a defect if that
			// call ordering ever changes.
			continue
		}
		state.CurrentState = domain.CurrentState{
			Phase:      entry.Phase,
			Stage:      entry.Stage,
			LastStatus: domain.StatusSUCCESS,
			LastAgent:  entry.Agent,
		}
		return state
	}
	state.CurrentState = domain.CurrentState{}
	return state
}

// isRoutingTableAgent reports whether agentID names a participant in table,
// in any row.
func isRoutingTableAgent(table domain.RoutingTable, agentID string) bool {
	for _, row := range table.Rows {
		if row.Agent == agentID {
			return true
		}
	}
	return false
}

// bindRunContext calls BindRunContext on v if v is non-nil and implements
// domain.RunContextBinder. Consultants that do not implement the interface are
// silently skipped, so callers need not check before calling.
func bindRunContext(v any, rc domain.RunContext) {
	if v == nil {
		return
	}
	if binder, ok := v.(domain.RunContextBinder); ok {
		binder.BindRunContext(rc)
	}
}

// refusal logs the refusal reason and constructs a RunOutcome with RunRefused
// status. Every run-start refusal (pre-Store.Create) passes through here so
// that paths which never reach Store.Apply still leave a debug-log trace.
func (s *sessionImpl) refusal(message string) domain.RunOutcome {
	s.deps.Debug.Log(domain.EventSessionRefusal, message)
	return domain.RunOutcome{
		Status:  domain.RunRefused,
		Message: message,
	}
}

// isWorkflowNotFound reports whether err is orchfile's "this identifier is not
// declared here" refusal for the given workflow ID, as opposed to any other way
// reading the orchestrator file can fail. Component and resource alone do not
// settle it: a workflow region missing its version attribute and a duplicated
// identifier both refuse under the same component naming the same workflow, and
// reporting either of those as a workflow that has gone missing sends the user
// looking for something that is sitting in the file where they expect it. The
// reason is what separates them, so that is what is matched.
func isWorkflowNotFound(err error, workflowID string) bool {
	var refErr *domain.RefusalError
	return errors.As(err, &refErr) &&
		refErr.Component == "orchfile" &&
		refErr.Resource == workflowID &&
		refErr.Reason == orchfile.WorkflowNotFoundReason(workflowID)
}

// resumeWorkflowGoneRefusal refuses a resumed run whose workflow the
// orchestrator file does not declare. The message names both halves of the
// problem -- which workflow is missing and which run is stranded by its absence
// -- because a workspace holds many runs and naming only one of the two leaves
// the user unable to tell whether to fix the file or abandon the run. It says
// nothing about where the identifier came from: the TUI carries it from the
// run's own artifact, but a CLI resume takes it from --workflow, and telling a
// user who has just mistyped that flag to go inspect the run's history sends
// them away from the mistake rather than towards it.
func (s *sessionImpl) resumeWorkflowGoneRefusal(runID, workflowID string) domain.RunOutcome {
	reason := fmt.Sprintf("workflow %q for run %s is not declared in the current "+
		"orchestrator file; it may have been renamed or removed", workflowID, runID)
	return s.refusalCaused(reason, &domain.RefusalError{
		Component: "workflow",
		Resource:  runID,
		Reason:    reason,
	})
}

// resumeRecordedNoWorkflowRefusal refuses a resumed run that carries no
// workflow at all. The message says exactly that rather than reporting a lookup
// that failed for an identifier the user never sees: there is no workflow to go
// looking for, and implying otherwise sends them somewhere with nothing to
// find. The causes are listed as possibilities rather than as a diagnosis --
// the artifact may be missing or unreadable, but it may equally be present and
// well-formed while recording no workflow, and the caller does not tell the
// three apart.
func (s *sessionImpl) resumeRecordedNoWorkflowRefusal(runID string) domain.RunOutcome {
	reason := fmt.Sprintf("run %s records no workflow, so there is nothing to resume it as; "+
		"its artifact may be missing, unreadable, or may record no workflow", runID)
	return s.refusalCaused(reason, &domain.RefusalError{
		Component: "workflow",
		Resource:  runID,
		Reason:    reason,
	})
}

// refusalCaused is like refusal but also carries an error cause in the
// outcome's Cause field. It is used when the refusal is attributable to a
// structured error whose identity must survive to the frontend (e.g. a
// *domain.HarnessLaunchError that the TUI needs to detect via errors.As).
func (s *sessionImpl) refusalCaused(message string, cause error) domain.RunOutcome {
	s.deps.Debug.Log(domain.EventSessionRefusal, message)
	return domain.RunOutcome{
		Status:  domain.RunRefused,
		Message: message,
		Cause:   cause,
	}
}

// consultRoute handles the full consultation-record-reread-dispatch cycle for
// one routing decision. It is called whenever the session must defer a routing
// choice to the RoutingConsultant — both for orchestrated-mode normal flow
// (Consult decision) and for deviations (Deviation decision, harness errors,
// HITL escalations) when Routing is wired.
//
// All mutable loop state is passed as pointers so that consultRoute's writes
// are immediately visible to the caller on return.
//
// Returns done=true with a final outcome when the run should terminate.
// Returns done=false when the dispatch loop should continue.
func (s *sessionImpl) consultRoute(
	ctx context.Context,
	deviation *domain.DeviationInfo,
	state *domain.ArtifactState,
	seq *int,
	lastResponse **domain.ProtocolResponse,
	prevWorkflowStep **domain.CompletedStep,
	refreshedStages **domain.StageSet,
	stages **domain.StageSet,
	table domain.RoutingTable,
	agents map[string]domain.AgentReference,
	config domain.RunConfig,
	declaredInfraAgents []domain.DeclaredInfraAgent,
	admitted domain.AdmittedWorkflow,
	antiLoop *antiLoopState,
) (done bool, outcome domain.RunOutcome, outErr error) {
	// Capture the stage in force at this entry point so that every CompletedStep
	// produced below carries accurate context and notifications show real stage values.
	// This value is stable throughout the function body: the only intermediate
	// Store.Apply (consultStep below) is IsInfrastructure=true and does not modify
	// CurrentState. Recursive calls each capture their own entryStage independently.
	entryStage := state.CurrentState.Stage

	// Build the consultation request. LastStatusMessage is nil on the first
	// step of a new run and for every consultation where no prior agent result
	// exists.
	req := domain.ConsultationRequest{
		OrchestrationArtifact: filepath.Join(config.RunFolder, "Orchestration.md"),
		Context:               domain.ConsultContextRouting,
		Deviation:             deviation,
	}
	if *lastResponse != nil {
		msg := (*lastResponse).StatusMessage
		req.LastStatusMessage = &msg
	}

	// Route the consultation request to the appropriate resolver.
	//
	// When the ManualDispatch one-shot signal is armed (set by config.ManualDispatch
	// at the start of Start), the ManualResolver handles this single decision so the
	// user can name the target agent and task description. The flag is cleared
	// immediately so all subsequent decisions route through the configured consultant.
	//
	// Otherwise, call the primary routing consultant with a fallback to the manual
	// resolver when the primary fails and ManualResolution is enabled.
	var instr domain.RoutingInstruction
	var consultErr error
	if s.manualDispatchPending && s.deps.Manual != nil {
		s.manualDispatchPending = false
		instr, consultErr = s.deps.Manual.ConsultRouting(ctx, req)
	} else {
		instr, consultErr = s.deps.Routing.ConsultRouting(ctx, req)
		if consultErr != nil && config.ManualResolution && s.deps.Manual != nil {
			s.deps.Debug.Log(domain.EventSessionManualResolve, "primary consultation failed; trying manual resolver",
				domain.F("error", consultErr.Error()),
			)
			instr, consultErr = s.deps.Manual.ConsultRouting(ctx, req)
		}
	}
	if consultErr != nil {
		s.deps.Debug.Log(domain.EventSessionConsultFailed, consultErr.Error())
		return true, domain.RunOutcome{
			Status:  domain.RunStoppedByConsultant,
			Message: "consultation failed: " + consultErr.Error(),
			Cause:   consultErr,
		}, nil
	}

	// Stop instruction: the consultant wants to end the run here.
	if instr.Stop != nil {
		s.deps.Debug.Log(domain.EventSessionConsultStop, "consultant stop instruction",
			domain.F("reason", instr.Stop.Reason),
		)
		return true, domain.RunOutcome{
			Status:     domain.RunStoppedByConsultant,
			Message:    "consultant stop: " + instr.Stop.Reason,
			StopReason: instr.Stop.Reason,
		}, nil
	}

	if instr.Dispatch == nil {
		// Neither Stop nor Dispatch — treat as a malformed instruction.
		s.deps.Debug.Log(domain.EventSessionConsultFailed, "instruction has neither stop nor dispatch")
		return true, domain.RunOutcome{
			Status:  domain.RunStoppedByConsultant,
			Message: "consultation failed: instruction has neither stop nor dispatch",
		}, nil
	}
	dispInstr := instr.Dispatch

	// Look up the routing table row early so its phase name is available for
	// the consultation log entry and for subsequent field resolution. When the
	// consultant dispatches to a conceptual expanded row index that does not
	// exist in the physical routing table (e.g. because the table uses
	// [StageNumber] templates and the consultant uses conceptual stage-expanded
	// indices), fall back to the deviation's physical row so artifact templates
	// and phase metadata are still available for resolution.
	row, found := rowAtIndex(table, dispInstr.RowIndex)
	if !found && deviation != nil {
		row, _ = rowAtIndex(table, deviation.CurrentRow)
	}

	// Derive the stage for all CompletedSteps produced in this consultRoute
	// call. When responding to a deviation and the target row is staged, use
	// the deviation's CurrentStage rather than state.CurrentState.Stage
	// (entryStage) to prevent prior-stage values from being stamped on rows
	// dispatched in a different stage.
	effectiveStage := entryStage
	if deviation != nil && row.PhaseParsed.IsStaged {
		_, stageNum, stageOK := domain.ParseStageValue(deviation.CurrentStage)
		if stageOK {
			effectiveStage = domain.FormatStageValue(row.PhaseParsed.Group, stageNum)
		}
	}

	// Record the consultation as an infrastructure-flagged Execution Log row.
	// The row consumes a global_sequence slot so the log has no duplicate
	// positions; Store.Apply's IsInfrastructure path leaves current_state alone.
	consultSeq := state.GlobalSequence + 1
	consultStep := domain.CompletedStep{
		Seq:              consultSeq,
		AgentInstance:    s.orchRef.Identifier + "#" + strconv.Itoa(consultSeq),
		Phase:            row.PhaseParsed.Name,
		Stage:            effectiveStage,
		Status:           domain.StatusSUCCESS,
		Summary:          dispInstr.TaskDescription,
		Timestamp:        s.deps.Clock.Now(),
		IsInfrastructure: true,
	}
	newState, applyErr := s.deps.Store.Apply(ctx, *state, consultStep)
	if applyErr != nil {
		return true, domain.RunOutcome{Status: domain.RunFailed, Message: applyErr.Error()}, applyErr
	}
	*state = newState

	// Re-read the artifact so any Workflow Notes the orchestrator appended
	// during its deliberation are visible to the session.
	if freshState, readErr := s.deps.Store.Read(ctx); readErr == nil {
		*state = freshState
	}

	// Resolve the dispatched agent.
	agentRef, agentFound := agents[dispInstr.Agent]
	if !agentFound {
		return true, domain.RunOutcome{
			Status:  domain.RunFailed,
			Message: "consultant dispatched unknown agent: " + dispInstr.Agent,
		}, nil
	}

	// Resolve each field using the priority table: non-nil pointer overrides;
	// nil pointer falls back to the routing table row.
	var constraints string
	if dispInstr.Constraints != nil {
		constraints = *dispInstr.Constraints
	}

	var inputArts []string
	if dispInstr.InputArtifacts != nil {
		inputArts = *dispInstr.InputArtifacts
	} else {
		inputArts = row.InputArtifacts
	}

	var outputArts []string
	if dispInstr.OutputArtifacts != nil {
		outputArts = *dispInstr.OutputArtifacts
	} else {
		outputArts = row.OutputArtifacts
	}

	// Resolve artifact template tokens ({StageNumber}) for consultant-routed
	// dispatches. Without this, rows whose declared artifact paths use the
	// {StageNumber} template pass unresolved tokens into the ProtocolRequest
	// and the persisted CompletedStep. Use the same stage context derived
	// above so the resolved paths match the target row's actual stage.
	if row.PhaseParsed.IsStaged && effectiveStage != "" {
		_, artStageNum, stageOK := domain.ParseStageValue(effectiveStage)
		if stageOK {
			if resolved, resolveErr := engine.ResolveArtifacts(inputArts, artStageNum, effectiveStage, *stages, *refreshedStages, true); resolveErr == nil {
				inputArts = resolved
			}
			if resolved, resolveErr := engine.ResolveArtifacts(outputArts, artStageNum, effectiveStage, *stages, *refreshedStages, false); resolveErr == nil {
				outputArts = resolved
			}
		}
	}

	effectiveHITL := row.HITL
	if dispInstr.HITLOverride != nil {
		effectiveHITL = *dispInstr.HITLOverride
	}

	// AgentInstanceID is always Runner-assigned using the workflow-step sequence
	// counter (*seq+1). The consultant's identifier naming preference is ignored.
	dispSeq := *seq + 1
	agentReq := domain.ProtocolRequest{
		AgentInstanceID: fmt.Sprintf("%s#%d", agentRef.Identifier, dispSeq),
		RunID:           state.RunID,
		TaskDescription: dispInstr.TaskDescription,
		Constraints:     constraints,
		InputArtifacts:  inputArts,
		OutputArtifacts: outputArts,
		HumanInTheLoop:  effectiveHITL,
	}
	if state.RunID != "" {
		folder := domain.RunScopedFolder(state.RunID) + "/"
		agentReq.InputArtifacts = resolveToRunScoped(agentReq.InputArtifacts, folder)
		agentReq.OutputArtifacts = resolveToRunScoped(agentReq.OutputArtifacts, folder)
	}

	phase := row.PhaseParsed.Name

	// Anti-loop guard: prevent the same agent from being dispatched more than
	// maxConsecutiveSameAgentDispatches consecutive times for the same step.
	// The counter is shared across recursive consultRoute calls via the antiLoop
	// pointer so that escalation chains do not reset it.
	if !antiLoop.recordDispatch(dispInstr.RowIndex, agentRef.Identifier) {
		s.deps.Debug.Log(domain.EventSessionDeviation, "anti-loop guard triggered; escalating instead of dispatching",
			domain.F("agent", agentRef.Identifier),
			domain.F("count", strconv.Itoa(antiLoop.count)),
			domain.F("row", strconv.Itoa(dispInstr.RowIndex)),
		)
		var lastResp domain.ProtocolResponse
		if *lastResponse != nil {
			lastResp = **lastResponse
		}
		guardDevInfo := domain.DeviationInfo{
			Kind: domain.DeviationNonSuccess,
			Response: domain.ProtocolResponse{
				AgentInstanceID: fmt.Sprintf("%s#guard", agentRef.Identifier),
				StatusCode:      domain.StatusBLOCKED,
				StatusMessage: fmt.Sprintf("anti-loop guard: %q dispatched %d consecutive times for row %d; escalating",
					agentRef.Identifier, antiLoop.count, dispInstr.RowIndex),
				ErrorCode: lastResp.ErrorCode,
			},
			CurrentRow:    dispInstr.RowIndex,
			CurrentPhase:  phase,
			ArtifactState: *state,
		}
		return s.consultRoute(ctx, &guardDevInfo, state, seq, lastResponse, prevWorkflowStep,
			refreshedStages, stages, table, agents, config, declaredInfraAgents, admitted, antiLoop)
	}

	s.deps.Debug.Log(domain.EventSessionDispatchStart, "dispatching consultant-routed step",
		domain.F("agent", agentReq.AgentInstanceID),
		domain.F("phase", phase),
		domain.F("row", strconv.Itoa(dispInstr.RowIndex)),
	)
	s.deps.Interact.Notify(ctx, interaction.Notice{
		Level:   interaction.NoticeInfo,
		Title:   agentReq.AgentInstanceID,
		Message: fmt.Sprintf("phase=%s stage=%q status=running", phase, effectiveStage),
	})

	// Graceful-stop checkpoint: the consultation record above is already
	// persisted, so it is safe to stop before the consultant-routed dispatch.
	if s.deps.StopRequested() {
		return true, domain.RunOutcome{Status: domain.RunStopped, Message: "run stopped: graceful stop confirmed"}, nil
	}

	// Invoke the harness. A harness error is a deviation, not a crash.
	response, invokeErr := s.invokeAndLog(ctx, agentRef, agentReq)
	if invokeErr != nil {
		if ctx.Err() != nil {
			return true, domain.RunOutcome{Status: domain.RunStopped, Message: "run stopped: context cancelled"}, nil
		}
		s.deps.Debug.Log(domain.EventSessionHarnessError, invokeErr.Error(),
			domain.F("agent", agentReq.AgentInstanceID),
		)
		devInfo := domain.DeviationInfo{
			Kind: domain.DeviationHarnessError,
			Response: domain.ProtocolResponse{
				AgentInstanceID: agentReq.AgentInstanceID,
				StatusCode:      domain.StatusBLOCKED,
				StatusMessage:   invokeErr.Error(),
			},
			CurrentRow:    dispInstr.RowIndex,
			CurrentPhase:  phase,
			ArtifactState: *state,
		}
		return s.consultRoute(ctx, &devInfo, state, seq, lastResponse, prevWorkflowStep,
			refreshedStages, stages, table, agents, config, declaredInfraAgents, admitted, antiLoop)
	}

	// HITL compliance verification. Each rejected attempt is persisted with
	// HITLRejected=true and IsInfrastructure=true before the redispatch or
	// escalation, ensuring a non-compliant SUCCESS never enters the log as an
	// accepted step and that sequence numbers remain strictly increasing.
	// The redispatch allowance (one redispatch per step) is scoped here; it
	// resets on every consultRoute call.
	finalResponse := response
	currentAttemptSeq := dispSeq
	currentOutputArts := agentReq.OutputArtifacts
	hitlRedispatchUsed := false

	// Pre-HITL-check stage-set re-derivation for self-referential rows.
	// When this step's output contains Stage-* and no stage set has been
	// established yet (the current row is both the Stage-* producer and
	// the HITL subject, with no prior row having populated stages), derive
	// the stage set from Plan.md now so that expandStageGlobs receives a
	// non-nil stage set and produces expanded paths for approval reads.
	// The *stages == nil guard prevents redundant re-derivation across
	// recursive consultRoute re-entries. The existing post-loop re-derivation
	// block below remains in place for rows where this guard does not apply.
	if *stages == nil && hasStageStarArtifact(currentOutputArts) {
		earlyPlanPath := filepath.Join(config.RunFolder, "Plan.md")
		ss, ssErr := planstages.ReadStages(earlyPlanPath, admitted.GroupsDeclared)
		if ssErr != nil {
			s.deps.Interact.Notify(ctx, interaction.Notice{
				Level:   interaction.NoticeWarning,
				Message: fmt.Sprintf("failed to re-read stage set after Stage-* output: %v", ssErr),
			})
		} else {
			*refreshedStages = &ss
			*stages = &ss
		}
	}

hitlLoop:
	for {
		var approvals []domain.ArtifactApproval
		expandedPaths := expandStageGlobs(currentOutputArts, *stages)
		for _, path := range expandedPaths {
			approvals = append(approvals, domain.ArtifactApproval{
				Path:     path,
				Approval: s.deps.Approvals.ReadApproval(ctx, path),
			})
		}
		hitlDec := domain.DecideHITLCompliance(domain.HITLComplianceInput{
			EffectiveHITL:  effectiveHITL,
			Status:         finalResponse.StatusCode,
			Approvals:      approvals,
			RedispatchUsed: hitlRedispatchUsed,
		})
		switch hitlDec.Outcome {
		case domain.HITLAccept:
			break hitlLoop

		case domain.HITLRedispatch:
			hitlRedispatchUsed = true
			// Persist the rejected attempt before redispatching. Use
			// state.GlobalSequence+1 (not currentAttemptSeq) because the
			// consultStep Apply above already consumed the currentAttemptSeq
			// slot. IsInfrastructure=true leaves current_state unchanged.
			// OutputArtifacts=nil avoids polluting the artifact registry.
			rlRejStep := domain.CompletedStep{
				Seq:              (*state).GlobalSequence + 1,
				AgentInstance:    fmt.Sprintf("%s#%d", agentRef.Identifier, currentAttemptSeq),
				Phase:            phase,
				Stage:            effectiveStage,
				Status:           finalResponse.StatusCode,
				ErrorCode:        finalResponse.ErrorCode,
				Summary:          finalResponse.StatusMessage,
				Timestamp:        s.deps.Clock.Now(),
				Inputs:           formatInputs(agentReq.InputArtifacts),
				IsInfrastructure: true,
				HITLRejected:     true,
			}
			var rlApplyErr error
			*state, rlApplyErr = s.deps.Store.Apply(ctx, *state, rlRejStep)
			if rlApplyErr != nil {
				return true, domain.RunOutcome{Status: domain.RunFailed, Message: rlApplyErr.Error()}, rlApplyErr
			}
			*seq = (*state).GlobalSequence
			// Anti-loop guard: check before performing the HITL redispatch.
			// The rejected step was already persisted above; if the guard fires
			// we escalate via the consultant rather than redispatching.
			if !antiLoop.recordDispatch(dispInstr.RowIndex, agentRef.Identifier) {
				s.deps.Debug.Log(domain.EventSessionHITLEscalate, "anti-loop guard triggered in hitlLoop redispatch; escalating",
					domain.F("agent", agentRef.Identifier),
				)
				alrlDevInfo := domain.DeviationInfo{
					Kind:          domain.DeviationNonSuccess,
					Response:      finalResponse,
					CurrentRow:    dispInstr.RowIndex,
					CurrentPhase:  phase,
					ArtifactState: *state,
				}
				return s.consultRoute(ctx, &alrlDevInfo, state, seq, lastResponse, prevWorkflowStep,
					refreshedStages, stages, table, agents, config, declaredInfraAgents, admitted, antiLoop)
			}
			s.deps.Debug.Log(domain.EventSessionHITLRedispatch, "HITL non-compliant; redispatching same agent",
				domain.F("agent", agentRef.Identifier),
			)
			currentAttemptSeq++
			rdReq := agentReq
			rdReq.AgentInstanceID = fmt.Sprintf("%s#%d", agentRef.Identifier, currentAttemptSeq)
			// Graceful-stop checkpoint: the rejected attempt above is already
			// persisted, so it is safe to stop before the redispatch call.
			if s.deps.StopRequested() {
				return true, domain.RunOutcome{Status: domain.RunStopped, Message: "run stopped: graceful stop confirmed"}, nil
			}
			rdResp, rdErr := s.invokeAndLog(ctx, agentRef, rdReq)
			if rdErr != nil {
				if ctx.Err() != nil {
					return true, domain.RunOutcome{Status: domain.RunStopped, Message: "run stopped: context cancelled"}, nil
				}
				s.deps.Debug.Log(domain.EventSessionHarnessError, rdErr.Error(),
					domain.F("agent", rdReq.AgentInstanceID),
				)
				rdDevInfo := domain.DeviationInfo{
					Kind: domain.DeviationHarnessError,
					Response: domain.ProtocolResponse{
						AgentInstanceID: rdReq.AgentInstanceID,
						StatusCode:      domain.StatusBLOCKED,
						StatusMessage:   rdErr.Error(),
					},
					CurrentRow:    dispInstr.RowIndex,
					CurrentPhase:  phase,
					ArtifactState: *state,
				}
				return s.consultRoute(ctx, &rdDevInfo, state, seq, lastResponse, prevWorkflowStep,
					refreshedStages, stages, table, agents, config, declaredInfraAgents, admitted, antiLoop)
			}
			finalResponse = rdResp
			// Loop to re-check with RedispatchUsed=true.

		case domain.HITLEscalate:
			// Persist the final rejected attempt before escalating. Follows
			// the same contract as the HITLRedispatch rejected-step Apply above:
			// IsInfrastructure=true to preserve current_state, OutputArtifacts=nil
			// to avoid polluting the registry, and seq derived from state.GlobalSequence+1
			// to ensure strict monotonicity across all persisted dispatches.
			elRejStep := domain.CompletedStep{
				Seq:              (*state).GlobalSequence + 1,
				AgentInstance:    fmt.Sprintf("%s#%d", agentRef.Identifier, currentAttemptSeq),
				Phase:            phase,
				Stage:            effectiveStage,
				Status:           finalResponse.StatusCode,
				ErrorCode:        finalResponse.ErrorCode,
				Summary:          finalResponse.StatusMessage,
				Timestamp:        s.deps.Clock.Now(),
				Inputs:           formatInputs(agentReq.InputArtifacts),
				IsInfrastructure: true,
				HITLRejected:     true,
			}
			var elApplyErr error
			*state, elApplyErr = s.deps.Store.Apply(ctx, *state, elRejStep)
			if elApplyErr != nil {
				return true, domain.RunOutcome{Status: domain.RunFailed, Message: elApplyErr.Error()}, elApplyErr
			}
			*seq = (*state).GlobalSequence
			s.deps.Debug.Log(domain.EventSessionHITLEscalate, "HITL redispatch exhausted; escalating to deviation",
				domain.F("agent", agentRef.Identifier),
			)
			escDevInfo := domain.DeviationInfo{
				Kind:          domain.DeviationNonSuccess,
				Response:      finalResponse,
				CurrentRow:    dispInstr.RowIndex,
				CurrentPhase:  phase,
				ArtifactState: *state,
			}
			return s.consultRoute(ctx, &escDevInfo, state, seq, lastResponse, prevWorkflowStep,
				refreshedStages, stages, table, agents, config, declaredInfraAgents, admitted, antiLoop)
		}
	}

	// Apply the HITL-compliant response to the artifact.
	workflowSeq := state.GlobalSequence + 1
	finalAgentInstanceID := fmt.Sprintf("%s#%d", agentRef.Identifier, currentAttemptSeq)
	completedStep := domain.CompletedStep{
		Seq:             workflowSeq,
		AgentInstance:   finalAgentInstanceID,
		Phase:           phase,
		Stage:           effectiveStage,
		Status:          finalResponse.StatusCode,
		ErrorCode:       finalResponse.ErrorCode,
		Summary:         finalResponse.StatusMessage,
		Timestamp:       s.deps.Clock.Now(),
		Inputs:          formatInputs(agentReq.InputArtifacts),
		OutputArtifacts: currentOutputArts,
	}
	*state, applyErr = s.deps.Store.Apply(ctx, *state, completedStep)
	if applyErr != nil {
		return true, domain.RunOutcome{Status: domain.RunFailed, Message: applyErr.Error()}, applyErr
	}
	s.deps.Debug.Log(domain.EventSessionStepDone, "step applied to artifact",
		domain.F("agent", finalAgentInstanceID),
		domain.F("status", string(completedStep.Status)),
	)
	*seq = currentAttemptSeq
	*lastResponse = &finalResponse
	s.deps.Interact.Notify(ctx, interaction.Notice{
		Level:   interaction.NoticeInfo,
		Title:   finalAgentInstanceID,
		Message: fmt.Sprintf("phase=%s stage=%q status=%s", phase, effectiveStage, string(finalResponse.StatusCode)),
	})

	// Infrastructure-agent trigger evaluation.
	orchDir := filepath.Dir(config.OrchestratorFilePath)
	if !completedStep.IsInfrastructure {
		halt, trigStopped, trigErr := s.evaluateTriggers(
			ctx, state, seq, completedStep, *prevWorkflowStep,
			declaredInfraAgents, config,
			buildActiveAgentsFilter(declaredInfraAgents, config.InfraClassSelections),
			orchDir,
		)
		if trigErr != nil {
			if ctx.Err() != nil {
				return true, domain.RunOutcome{Status: domain.RunStopped, Message: "run stopped: context cancelled"}, nil
			}
			return true, domain.RunOutcome{Status: domain.RunFailed, Message: trigErr.Error()}, trigErr
		}
		if trigStopped {
			return true, domain.RunOutcome{Status: domain.RunStopped, Message: "run stopped: graceful stop confirmed"}, nil
		}
		if halt {
			return true, domain.RunOutcome{Status: domain.RunStopped, Message: "infrastructure agent halted the run"}, nil
		}
		cp := completedStep
		*prevWorkflowStep = &cp
		onInfrastructureAgentTrigger()
		if s.deps.OnInfrastructureTrigger != nil {
			s.deps.OnInfrastructureTrigger()
		}
	}

	// Stage-* output re-derivation.
	if hasStageStarArtifact(currentOutputArts) {
		rederivePlanPath := filepath.Join(config.RunFolder, "Plan.md")
		ss, ssErr := planstages.ReadStages(rederivePlanPath, admitted.GroupsDeclared)
		if ssErr != nil {
			s.deps.Interact.Notify(ctx, interaction.Notice{
				Level:   interaction.NoticeWarning,
				Message: fmt.Sprintf("failed to re-read stage set after Stage-* output: %v", ssErr),
			})
		} else {
			*refreshedStages = &ss
			*stages = &ss
		}
	}

	return false, domain.RunOutcome{}, nil
}

// uniqueAgentIdentifiers returns the unique agent identifiers from the routing
// table in first-occurrence order.
func uniqueAgentIdentifiers(table domain.RoutingTable) []string {
	seen := make(map[string]bool, len(table.Rows))
	result := make([]string, 0, len(table.Rows))
	for _, row := range table.Rows {
		if !seen[row.Agent] {
			seen[row.Agent] = true
			result = append(result, row.Agent)
		}
	}
	return result
}

// hasStageStarArtifact reports whether any artifact path in the slice contains
// the Stage-* wildcard pattern.
func hasStageStarArtifact(artifacts []string) bool {
	for _, a := range artifacts {
		if strings.Contains(a, "Stage-*") {
			return true
		}
	}
	return false
}

// onInfrastructureAgentTrigger is the named no-op hook that marks the
// infrastructure-agent trigger point in the dispatch loop (FR-40). It is
// called alongside Deps.OnInfrastructureTrigger after each harness invocation,
// making the trigger point discoverable by name. Production code passes nil
// for Deps.OnInfrastructureTrigger; tests inject a counter.
func onInfrastructureAgentTrigger() {
	// no-op: production hook; real infrastructure-agent dispatch goes here.
}

// resolveToRunScoped prepends the run-scoped folder prefix to each artifact
// path that does not already carry it. Paths that already start with prefix
// are passed through unchanged, preventing double-prefixing.
func resolveToRunScoped(paths []string, prefix string) []string {
	if len(paths) == 0 {
		return paths
	}
	resolved := make([]string, len(paths))
	for i, p := range paths {
		if strings.HasPrefix(p, prefix) {
			resolved[i] = p
		} else {
			resolved[i] = prefix + p
		}
	}
	return resolved
}

// formatInputs formats a list of input artifact paths as a comma-separated
// string for the Inputs column of the Execution Log. Returns "" when the
// list is empty (rendered as "-" in the table).
func formatInputs(paths []string) string {
	return strings.Join(paths, ", ")
}

// hasCheckpointClassAgent reports whether any declared infrastructure agent
// has Class == "checkpoint".
func hasCheckpointClassAgent(agents []domain.DeclaredInfraAgent) bool {
	for _, a := range agents {
		if a.Class == "checkpoint" {
			return true
		}
	}
	return false
}

// hasCommitClassAgent reports whether any declared infrastructure agent
// has Class == "commit".
func hasCommitClassAgent(agents []domain.DeclaredInfraAgent) bool {
	for _, a := range agents {
		if a.Class == "commit" {
			return true
		}
	}
	return false
}

// firstCommitClassAgent returns the first declared infrastructure agent with
// Class == "commit", or nil if none is declared.
func firstCommitClassAgent(agents []domain.DeclaredInfraAgent) *domain.DeclaredInfraAgent {
	for i := range agents {
		if agents[i].Class == "commit" {
			return &agents[i]
		}
	}
	return nil
}

// branchMarkerRe matches the [branch:{name}] marker pattern in a status_message.
// The branch name is captured in group 1.
var branchMarkerRe = regexp.MustCompile(`\[branch:([^\]]+)\]`)

// extractBranchRef scans statusMessage for a [branch:{name}] marker and
// returns the branch name, trimmed of surrounding whitespace. Returns "" when
// no marker is found.
func extractBranchRef(statusMessage string) string {
	m := branchMarkerRe.FindStringSubmatch(statusMessage)
	if m == nil {
		return ""
	}
	return strings.TrimSpace(m[1])
}

// commitSetupRecord holds the result of a successful commit setup dispatch,
// kept in memory until the artifact exists so it can be recorded retroactively
// as the first Execution Log row.
type commitSetupRecord struct {
	agentInstance string
	branchName    string
	status        domain.StatusCode
	summary       string
	completedAt   time.Time
}

// doCommitSetupDispatch dispatches the commit-class infrastructure agent to
// establish the target branch for this run. It returns a populated
// commitSetupRecord on success, or a non-empty refusal message on failure.
// A failed dispatch, missing branch marker, or nil commit agent all result in
// a refusal; no artifact is created before this call.
func (s *sessionImpl) doCommitSetupDispatch(
	ctx context.Context,
	declared []domain.DeclaredInfraAgent,
	config domain.RunConfig,
	orchDir string,
) (*commitSetupRecord, string) {
	commitAgent := firstCommitClassAgent(declared)
	if commitAgent == nil {
		return nil, "commits enabled but no commit-class agent found"
	}
	agentRef := domain.AgentReference{
		Identifier:     commitAgent.Name,
		DefinitionPath: filepath.Join(orchDir, commitAgent.Name+".md"),
	}
	req := domain.ProtocolRequest{
		AgentInstanceID: fmt.Sprintf("%s#1", commitAgent.Name),
		RunID:           config.RunID,
		TaskDescription: "commit setup: establish the target branch for this run",
	}
	response, invokeErr := s.invokeAndLog(ctx, agentRef, req)
	completedAt := s.deps.Clock.Now()
	if invokeErr != nil {
		return nil, "commit setup dispatch failed: " + invokeErr.Error()
	}
	branchName := extractBranchRef(response.StatusMessage)
	if branchName == "" {
		return nil, "commit setup: no [branch:{name}] marker in status_message"
	}
	return &commitSetupRecord{
		agentInstance: req.AgentInstanceID,
		branchName:    branchName,
		status:        response.StatusCode,
		summary:       response.StatusMessage,
		completedAt:   completedAt,
	}, ""
}

// allowedTriggersForClass returns the set of trigger names that are permitted
// for the given infrastructure agent class. A nil return means all triggers
// are allowed (no class-level restriction). Currently:
//   - "commit" class: only STAGE_END is permitted
//   - All other classes: no restriction
func allowedTriggersForClass(class string) map[string]bool {
	switch class {
	case "commit":
		return map[string]bool{"STAGE_END": true}
	default:
		return nil
	}
}

// checkpointMarkerRe matches the [checkpoint:{sha}] marker pattern in a
// status_message. The sha is captured in group 1.
var checkpointMarkerRe = regexp.MustCompile(`\[checkpoint:([^\]]+)\]`)

// extractCheckpointRef scans statusMessage for a [checkpoint:{sha}] marker
// and returns the sha string. Returns "" when no marker is found.
func extractCheckpointRef(statusMessage string) string {
	m := checkpointMarkerRe.FindStringSubmatch(statusMessage)
	if m == nil {
		return ""
	}
	return m[1]
}

// lastInfraSeqInLog returns the Seq of the most recent execution log entry
// whose agent identifier matches agentName. Returns -1 when no matching entry
// is found (the agent has never been dispatched).
func lastInfraSeqInLog(agentName string, log []domain.ExecutionLogEntry) int {
	for i := len(log) - 1; i >= 0; i-- {
		if extractAgentIdentifier(log[i].Agent) == agentName {
			return log[i].Seq
		}
	}
	return -1
}

// infraTriggerFires reports whether the given trigger should fire after the
// workflow step described by completedStep and prevWorkflowStep.
//
// currentSeq is the global sequence after the most recent Store.Apply call
// (including any infra dispatches that have already completed during this
// evaluation pass). log is the current execution log, used for
// INVOCATION_INTERVAL interval arithmetic.
func infraTriggerFires(
	trigger domain.DeclaredInfraTrigger,
	currentSeq int,
	log []domain.ExecutionLogEntry,
	agentName string,
	completedStep domain.CompletedStep,
	prevWorkflowStep *domain.CompletedStep,
) bool {
	switch trigger.Trigger {
	case "INVOCATION_INTERVAL":
		param, convErr := strconv.Atoi(trigger.Param)
		if convErr != nil || param <= 0 {
			return false
		}
		lastSeq := lastInfraSeqInLog(agentName, log)
		if lastSeq < 0 {
			// No prior dispatch row: fires when global_sequence >= param.
			return currentSeq >= param
		}
		return (currentSeq - lastSeq) >= param

	case "STAGE_END":
		if prevWorkflowStep == nil {
			// First workflow step: no prior step to compare against.
			return false
		}
		return completedStep.Stage != prevWorkflowStep.Stage

	case "PHASE_END":
		if prevWorkflowStep == nil {
			return false
		}
		return completedStep.Phase != prevWorkflowStep.Phase

	case "MANUAL":
		// MANUAL triggers never fire automatically.
		return false

	default:
		return false
	}
}

// evaluateTriggers checks all declared infrastructure agent triggers against
// the current artifact state after a workflow step completes. Agents are
// evaluated in declaration order; each agent fires at most once per
// evaluation even if multiple triggers match.
//
// Dispatch is performed synchronously: each matching agent's invocation
// completes (including its Execution Log row via Store.Apply) before the
// next declared agent's triggers are evaluated.
//
// Returns (true, false, nil) when an on_failure=halt agent stops the run.
// Returns (false, false, non-nil) on unexpected infrastructure errors.
// Returns (false, true, nil) when a graceful stop is confirmed between two
// declared agents' dispatches within this evaluation pass; any agent that
// already fired and dispatched in this pass keeps its already-applied
// outcome, and no further agent in the pass is dispatched.
// Returns (false, false, nil) when all evaluations complete without a halt
// or a confirmed stop.
func (s *sessionImpl) evaluateTriggers(
	ctx context.Context,
	state *domain.ArtifactState,
	seq *int,
	completedStep domain.CompletedStep,
	prevWorkflowStep *domain.CompletedStep,
	declared []domain.DeclaredInfraAgent,
	config domain.RunConfig,
	activeAgents map[string]bool,
	orchDir string,
) (haltRun bool, stopRequested bool, err error) {
	for _, agent := range declared {
		// Restore-class agents are never dispatched by automatic trigger
		// evaluation; they act only on explicit manual instruction (MANUAL
		// trigger). The exclusion is by class so that any restore-class agent
		// (e.g. a future checkpoint-restore-s3) is automatically excluded
		// without a code change.
		if agent.Class == "restore" {
			continue
		}

		// activeAgents filter: when non-nil, only listed agents are evaluated.
		if activeAgents != nil && !activeAgents[agent.Name] {
			continue
		}

		// Activation gating: checkpoint-class agents require checkpoints to be
		// enabled for the run. Other classes are always active.
		if agent.Class == "checkpoint" && !config.Checkpoints {
			continue
		}

		// Check whether any declared trigger fires. An agent fires at most once
		// per evaluation pass even if multiple triggers match.
		fired := false
		for _, trigger := range agent.Triggers {
			if infraTriggerFires(trigger, *seq, state.ExecutionLog, agent.Name, completedStep, prevWorkflowStep) {
				fired = true
				break
			}
		}
		if !fired {
			continue
		}

		// Dispatch the infrastructure agent.
		infraSeq := *seq + 1
		agentRef := domain.AgentReference{
			Identifier:     agent.Name,
			DefinitionPath: filepath.Join(orchDir, agent.Name+".md"),
		}
		req := domain.ProtocolRequest{
			AgentInstanceID: fmt.Sprintf("%s#%d", agent.Name, infraSeq),
			RunID:           state.RunID,
			TaskDescription: fmt.Sprintf("infrastructure agent dispatch: %s", agent.Name),
		}

		// Graceful-stop checkpoint: any earlier agent in this pass that already
		// fired and dispatched keeps its already-applied outcome; only this
		// not-yet-dispatched agent (and any later declared agents) are skipped.
		if s.deps.StopRequested() {
			return false, true, nil
		}

		response, invokeErr := s.invokeAndLog(ctx, agentRef, req)
		if invokeErr != nil {
			if ctx.Err() != nil {
				return true, false, ctx.Err()
			}
			// Harness-level error: treat as non-SUCCESS and apply on_failure policy.
			response = domain.ProtocolResponse{
				AgentInstanceID: req.AgentInstanceID,
				StatusCode:      domain.StatusBLOCKED,
				StatusMessage:   invokeErr.Error(),
			}
		}

		// Extract a checkpoint content-reference from checkpoint-class responses.
		checkpoint := ""
		if agent.Class == "checkpoint" {
			checkpoint = extractCheckpointRef(response.StatusMessage)
		}

		// Record the completed infrastructure step in the Execution Log.
		infraStep := domain.CompletedStep{
			Seq:              infraSeq,
			AgentInstance:    req.AgentInstanceID,
			Phase:            completedStep.Phase,
			Stage:            completedStep.Stage,
			Status:           response.StatusCode,
			Summary:          response.StatusMessage,
			Timestamp:        s.deps.Clock.Now(),
			Checkpoint:       checkpoint,
			IsInfrastructure: true,
		}
		newState, applyErr := s.deps.Store.Apply(ctx, *state, infraStep)
		if applyErr != nil {
			return false, false, applyErr
		}
		*state = newState
		*seq = infraSeq

		// Apply on_failure policy for non-SUCCESS outcomes. Infrastructure
		// failures never enter the deviation resolver; they follow this path
		// exclusively.
		if response.StatusCode != domain.StatusSUCCESS {
			if agent.OnFailure == "halt" {
				return true, false, nil
			}
			// continue policy: record the failure and proceed.
		}
	}
	return false, false, nil
}

// validateAndApplyOverrides validates each infrastructure_overrides entry
// against the declared infrastructure agents, then applies replacement semantics:
// each override replaces the named agent's trigger list entirely.
//
// Returns an error if:
//   - An override names an agent not in declaredAgents (unknown agent name).
//   - An override specifies a trigger not allowed for the agent's class.
//
// Returns the modified declared-agent slice (with trigger lists replaced by
// any matching overrides). When overrides is empty, the input slice is returned
// unchanged.
func validateAndApplyOverrides(overrides []domain.InfrastructureOverride, declared []domain.DeclaredInfraAgent) ([]domain.DeclaredInfraAgent, error) {
	if len(overrides) == 0 {
		return declared, nil
	}

	// Build a lookup map of declared agents by name.
	agentByName := make(map[string]domain.DeclaredInfraAgent, len(declared))
	for _, a := range declared {
		agentByName[a.Name] = a
	}

	for _, ov := range overrides {
		agent, ok := agentByName[ov.AgentName]
		if !ok {
			return nil, fmt.Errorf("infrastructure_overrides: agent %q is not declared in the orchestrator file", ov.AgentName)
		}

		// Validate trigger restrictions for the agent's class.
		allowed := allowedTriggersForClass(agent.Class)
		if allowed != nil {
			for _, tr := range ov.Triggers {
				if !allowed[tr.Trigger] {
					return nil, fmt.Errorf(
						"infrastructure_overrides: trigger %q is not allowed for %s-class agent %q",
						tr.Trigger, agent.Class, ov.AgentName,
					)
				}
			}
		}
	}

	// Build a map of overrides by agent name for fast lookup.
	overrideMap := make(map[string][]domain.DeclaredInfraTrigger, len(overrides))
	for _, ov := range overrides {
		overrideMap[ov.AgentName] = ov.Triggers
	}

	// Apply replacement semantics: copy the slice and replace trigger lists.
	result := make([]domain.DeclaredInfraAgent, len(declared))
	copy(result, declared)
	for i := range result {
		if newTriggers, ok := overrideMap[result[i].Name]; ok {
			result[i].Triggers = newTriggers
		}
	}
	return result, nil
}
