// Package deviation implements the two-action orchestrator consultation contract.
//
// Two implementations are provided:
//   - OrchestratorConsultant: implements the two-action consultation contract
//     (domain.RoutingConsultant and domain.PreConsultant) by carrying a raw JSON
//     payload to the orchestrator over the RawInvoker transport.
//   - ManualResolver: drives the Interaction port to produce a RoutingInstruction
//     from user input.
//
// Neither OrchestratorConsultant nor ManualResolver originates a HITL override.
// HITLOverride is carried only when explicitly set by the orchestrator's response
// or when the user explicitly asks for one.
package deviation

import (
	"context"
	"encoding/json"
	"fmt"

	"mosaic-common/interaction"
	"mosaic-run/internal/domain"
)

// ---- wire types for the two-action orchestrator consultation contract ----

// wireRequest is the JSON payload the Runner sends to the orchestrator for both
// routing and pre-consultation. All three fields are always present on the wire;
// last_status_message is null (not absent) when the ConsultationRequest carries nil.
type wireRequest struct {
	OrchestrationArtifact string  `json:"orchestration_artifact"`
	Context               string  `json:"context"`
	LastStatusMessage     *string `json:"last_status_message"` // no omitempty: null must be emitted
}

// wireRoutingResponse is the JSON payload the orchestrator returns for a routing
// consultation. Unknown extra fields are silently ignored (AC3.4).
type wireRoutingResponse struct {
	Action          string    `json:"action"`          // "dispatch" | "stop"
	Agent           string    `json:"agent"`           // dispatch only; required
	TaskDescription string    `json:"task_description"` // dispatch only; required
	Constraints     *string   `json:"constraints"`     // optional; nil = fall back to table row
	InputArtifacts  *[]string `json:"input_artifacts"` // optional; nil = fall back; non-nil empty = "no inputs"
	OutputArtifacts *[]string `json:"output_artifacts"` // optional; same semantics
	HITLOverride    *bool     `json:"hitl_override"`   // optional; nil = fall back
	Reason          string    `json:"reason"`          // stop only
}

// wirePreConsultResponse is the JSON payload the orchestrator returns for a
// pre-consultation. Both fields are optional strings.
type wirePreConsultResponse struct {
	TaskDescription string `json:"task_description"`
	Constraints     string `json:"constraints"`
}

// OrchestratorConsultant implements domain.RoutingConsultant and
// domain.PreConsultant by invoking the script-mode orchestrator agent over the
// raw transport and parsing the two-action contract.
type OrchestratorConsultant struct {
	// Invoker carries the raw JSON payload to the orchestrator.
	Invoker domain.RawInvoker
	// Orchestrator is the resolved orchestrator agent. Its InvocationKind must
	// be domain.InvocationOrchestrator.
	Orchestrator domain.AgentReference
	// Table resolves a dispatch instruction's agent identifier to a row index
	// and supplies the available-agent list for the unknown-agent error.
	Table domain.RoutingTable
}

// ConsultRouting implements domain.RoutingConsultant. It serialises the
// ConsultationRequest onto the two-action wire schema, invokes the orchestrator
// via RawInvoker, and parses the reply into a RoutingInstruction.
//
// Failure classes returned:
//   - ConsultFailTransport: harness error from InvokeRaw
//   - ConsultFailMalformedJSON: reply is not valid JSON
//   - ConsultFailUnknownAction: action is not "dispatch" or "stop"
//   - ConsultFailMissingField: required field (agent, task_description) absent or empty
//   - ConsultFailUnknownAgent: dispatch agent not in the routing table
func (c *OrchestratorConsultant) ConsultRouting(ctx context.Context, req domain.ConsultationRequest) (domain.RoutingInstruction, error) {
	wr := wireRequest{
		OrchestrationArtifact: req.OrchestrationArtifact,
		Context:               string(req.Context),
		LastStatusMessage:     req.LastStatusMessage,
	}
	payload, err := json.Marshal(wr)
	if err != nil {
		// Marshal of a struct with only string and *string fields cannot fail
		// in practice, but we return a classified error rather than panic.
		return domain.RoutingInstruction{}, &domain.ConsultationError{
			Failure: domain.ConsultFailMalformedJSON,
			Detail:  fmt.Sprintf("failed to marshal consultation request: %v", err),
			Err:     err,
		}
	}

	reply, err := c.Invoker.InvokeRaw(ctx, c.Orchestrator, payload)
	if err != nil {
		return domain.RoutingInstruction{}, &domain.ConsultationError{
			Failure: domain.ConsultFailTransport,
			Detail:  fmt.Sprintf("InvokeRaw failed: %v", err),
			Err:     err,
		}
	}

	extracted, err := ExtractJSONObject(reply)
	if err != nil {
		return domain.RoutingInstruction{}, err
	}
	var resp wireRoutingResponse
	if err := json.Unmarshal(extracted, &resp); err != nil {
		return domain.RoutingInstruction{}, &domain.ConsultationError{
			Failure: domain.ConsultFailMalformedJSON,
			Detail:  fmt.Sprintf("malformed routing response JSON: %v", err),
			Err:     err,
		}
	}

	switch resp.Action {
	case "dispatch":
		if resp.Agent == "" {
			return domain.RoutingInstruction{}, &domain.ConsultationError{
				Failure: domain.ConsultFailMissingField,
				Detail:  "missing required field: agent",
			}
		}
		if resp.TaskDescription == "" {
			return domain.RoutingInstruction{}, &domain.ConsultationError{
				Failure: domain.ConsultFailMissingField,
				Detail:  "missing required field: task_description",
			}
		}
		rowIndex := -1
		for _, row := range c.Table.Rows {
			if row.Agent == resp.Agent {
				rowIndex = row.Index
				break
			}
		}
		if rowIndex == -1 {
			agents := make([]string, 0, len(c.Table.Rows))
			for _, row := range c.Table.Rows {
				agents = append(agents, row.Agent)
			}
			return domain.RoutingInstruction{}, &domain.ConsultationError{
				Failure: domain.ConsultFailUnknownAgent,
				Detail:  fmt.Sprintf("agent %q not found in routing table; available: %v", resp.Agent, agents),
				Agents:  agents,
			}
		}
		return domain.RoutingInstruction{
			Dispatch: &domain.DispatchInstruction{
				Agent:           resp.Agent,
				RowIndex:        rowIndex,
				TaskDescription: resp.TaskDescription,
				Constraints:     resp.Constraints,
				InputArtifacts:  resp.InputArtifacts,
				OutputArtifacts: resp.OutputArtifacts,
				HITLOverride:    resp.HITLOverride,
			},
		}, nil

	case "stop":
		return domain.RoutingInstruction{
			Stop: &domain.StopInstruction{Reason: resp.Reason},
		}, nil

	default:
		return domain.RoutingInstruction{}, &domain.ConsultationError{
			Failure: domain.ConsultFailUnknownAction,
			Detail:  fmt.Sprintf("unknown action %q (valid: dispatch, stop)", resp.Action),
		}
	}
}

// PreConsult implements domain.PreConsultant. It invokes the orchestrator with
// context=pre_consultation and last_status_message=null (always, per the contract),
// then parses the optional-string-pair reply into a PreConsultationAdvice.
//
// Failure classes returned:
//   - ConsultFailTransport: harness error from InvokeRaw
//   - ConsultFailMalformedJSON: reply is not valid JSON
func (c *OrchestratorConsultant) PreConsult(ctx context.Context, req domain.ConsultationRequest) (domain.PreConsultationAdvice, error) {
	wr := wireRequest{
		OrchestrationArtifact: req.OrchestrationArtifact,
		Context:               string(domain.ConsultContextPreConsultation),
		LastStatusMessage:     nil, // always null for pre-consultation, per the contract
	}
	payload, err := json.Marshal(wr)
	if err != nil {
		return domain.PreConsultationAdvice{}, &domain.ConsultationError{
			Failure: domain.ConsultFailMalformedJSON,
			Detail:  fmt.Sprintf("failed to marshal pre-consultation request: %v", err),
			Err:     err,
		}
	}

	reply, err := c.Invoker.InvokeRaw(ctx, c.Orchestrator, payload)
	if err != nil {
		return domain.PreConsultationAdvice{}, &domain.ConsultationError{
			Failure: domain.ConsultFailTransport,
			Detail:  fmt.Sprintf("InvokeRaw failed: %v", err),
			Err:     err,
		}
	}

	extracted, err := ExtractJSONObject(reply)
	if err != nil {
		return domain.PreConsultationAdvice{}, err
	}
	var resp wirePreConsultResponse
	if err := json.Unmarshal(extracted, &resp); err != nil {
		return domain.PreConsultationAdvice{}, &domain.ConsultationError{
			Failure: domain.ConsultFailMalformedJSON,
			Detail:  fmt.Sprintf("malformed pre-consultation response JSON: %v", err),
			Err:     err,
		}
	}

	return domain.PreConsultationAdvice{
		TaskDescription: resp.TaskDescription,
		Constraints:     resp.Constraints,
	}, nil
}

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

