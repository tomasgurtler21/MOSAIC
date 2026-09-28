package engine

import (
	"strings"

	"mosaic-run/internal/domain"
)

// isUnambiguousHint returns true when the hint column is present, the value is
// non-empty, and the value contains no spaces or parentheses (making it a
// plain agent identifier or a reserved keyword such as "COMPLETE" or "next").
func isUnambiguousHint(hint domain.OptionalHint) bool {
	if !hint.ColumnPresent || hint.Value == "" {
		return false
	}
	return !strings.ContainsAny(hint.Value, " ()")
}

// extractAgentName strips the "#seq" suffix from an agent instance ID.
// For example, "plan-review#3" becomes "plan-review".
func extractAgentName(instanceID string) string {
	if i := strings.LastIndex(instanceID, "#"); i >= 0 {
		return instanceID[:i]
	}
	return instanceID
}

// parseStageNumber extracts the integer stage number from a recorded stage
// value, accepting both the target form ("Test.1", "1") and the legacy form
// ("Stage-1") via domain.ParseStageValue. Returns 0 when the string is empty
// or cannot be parsed.
func parseStageNumber(stageStr string) domain.StageNumber {
	_, n, ok := domain.ParseStageValue(stageStr)
	if !ok {
		return 0
	}
	return n
}

// isExecutionPhase returns true when the phase value -- in either the target
// bare form ("EXECUTION") or a legacy qualified form
// ("EXECUTION.Test.[StageNumber]") -- represents a staged EXECUTION phase.
func isExecutionPhase(phase string) bool {
	return domain.RecordedPhaseName(phase) == "EXECUTION"
}

// firstRowPhase returns the phase of row 0, or "" if the table is empty.
func firstRowPhase(workflow domain.AdmittedWorkflow) string {
	if len(workflow.Table.Rows) == 0 {
		return ""
	}
	return workflow.Table.Rows[0].Phase
}
