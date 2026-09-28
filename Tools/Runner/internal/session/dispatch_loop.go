package session

import (
	"context"
	"fmt"
	"path/filepath"
	"strconv"

	"mosaic-common/interaction"
	"mosaic-run/internal/domain"
	"mosaic-run/internal/engine"
	"mosaic-run/internal/planstages"
)

// runDispatchLoop arms the ManualDispatch flag and executes the dispatch loop
// until the engine signals completion, stop, or an error occurs. All loop
// state is held in rs, which must have been fully populated by the run-start
// sequence before this function is called.
func (s *sessionImpl) runDispatchLoop(ctx context.Context, rs *runStartCtx) (domain.RunOutcome, error) {
	s.manualDispatchPending = rs.config.ManualDispatch

	for {
		decision := engine.Next(engine.NextInput{
			Workflow:            rs.admitted,
			Stages:              rs.stages,
			State:               rs.state,
			LastResponse:        rs.lastResponse,
			Agents:              rs.agents,
			Seq:                 rs.seq,
			Now:                 s.deps.Clock.Now(),
			RefreshedStages:     rs.refreshedStages,
			StageSource:         rs.stageSource,
			Mode:                rs.state.Mode,
			LastOutputArtifacts: rs.lastOutputArtifacts,
		})
		rs.refreshedStages = nil

		if decision.Dispatch != nil {
			out, done, _, err := s.handleEngineDispatch(ctx, rs, decision)
			if done {
				return out, err
			}
			continue
		}

		if decision.Complete != nil {
			return domain.RunOutcome{
				Status:  domain.RunCompleted,
				Message: "run completed successfully",
			}, nil
		}

		if decision.Consult != nil {
			out, done, err := s.handleEngineConsult(ctx, rs)
			if done {
				return out, err
			}
			continue
		}

		if decision.Deviation != nil {
			out, done, err := s.handleEngineDeviation(ctx, rs, decision)
			if done {
				return out, err
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

// handleEngineConsult handles an engine Consult decision by calling
// consultRoute and returning (out, done, err) to the dispatch loop.
func (s *sessionImpl) handleEngineConsult(ctx context.Context, rs *runStartCtx) (domain.RunOutcome, bool, error) {
	if s.deps.Routing == nil && !(s.manualDispatchPending && s.deps.Manual != nil) {
		return domain.RunOutcome{
			Status:  domain.RunFailed,
			Message: "orchestrated mode requires a RoutingConsultant but none was wired",
		}, true, nil
	}
	done, out, err := s.consultRoute(ctx, nil, &rs.state, &rs.seq,
		&rs.lastResponse, &rs.prevWorkflowStep, &rs.refreshedStages, &rs.stages,
		rs.table, rs.agents, rs.config, rs.declaredInfraAgents, rs.admitted, &rs.antiLoop)
	return out, done, err
}

// handleEngineDeviation handles an engine Deviation decision by calling
// consultRoute and returning (out, done, err) to the dispatch loop.
func (s *sessionImpl) handleEngineDeviation(ctx context.Context, rs *runStartCtx, decision domain.EngineDecision) (domain.RunOutcome, bool, error) {
	s.deps.Debug.Log(domain.EventSessionDeviation, "engine returned deviation",
		domain.F("kind", string(decision.Deviation.Info.Kind)),
	)
	if s.deps.Routing == nil && !(s.manualDispatchPending && s.deps.Manual != nil) {
		msg := fmt.Sprintf("deviation: no routing consultant configured (kind=%s)", decision.Deviation.Info.Kind)
		s.deps.Debug.Log(domain.EventSessionDeviationUnresolved, msg)
		return domain.RunOutcome{Status: domain.RunDeviationUnresolved, Message: msg}, true, nil
	}
	done, out, err := s.consultRoute(ctx, &decision.Deviation.Info, &rs.state, &rs.seq,
		&rs.lastResponse, &rs.prevWorkflowStep, &rs.refreshedStages, &rs.stages,
		rs.table, rs.agents, rs.config, rs.declaredInfraAgents, rs.admitted, &rs.antiLoop)
	return out, done, err
}

// handleEngineDispatch handles an engine Dispatch decision: prepares the
// request, invokes the harness (with raw-text bypass), runs HITL compliance
// verification, and applies the accepted response.
//
// Returns (out, done, cont, err): done=true means the run is terminal;
// cont=true means the outer loop should continue to the next engine.Next call.
func (s *sessionImpl) handleEngineDispatch(ctx context.Context, rs *runStartCtx, decision domain.EngineDecision) (domain.RunOutcome, bool, bool, error) {
	if len(decision.Dispatch.Steps) == 0 {
		return domain.RunOutcome{Status: domain.RunFailed, Message: "engine returned empty dispatch"}, true, false, nil
	}
	step := s.prepareAutoDispatchRequest(ctx, rs, decision.Dispatch.Steps[0])
	rs.antiLoop.recordDispatch(step.RowIndex, step.Agent.Identifier)

	if s.deps.StopRequested() {
		s.deps.Debug.Log(domain.EventSessionStopObserved, "graceful stop observed; not dispatching",
			domain.F("checkpoint", StopCheckpointEngineStep),
		)
		return domain.RunOutcome{Status: domain.RunStopped, Message: "run stopped: graceful stop confirmed"}, true, false, nil
	}

	response, invokeErr := s.invokeAndLog(ctx, step.Agent, step.Request)
	if invokeErr != nil {
		var bypassResp domain.ProtocolResponse
		var bypassStep domain.DispatchStep
		var done, cont bool
		var out domain.RunOutcome
		var err error
		bypassResp, bypassStep, done, cont, out, err = s.handleAutoHarnessErrorAndBypass(ctx, rs, step, invokeErr)
		if done || cont {
			return out, done, cont, err
		}
		// Bypass succeeded; use the bypass response and updated step for the HITL check.
		response = bypassResp
		step = bypassStep
	}

	result := s.runAutoDispatchHITL(ctx, rs, step, response)
	if result.done {
		return result.outcome, true, false, result.err
	}
	if !result.accepted {
		return domain.RunOutcome{}, false, true, nil
	}
	out, done, err := s.postAutoDispatchApply(ctx, rs, result.hitlStep, result.hitlResponse, result.hitlAttemptSeq)
	return out, done, false, err
}

// prepareAutoDispatchRequest fills in the auto-routed dispatch request fields:
// task description (with pre-consultation advice), RunID, and run-scoped
// artifact paths. It also logs the dispatch start and sends the progress notice.
func (s *sessionImpl) prepareAutoDispatchRequest(ctx context.Context, rs *runStartCtx, step domain.DispatchStep) domain.DispatchStep {
	if step.Request.TaskDescription == "" {
		step.Request.TaskDescription = domain.GenericTaskDescription
	}
	adv := rs.preConsultAdvice
	if adv.TaskDescription != "" {
		step.Request.TaskDescription += "\n\n" + adv.TaskDescription
	}
	if adv.Constraints != "" {
		if step.Request.Constraints != "" {
			step.Request.Constraints += "\n\n" + adv.Constraints
		} else {
			step.Request.Constraints = adv.Constraints
		}
	}
	step.Request.RunID = rs.state.RunID
	if rs.state.RunID != "" {
		folder := domain.RunScopedFolder(rs.state.RunID) + "/"
		step.Request.InputArtifacts = resolveToRunScoped(step.Request.InputArtifacts, folder)
		step.Request.OutputArtifacts = resolveToRunScoped(step.Request.OutputArtifacts, folder)
	}
	s.deps.Debug.Log(domain.EventSessionDispatchStart, "dispatching step",
		domain.F("agent", step.Request.AgentInstanceID),
		domain.F("phase", step.Phase),
		domain.F("stage", step.Stage),
		domain.F("row", strconv.Itoa(step.RowIndex)),
	)
	s.deps.Interact.Notify(ctx, interaction.Notice{
		Level:   interaction.NoticeInfo,
		Title:   step.Request.AgentInstanceID,
		Message: fmt.Sprintf("phase=%s stage=%q status=running", step.Phase, step.Stage),
	})
	return step
}

// handleAutoHarnessErrorAndBypass handles a harness-level error from an
// auto-routed dispatch. It logs the error, persists the failed attempt,
// attempts a raw-text bypass, and on bypass failure calls consultRoute.
//
// Returns: (response, step, done, cont, out, err). On bypass success, done and
// cont are both false and the returned response/step are the bypass values.
// On consultRoute handling, cont=true. On terminal outcome, done=true.
func (s *sessionImpl) handleAutoHarnessErrorAndBypass(ctx context.Context, rs *runStartCtx, step domain.DispatchStep, invokeErr error) (domain.ProtocolResponse, domain.DispatchStep, bool, bool, domain.RunOutcome, error) {
	if ctx.Err() != nil {
		return domain.ProtocolResponse{}, step, true, false, domain.RunOutcome{Status: domain.RunStopped, Message: "run stopped: context cancelled"}, nil
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
		ArtifactState: rs.state,
	}
	if s.deps.Routing == nil {
		msg := fmt.Sprintf("harness error: no routing consultant configured: %s", invokeErr.Error())
		s.deps.Debug.Log(domain.EventSessionDeviationUnresolved, msg)
		return domain.ProtocolResponse{}, step, true, false, domain.RunOutcome{Status: domain.RunDeviationUnresolved, Message: msg}, nil
	}
	// Persist a record of the failed dispatch attempt.
	failedStep := domain.CompletedStep{
		Seq:              rs.state.GlobalSequence + 1,
		AgentInstance:    step.Request.AgentInstanceID,
		Phase:            step.Phase,
		Stage:            step.Stage,
		Status:           domain.StatusBLOCKED,
		Summary:          invokeErr.Error(),
		Timestamp:        s.deps.Clock.Now(),
		Inputs:           formatInputs(step.Request.InputArtifacts),
		IsInfrastructure: true,
	}
	newState, applyErr := s.deps.Store.Apply(ctx, rs.state, failedStep)
	if applyErr != nil {
		s.deps.Debug.Log(domain.EventSessionApplyFailed, applyErr.Error())
		return domain.ProtocolResponse{}, step, true, false, domain.RunOutcome{Status: domain.RunFailed, Message: applyErr.Error()}, applyErr
	}
	rs.state = newState
	rs.seq = rs.state.GlobalSequence
	// Raw-text bypass: one direct retry before consulting the orchestrator.
	if isRawTextHarnessError(invokeErr) && rs.antiLoop.recordDispatch(step.RowIndex, step.Agent.Identifier) {
		bypassSeq := rs.state.GlobalSequence + 1
		bypassReq := step.Request
		bypassReq.AgentInstanceID = fmt.Sprintf("%s#%d", step.Agent.Identifier, bypassSeq)
		if bypassResp, bypassErr := s.invokeAndLog(ctx, step.Agent, bypassReq); bypassErr == nil {
			step.Request = bypassReq
			return bypassResp, step, false, false, domain.RunOutcome{}, nil
		}
	}
	done, out, outErr := s.consultRoute(ctx, &deviationInfo, &rs.state, &rs.seq,
		&rs.lastResponse, &rs.prevWorkflowStep, &rs.refreshedStages, &rs.stages,
		rs.table, rs.agents, rs.config, rs.declaredInfraAgents, rs.admitted, &rs.antiLoop)
	return domain.ProtocolResponse{}, step, done, true, out, outErr
}

// postAutoDispatchApply applies the HITL-accepted response to the artifact,
// sends the progress notice, evaluates infrastructure triggers, and performs
// stage-set re-derivation.
func (s *sessionImpl) postAutoDispatchApply(ctx context.Context, rs *runStartCtx, hitlStep domain.DispatchStep, hitlResp domain.ProtocolResponse, hitlAttemptSeq int) (domain.RunOutcome, bool, error) {
	completedStep := domain.CompletedStep{
		Seq:             hitlAttemptSeq,
		AgentInstance:   hitlStep.Request.AgentInstanceID,
		Phase:           hitlStep.Phase,
		Stage:           hitlStep.Stage,
		Status:          hitlResp.StatusCode,
		ErrorCode:       hitlResp.ErrorCode,
		Summary:         hitlResp.StatusMessage,
		Timestamp:       s.deps.Clock.Now(),
		Inputs:          formatInputs(hitlStep.Request.InputArtifacts),
		OutputArtifacts: hitlStep.Request.OutputArtifacts,
	}
	newState, err := s.deps.Store.Apply(ctx, rs.state, completedStep)
	if err != nil {
		s.deps.Debug.Log(domain.EventSessionApplyFailed, err.Error())
		return domain.RunOutcome{Status: domain.RunFailed, Message: err.Error()}, true, err
	}
	rs.state = newState
	s.deps.Debug.Log(domain.EventSessionStepDone, "step applied to artifact",
		domain.F("agent", completedStep.AgentInstance),
		domain.F("status", string(completedStep.Status)),
		domain.F("error_code", string(completedStep.ErrorCode)),
	)
	rs.seq = hitlAttemptSeq
	rs.lastResponse = &hitlResp
	rs.lastOutputArtifacts = hitlStep.Request.OutputArtifacts
	s.deps.Interact.Notify(ctx, interaction.Notice{
		Level:   interaction.NoticeInfo,
		Title:   completedStep.AgentInstance,
		Message: fmt.Sprintf("phase=%s stage=%q status=%s", completedStep.Phase, completedStep.Stage, string(completedStep.Status)),
	})
	orchDir := filepath.Dir(rs.config.OrchestratorFilePath)
	if !completedStep.IsInfrastructure {
		out, done, trigErr := s.postDispatchTriggers(ctx, rs, completedStep, hitlStep.RowIndex, orchDir)
		if done {
			return out, true, trigErr
		}
	}
	// Stage-* output re-derivation.
	if hasStageStarArtifact(hitlStep.Request.OutputArtifacts) {
		ss, ssErr := planstages.ReadStages(filepath.Join(rs.config.RunFolder, "Plan.md"), rs.admitted.GroupsDeclared)
		if ssErr != nil {
			s.deps.Interact.Notify(ctx, interaction.Notice{
				Level:   interaction.NoticeWarning,
				Message: fmt.Sprintf("failed to re-read stage set after Stage-* output: %v", ssErr),
			})
		} else {
			rs.refreshedStages = &ss
			rs.stages = &ss
		}
	}
	return domain.RunOutcome{}, false, nil
}

// postDispatchTriggers evaluates infrastructure-agent triggers after a
// completed workflow step. Returns (out, done, err) where done=true means the
// run is terminal (halt or stop).
func (s *sessionImpl) postDispatchTriggers(ctx context.Context, rs *runStartCtx, completedStep domain.CompletedStep, rowIdx int, orchDir string) (domain.RunOutcome, bool, error) {
	halt, trigStopped, reviewConsult, trigErr := s.evaluateTriggers(
		ctx, &rs.state, &rs.seq, completedStep, rs.prevWorkflowStep,
		rs.declaredInfraAgents, rs.config,
		buildActiveAgentsFilter(rs.declaredInfraAgents, rs.config.InfraClassSelections),
		orchDir, rowIdx, rs.admitted, rs.stages,
	)
	if trigErr != nil {
		if ctx.Err() != nil {
			return domain.RunOutcome{Status: domain.RunStopped, Message: "run stopped: context cancelled"}, true, nil
		}
		return domain.RunOutcome{Status: domain.RunFailed, Message: trigErr.Error()}, true, trigErr
	}
	if trigStopped {
		return domain.RunOutcome{Status: domain.RunStopped, Message: "run stopped: graceful stop confirmed"}, true, nil
	}
	if halt {
		return domain.RunOutcome{Status: domain.RunStopped, Message: "infrastructure agent halted the run"}, true, nil
	}
	cp := completedStep
	rs.prevWorkflowStep = &cp
	onInfrastructureAgentTrigger()
	if s.deps.OnInfrastructureTrigger != nil {
		s.deps.OnInfrastructureTrigger()
	}
	if reviewConsult != nil && s.deps.Routing != nil {
		syntheticResp := domain.ProtocolResponse{StatusMessage: reviewConsult.StatusMessage}
		rs.lastResponse = &syntheticResp
		done, out, consultErr := s.consultRoute(ctx, nil, &rs.state, &rs.seq,
			&rs.lastResponse, &rs.prevWorkflowStep, &rs.refreshedStages, &rs.stages,
			rs.table, rs.agents, rs.config, rs.declaredInfraAgents, rs.admitted, &rs.antiLoop)
		if consultErr != nil {
			return domain.RunOutcome{Status: domain.RunFailed, Message: consultErr.Error()}, true, consultErr
		}
		if done {
			return out, true, nil
		}
	}
	return domain.RunOutcome{}, false, nil
}
