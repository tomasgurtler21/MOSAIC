package session

import (
	"context"
	"fmt"
	"strconv"

	"mosaic-common/interaction"
	"mosaic-run/internal/domain"
)

// stopBeforeConsult reports whether a graceful stop was requested before a
// routing consultation begins. The consultation then does not start: it
// writes nothing to the artifact, so a resume derives the same decision again.
func (s *sessionImpl) stopBeforeConsult() (bool, domain.RunOutcome) {
	if !s.deps.StopRequested() {
		return false, domain.RunOutcome{}
	}
	s.deps.Debug.Log(domain.EventSessionStopObserved, "graceful stop observed; not consulting",
		domain.F("checkpoint", StopCheckpointConsultEntry),
	)
	return true, domain.RunOutcome{Status: domain.RunStopped, Message: "run stopped: graceful stop confirmed"}
}

// discardDecisionOnStop handles a stop requested while a consultation was in
// progress. The completed decision is not dispatched; it is discarded visibly:
// a warning notice, a debug log entry and a stop outcome message that names
// it. The artifact is untouched, so a resume re-derives the decision.
func (s *sessionImpl) discardDecisionOnStop(ctx context.Context, dispInstr *domain.DispatchInstruction, stage string) (bool, domain.RunOutcome) {
	if !s.deps.StopRequested() {
		return false, domain.RunOutcome{}
	}
	row := dispInstr.RowIndex + 1
	subject := fmt.Sprintf("%s (workflow row %d", dispInstr.Agent, row)
	if stage != "" {
		subject += ", stage " + stage
	}
	subject += ")"
	s.deps.Debug.Log(domain.EventSessionConsultDiscarded, "routing decision completed after a stop request; discarded",
		domain.F("agent", dispInstr.Agent),
		domain.F("row", strconv.Itoa(row)),
		domain.F("stage", stage),
	)
	s.deps.Debug.Log(domain.EventSessionStopObserved, "graceful stop observed; not dispatching",
		domain.F("checkpoint", StopCheckpointConsultDispatch),
	)
	s.deps.Interact.Notify(ctx, interaction.Notice{
		Level:   interaction.NoticeWarning,
		Title:   "Routing decision discarded",
		Message: fmt.Sprintf("The routing decision to dispatch %s was discarded because of the stop request; it will be re-derived on resume.", subject),
	})
	return true, domain.RunOutcome{
		Status: domain.RunStopped,
		Message: fmt.Sprintf("run stopped: graceful stop confirmed; %s: %s; it will be re-derived on resume",
			DiscardedRoutingDecisionNote, subject),
	}
}
