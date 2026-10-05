package mosaiclog

import (
	"context"
	"errors"

	"mosaic-run/internal/domain"
)

// Outcome is the run_end outcome vocabulary.
type Outcome string

const (
	OutcomeCompleted   Outcome = "completed"
	OutcomeStopped     Outcome = "stopped"
	OutcomeFailed      Outcome = "failed"
	OutcomeAborted     Outcome = "aborted"
	OutcomeInterrupted Outcome = "interrupted"
)

// OutcomeForRun maps the result of session.Session.Start onto Outcome.
// Pinned: ctxErr != nil or errors.Is(err, context.Canceled) -> interrupted
// (checked first); domain.RunRefused -> aborted. Never returns "".
//
// Chosen mapping of the remaining values:
//   - completed -> completed
//   - stopped, stopped-by-consultant, deviation-unresolved -> stopped (the run
//     halted deliberately or awaiting a decision and can be resumed)
//   - failed, start-failed, an unknown or empty status, any other error -> failed
func OutcomeForRun(o domain.RunOutcome, err error, ctxErr error) Outcome {
	if ctxErr != nil || errors.Is(err, context.Canceled) {
		return OutcomeInterrupted
	}
	if err != nil {
		return OutcomeFailed
	}
	switch o.Status {
	case domain.RunCompleted:
		return OutcomeCompleted
	case domain.RunStopped, domain.RunStoppedByConsultant, domain.RunDeviationUnresolved:
		return OutcomeStopped
	case domain.RunRefused:
		return OutcomeAborted
	default:
		return OutcomeFailed
	}
}
