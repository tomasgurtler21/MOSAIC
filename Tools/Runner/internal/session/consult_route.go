package session

import (
	"context"
	"fmt"
	"path/filepath"
	"strconv"

	"mosaic-common/interaction"
	"mosaic-run/internal/domain"
	"mosaic-run/internal/engine"
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
	// Capture the stage in force at this entry point so that every CompletedStep
	// produced below carries accurate context. Recursive calls each capture their
	// own entryStage independently.
	entryStage := state.CurrentState.Stage

	// Build the consultation request.
	req := domain.ConsultationRequest{
		OrchestrationArtifact: filepath.Join(config.RunFolder, "Orchestration.md"),
		Context:               domain.ConsultContextRouting,
		Deviation:             deviation,
	}
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

	// Look up the routing table row and derive the effective stage.
	row, effectiveStage := consultRowAndStage(table, dispInstr, deviation, entryStage)

	// The consultation itself writes nothing to the artifact: no Execution Log
	// row, no global_sequence advance, no current_state change. Re-read the
	// artifact so any Workflow Notes the orchestrator appended directly during
	// its deliberation are visible to the session before any later state write.
	s.consultReread(ctx, state, seq)

	// Resolve the dispatched agent and build the ProtocolRequest.
	agentRef, agentReq, phase, dispSeq, ok := buildConsultAgentRequest(
		dispInstr, agents, row, effectiveStage, *seq, *state, stages, refreshedStages,
	)
	if !ok {
		return true, domain.RunOutcome{Status: domain.RunFailed, Message: "consultant dispatched unknown agent: " + dispInstr.Agent}, nil
	}

	// Anti-loop guard: prevent the same agent from being dispatched more than
	// maxConsecutiveSameAgentDispatches consecutive times for the same step.
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
	if s.deps.StopRequested() {
		s.deps.Debug.Log(domain.EventSessionStopObserved, "graceful stop observed; not dispatching",
			domain.F("checkpoint", StopCheckpointConsultDispatch),
		)
		return true, domain.RunOutcome{Status: domain.RunStopped, Message: "run stopped: graceful stop confirmed"}, nil
	}

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
		}
	}
	if consultErr != nil {
		s.deps.Debug.Log(domain.EventSessionConsultFailed, consultErr.Error())
		return instr, true, domain.RunOutcome{
			Status:  domain.RunStoppedByConsultant,
			Message: "consultation failed: " + consultErr.Error(),
			Cause:   consultErr,
		}, nil
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

// consultRowAndStage looks up the routing table row for dispInstr.RowIndex
// (falling back to the deviation's physical row when the index is not found)
// and derives the effective stage for all CompletedSteps in this consultRoute
// call.
func consultRowAndStage(
	table domain.RoutingTable,
	dispInstr *domain.DispatchInstruction,
	deviation *domain.DeviationInfo,
	entryStage string,
) (row domain.RoutingRow, effectiveStage string) {
	var found bool
	row, found = rowAtIndex(table, dispInstr.RowIndex)
	if !found && deviation != nil {
		row, _ = rowAtIndex(table, deviation.CurrentRow)
	}
	effectiveStage = entryStage
	if deviation != nil && row.PhaseParsed.IsStaged {
		_, stageNum, stageOK := domain.ParseStageValue(deviation.CurrentStage)
		if stageOK {
			effectiveStage = domain.FormatStageValue(row.PhaseParsed.Group, stageNum)
		}
	}
	return row, effectiveStage
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

// buildConsultAgentRequest resolves the dispatched agent reference, resolves
// field overrides (constraints, input/output artifacts, HITL), expands
// stage-number templates, prefixes run-scoped paths, and builds the
// ProtocolRequest. Returns ok=false when the agent identifier is not in agents.
func buildConsultAgentRequest(
	dispInstr *domain.DispatchInstruction,
	agents map[string]domain.AgentReference,
	row domain.RoutingRow,
	effectiveStage string,
	seq int,
	state domain.ArtifactState,
	stages **domain.StageSet,
	refreshedStages **domain.StageSet,
) (agentRef domain.AgentReference, agentReq domain.ProtocolRequest, phase string, dispSeq int, ok bool) {
	agentRef, ok = agents[dispInstr.Agent]
	if !ok {
		return domain.AgentReference{}, domain.ProtocolRequest{}, "", 0, false
	}
	var constraints string
	if dispInstr.Constraints != nil {
		constraints = *dispInstr.Constraints
	}
	inputArts := row.InputArtifacts
	if dispInstr.InputArtifacts != nil {
		inputArts = *dispInstr.InputArtifacts
	}
	outputArts := row.OutputArtifacts
	if dispInstr.OutputArtifacts != nil {
		outputArts = *dispInstr.OutputArtifacts
	}
	// Resolve {StageNumber} template tokens so persisted paths match the target row.
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
	dispSeq = seq + 1
	agentReq = domain.ProtocolRequest{
		AgentInstanceID: fmt.Sprintf("%s#%d", agentRef.Identifier, dispSeq),
		RunID:           state.RunID,
		TaskDescription: dispInstr.TaskDescription,
		Constraints:     constraints,
		InputArtifacts:  inputArts,
		OutputArtifacts: outputArts,
		HumanInTheLoop:  effectiveHITL,
	}
	folder := domain.RunScopedFolder(state.RunID) + "/"
	agentReq.InputArtifacts = resolveToRunScoped(agentReq.InputArtifacts, folder)
	agentReq.OutputArtifacts = resolveToRunScoped(agentReq.OutputArtifacts, folder)
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
	if isRawTextHarnessError(invokeErr) && antiLoop.recordDispatch(dispInstr.RowIndex, agentRef.Identifier) {
		bypassSeq := state.GlobalSequence + 1
		bypassReq := agentReq
		bypassReq.AgentInstanceID = fmt.Sprintf("%s#%d", agentRef.Identifier, bypassSeq)
		if bypassResp, bypassErr := s.invokeAndLog(ctx, agentRef, bypassReq); bypassErr == nil {
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
	}
	*lastResponse = &harnessResp
	return s.consultRoute(ctx, &devInfo, state, seq, lastResponse, prevWorkflowStep,
		refreshedStages, stages, table, agents, config, declaredInfraAgents, admitted, antiLoop)
}
