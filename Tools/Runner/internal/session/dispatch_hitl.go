package session

import (
	"context"
	"fmt"
	"path/filepath"

	"mosaic-common/interaction"
	"mosaic-run/internal/domain"
	"mosaic-run/internal/planstages"
)

// autoHITLResult carries the outcome of the runAutoDispatchHITL loop.
// When accepted is true, the outer caller should apply hitlStep/hitlResponse.
// When done is true, the run is terminal. When neither accepted nor done is
// set, consultRoute handled the non-compliance and the outer loop should
// continue via the dispatch loop's cont path.
type autoHITLResult struct {
	accepted       bool
	done           bool
	outcome        domain.RunOutcome
	err            error
	hitlStep       domain.DispatchStep
	hitlResponse   domain.ProtocolResponse
	hitlAttemptSeq int
	written        []string

	// routed is the original attempt's status and error code when a
	// gate-discharging re-dispatch returned SUCCESS; nil otherwise.
	// routedResp is that original response, which routing and the
	// consultation's last status message follow.
	routed     *domain.RoutedOutcome
	routedResp *domain.ProtocolResponse
}

// runAutoDispatchHITL runs the HITL compliance check loop for an auto-routed
// dispatch. It performs pre-HITL stage re-derivation, loops over compliance
// checks, and dispatches to the redispatch or escalate handlers as needed.
func (s *sessionImpl) runAutoDispatchHITL(ctx context.Context, rs *runStartCtx, step domain.DispatchStep, response domain.ProtocolResponse) autoHITLResult {
	hitlAttemptSeq := rs.state.GlobalSequence + 1
	hitlRedispatchUsed := false
	hitlStep := step
	hitlResponse := response
	var original *domain.ProtocolResponse

	// Pre-HITL stage set re-derivation for self-referential rows.
	if rs.stages == nil && hasStageStarArtifact(hitlStep.Request.OutputArtifacts) {
		earlyPlanPath := filepath.Join(rs.config.RunFolder, "Plan.md")
		ss, ssErr := planstages.ReadStages(earlyPlanPath, rs.admitted.GroupsDeclared)
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

	for {
		written := s.writtenOutputs(ctx, rs.stages)
		approvals := s.readApprovals(ctx, written)
		hitlDec := domain.DecideHITLCompliance(domain.HITLComplianceInput{
			EffectiveHITL:  hitlStep.EffectiveHITL,
			Status:         hitlResponse.StatusCode,
			ErrorCode:      hitlResponse.ErrorCode,
			Approvals:      approvals,
			RedispatchUsed: hitlRedispatchUsed,
		})

		switch hitlDec.Outcome {
		case domain.HITLAccept:
			res := autoHITLResult{
				accepted:       true,
				hitlStep:       hitlStep,
				hitlResponse:   hitlResponse,
				hitlAttemptSeq: hitlAttemptSeq,
				written:        written,
			}
			if original != nil {
				if routed := routedAfterRedispatch(*original, hitlResponse); routed != nil {
					res.routed = routed
					res.routedResp = original
				}
			}
			return res

		case domain.HITLRedispatch:
			hitlRedispatchUsed = true
			if original == nil {
				first := hitlResponse
				original = &first
			}
			updated, rdDone, rdCont, rdOut, rdErr := s.handleAutoHITLRedispatch(ctx, rs, hitlStep, hitlResponse, hitlAttemptSeq)
			if rdErr != nil || rdDone {
				return autoHITLResult{done: true, outcome: rdOut, err: rdErr}
			}
			if rdCont {
				// consultRoute handled it; outer dispatch loop must continue.
				return autoHITLResult{}
			}
			// Bypass or redispatch succeeded; continue loop with new state.
			hitlStep = updated.step
			hitlResponse = updated.response
			hitlAttemptSeq = updated.hitlAttemptSeq

		case domain.HITLEscalate:
			escDone, escOut, escErr := s.handleAutoHITLEscalate(ctx, rs, hitlStep, hitlAttemptSeq, hitlResponse)
			// Both done and !done paths end the loop: done means terminal,
			// !done means consultRoute returned done=false so outer loop continues.
			return autoHITLResult{done: escDone, outcome: escOut, err: escErr}
		}
	}
}

// hitlRedispatchState carries the output of a successful handleAutoHITLRedispatch
// call: the updated step (with new AgentInstanceID), the response from the
// redispatch invocation, and the new hitlAttemptSeq.
type hitlRedispatchState struct {
	step           domain.DispatchStep
	response       domain.ProtocolResponse
	hitlAttemptSeq int
}

// handleAutoHITLRedispatch handles the HITLRedispatch case for an auto-routed
// dispatch. It persists the rejected attempt, checks the anti-loop guard,
// optionally stops, invokes the harness for the redispatch, and handles any
// redispatch error.
//
// Returns (updated, done, cont, out, err):
//   - done=true: run is terminal.
//   - cont=true: consultRoute handled it; outer dispatch loop continues.
//   - Otherwise: updated carries the new step/response/seq; loop should continue.
func (s *sessionImpl) handleAutoHITLRedispatch(ctx context.Context, rs *runStartCtx, hitlStep domain.DispatchStep, hitlResponse domain.ProtocolResponse, hitlAttemptSeq int) (hitlRedispatchState, bool, bool, domain.RunOutcome, error) {
	// Persist the rejected attempt.
	rejStep := domain.CompletedStep{
		Seq:              hitlAttemptSeq,
		AgentInstance:    hitlStep.Request.AgentInstanceID,
		Phase:            hitlStep.Phase,
		Stage:            hitlStep.Stage,
		WorkflowRow:      domain.WorkflowRowFromIndex(hitlStep.RowIndex),
		Status:           hitlResponse.StatusCode,
		ErrorCode:        hitlResponse.ErrorCode,
		Summary:          hitlResponse.StatusMessage,
		Timestamp:        s.deps.Clock.Now(),
		Inputs:           formatInputs(hitlStep.Request.InputArtifacts),
		IsInfrastructure: true,
		HITLRejected:     true,
	}
	newState, err := s.deps.Store.Apply(ctx, rs.state, rejStep)
	if err != nil {
		s.deps.Debug.Log(domain.EventSessionApplyFailed, err.Error())
		return hitlRedispatchState{}, true, false, domain.RunOutcome{Status: domain.RunFailed, Message: err.Error()}, err
	}
	rs.state = newState
	rs.seq = rs.state.GlobalSequence
	hitlAttemptSeq++

	// Anti-loop guard before redispatch.
	if !rs.antiLoop.recordDispatch(hitlStep.RowIndex, hitlStep.Agent.Identifier) {
		if stopOut, stop := s.guardEscalationExhausted(&rs.antiLoop, hitlStep.Agent.Identifier, hitlStep.RowIndex); stop {
			return hitlRedispatchState{}, true, false, stopOut, nil
		}
		s.deps.Debug.Log(domain.EventSessionHITLEscalate, "anti-loop guard triggered in HITL redispatch; escalating",
			domain.F("agent", hitlStep.Agent.Identifier),
		)
		if s.deps.Routing == nil {
			alMsg := "anti-loop guard: no routing consultant configured"
			s.deps.Debug.Log(domain.EventSessionDeviationUnresolved, alMsg)
			return hitlRedispatchState{}, true, false, domain.RunOutcome{Status: domain.RunDeviationUnresolved, Message: alMsg}, nil
		}
		alDevInfo := domain.DeviationInfo{
			Kind: domain.DeviationNonSuccess, Response: hitlResponse,
			CurrentRow: hitlStep.RowIndex, CurrentPhase: hitlStep.Phase, CurrentStage: hitlStep.Stage,
			ArtifactState: rs.state,
		}
		done, out, outErr := s.consultRoute(ctx, &alDevInfo, &rs.state, &rs.seq,
			&rs.lastResponse, &rs.prevWorkflowStep, &rs.refreshedStages, &rs.stages,
			rs.table, rs.agents, rs.config, rs.declaredInfraAgents, rs.admitted, &rs.antiLoop)
		return hitlRedispatchState{}, done, true, out, outErr
	}

	s.deps.Debug.Log(domain.EventSessionHITLRedispatch, "HITL non-compliant; redispatching same agent",
		domain.F("agent", hitlStep.Agent.Identifier),
	)
	rdReq := hitlStep.Request
	rdReq.AgentInstanceID = fmt.Sprintf("%s#%d", hitlStep.Agent.Identifier, hitlAttemptSeq)
	hitlStep.Request = rdReq

	// Graceful-stop checkpoint before redispatch.
	if s.deps.StopRequested() {
		s.deps.Debug.Log(domain.EventSessionStopObserved, "graceful stop observed; not dispatching",
			domain.F("checkpoint", StopCheckpointEngineHITLRedispatch),
		)
		return hitlRedispatchState{}, true, false, domain.RunOutcome{Status: domain.RunStopped, Message: "run stopped: graceful stop confirmed"}, nil
	}

	rdResp, rdErr := s.invokeAndLog(ctx, hitlStep.Agent, hitlStep.Request)
	if rdErr != nil {
		return s.handleAutoHITLRedispatchError(ctx, rs, hitlStep, rdErr, hitlAttemptSeq)
	}
	return hitlRedispatchState{step: hitlStep, response: rdResp, hitlAttemptSeq: hitlAttemptSeq}, false, false, domain.RunOutcome{}, nil
}

// handleAutoHITLRedispatchError handles a harness error during a HITL
// redispatch. It persists the failure record, tries a raw-text bypass,
// and on bypass failure calls consultRoute.
func (s *sessionImpl) handleAutoHITLRedispatchError(ctx context.Context, rs *runStartCtx, hitlStep domain.DispatchStep, rdErr error, hitlAttemptSeq int) (hitlRedispatchState, bool, bool, domain.RunOutcome, error) {
	if ctx.Err() != nil {
		return hitlRedispatchState{}, true, false, domain.RunOutcome{Status: domain.RunStopped, Message: "run stopped: context cancelled"}, nil
	}
	s.deps.Debug.Log(domain.EventSessionHarnessError, rdErr.Error(),
		domain.F("agent", hitlStep.Request.AgentInstanceID),
	)
	rdFailedStep := domain.CompletedStep{
		Seq:           rs.state.GlobalSequence + 1,
		AgentInstance: hitlStep.Request.AgentInstanceID,
		Phase:         hitlStep.Phase,
		Stage:         hitlStep.Stage,
		WorkflowRow:   domain.WorkflowRowFromIndex(hitlStep.RowIndex),
		Status:        domain.StatusBLOCKED,
		ErrorCode:     domain.ErrorTOOL_UNAVAILABLE,
		Summary:       rdErr.Error(),
		Timestamp:     s.deps.Clock.Now(),
		Inputs:        formatInputs(hitlStep.Request.InputArtifacts),
	}
	newState, applyErr := s.deps.Store.Apply(ctx, rs.state, rdFailedStep)
	if applyErr != nil {
		s.deps.Debug.Log(domain.EventSessionApplyFailed, applyErr.Error())
		return hitlRedispatchState{}, true, false, domain.RunOutcome{Status: domain.RunFailed, Message: applyErr.Error()}, applyErr
	}
	rs.state = newState
	rs.seq = rs.state.GlobalSequence
	// Raw-text bypass within HITL redispatch.
	if isRawTextHarnessError(rdErr) && rs.antiLoop.recordDispatch(hitlStep.RowIndex, hitlStep.Agent.Identifier) {
		bypassSeq := rs.state.GlobalSequence + 1
		bypassReq := hitlStep.Request
		bypassReq.AgentInstanceID = fmt.Sprintf("%s#%d", hitlStep.Agent.Identifier, bypassSeq)
		if bypassResp, bypassErr := s.invokeAndLog(ctx, hitlStep.Agent, bypassReq); bypassErr == nil {
			hitlStep.Request = bypassReq
			return hitlRedispatchState{step: hitlStep, response: bypassResp, hitlAttemptSeq: bypassSeq}, false, false, domain.RunOutcome{}, nil
		}
	}
	rdResp := domain.HarnessErrorResponse(hitlStep.Request.AgentInstanceID, rs.config.RunID, rdErr)
	rdDevInfo := domain.DeviationInfo{
		Kind:       domain.DeviationNonSuccess,
		Response:   rdResp,
		CurrentRow: hitlStep.RowIndex, CurrentPhase: hitlStep.Phase, CurrentStage: hitlStep.Stage,
		ArtifactState: rs.state,
	}
	if s.deps.Routing == nil {
		rdMsg := fmt.Sprintf("HITL redispatch harness error: no routing consultant configured: %s", rdErr.Error())
		s.deps.Debug.Log(domain.EventSessionDeviationUnresolved, rdMsg)
		return hitlRedispatchState{}, true, false, domain.RunOutcome{Status: domain.RunDeviationUnresolved, Message: rdMsg}, nil
	}
	rs.lastResponse = &rdResp
	done, out, outErr := s.consultRoute(ctx, &rdDevInfo, &rs.state, &rs.seq,
		&rs.lastResponse, &rs.prevWorkflowStep, &rs.refreshedStages, &rs.stages,
		rs.table, rs.agents, rs.config, rs.declaredInfraAgents, rs.admitted, &rs.antiLoop)
	return hitlRedispatchState{}, done, true, out, outErr
}

// handleAutoHITLEscalate handles the HITLEscalate case: persists the final
// rejected attempt and escalates to consultRoute.
func (s *sessionImpl) handleAutoHITLEscalate(ctx context.Context, rs *runStartCtx, hitlStep domain.DispatchStep, hitlAttemptSeq int, hitlResponse domain.ProtocolResponse) (bool, domain.RunOutcome, error) {
	escRejStep := domain.CompletedStep{
		Seq:              hitlAttemptSeq,
		AgentInstance:    hitlStep.Request.AgentInstanceID,
		Phase:            hitlStep.Phase,
		Stage:            hitlStep.Stage,
		WorkflowRow:      domain.WorkflowRowFromIndex(hitlStep.RowIndex),
		Status:           hitlResponse.StatusCode,
		ErrorCode:        hitlResponse.ErrorCode,
		Summary:          hitlResponse.StatusMessage,
		Timestamp:        s.deps.Clock.Now(),
		Inputs:           formatInputs(hitlStep.Request.InputArtifacts),
		IsInfrastructure: true,
		HITLRejected:     true,
	}
	newState, err := s.deps.Store.Apply(ctx, rs.state, escRejStep)
	if err != nil {
		s.deps.Debug.Log(domain.EventSessionApplyFailed, err.Error())
		return true, domain.RunOutcome{Status: domain.RunFailed, Message: err.Error()}, err
	}
	rs.state = newState
	rs.seq = rs.state.GlobalSequence
	s.deps.Debug.Log(domain.EventSessionHITLEscalate, "HITL redispatch exhausted; escalating to deviation",
		domain.F("agent", hitlStep.Agent.Identifier),
	)
	escDevInfo := domain.DeviationInfo{
		Kind:          domain.DeviationNonSuccess,
		Response:      hitlResponse,
		CurrentRow:    hitlStep.RowIndex,
		CurrentPhase:  hitlStep.Phase,
		CurrentStage:  hitlStep.Stage,
		ArtifactState: rs.state,
	}
	if s.deps.Routing == nil {
		escMsg := "HITL escalation: no routing consultant configured"
		s.deps.Debug.Log(domain.EventSessionDeviationUnresolved, escMsg)
		return true, domain.RunOutcome{Status: domain.RunDeviationUnresolved, Message: escMsg}, nil
	}
	done, out, outErr := s.consultRoute(ctx, &escDevInfo, &rs.state, &rs.seq,
		&rs.lastResponse, &rs.prevWorkflowStep, &rs.refreshedStages, &rs.stages,
		rs.table, rs.agents, rs.config, rs.declaredInfraAgents, rs.admitted, &rs.antiLoop)
	return done, out, outErr
}
