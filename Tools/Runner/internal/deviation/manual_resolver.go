package deviation

import (
	"context"

	"mosaic-run/internal/domain"
)

// ManualResolver resolves deviations by driving the Interaction port to
// obtain a RoutingInstruction from the user.
type ManualResolver struct {
	// Interact is the user-interaction port. Must not block indefinitely in
	// non-interactive (CLI) mode.
	Interact domain.Interaction
	// Table is the routing table for the current workflow; used to populate
	// the row-selection prompt with 1-based row labels and to validate the result.
	Table domain.RoutingTable
}

// ConsultRouting implements domain.RoutingConsultant by running the manual
// routing dialogue over the Interaction port: row (with a mandatory stop
// option, ID domain.ManualStopOptionID), stage for a staged row, task
// message, input and output artifacts, constraints and a HITL override.
// Esc (a cancelled answer) returns to the previous step. See manualDialogue.
//
// Choosing the stop option returns a StopInstruction. A complete result is
// checked with domain.ValidateDispatchTarget before it is returned; a rejected
// one is never returned, the user is taken back to correct it.
//
// Routing that ends without a dispatch (Esc at the first step, an unavailable
// Interaction, an exceeded bound) is returned as a *domain.ConsultationError
// with the matching failure class and no instruction.
func (m *ManualResolver) ConsultRouting(ctx context.Context, req domain.ConsultationRequest) (domain.RoutingInstruction, error) {
	return newManualDialogue(m, req).run(ctx)
}
