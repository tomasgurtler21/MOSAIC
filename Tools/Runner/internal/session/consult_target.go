package session

import (
	"strconv"

	"mosaic-run/internal/domain"
)

// resolveConsultTarget validates a consultation dispatch instruction against
// the routing table and the stage set in force, and returns the routing row
// and the stage to record for the step: the instruction's own stage for a
// staged row, empty for a non-staged row. Nothing is inherited from the
// state the consultation was entered with.
//
// done is true, with a terminal outcome, when the instruction names an unknown
// agent or fails validation; the caller must then neither dispatch nor record.
func (s *sessionImpl) resolveConsultTarget(
	table domain.RoutingTable,
	dispInstr *domain.DispatchInstruction,
	agents map[string]domain.AgentReference,
	admitted domain.AdmittedWorkflow,
	stages, refreshedStages *domain.StageSet,
) (row domain.RoutingRow, stage string, done bool, outcome domain.RunOutcome) {
	if _, known := agents[dispInstr.Agent]; !known {
		return row, "", true, domain.RunOutcome{Status: domain.RunFailed, Message: "consultant dispatched unknown agent: " + dispInstr.Agent}
	}
	stageSet := currentStageSet(stages, refreshedStages)
	target := domain.DispatchTarget{
		Agent: dispInstr.Agent,
		Row:   domain.WorkflowRowFromIndex(dispInstr.RowIndex),
		Stage: dispInstr.Stage,
	}
	resolved, err := domain.ValidateDispatchTarget(table, stageSet, target)
	if err == nil {
		err = checkStageApproachHasGroup(admitted, stageSet, resolved, target)
	}
	if err != nil {
		s.deps.Debug.Log(domain.EventSessionConsultFailed, "consultation dispatch target invalid: "+err.Error(),
			domain.F("agent", dispInstr.Agent),
			domain.F("row", strconv.Itoa(dispInstr.RowIndex)),
			domain.F("stage", strconv.Itoa(int(dispInstr.Stage))),
		)
		return row, "", true, domain.RunOutcome{
			Status:  domain.RunStoppedByConsultant,
			Message: "consultation failed: invalid dispatch target: " + err.Error(),
			Cause: &domain.ConsultationError{
				Failure: domain.ConsultFailMalformedJSON,
				Detail:  "invalid dispatch target: " + err.Error(),
				Err:     err,
			},
		}
	}
	return resolved.Row, resolved.RecordedStage, false, domain.RunOutcome{}
}

// checkStageApproachHasGroup rejects a staged row whose execution group is not
// part of the group sequence the stage's approach selects. Workflows without
// declared groups, and approaches the table does not know, are not checked.
func checkStageApproachHasGroup(
	admitted domain.AdmittedWorkflow,
	stages *domain.StageSet,
	resolved domain.ResolvedDispatchTarget,
	target domain.DispatchTarget,
) error {
	if !resolved.Row.PhaseParsed.IsStaged || !admitted.GroupsDeclared || stages == nil {
		return nil
	}
	entry, ok := stages.Entry(resolved.StageNumber)
	if !ok {
		return nil
	}
	sequence, found := admitted.ApproachTable.Sequence(entry.Approach)
	if !found {
		return nil
	}
	group := resolved.Row.PhaseParsed.Group
	for _, g := range sequence {
		if g == group {
			return nil
		}
	}
	return &domain.DispatchTargetError{
		Reason: domain.ReasonStageGroupMismatch,
		Target: target,
		Detail: "stage " + strconv.Itoa(int(resolved.StageNumber)) + " approach " + string(entry.Approach) +
			" does not include group " + string(group),
	}
}
