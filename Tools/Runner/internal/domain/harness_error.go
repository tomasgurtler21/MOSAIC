package domain

// HarnessErrorResponse builds the synthetic response recorded for a
// harness-level failure: StatusCode BLOCKED, ErrorCode E501, StatusMessage
// and ErrorReason both err.Error(), AgentInstanceID and RunID as given.
func HarnessErrorResponse(agentInstanceID, runID string, err error) ProtocolResponse {
	msg := err.Error()
	return ProtocolResponse{
		AgentInstanceID: agentInstanceID,
		RunID:           runID,
		StatusCode:      "BLOCKED",
		StatusMessage:   msg,
		ErrorCode:       "E501",
		ErrorReason:     msg,
	}
}

// LastErrorReasonFor returns the last_error_reason value for a consultation
// triggered by resp: nil when resp is nil or resp.StatusCode != BLOCKED;
// otherwise a pointer to resp.ErrorReason verbatim (possibly "").
func LastErrorReasonFor(resp *ProtocolResponse) *string {
	if resp == nil || resp.StatusCode != "BLOCKED" {
		return nil
	}
	reason := resp.ErrorReason
	return &reason
}
