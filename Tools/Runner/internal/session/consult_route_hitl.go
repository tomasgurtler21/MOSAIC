package session

import (
	"context"
	"fmt"
	"path/filepath"

	"mosaic-common/interaction"
	"mosaic-run/internal/domain"
	"mosaic-run/internal/planstages"
)

// runConsultHITL runs the HITL compliance check loop for a consultant-routed
// dispatch. It performs pre-HITL stage re-derivation, loops over compliance
// checks, and calls consultHITLRedispatch or consultHITLEscalate as needed.
//
// Returns (finalResponse, finalSeq, routed, routedResp, cont, done, outcome, err):
//   - cont=true: a recursive consultRoute call handled the result; propagate
//     (done, outcome, err) without applying the response.
//   - done=true or err!=nil: terminal outcome; return it.
//   - otherwise: finalResponse and finalSeq carry the HITL-accepted result.
//     When a gate-discharging re-dispatch returned SUCCESS, routed and
//     routedResp carry the original attempt's outcome and response.
func (s *sessionImpl) runConsultHITL(
	ctx context.Context,
	agentRef domain.AgentReference,
	agentReq domain.ProtocolRequest,
	response domain.ProtocolResponse,
	dispSeq int,
	effectiveHITL bool,
	dispInstr *domain.DispatchInstruction,
	effectiveStage, phase string,
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
) (finalResponse domain.ProtocolResponse, finalSeq int, routed *domain.RoutedOutcome, routedResp *domain.ProtocolResponse, cont bool, done bool, outcome domain.RunOutcome, err error) {
	currentOutputArts := agentReq.OutputArtifacts
	// Pre-HITL stage set re-derivation for self-referential rows.
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

	currentResp := response
	var original *domain.ProtocolResponse
	currentAttemptSeq := dispSeq
	hitlRedispatchUsed := false

hitlLoop:
	for {
		approvals := s.readApprovals(ctx, s.writtenOutputs(ctx, *stages))
		hitlDec := domain.DecideHITLCompliance(domain.HITLComplianceInput{
			EffectiveHITL:  effectiveHITL,
			Status:         currentResp.StatusCode,
			ErrorCode:      currentResp.ErrorCode,
			Approvals:      approvals,
			RedispatchUsed: hitlRedispatchUsed,
		})
		switch hitlDec.Outcome {
		case domain.HITLAccept:
			break hitlLoop

		case domain.HITLRedispatch:
			updResp, updSeq, rdCont, rdDone, rdOut, rdErr := s.consultHITLRedispatch(
				ctx, agentRef, agentReq, currentResp, currentAttemptSeq, effectiveHITL,
				effectiveStage, phase, dispInstr, state, seq, lastResponse, prevWorkflowStep,
				refreshedStages, stages, table, agents, config, declaredInfraAgents, admitted, antiLoop,
			)
			if rdDone || rdErr != nil {
				return domain.ProtocolResponse{}, 0, nil, nil, false, rdDone, rdOut, rdErr
			}
			if rdCont {
				return domain.ProtocolResponse{}, 0, nil, nil, true, false, domain.RunOutcome{}, nil
			}
			hitlRedispatchUsed = true
			if original == nil {
				first := currentResp
				original = &first
			}
			currentResp = updResp
			currentAttemptSeq = updSeq

		case domain.HITLEscalate:
			escDone, escOut, escErr := s.consultHITLEscalate(
				ctx, agentRef, agentReq, currentResp, currentAttemptSeq,
				effectiveStage, phase, dispInstr, state, seq, lastResponse, prevWorkflowStep,
				refreshedStages, stages, table, agents, config, declaredInfraAgents, admitted, antiLoop,
			)
			return domain.ProtocolResponse{}, 0, nil, nil, true, escDone, escOut, escErr
		}
	}
	if original != nil {
		if routed = routedAfterRedispatch(*original, currentResp); routed != nil {
			routedResp = original
		}
	}
	return currentResp, currentAttemptSeq, routed, routedResp, false, false, domain.RunOutcome{}, nil
}

// consultHITLRedispatch handles the HITLRedispatch case: persists the rejected
// attempt, checks the anti-loop guard, optionally stops, and invokes the
// harness for the redispatch.
//
// Returns (updResp, updSeq, cont, done, outcome, err):
//   - done=true: run is terminal.
//   - cont=true: consultRoute handled it; HITL loop should exit and outer loop continue.
//   - otherwise: updResp/updSeq carry the new response; HITL loop should continue.
func (s *sessionImpl) consultHITLRedispatch(
	ctx context.Context,
	agentRef domain.AgentReference,
	agentReq domain.ProtocolRequest,
	currentResp domain.ProtocolResponse,
	currentAttemptSeq int,
	effectiveHITL bool,
	effectiveStage, phase string,
	dispInstr *domain.DispatchInstruction,
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
) (updResp domain.ProtocolResponse, updSeq int, cont bool, done bool, outcome domain.RunOutcome, err error) {
	rlRejStep := domain.CompletedStep{
		Seq:              (*state).GlobalSequence + 1,
		AgentInstance:    fmt.Sprintf("%s#%d", agentRef.Identifier, currentAttemptSeq),
		Phase:            phase,
		Stage:            effectiveStage,
		Status:           currentResp.StatusCode,
		ErrorCode:        currentResp.ErrorCode,
		Summary:          currentResp.StatusMessage,
		Timestamp:        s.deps.Clock.Now(),
		Inputs:           formatInputs(agentReq.InputArtifacts),
		IsInfrastructure: true,
		HITLRejected:     true,
	}
	newState, rlApplyErr := s.deps.Store.Apply(ctx, *state, rlRejStep)
	if rlApplyErr != nil {
		return domain.ProtocolResponse{}, 0, false, true, domain.RunOutcome{Status: domain.RunFailed, Message: rlApplyErr.Error()}, rlApplyErr
	}
	*state = newState
	*seq = (*state).GlobalSequence
	if !antiLoop.recordDispatch(dispInstr.RowIndex, agentRef.Identifier) {
		s.deps.Debug.Log(domain.EventSessionHITLEscalate, "anti-loop guard triggered in hitlLoop redispatch; escalating",
			domain.F("agent", agentRef.Identifier),
		)
		alrlDevInfo := domain.DeviationInfo{
			Kind:          domain.DeviationNonSuccess,
			Response:      currentResp,
			CurrentRow:    dispInstr.RowIndex,
			CurrentPhase:  phase,
			ArtifactState: *state,
		}
		crDone, crOut, crErr := s.consultRoute(ctx, &alrlDevInfo, state, seq, lastResponse, prevWorkflowStep,
			refreshedStages, stages, table, agents, config, declaredInfraAgents, admitted, antiLoop)
		return domain.ProtocolResponse{}, 0, true, crDone, crOut, crErr
	}
	s.deps.Debug.Log(domain.EventSessionHITLRedispatch, "HITL non-compliant; redispatching same agent",
		domain.F("agent", agentRef.Identifier),
	)
	newAttemptSeq := currentAttemptSeq + 1
	rdReq := agentReq
	rdReq.AgentInstanceID = fmt.Sprintf("%s#%d", agentRef.Identifier, newAttemptSeq)
	if s.deps.StopRequested() {
		s.deps.Debug.Log(domain.EventSessionStopObserved, "graceful stop observed; not dispatching",
			domain.F("checkpoint", StopCheckpointConsultHITLRedispatch),
		)
		return domain.ProtocolResponse{}, 0, false, true, domain.RunOutcome{Status: domain.RunStopped, Message: "run stopped: graceful stop confirmed"}, nil
	}
	rdResp, rdErr := s.invokeAndLog(ctx, agentRef, rdReq)
	if rdErr != nil {
		if ctx.Err() != nil {
			return domain.ProtocolResponse{}, 0, false, true, domain.RunOutcome{Status: domain.RunStopped, Message: "run stopped: context cancelled"}, nil
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
		crDone, crOut, crErr := s.consultRoute(ctx, &rdDevInfo, state, seq, lastResponse, prevWorkflowStep,
			refreshedStages, stages, table, agents, config, declaredInfraAgents, admitted, antiLoop)
		return domain.ProtocolResponse{}, 0, true, crDone, crOut, crErr
	}
	return rdResp, newAttemptSeq, false, false, domain.RunOutcome{}, nil
}

// consultHITLEscalate handles the HITLEscalate case: persists the final
// rejected attempt and escalates to consultRoute.
func (s *sessionImpl) consultHITLEscalate(
	ctx context.Context,
	agentRef domain.AgentReference,
	agentReq domain.ProtocolRequest,
	currentResp domain.ProtocolResponse,
	currentAttemptSeq int,
	effectiveStage, phase string,
	dispInstr *domain.DispatchInstruction,
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
) (done bool, outcome domain.RunOutcome, err error) {
	elRejStep := domain.CompletedStep{
		Seq:              (*state).GlobalSequence + 1,
		AgentInstance:    fmt.Sprintf("%s#%d", agentRef.Identifier, currentAttemptSeq),
		Phase:            phase,
		Stage:            effectiveStage,
		Status:           currentResp.StatusCode,
		ErrorCode:        currentResp.ErrorCode,
		Summary:          currentResp.StatusMessage,
		Timestamp:        s.deps.Clock.Now(),
		Inputs:           formatInputs(agentReq.InputArtifacts),
		IsInfrastructure: true,
		HITLRejected:     true,
	}
	newState, elApplyErr := s.deps.Store.Apply(ctx, *state, elRejStep)
	if elApplyErr != nil {
		return true, domain.RunOutcome{Status: domain.RunFailed, Message: elApplyErr.Error()}, elApplyErr
	}
	*state = newState
	*seq = (*state).GlobalSequence
	s.deps.Debug.Log(domain.EventSessionHITLEscalate, "HITL redispatch exhausted; escalating to deviation",
		domain.F("agent", agentRef.Identifier),
	)
	escDevInfo := domain.DeviationInfo{
		Kind:          domain.DeviationNonSuccess,
		Response:      currentResp,
		CurrentRow:    dispInstr.RowIndex,
		CurrentPhase:  phase,
		ArtifactState: *state,
	}
	return s.consultRoute(ctx, &escDevInfo, state, seq, lastResponse, prevWorkflowStep,
		refreshedStages, stages, table, agents, config, declaredInfraAgents, admitted, antiLoop)
}

// applyConsultStep applies the HITL-compliant response to the artifact,
// logs the completion, sends the progress notice, evaluates infrastructure
// triggers, and performs Stage-* output re-derivation.
func (s *sessionImpl) applyConsultStep(
	ctx context.Context,
	agentRef domain.AgentReference,
	agentReq domain.ProtocolRequest,
	finalResponse domain.ProtocolResponse,
	currentAttemptSeq int,
	routed *domain.RoutedOutcome,
	routedResp *domain.ProtocolResponse,
	phase, effectiveStage string,
	dispInstr *domain.DispatchInstruction,
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
) (done bool, outcome domain.RunOutcome, err error) {
	workflowSeq := state.GlobalSequence + 1
	finalAgentInstanceID := fmt.Sprintf("%s#%d", agentRef.Identifier, currentAttemptSeq)
	currentOutputArts := agentReq.OutputArtifacts
	written := s.writtenOutputs(ctx, *stages)
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
		WrittenArtifacts: written,
		Routed:           routed,
	}
	*state, err = s.deps.Store.Apply(ctx, *state, completedStep)
	if err != nil {
		return true, domain.RunOutcome{Status: domain.RunFailed, Message: err.Error()}, err
	}
	s.deps.Debug.Log(domain.EventSessionStepDone, "step applied to artifact",
		domain.F("agent", finalAgentInstanceID),
		domain.F("status", string(completedStep.Status)),
	)
	*seq = currentAttemptSeq
	*lastResponse = &finalResponse
	if routedResp != nil {
		// Routing follows the original attempt, not the repairing re-dispatch.
		*lastResponse = routedResp
	}
	s.deps.Interact.Notify(ctx, interaction.Notice{
		Level:   interaction.NoticeInfo,
		Title:   finalAgentInstanceID,
		Message: fmt.Sprintf("phase=%s stage=%q status=%s", phase, effectiveStage, string(finalResponse.StatusCode)),
	})
	orchDir := filepath.Dir(config.OrchestratorFilePath)
	if !completedStep.IsInfrastructure {
		halt, trigStopped, reviewConsult, trigErr := s.evaluateTriggers(
			ctx, state, seq, completedStep, *prevWorkflowStep,
			declaredInfraAgents, config,
			buildActiveAgentsFilter(declaredInfraAgents, config.InfraClassSelections),
			orchDir, dispInstr.RowIndex, admitted, *stages,
			true, // only HITL-accepted steps reach trigger evaluation
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
		if reviewConsult != nil && s.deps.Routing != nil {
			syntheticResp := domain.ProtocolResponse{StatusMessage: reviewConsult.StatusMessage}
			*lastResponse = &syntheticResp
			done, outcome, consultErr := s.consultRoute(ctx, nil, state, seq, lastResponse, prevWorkflowStep,
				refreshedStages, stages, table, agents, config, declaredInfraAgents, admitted, antiLoop)
			if consultErr != nil {
				return true, domain.RunOutcome{Status: domain.RunFailed, Message: consultErr.Error()}, consultErr
			}
			if done {
				return done, outcome, nil
			}
		}
	}
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

// routedAfterRedispatch returns the outcome a step is routed on after a
// gate-discharging re-dispatch. When the re-dispatch returned SUCCESS but the
// original attempt did not, the original status and error code stand in for
// the step's routing and current_state. Otherwise the re-dispatch's own
// response is authoritative and nil is returned.
func routedAfterRedispatch(original, final domain.ProtocolResponse) *domain.RoutedOutcome {
	if final.StatusCode != domain.StatusSUCCESS || original.StatusCode == domain.StatusSUCCESS {
		return nil
	}
	return &domain.RoutedOutcome{Status: original.StatusCode, ErrorCode: original.ErrorCode}
}
