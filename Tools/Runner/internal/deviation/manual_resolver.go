package deviation

import (
	"context"
	"fmt"

	"mosaic-common/interaction"
	"mosaic-run/internal/domain"
)

// ManualResolver resolves deviations by driving the Interaction port to
// obtain a RoutingInstruction from the user.
type ManualResolver struct {
	// Interact is the user-interaction port. Must not block indefinitely in
	// non-interactive (CLI) mode.
	Interact domain.Interaction
	// Table is the routing table for the current workflow; used to populate
	// the row-selection prompt with agent identifier labels.
	Table domain.RoutingTable
}

// ConsultRouting implements domain.RoutingConsultant by driving the Interaction
// port. It presents one option per routing table row plus a mandatory stop
// option (ID "stop"), then collects a free-text task description for the chosen
// agent. HITLOverride is never set here — it is nil in every returned
// DispatchInstruction.
func (m *ManualResolver) ConsultRouting(ctx context.Context, req domain.ConsultationRequest) (domain.RoutingInstruction, error) {
	// Build one option per routing table row, using the agent identifier as the
	// option ID so we can map the user's selection back to a row without
	// relying on numeric index strings.
	options := make([]interaction.Option, 0, len(m.Table.Rows)+1)
	for _, row := range m.Table.Rows {
		options = append(options, interaction.Option{
			ID:    row.Agent,
			Label: fmt.Sprintf("[%d] %s (%s)", row.Index, row.Agent, row.Phase),
		})
	}
	// The stop option must have ID "stop" per the ManualResolver contract.
	options = append(options, interaction.Option{
		ID:    "stop",
		Label: "Stop the run",
	})

	q := interaction.ChoiceQuestion{
		Question: interaction.Question{
			Title:  "Manual Routing",
			Prompt: "Choose which agent to dispatch next, or stop the run:",
		},
		Options: options,
	}

	answer, err := m.Interact.SelectOne(ctx, q)
	if err != nil {
		return domain.RoutingInstruction{}, &domain.ConsultationError{
			Failure: domain.ConsultFailUserAbandoned,
			Detail:  fmt.Sprintf("SelectOne failed: %v", err),
			Err:     err,
		}
	}

	if answer.OptionID == "stop" {
		return domain.RoutingInstruction{
			Stop: &domain.StopInstruction{Reason: "user requested stop"},
		}, nil
	}

	// Resolve the selected agent identifier to its routing table row.
	selectedAgent := answer.OptionID
	rowIndex := -1
	for _, row := range m.Table.Rows {
		if row.Agent == selectedAgent {
			rowIndex = row.Index
			break
		}
	}

	// Collect the free-text task description from the user.
	textQ := interaction.TextQuestion{
		Question: interaction.Question{
			Title:  "Task Description",
			Prompt: fmt.Sprintf("Enter task description for %s:", selectedAgent),
		},
	}
	textAns, err := m.Interact.AskText(ctx, textQ)
	if err != nil {
		return domain.RoutingInstruction{}, &domain.ConsultationError{
			Failure: domain.ConsultFailUserAbandoned,
			Detail:  fmt.Sprintf("AskText failed: %v", err),
			Err:     err,
		}
	}

	return domain.RoutingInstruction{
		Dispatch: &domain.DispatchInstruction{
			Agent:           selectedAgent,
			RowIndex:        rowIndex,
			TaskDescription: textAns.Text,
			// HITLOverride intentionally nil: ManualResolver never originates a HITL override.
		},
	}, nil
}
