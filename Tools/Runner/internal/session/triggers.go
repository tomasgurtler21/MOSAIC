package session

import (
	"context"
	"fmt"
	"strconv"

	"mosaic-run/internal/agentresolve"
	"mosaic-run/internal/domain"
	"mosaic-run/internal/engine"
)

// reviewConsultSignal carries the status message from the last successful
// review-class infrastructure agent in an evaluateTriggers pass. When
// non-nil, the caller must perform a routing consultation via consultRoute,
// supplying StatusMessage as last_status_message.
type reviewConsultSignal struct {
	StatusMessage string
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
// workflow step described by completedStep.
//
// currentSeq is the global sequence after the most recent Store.Apply call
// (including any infra dispatches that have already completed during this
// evaluation pass). log is the current execution log, used for
// INVOCATION_INTERVAL interval arithmetic.
//
// rowIdx is the routing table row index of the completed step. admitted and
// stages are the admitted workflow and stage set, used for prospective look-ahead
// in STAGE_END and PHASE_END evaluation.
func infraTriggerFires(
	trigger domain.DeclaredInfraTrigger,
	currentSeq int,
	log []domain.ExecutionLogEntry,
	agentName string,
	completedStep domain.CompletedStep,
	rowIdx int,
	admitted domain.AdmittedWorkflow,
	stages *domain.StageSet,
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
		// Prospective semantics: fires when the completed step is the last
		// step of its stage (look-ahead), not by comparing against a prior step.
		// STAGE_END only applies to EXECUTION-phase steps (Stage != "").
		if completedStep.Stage == "" {
			return false
		}
		_, stageNum, ok := domain.ParseStageValue(completedStep.Stage)
		if !ok || stageNum == 0 {
			return false
		}
		return engine.IsLastRowOfStage(admitted, stages, rowIdx, stageNum)

	case "PHASE_END":
		// Prospective semantics: fires when the completed step is the last
		// step of its phase (look-ahead), not by comparing against a prior step.
		_, stageNum, _ := domain.ParseStageValue(completedStep.Stage)
		return engine.IsLastRowOfPhase(admitted, stages, rowIdx, stageNum)

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
// Returns (true, false, nil, nil) when an on_failure=halt agent stops the run.
// Returns (false, false, nil, non-nil) on unexpected infrastructure errors.
// Returns (false, true, nil, nil) when a graceful stop is confirmed between two
// declared agents' dispatches within this evaluation pass.
// Returns (false, false, non-nil, nil) when all evaluations complete and at
// least one review-class agent succeeded.
// Returns (false, false, nil, nil) when all evaluations complete without a
// halt, stop, or successful review.
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
	rowIdx int,
	admitted domain.AdmittedWorkflow,
	stages *domain.StageSet,
) (haltRun bool, stopRequested bool, reviewConsult *reviewConsultSignal, err error) {
	var reviewSignal *reviewConsultSignal
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
			if infraTriggerFires(trigger, *seq, state.ExecutionLog, agent.Name, completedStep, rowIdx, admitted, stages) {
				fired = true
				break
			}
		}
		if !fired {
			continue
		}

		halt, stop, rev, trigErr := s.dispatchInfraAgent(ctx, state, seq, completedStep, agent, config, orchDir)
		if trigErr != nil {
			return false, false, nil, trigErr
		}
		if stop {
			return false, true, nil, nil
		}
		if halt {
			return true, false, nil, nil
		}
		if rev != nil {
			reviewSignal = rev
		}
		_ = prevWorkflowStep // kept for future use
	}
	return false, false, reviewSignal, nil
}

// dispatchInfraAgent dispatches a single infrastructure agent that has fired
// its trigger and records the result in the Execution Log. It returns
// (halt, stop, reviewSignal, err) matching the semantics of evaluateTriggers.
func (s *sessionImpl) dispatchInfraAgent(
	ctx context.Context,
	state *domain.ArtifactState,
	seq *int,
	completedStep domain.CompletedStep,
	agent domain.DeclaredInfraAgent,
	config domain.RunConfig,
	orchDir string,
) (haltRun bool, stopRequested bool, reviewSignal *reviewConsultSignal, err error) {
	infraSeq := *seq + 1
	req := domain.ProtocolRequest{
		AgentInstanceID: fmt.Sprintf("%s#%d", agent.Name, infraSeq),
		RunID:           state.RunID,
		TaskDescription: fmt.Sprintf("infrastructure agent dispatch: %s", agent.Name),
	}

	agentRef, resolveErr := agentresolve.ResolveOne(orchDir, agent.Name)
	var response domain.ProtocolResponse
	if resolveErr != nil {
		// Cannot locate the agent definition file; treat as non-SUCCESS and
		// apply the on_failure policy without dispatching.
		response = domain.ProtocolResponse{
			AgentInstanceID: req.AgentInstanceID,
			StatusCode:      domain.StatusBLOCKED,
			StatusMessage:   resolveErr.Error(),
		}
	} else {
		// Graceful-stop checkpoint before dispatching.
		if s.deps.StopRequested() {
			s.deps.Debug.Log(domain.EventSessionStopObserved, "graceful stop observed; not dispatching",
				domain.F("checkpoint", StopCheckpointInfraDispatch),
			)
			return false, true, nil, nil
		}

		var invokeErr error
		response, invokeErr = s.invokeAndLog(ctx, agentRef, req)
		if invokeErr != nil {
			if ctx.Err() != nil {
				return true, false, nil, ctx.Err()
			}
			// Harness-level error: treat as non-SUCCESS and apply on_failure policy.
			response = domain.ProtocolResponse{
				AgentInstanceID: req.AgentInstanceID,
				StatusCode:      domain.StatusBLOCKED,
				StatusMessage:   invokeErr.Error(),
			}
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
		return false, false, nil, applyErr
	}
	*state = newState
	*seq = infraSeq

	// Apply on_failure policy for non-SUCCESS outcomes.
	if response.StatusCode != domain.StatusSUCCESS {
		if agent.OnFailure == "halt" {
			return true, false, nil, nil
		}
		// continue policy: record the failure and proceed.
	} else if agent.Class == "review" {
		// Accumulate review signal: overwrite on each successful review so
		// the last successful review's message is used for the consultation.
		reviewSignal = &reviewConsultSignal{StatusMessage: response.StatusMessage}
	}
	return false, false, reviewSignal, nil
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
