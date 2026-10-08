package session

import (
	"fmt"

	"mosaic-run/internal/domain"
)

// maxConsecutiveGuardEscalations bounds the guard's own escalation path: after
// this many consecutive escalations in which the consultant keeps requesting
// the dispatch the guard blocks, the run ends resumably instead of consulting
// again.
const maxConsecutiveGuardEscalations = 2

// guardEscalationExhausted records one guard escalation for the blocked
// dispatch of agentID at rowIndex. When the consecutive escalations exceed
// maxConsecutiveGuardEscalations it returns the resumable ending outcome and
// true; the caller must then end the run instead of consulting. Blocked
// attempts write no Execution Log row, so the artifact stays resumable.
func (s *sessionImpl) guardEscalationExhausted(a *antiLoopState, agentID string, rowIndex int) (domain.RunOutcome, bool) {
	a.escalations++
	if a.escalations <= maxConsecutiveGuardEscalations {
		return domain.RunOutcome{}, false
	}
	msg := fmt.Sprintf("anti-loop guard: the consultant kept requesting the dispatch of %q at row %d that the anti-loop guard blocked; run stopped, resume to continue",
		agentID, rowIndex+1)
	s.deps.Debug.Log(domain.EventSessionDeviationUnresolved, msg)
	return domain.RunOutcome{Status: domain.RunStopped, Message: msg}, true
}
