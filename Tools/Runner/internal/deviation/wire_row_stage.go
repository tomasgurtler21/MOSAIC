package deviation

import (
	"encoding/json"
	"fmt"
	"strconv"

	"mosaic-run/internal/domain"
)

// Wire shape of the workflow position a dispatch reply names:
//
//	"row":   1-based row number of the routing table (the Row column of the
//	         workflow table and the WorkflowRow column of the Execution Log).
//	         Required for every dispatch. A JSON integer or a decimal string.
//	"stage": plan stage for a staged (EXECUTION) row, required exactly for such
//	         rows. A JSON integer, a decimal string, or the Execution Log group
//	         form "Group.N" (for example "Test.2").
//
// Absent and null both mean "not given". A value of the wrong JSON type or a
// non-integer is a malformed reply.

// coerceRowField decodes the "row" member. Absent or null yields NoWorkflowRow.
func coerceRowField(raw json.RawMessage) (domain.WorkflowRow, error) {
	n, ok, err := coerceIntegerField("row", raw)
	if err != nil || !ok {
		return domain.NoWorkflowRow, err
	}
	return domain.WorkflowRow(n), nil
}

// coerceStageField decodes the "stage" member into a stage number and, for
// the group form, the group. Absent or null yields (0, "").
func coerceStageField(raw json.RawMessage) (domain.StageNumber, domain.GroupName, error) {
	if len(raw) > 0 && raw[0] == '"' {
		var s string
		if err := json.Unmarshal(raw, &s); err != nil {
			return 0, "", malformedField("stage", err.Error())
		}
		if group, n, ok := domain.ParseStageValue(s); ok {
			return n, group, nil
		}
	}
	n, ok, err := coerceIntegerField("stage", raw)
	if err != nil || !ok {
		return 0, "", err
	}
	return domain.StageNumber(n), "", nil
}

// coerceIntegerField decodes a JSON integer or a decimal string. ok is false
// for an absent or null value.
func coerceIntegerField(fieldName string, raw json.RawMessage) (n int, ok bool, err error) {
	if len(raw) == 0 || string(raw) == "null" {
		return 0, false, nil
	}
	text := string(raw)
	if raw[0] == '"' {
		var s string
		if err := json.Unmarshal(raw, &s); err != nil {
			return 0, false, malformedField(fieldName, err.Error())
		}
		text = s
	}
	v, convErr := strconv.Atoi(text)
	if convErr != nil {
		return 0, false, malformedField(fieldName, fmt.Sprintf("must be an integer, got: %s", string(raw)))
	}
	return v, true, nil
}

func malformedField(fieldName, detail string) error {
	return &domain.ConsultationError{
		Failure: domain.ConsultFailMalformedJSON,
		Detail:  fmt.Sprintf("malformed routing response: %s: %s", fieldName, detail),
	}
}
