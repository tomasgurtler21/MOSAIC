package session

import (
	"context"
	"errors"
	"fmt"
	"strconv"

	"mosaic-common/interaction"
	"mosaic-run/internal/domain"
)

// consultRoute handles the full consultation-record-reread-dispatch cycle for
// one routing decision. It is called whenever the session must defer a routing
// choice to the RoutingConsultant -- both for orchestrated-mode normal flow
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
	// A stop requested before the consultation begins takes effect first.
	if stopped, stopOut := s.stopBeforeConsult(); stopped {
		return true, stopOut, nil
	}

	// Build the consultation request.
	req := newConsultRequest(config, deviation, *state, stages, refreshedStages)
	if *lastResponse != nil {
		msg := (*lastResponse).StatusMessage
		req.LastStatusMessage = &msg
		req.LastErrorReason = domain.LastErrorReasonFor(*lastResponse)
	}

	// Route to the appropriate resolver; handle errors and terminal instructions.
	instr, selDone, selOut, selErr := s.consultSelectResolver(ctx, req, config)
	if selErr != nil || selDone {
		return selDone, selOut, selErr
	}
	dispInstr := instr.Dispatch

	// The row and stage to record come from the validated instruction.
	row, effectiveStage, tDone, tOut := s.resolveConsultTarget(table, dispInstr, agents, admitted, *stages, *refreshedStages)
	if tDone {
		return true, tOut, nil
	}

	// A stop requested during the consultation discards its decision visibly.
	if stopped, stopOut := s.discardDecisionOnStop(ctx, dispInstr, effectiveStage); stopped {
		return true, stopOut, nil
	}

	// The consultation itself writes nothing to the artifact: no Execution Log
	// row, no global_sequence advance, no current_state change. Re-read the
	// artifact so any Workflow Notes the orchestrator appended directly during
	// its deliberation are visible to the session before any later state write.
	s.consultReread(ctx, state, seq)

	// Resolve the artifacts and HITL: row defaults as the engine resolves them,
	// explicit overrides verbatim. A resolution failure ends the run unrecorded.
	payload, payloadErr := resolveConsultPayload(dispInstr, row, effectiveStage, *stages, *refreshedStages)
	if payloadErr != nil {
		s.deps.Debug.Log(domain.EventSessionConsultFailed, "consultation dispatch defaults unresolvable: "+payloadErr.Error(),
			domain.F("agent", dispInstr.Agent),
			domain.F("row", strconv.Itoa(dispInstr.RowIndex)),
		)
		return true, domain.RunOutcome{
			Status:  domain.RunStoppedByConsultant,
			Message: "consultation failed: cannot resolve dispatch artifacts: " + payloadErr.Error(),
			Cause:   payloadErr,
		}, nil
	}

	// Resolve the dispatched agent and build the ProtocolRequest.
	agentRef, agentReq, phase, dispSeq, ok := buildConsultAgentRequest(
		dispInstr, agents, row, payload, *seq, *state,
	)
	if !ok {
		return true, domain.RunOutcome{Status: domain.RunFailed, Message: "consultant dispatched unknown agent: " + dispInstr.Agent}, nil
	}

	// Anti-loop guard: prevent the same agent from being dispatched more than
	// maxConsecutiveSameAgentDispatches consecutive times for the same step.
	if !antiLoop.recordDispatch(dispInstr.RowIndex, agentRef.Identifier) {
		if stopOut, stop := s.guardEscalationExhausted(antiLoop, agentRef.Identifier, dispInstr.RowIndex); stop {
			return true, stopOut, nil
		}
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
	// Invoke the harness. A harness error is a deviation, not a crash.
	s.beginOutputs(ctx, config.RunFolder, agentReq.OutputArtifacts)
	response, invokeErr := s.invokeAndLog(ctx, agentRef, agentReq)
	if invokeErr != nil {
		return s.consultHandleHarnessErr(ctx, agentRef, agentReq, invokeErr, dispSeq, phase, effectiveStage,
			dispInstr, state, seq, lastResponse, prevWorkflowStep, refreshedStages, stages,
			table, agents, config, declaredInfraAgents, admitted, antiLoop)
	}

	// Run HITL compliance verification. Use agentReq.HumanInTheLoop (which
	// already incorporates any HITLOverride from dispInstr) as effectiveHITL.
	finalResp, finalSeq, routed, routedResp, cont, hitlDone, hitlOut, hitlErr := s.runConsultHITL(
		ctx, agentRef, agentReq, response, dispSeq, agentReq.HumanInTheLoop, dispInstr, effectiveStage, phase,
		state, seq, lastResponse, prevWorkflowStep, refreshedStages, stages,
		table, agents, config, declaredInfraAgents, admitted, antiLoop,
	)
	if cont || hitlDone || hitlErr != nil {
		return hitlDone, hitlOut, hitlErr
	}

	// Apply the accepted response and evaluate infrastructure triggers.
	return s.applyConsultStep(
		ctx, agentRef, agentReq, finalResp, finalSeq, routed, routedResp, phase, effectiveStage, dispInstr,
		state, seq, lastResponse, prevWorkflowStep, refreshedStages, stages,
		table, agents, config, declaredInfraAgents, admitted, antiLoop,
	)
}

// consultSelectResolver routes a consultation request to the appropriate
// resolver (manual one-shot, primary, or primary-with-manual-fallback) and
// handles all terminal cases: consultation error, stop instruction, and
// missing dispatch instruction.
func (s *sessionImpl) consultSelectResolver(ctx context.Context, req domain.ConsultationRequest, config domain.RunConfig) (instr domain.RoutingInstruction, done bool, outcome domain.RunOutcome, err error) {
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
		} else if consultErr != nil && config.ManualResolution {
			// Unreachable after the start-time port check; reported rather
			// than silently skipped.
			consultErr = errors.Join(consultErr, newManualMissing())
		}
	}
	if consultErr != nil {
		s.deps.Debug.Log(domain.EventSessionConsultFailed, consultErr.Error())
		return instr, true, consultFailureOutcome(consultErr), nil
	}
	if instr.Stop != nil {
		s.deps.Debug.Log(domain.EventSessionConsultStop, "consultant stop instruction",
			domain.F("reason", instr.Stop.Reason),
		)
		return instr, true, domain.RunOutcome{
			Status:     domain.RunStoppedByConsultant,
			Message:    "consultant stop: " + instr.Stop.Reason,
			StopReason: instr.Stop.Reason,
		}, nil
	}
	if instr.Dispatch == nil {
		s.deps.Debug.Log(domain.EventSessionConsultFailed, "instruction has neither stop nor dispatch")
		return instr, true, domain.RunOutcome{
			Status:  domain.RunStoppedByConsultant,
			Message: "consultation failed: instruction has neither stop nor dispatch",
		}, nil
	}
	return instr, false, domain.RunOutcome{}, nil
}

// consultReread re-reads the artifact after a successful routing consultation
// returns, before any later state write, so that any Workflow Notes the
// script orchestrator appended directly to the artifact during its
// deliberation are preserved. The consultation itself is never recorded: it
// consumes no global_sequence slot, writes no Execution Log row and does not
// move current_state. The session's local sequence follows the re-read
// artifact's global_sequence directly, so the agent the consultation
// dispatches takes the next Seq with no collision from a consultation row.
func (s *sessionImpl) consultReread(ctx context.Context, state *domain.ArtifactState, seq *int) {
	if freshState, readErr := s.deps.Store.Read(ctx); readErr == nil {
		*state = freshState
	}
	*seq = state.GlobalSequence
}

// buildConsultAgentRequest resolves the dispatched agent reference and builds
// the ProtocolRequest from the resolved payload, prefixing run-scoped paths.
// Returns ok=false when the agent identifier is not in agents.
func buildConsultAgentRequest(
	dispInstr *domain.DispatchInstruction,
	agents map[string]domain.AgentReference,
	row domain.RoutingRow,
	payload consultPayload,
	seq int,
	state domain.ArtifactState,
) (agentRef domain.AgentReference, agentReq domain.ProtocolRequest, phase string, dispSeq int, ok bool) {
	agentRef, ok = agents[dispInstr.Agent]
	if !ok {
		return domain.AgentReference{}, domain.ProtocolRequest{}, "", 0, false
	}
	var constraints string
	if dispInstr.Constraints != nil {
		constraints = *dispInstr.Constraints
	}
	dispSeq = seq + 1
	folder := domain.RunScopedFolder(state.RunID) + "/"
	agentReq = domain.ProtocolRequest{
		AgentInstanceID: fmt.Sprintf("%s#%d", agentRef.Identifier, dispSeq),
		RunID:           state.RunID,
		TaskDescription: dispInstr.TaskDescription,
		Constraints:     constraints,
		InputArtifacts:  domain.DedupArtifactPaths(state.RunID, resolveToRunScoped(payload.Inputs, folder)),
		OutputArtifacts: resolveToRunScoped(payload.Outputs, folder),
		HumanInTheLoop:  payload.HITL,
	}
	phase = row.PhaseParsed.Name
	return agentRef, agentReq, phase, dispSeq, true
}

// consultHandleHarnessErr handles a harness-level error from a consultant-
// routed invocation. It persists the failed attempt, attempts a raw-text
// bypass, and on bypass failure calls consultRoute recursively.
func (s *sessionImpl) consultHandleHarnessErr(
	ctx context.Context,
	agentRef domain.AgentReference,
	agentReq domain.ProtocolRequest,
	invokeErr error,
	dispSeq int,
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
	if ctx.Err() != nil {
		return true, domain.RunOutcome{Status: domain.RunStopped, Message: "run stopped: context cancelled"}, nil
	}
	s.deps.Debug.Log(domain.EventSessionHarnessError, invokeErr.Error(),
		domain.F("agent", agentReq.AgentInstanceID),
	)
	crFailedStep := domain.CompletedStep{
		Seq:           state.GlobalSequence + 1,
		AgentInstance: agentReq.AgentInstanceID,
		Phase:         phase,
		Stage:         effectiveStage,
		WorkflowRow:   domain.WorkflowRowFromIndex(dispInstr.RowIndex),
		Status:        domain.StatusBLOCKED,
		ErrorCode:     domain.ErrorTOOL_UNAVAILABLE,
		Summary:       invokeErr.Error(),
		Timestamp:     s.deps.Clock.Now(),
		Inputs:        formatInputs(agentReq.InputArtifacts),
	}
	crNewState, crApplyErr := s.deps.Store.Apply(ctx, *state, crFailedStep)
	if crApplyErr != nil {
		return true, domain.RunOutcome{Status: domain.RunFailed, Message: crApplyErr.Error()}, crApplyErr
	}
	*state = crNewState
	harnessResp := domain.HarnessErrorResponse(agentReq.AgentInstanceID, config.RunID, invokeErr)
	devInfo := domain.DeviationInfo{
		Kind:          domain.DeviationNonSuccess,
		Response:      harnessResp,
		CurrentRow:    dispInstr.RowIndex,
		CurrentPhase:  phase,
		ArtifactState: *state,
	}
	// Raw-text bypass: attempt one direct redispatch before triggering a
	// recursive consultRoute call.
	if isRawTextHarnessError(invokeErr) && bypassPermitted(*state, dispInstr.RowIndex, effectiveStage, agentRef.Identifier, antiLoop) {
		bypassSeq := state.GlobalSequence + 1
		bypassReq := agentReq
		bypassReq.AgentInstanceID = fmt.Sprintf("%s#%d", agentRef.Identifier, bypassSeq)
		bypassResp, bypassErr := s.invokeAndLog(ctx, agentRef, bypassReq)
		if bypassErr == nil {
			finalResp, finalSeq, routed, routedResp, cont, hitlDone, hitlOut, hitlErr := s.runConsultHITL(
				ctx, agentRef, bypassReq, bypassResp, bypassSeq, agentReq.HumanInTheLoop, dispInstr,
				effectiveStage, phase, state, seq, lastResponse, prevWorkflowStep, refreshedStages, stages,
				table, agents, config, declaredInfraAgents, admitted, antiLoop,
			)
			if cont || hitlDone || hitlErr != nil {
				return hitlDone, hitlOut, hitlErr
			}
			return s.applyConsultStep(ctx, agentRef, bypassReq, finalResp, finalSeq, routed, routedResp, phase, effectiveStage, dispInstr,
				state, seq, lastResponse, prevWorkflowStep, refreshedStages, stages,
				table, agents, config, declaredInfraAgents, admitted, antiLoop)
		}
		if recErr := s.recordFailedBypass(ctx, state, bypassReq, phase, effectiveStage, dispInstr.RowIndex, bypassErr); recErr != nil {
			return true, domain.RunOutcome{Status: domain.RunFailed, Message: recErr.Error()}, recErr
		}
	}
	*lastResponse = &harnessResp
	if isMechanicalRetryMode(state.Mode) && e501BudgetLeft(*state, dispInstr.RowIndex, effectiveStage) {
		// Budget remains: the engine decision re-dispatches the same row and stage.
		*seq = state.GlobalSequence
		return false, domain.RunOutcome{}, nil
	}
	return s.consultRoute(ctx, &devInfo, state, seq, lastResponse, prevWorkflowStep,
		refreshedStages, stages, table, agents, config, declaredInfraAgents, admitted, antiLoop)
}

// currentStageSet returns the stage set in force: the refreshed set when the
// plan was re-read mid-run, otherwise the set read at the start of the run.
// Nil when no plan has been read.
func currentStageSet(stages, refreshed *domain.StageSet) *domain.StageSet {
	if refreshed != nil {
		return refreshed
	}
	return stages
}
