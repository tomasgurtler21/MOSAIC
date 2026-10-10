package domain

import "fmt"

// DispatchTarget is a consultant's proposed workflow position, as received.
type DispatchTarget struct {
	Agent      string
	Row        WorkflowRow // 1-based; NoWorkflowRow = missing
	Stage      StageNumber // 0 = missing/absent
	StageGroup GroupName   // group given with the stage (wire group form), "" when a bare number
}

// ResolvedDispatchTarget is a validated target.
type ResolvedDispatchTarget struct {
	Row           RoutingRow
	StageNumber   StageNumber // 0 for non-staged rows
	RecordedStage string      // FormatStageValue(Row.PhaseParsed.Group, StageNumber), "" for non-staged rows
}

// DispatchTargetReason names why a dispatch target is invalid.
type DispatchTargetReason string

const (
	ReasonMissingRow          DispatchTargetReason = "missing-row"
	ReasonUnknownRow          DispatchTargetReason = "unknown-row"
	ReasonAgentMismatch       DispatchTargetReason = "agent-mismatch"
	ReasonMissingStage        DispatchTargetReason = "missing-stage"
	ReasonUnknownStage        DispatchTargetReason = "unknown-stage"
	ReasonStageOnNonStagedRow DispatchTargetReason = "stage-on-non-staged-row"
	ReasonStageGroupMismatch  DispatchTargetReason = "stage-group-mismatch"
)

// DispatchTargetError names why a target is invalid. Callers switch on Reason,
// never on text.
type DispatchTargetError struct {
	Reason DispatchTargetReason
	Target DispatchTarget
	Detail string
}

func (e *DispatchTargetError) Error() string {
	return "invalid dispatch target: " + string(e.Reason)
}

// ValidateDispatchTarget checks a consultant's dispatch target against the
// admitted routing table and the current stage set. It never corrects a target.
//
// Checks run in this order: row present, row exists, agent equals the row's
// agent, then the stage rules: a non-staged row must carry no stage; a staged
// row must carry a stage that exists in the current stage set and, when a
// group was given, a group equal to the row's group.
func ValidateDispatchTarget(table RoutingTable, stages *StageSet, t DispatchTarget) (ResolvedDispatchTarget, error) {
	fail := func(reason DispatchTargetReason, detail string) (ResolvedDispatchTarget, error) {
		return ResolvedDispatchTarget{}, &DispatchTargetError{Reason: reason, Target: t, Detail: detail}
	}
	if t.Row == NoWorkflowRow {
		return fail(ReasonMissingRow, "no row named")
	}
	if t.Row < 1 || t.Row.Index() >= len(table.Rows) {
		return fail(ReasonUnknownRow, fmt.Sprintf("row %d is not in the routing table (%d rows)", t.Row, len(table.Rows)))
	}
	row := table.Rows[t.Row.Index()]
	if row.Agent != t.Agent {
		return fail(ReasonAgentMismatch, fmt.Sprintf("row %d belongs to agent %q, not %q", t.Row, row.Agent, t.Agent))
	}

	if !row.PhaseParsed.IsStaged {
		if t.Stage != 0 || t.StageGroup != "" {
			return fail(ReasonStageOnNonStagedRow, fmt.Sprintf("row %d is not a staged row", t.Row))
		}
		return ResolvedDispatchTarget{Row: row}, nil
	}

	if t.Stage == 0 {
		return fail(ReasonMissingStage, fmt.Sprintf("row %d is a staged row and needs a stage", t.Row))
	}
	if t.StageGroup != "" && t.StageGroup != row.PhaseParsed.Group {
		return fail(ReasonStageGroupMismatch,
			fmt.Sprintf("stage group %q differs from row %d group %q", t.StageGroup, t.Row, row.PhaseParsed.Group))
	}
	if stages == nil {
		return fail(ReasonUnknownStage, "no stage set is available")
	}
	if _, ok := stages.Entry(t.Stage); !ok {
		return fail(ReasonUnknownStage, fmt.Sprintf("stage %d is not in the current stage set", t.Stage))
	}
	return ResolvedDispatchTarget{
		Row:           row,
		StageNumber:   t.Stage,
		RecordedStage: FormatStageValue(row.PhaseParsed.Group, t.Stage),
	}, nil
}
