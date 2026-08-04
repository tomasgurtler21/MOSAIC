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
//
//  2. Dispatch loop: ask engine → dispatch via harness → apply to artifact → repeat.
//
//  3. Special cases in the dispatch loop:
//     - On Findings auto-routing: engine returns Dispatch for COMPLETED_NEEDS_ACTION
//       with an unambiguous hint; session treats it identically to any other
//       Dispatch (harness → artifact update). No deviation resolver is invoked.
//     - Stage-* output re-derivation: after a completed row's output artifacts
//       include a Stage-* pattern, session re-reads the plan artifact via
//       planstages to obtain a refreshed stage set for the engine's next call.
//     - Deviation: engine returns Deviation; session invokes the deviation
//       resolver, re-reads the artifact from disk (FR-23), then resumes.
//     - Harness error: a harness-level failure is treated as a deviation with
//       kind DeviationHarnessError rather than a run failure ("never a crash"
//       per port contract). The deviation resolver decides whether to rejoin,
//       dispatch a custom agent, or stop.
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

	"mosaic-common/interaction"
	"mosaic-run/internal/agentresolve"
	"mosaic-run/internal/compat"
	"mosaic-run/internal/domain"
	"mosaic-run/internal/engine"
	"mosaic-run/internal/orchfile"
	"mosaic-run/internal/planstages"
	"mosaic-run/internal/seed"
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
	// Deviation resolves situations where the engine cannot decide.
	Deviation domain.DeviationResolver
	// Clock provides deterministic timestamps.
	Clock domain.Clock
	// Interact provides the user-interaction channel (progress events, etc.).
	Interact domain.Interaction
	// OnInfrastructureTrigger is an optional hook called after each harness
	// invocation (FR-40). If nil, no action is taken. In production this is the
	// named no-op; tests inject a counter function to verify the hook is invoked
	// exactly once per dispatch cycle (AC8.7).
	OnInfrastructureTrigger func()
}

// New creates a new Session with the given port dependencies.
//
// The returned session uses the following fixed-path dependencies from the
// runner's package set: orchfile, workflow, compat, agentresolve, planstages,
// and engine. These are not behind ports because they are pure functions or
// read-only loaders that impose no testability burden of their own.
func New(deps Deps) Session {
	return &sessionImpl{deps: deps}
}

// sessionImpl is the concrete implementation of Session.
type sessionImpl struct {
	deps Deps
}

// Start implements Session.
func (s *sessionImpl) Start(ctx context.Context, config domain.RunConfig) (domain.RunOutcome, error) {
	// =========================================================================
	// Run-start sequence (fixed order)
	// =========================================================================

	// Step 1: Load orchestrator file and get the selected workflow region.
	region, err := orchfile.GetWorkflow(config.OrchestratorFilePath, string(config.WorkflowID))
	if err != nil {
		return refusal(err.Error()), nil
	}

	// Step 2: Parse the routing table.
	table, err := workflow.Parse(region.Content, region.Info)
	if err != nil {
		return refusal(err.Error()), nil
	}

	// Step 3: Read existing artifact (FR-7a: refuse non-canonical; ErrNotExist = no artifact).
	existingState, readErr := s.deps.Store.Read(ctx)
	if readErr != nil {
		var refErr *domain.RefusalError
		if errors.As(readErr, &refErr) {
			// Non-canonical format: always refuse regardless of IsNewRun.
			return refusal(refErr.Error()), nil
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
			return refusal("run folder already contains an artifact; cannot create a new run here"), nil
		}
		// Expected: os.ErrNotExist — proceed to create below.
	} else {
		// Resume: refuse if no artifact exists (stale scan guard).
		if errors.Is(readErr, os.ErrNotExist) {
			return refusal("no artifact found at the resolved run folder; cannot resume"), nil
		}
		// Artifact exists — proceed to resume below.
	}

	// FR-7b: version check for resume mode.
	if !config.IsNewRun && !config.AllowVersionDrift {
		if existingState.WorkflowVersion != region.Info.Version {
			return refusal(fmt.Sprintf(
				"workflow version mismatch: artifact has %q, selected workflow has %q",
				existingState.WorkflowVersion, region.Info.Version,
			)), nil
		}
	}

	// Step 4: Admit the workflow (FR-18a compat checks and group resolution).
	admitted, err := compat.Admit(table)
	if err != nil {
		return refusal(err.Error()), nil
	}

	// Step 5: Resolve every agent identifier to a definition file.
	orchDir := filepath.Dir(config.OrchestratorFilePath)
	identifiers := uniqueAgentIdentifiers(table)
	agents, err := agentresolve.ResolveAll(orchDir, identifiers)
	if err != nil {
		return refusal(err.Error()), nil
	}

	// Step 6: Read stage set if the workflow has a staged EXECUTION phase.
	var stages *domain.StageSet
	if admitted.HasStagedPhase {
		planPath := filepath.Join(orchDir, "Plan.md")
		ss, stageErr := planstages.ReadStages(planPath, admitted.GroupsDeclared)
		if stageErr != nil {
			return refusal(stageErr.Error()), nil
		}
		stages = &ss
	}

	// Step 6b: Enumerate declared infrastructure agents from the orchestrator file.
	// This must happen before Step 7 (checkpoint refusal) so we can check for a
	// checkpoint-class agent. An empty slice is valid — no infrastructure agents deployed.
	declaredInfraAgents, err := orchfile.EnumerateInfrastructureAgents(config.OrchestratorFilePath)
	if err != nil {
		return refusal(err.Error()), nil
	}

	// Step 6c: Validate per-class agent selection. When multiple agents of the
	// same gated class are declared and no selection is provided in RunConfig,
	// refuse at run start (non-interactive CLI runs must supply --infra-class).
	if err := validateClassSelections(declaredInfraAgents, config.InfraClassSelections); err != nil {
		return refusal(err.Error()), nil
	}

	// Step 7: Settle checkpoints (FR-9). Refuse only when checkpoints are enabled
	// AND no checkpoint-class infrastructure agent is declared for this run.
	if config.Checkpoints && !hasCheckpointClassAgent(declaredInfraAgents) {
		return refusal("checkpoints requested but no checkpoint provider is available"), nil
	}

	// Step 7a: Build and validate the seed plan. New runs only: a resumed run
	// ignores SeedInputs entirely. Validation precedes Store.Create so an invalid
	// seed set leaves no run folder behind.
	var seedPlan seed.Plan
	if config.IsNewRun && len(config.SeedInputs) > 0 {
		p, seedErr := seed.BuildPlan(config.SeedInputs)
		if seedErr != nil {
			return refusal(seedErr.Error()), nil
		}
		seedPlan = p
	}

	// Step 8: Create or resume the artifact.
	var state domain.ArtifactState
	var seq int
	var lastResponse *domain.ProtocolResponse

	if config.IsNewRun {
		// New run: create a fresh artifact.
		state, err = s.deps.Store.Create(ctx, region.Info, config.Task, false, s.deps.Clock.Now(), config.RunID)
		if err != nil {
			return domain.RunOutcome{Status: domain.RunFailed, Message: err.Error()}, err
		}
		// Apply the seed plan into the run folder. If any copy fails, remove the
		// entire run folder (including Orchestration.md and already-copied files)
		// so no trace of the failed attempt remains. Only reachable on the new-run
		// path, after a Create this attempt performed.
		if applyErr := seed.Apply(seedPlan, config.RunFolder); applyErr != nil {
			if rmErr := os.RemoveAll(config.RunFolder); rmErr != nil {
				return refusal(fmt.Sprintf("%s; additionally, removing the run folder %s failed: %v",
					applyErr.Error(), config.RunFolder, rmErr)), nil
			}
			return refusal(applyErr.Error()), nil
		}
		seq = 0
	} else {
		// Resume: use the existing artifact (already validated above).
		state = existingState
		resume, resumeErr := engine.ResumePoint(admitted, stages, state)
		if resumeErr != nil {
			return domain.RunOutcome{Status: domain.RunFailed, Message: resumeErr.Error()}, resumeErr
		}
		seq = resume.Seq

		// FR-33: if the last step was interrupted, rewind CurrentState so that
		// engine.Next re-dispatches the interrupted row.
		if resume.RerunLast {
			state = rewindStateForRerun(state)
		}
	}

	// Step 8b: Validate and apply infrastructure_overrides from the artifact state.
	// Each override must name a declared infrastructure agent; unknown names are
	// a run-start refusal. Trigger restrictions are also validated per agent class.
	// The returned slice has replacement trigger lists applied (override semantics).
	declaredInfraAgents, err = validateAndApplyOverrides(state.InfrastructureOverrides, declaredInfraAgents)
	if err != nil {
		return refusal(err.Error()), nil
	}

	// =========================================================================
	// Dispatch loop
	// =========================================================================
	var refreshedStages *domain.StageSet
	// hitlOverride carries an explicit HITL value from a deviation rejoin
	// instruction to the next harness invocation (FR-20 / FR-24). The override
	// is applied once and then cleared; it is never originated by the session.
	var hitlOverride *bool
	// prevWorkflowStep tracks the most recently completed workflow step for
	// retrospective STAGE_END / PHASE_END trigger evaluation. Nil until the
	// first workflow step completes. Updated only for workflow steps, not for
	// infrastructure agent completions (no-cascades rule).
	var prevWorkflowStep *domain.CompletedStep

	for {
		decision := engine.Next(
			admitted,
			stages,
			state,
			lastResponse,
			agents,
			seq,
			s.deps.Clock.Now(),
			refreshedStages,
		)
		// Consume the refreshed stages once (they are only for one engine.Next call).
		refreshedStages = nil

		if decision.Dispatch != nil {
			if len(decision.Dispatch.Steps) == 0 {
				return domain.RunOutcome{Status: domain.RunFailed, Message: "engine returned empty dispatch"}, nil
			}
			step := decision.Dispatch.Steps[0]

			// Populate RunID from the artifact state and resolve artifact paths to
			// run-scoped form. The session derives RunID from state (not from
			// RunConfig) so that resumed runs carry the RunID minted at creation.
			step.Request.RunID = state.RunID
			if state.RunID != "" {
				folder := domain.RunScopedFolder(state.RunID) + "/"
				step.Request.InputArtifacts = resolveToRunScoped(step.Request.InputArtifacts, folder)
				step.Request.OutputArtifacts = resolveToRunScoped(step.Request.OutputArtifacts, folder)
			}

			// Apply the HITL override from a prior deviation rejoin instruction
			// (FR-24 / FR-20). The override is for this one invocation only.
			if hitlOverride != nil {
				step.Request.HumanInTheLoop = *hitlOverride
				hitlOverride = nil
			}

			// Notify the interaction port that a step is starting. The TUI frontend
			// uses this to append a new progress row before the invocation blocks.
			s.deps.Interact.Notify(ctx, interaction.Notice{
				Level:   interaction.NoticeInfo,
				Title:   step.Request.AgentInstanceID,
				Message: fmt.Sprintf("phase=%s stage=%q status=running", step.Phase, step.Stage),
			})

			// Invoke the harness.
			response, invokeErr := s.deps.Harness.Invoke(ctx, step.Agent, step.Request)
			if invokeErr != nil {
				// Context cancellation: graceful stop.
				if ctx.Err() != nil {
					return domain.RunOutcome{
						Status:  domain.RunStopped,
						Message: "run stopped: context cancelled",
					}, nil
				}
				// Harness error: treat as a deviation rather than a run failure
				// ("the caller treats this as a deviation, never a crash" per the
				// HarnessAdapter port contract). Invoke the deviation resolver to
				// decide whether to rejoin, dispatch a custom agent, or stop.
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
				instr, resolveErr := s.deps.Deviation.Resolve(ctx, deviationInfo)
				if resolveErr != nil {
					return domain.RunOutcome{Status: domain.RunDeviationUnresolved, Message: resolveErr.Error()}, nil
				}
				done, outcome, outErr := s.applyRejoinInstruction(ctx, instr, &state, &hitlOverride, &lastResponse, table)
				if done {
					return outcome, outErr
				}
				continue
			}

			// Apply the completed step to the artifact.
			completedSeq := seq + 1
			completedStep := domain.CompletedStep{
				Seq:             completedSeq,
				AgentInstance:   step.Request.AgentInstanceID,
				Phase:           step.Phase,
				Stage:           step.Stage,
				Status:          response.StatusCode,
				ErrorCode:       response.ErrorCode,
				Summary:         response.StatusMessage,
				Timestamp:       s.deps.Clock.Now(),
				Inputs:          formatInputs(step.Request.InputArtifacts),
				OutputArtifacts: step.Request.OutputArtifacts,
			}
			state, err = s.deps.Store.Apply(ctx, state, completedStep)
			if err != nil {
				return domain.RunOutcome{Status: domain.RunFailed, Message: err.Error()}, err
			}
			seq = completedSeq
			lastResponse = &response

			// Report per-step completion via the Interaction port (FR-5).
			// The CLI frontend writes this as a machine-readable line; the TUI
			// frontend can render it differently. Both use the same notice format:
			// Title = agent instance ID, Message = structured key=value pairs.
			s.deps.Interact.Notify(ctx, interaction.Notice{
				Level:   interaction.NoticeInfo,
				Title:   completedStep.AgentInstance,
				Message: fmt.Sprintf("phase=%s stage=%q status=%s", completedStep.Phase, completedStep.Stage, string(completedStep.Status)),
			})

			// Infrastructure-agent trigger evaluation (FR-40).
			// Only workflow steps (IsInfrastructure=false) trigger evaluation;
			// infrastructure step completions skip this block entirely (no-cascades
			// rule). The named no-op and injected test hook are called once per
			// workflow step dispatch cycle, matching the pre-existing contract.
			if !completedStep.IsInfrastructure {
				// Save the workflow step's CurrentState so the engine sees the
				// correct last-workflow-agent after any infra dispatches complete.
				// Infra rows update CurrentState (via Store.Apply), which would
				// confuse engine.Next's row-lookup; restoring after evaluation
				// keeps the engine's view consistent with the workflow routing table.
				savedCurrentState := state.CurrentState

				halt, trigErr := s.evaluateTriggers(
					ctx, &state, &seq, completedStep, prevWorkflowStep,
					declaredInfraAgents, config,
					buildActiveAgentsFilter(declaredInfraAgents, config.InfraClassSelections),
					orchDir,
				)

				// Restore the workflow step's CurrentState so engine.Next can
				// locate the correct row on its next call.
				state.CurrentState = savedCurrentState

				if trigErr != nil {
					if ctx.Err() != nil {
						return domain.RunOutcome{Status: domain.RunStopped, Message: "run stopped: context cancelled"}, nil
					}
					return domain.RunOutcome{Status: domain.RunFailed, Message: trigErr.Error()}, trigErr
				}
				if halt {
					return domain.RunOutcome{Status: domain.RunStopped, Message: "infrastructure agent halted the run"}, nil
				}

				// Update prevWorkflowStep after all infra agents have been
				// evaluated for this workflow step completion.
				cp := completedStep
				prevWorkflowStep = &cp

				// Named no-op anchor point (FR-40) plus injected test hook.
				onInfrastructureAgentTrigger()
				if s.deps.OnInfrastructureTrigger != nil {
					s.deps.OnInfrastructureTrigger()
				}
			}

			// Stage-* output re-derivation: re-read Plan.md after any row whose
			// output artifacts contain Stage-* so the engine can expand wildcards
			// on the next row.
			if hasStageStarArtifact(step.Request.OutputArtifacts) {
				planPath := filepath.Join(orchDir, "Plan.md")
				ss, ssErr := planstages.ReadStages(planPath, admitted.GroupsDeclared)
				if ssErr != nil {
					s.deps.Interact.Notify(ctx, interaction.Notice{
						Level:   interaction.NoticeWarning,
						Message: fmt.Sprintf("failed to re-read stage set after Stage-* output: %v", ssErr),
					})
				} else {
					refreshedStages = &ss
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

		if decision.Deviation != nil {
			// Invoke the deviation resolver.
			instr, resolveErr := s.deps.Deviation.Resolve(ctx, decision.Deviation.Info)
			if resolveErr != nil {
				return domain.RunOutcome{Status: domain.RunDeviationUnresolved, Message: resolveErr.Error()}, nil
			}
			done, outcome, outErr := s.applyRejoinInstruction(ctx, instr, &state, &hitlOverride, &lastResponse, table)
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

// applyRejoinInstruction applies a RejoinInstruction after deviation resolution.
// It re-reads the artifact from disk (FR-23), then either:
//   - Stops the run (Stop field set).
//   - Adjusts state to re-enter at the specified row (Rejoin field set), carrying
//     any HITL override for the next invocation (FR-20 / FR-24).
//   - Executes a custom harness invocation then adjusts state (Custom field set).
//
// Returns done=true with an outcome when the run should terminate, or done=false
// when the dispatch loop should continue.
func (s *sessionImpl) applyRejoinInstruction(
	ctx context.Context,
	instr domain.RejoinInstruction,
	state *domain.ArtifactState,
	hitlOverride **bool,
	lastResponse **domain.ProtocolResponse,
	table domain.RoutingTable,
) (done bool, outcome domain.RunOutcome, outErr error) {
	// FR-23: re-read the artifact from disk. The orchestrator delegate may
	// have updated it out-of-band during resolution.
	if freshState, freshReadErr := s.deps.Store.Read(ctx); freshReadErr == nil {
		*state = freshState
	}

	if instr.Stop != nil {
		return true, domain.RunOutcome{
			Status:  domain.RunDeviationUnresolved,
			Message: "deviation resolver returned stop: " + instr.Stop.Reason,
		}, nil
	}

	if instr.Rejoin != nil {
		// Carry the HITL override to the next dispatch cycle (FR-24 / FR-20).
		// It is never originated here — only carried from an explicit instruction.
		*hitlOverride = instr.Rejoin.HITLOverride
		*state = applyRejoinAtRow(instr.Rejoin.RowIndex, *state, table)
		*lastResponse = nil
		return false, domain.RunOutcome{}, nil
	}

	if instr.Custom != nil {
		// Custom dispatch: execute a harness invocation that is not in the routing
		// table, then rejoin at the specified row. If Agent is unset (schema gap),
		// the custom invocation cannot be performed and the run stops.
		if instr.Custom.Agent.Identifier == "" {
			return true, domain.RunOutcome{
				Status:  domain.RunDeviationUnresolved,
				Message: "custom dispatch: agent not specified (custom_agent missing from orchestrator instruction)",
			}, nil
		}
		req := instr.Custom.Request
		if instr.Custom.HITLOverride != nil {
			req.HumanInTheLoop = *instr.Custom.HITLOverride
		}
		_, invokeErr := s.deps.Harness.Invoke(ctx, instr.Custom.Agent, req)
		if invokeErr != nil {
			if ctx.Err() != nil {
				return true, domain.RunOutcome{
					Status:  domain.RunStopped,
					Message: "run stopped: context cancelled during custom dispatch",
				}, nil
			}
			return true, domain.RunOutcome{
				Status:  domain.RunFailed,
				Message: "custom dispatch harness error: " + invokeErr.Error(),
			}, invokeErr
		}
		*state = applyRejoinAtRow(instr.Custom.RejoinRow, *state, table)
		*lastResponse = nil
		return false, domain.RunOutcome{}, nil
	}

	return true, domain.RunOutcome{
		Status:  domain.RunDeviationUnresolved,
		Message: "deviation resolver returned empty instruction",
	}, nil
}

// applyRejoinAtRow adjusts the artifact CurrentState so that engine.Next
// dispatches from the specified routing table row on the next call.
//
// For row 0 (first row), CurrentState is cleared to trigger initial dispatch.
// For row N > 0, the function searches the execution log for the last completed
// step whose agent matches the routing table row at index N-1. If found, that
// entry becomes CurrentState so the engine routes forward to row N. If no such
// entry exists (e.g. the target precedes any recorded step), CurrentState is
// cleared to restart from row 0.
//
// This supports arbitrary row jumps from the deviation resolver — not just
// re-running the most recently deviating row.
func applyRejoinAtRow(targetRowIdx int, state domain.ArtifactState, table domain.RoutingTable) domain.ArtifactState {
	if targetRowIdx == 0 {
		state.CurrentState = domain.CurrentState{}
		return state
	}

	// Find the agent identifier for the row that precedes targetRowIdx.
	prevRow, found := rowAtIndex(table, targetRowIdx-1)
	if !found {
		// Unknown preceding row: cannot determine correct CurrentState.
		state.CurrentState = domain.CurrentState{}
		return state
	}
	prevAgentID := prevRow.Agent

	// Search the execution log from the end for the last entry produced by
	// the preceding row. Entry agents are stored as instance IDs ("{id}#{seq}");
	// we match by extracting the identifier prefix.
	for i := len(state.ExecutionLog) - 1; i >= 0; i-- {
		entry := state.ExecutionLog[i]
		if extractAgentIdentifier(entry.Agent) == prevAgentID {
			state.CurrentState = domain.CurrentState{
				Phase:      entry.Phase,
				Stage:      entry.Stage,
				LastStatus: domain.StatusSUCCESS,
				LastAgent:  entry.Agent,
			}
			return state
		}
	}

	// No matching log entry: clear CurrentState so the engine starts from the
	// beginning (the deviation resolver is directing a rejoin earlier than any
	// recorded step).
	state.CurrentState = domain.CurrentState{}
	return state
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
// the second-to-last log entry (or empty) causes engine.Next to route to the
// interrupted row again.
func rewindStateForRerun(state domain.ArtifactState) domain.ArtifactState {
	if len(state.ExecutionLog) <= 1 {
		state.CurrentState = domain.CurrentState{}
		return state
	}
	prev := state.ExecutionLog[len(state.ExecutionLog)-2]
	state.CurrentState = domain.CurrentState{
		Phase:      prev.Phase,
		Stage:      prev.Stage,
		LastStatus: domain.StatusSUCCESS,
		LastAgent:  prev.Agent,
	}
	return state
}

// refusal constructs a RunOutcome with RunRefused status and a message.
func refusal(message string) domain.RunOutcome {
	return domain.RunOutcome{
		Status:  domain.RunRefused,
		Message: message,
	}
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
// Returns (true, nil) when an on_failure=halt agent stops the run.
// Returns (false, non-nil) on unexpected infrastructure errors.
// Returns (false, nil) when all evaluations complete without a halt.
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
) (haltRun bool, err error) {
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

		response, invokeErr := s.deps.Harness.Invoke(ctx, agentRef, req)
		if invokeErr != nil {
			if ctx.Err() != nil {
				return true, ctx.Err()
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
			return false, applyErr
		}
		*state = newState
		*seq = infraSeq

		// Apply on_failure policy for non-SUCCESS outcomes. Infrastructure
		// failures never enter the deviation resolver; they follow this path
		// exclusively.
		if response.StatusCode != domain.StatusSUCCESS {
			if agent.OnFailure == "halt" {
				return true, nil
			}
			// continue policy: record the failure and proceed.
		}
	}
	return false, nil
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
