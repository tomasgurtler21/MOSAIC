// Package deviation implements the DeviationResolver port.
//
// Two implementations are provided:
//   - OrchestratorDelegate: delegates to the script-mode orchestrator agent
//     (identified by its AgentReference) via the HarnessAdapter. The orchestrator
//     agent's response result_data encodes a machine-readable RejoinInstruction
//     (see Script-Mode Orchestrator Response Schema in ContractsDesign.md).
//   - ManualResolver: drives the Interaction port to compose a RejoinInstruction
//     from user input.
//
// Neither implementation originates HITL lowering. A HITLOverride value in
// the RejoinInstruction is only ever carried from an explicit instruction in
// the orchestrator's result_data or an explicit user choice -- it is never
// inserted by the resolver itself.
package deviation

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"

	"mosaic-common/interaction"
	"mosaic-run/internal/domain"
)

// orchestratorInstruction is the JSON schema produced by the script-mode
// orchestrator in its result_data field (FR-22).
type orchestratorInstruction struct {
	Action            string                  `json:"action"`              // "rejoin" | "custom" | "stop"
	RejoinAgent       string                  `json:"rejoin_agent"`        // when action="rejoin"
	HITLOverride      *bool                   `json:"hitl_override"`       // optional override
	CustomRequest     *domain.ProtocolRequest `json:"custom_request"`      // when action="custom"
	CustomAgent       string                  `json:"custom_agent"`        // when action="custom": agent identifier for the harness invocation
	RejoinAfterCustom string                  `json:"rejoin_after_custom"` // when action="custom"
	Reason            string                  `json:"reason"`              // when action="stop"
}

// OrchestratorDelegate resolves deviations by invoking the script-mode
// orchestrator agent through the HarnessAdapter and parsing its result_data
// as a RejoinInstruction.
//
// The routing table is required to resolve the orchestrator's rejoin_agent
// (an agent identifier string) to a row index in the admitted workflow.
type OrchestratorDelegate struct {
	// Harness is used to invoke the orchestrator agent.
	Harness domain.HarnessAdapter
	// Orchestrator is the AgentReference for the script-mode orchestrator.
	Orchestrator domain.AgentReference
	// Table is the routing table for the current workflow; used to map
	// rejoin_agent identifiers back to row indices.
	Table domain.RoutingTable
	// ArtifactPath is the path to Orchestration.md, used as the
	// input_artifacts / output_artifacts in the orchestrator's protocol request.
	ArtifactPath string
	// NextSeq returns the next agent-instance sequence number. Called once
	// per orchestrator invocation. May be a simple counter closure.
	NextSeq func() int
}

// Resolve implements domain.DeviationResolver.
//
// It assembles a protocol request for the script-mode orchestrator, invokes it
// via the harness, and parses the result_data JSON into a RejoinInstruction.
// HITL override values are carried only from the explicit instruction -- never
// originated here (AC8.2).
func (d *OrchestratorDelegate) Resolve(ctx context.Context, info domain.DeviationInfo) (domain.RejoinInstruction, error) {
	seq := d.NextSeq()
	instanceID := fmt.Sprintf("%s#%d", d.Orchestrator.Identifier, seq)

	taskDesc := fmt.Sprintf("Resolve deviation: %s returned %s at row %d in phase %s",
		info.Response.AgentInstanceID,
		info.Response.StatusCode,
		info.CurrentRow,
		info.CurrentPhase,
	)

	req := domain.ProtocolRequest{
		AgentInstanceID:      instanceID,
		TaskDescription:      taskDesc,
		InputArtifacts:       []string{d.ArtifactPath},
		OutputArtifacts:      []string{d.ArtifactPath},
		IncludeResultSummary: true,
		HumanInTheLoop:       false,
	}

	resp, err := d.Harness.Invoke(ctx, d.Orchestrator, req)
	if err != nil {
		return domain.RejoinInstruction{}, fmt.Errorf("deviation: harness error invoking orchestrator: %w", err)
	}

	if resp.StatusCode != domain.StatusSUCCESS {
		return domain.RejoinInstruction{}, fmt.Errorf("deviation: orchestrator returned non-SUCCESS status %q", resp.StatusCode)
	}

	if resp.ResultData == "" {
		return domain.RejoinInstruction{}, fmt.Errorf("deviation: orchestrator returned SUCCESS but result_data is empty")
	}

	var instr orchestratorInstruction
	if err := json.Unmarshal([]byte(resp.ResultData), &instr); err != nil {
		return domain.RejoinInstruction{}, fmt.Errorf("deviation: malformed result_data JSON: %w", err)
	}

	switch instr.Action {
	case "rejoin":
		rowIdx, err := d.findRowForAgent(instr.RejoinAgent)
		if err != nil {
			return domain.RejoinInstruction{}, err
		}
		return domain.RejoinInstruction{
			Rejoin: &domain.RejoinAtRow{
				RowIndex:     rowIdx,
				HITLOverride: instr.HITLOverride,
			},
		}, nil

	case "stop":
		return domain.RejoinInstruction{
			Stop: &domain.StopRun{Reason: instr.Reason},
		}, nil

	case "custom":
		if instr.CustomRequest == nil {
			return domain.RejoinInstruction{}, fmt.Errorf("deviation: action=custom but custom_request field is absent or null")
		}
		rejoinRowIdx, err := d.findRowForAgent(instr.RejoinAfterCustom)
		if err != nil {
			return domain.RejoinInstruction{}, fmt.Errorf("deviation: rejoin_after_custom: %w", err)
		}
		// Resolve the custom agent identifier to an AgentReference using the
		// routing table. When custom_agent is absent the Agent field is left
		// empty; the session will refuse the invocation with a clear error
		// rather than calling Harness.Invoke with a zero AgentReference.
		var customAgent domain.AgentReference
		if instr.CustomAgent != "" {
			customAgent = domain.AgentReference{Identifier: instr.CustomAgent}
		}
		return domain.RejoinInstruction{
			Custom: &domain.CustomDispatch{
				Request:      *instr.CustomRequest,
				Agent:        customAgent,
				HITLOverride: instr.HITLOverride,
				RejoinRow:    rejoinRowIdx,
			},
		}, nil

	default:
		return domain.RejoinInstruction{}, fmt.Errorf("deviation: unknown action %q in orchestrator instruction", instr.Action)
	}
}

// findRowForAgent returns the index of the first routing table row whose
// Agent matches agentName, or an error if not found.
func (d *OrchestratorDelegate) findRowForAgent(agentName string) (int, error) {
	for _, row := range d.Table.Rows {
		if row.Agent == agentName {
			return row.Index, nil
		}
	}
	return -1, fmt.Errorf("deviation: agent %q not found in routing table", agentName)
}

// ManualResolver resolves deviations by driving the Interaction port to
// obtain a RejoinInstruction from the user.
type ManualResolver struct {
	// Interact is the user-interaction port. Must not block indefinitely in
	// non-interactive (CLI) mode.
	Interact domain.Interaction
	// Table is the routing table for the current workflow; used to populate
	// the row-selection prompt with agent identifier labels.
	Table domain.RoutingTable
}

// Resolve implements domain.DeviationResolver.
//
// It presents a SelectOne prompt with one option per routing table row and a
// mandatory stop option (ID="stop"). If the user selects a row, it asks
// whether to override HITL for the next invocation; the HITLOverride is set
// only when the user explicitly confirms. HITL lowering is never originated
// here -- it is only carried when the user explicitly requests it (AC8.2).
func (m *ManualResolver) Resolve(ctx context.Context, info domain.DeviationInfo) (domain.RejoinInstruction, error) {
	// Build one option per routing table row.
	options := make([]interaction.Option, 0, len(m.Table.Rows)+1)
	for _, row := range m.Table.Rows {
		options = append(options, interaction.Option{
			ID:    strconv.Itoa(row.Index),
			Label: fmt.Sprintf("[%d] %s (%s)", row.Index, row.Agent, row.Phase),
		})
	}
	// Always include a stop option; its ID must be "stop" by convention.
	options = append(options, interaction.Option{
		ID:    "stop",
		Label: "Stop the run",
	})

	q := interaction.ChoiceQuestion{
		Question: interaction.Question{
			Title:  "Resolve Deviation",
			Prompt: fmt.Sprintf("Deviation at row %d (phase %s): choose how to proceed", info.CurrentRow, info.CurrentPhase),
		},
		Options: options,
	}

	answer, err := m.Interact.SelectOne(ctx, q)
	if err != nil {
		return domain.RejoinInstruction{}, fmt.Errorf("deviation: SelectOne failed: %w", err)
	}

	if answer.OptionID == "stop" {
		return domain.RejoinInstruction{Stop: &domain.StopRun{Reason: "user requested stop"}}, nil
	}

	rowIdx, err := strconv.Atoi(answer.OptionID)
	if err != nil {
		return domain.RejoinInstruction{}, fmt.Errorf("deviation: invalid option ID %q: %w", answer.OptionID, err)
	}

	// Ask whether the user wants to override HITL for the next invocation.
	// HITLOverride is only set when the user explicitly confirms -- it is
	// never originated by the resolver (AC8.2).
	confirmQ := interaction.Question{
		Title:  "HITL Override",
		Prompt: "Override HITL for the next invocation?",
	}
	confirmAns, err := m.Interact.Confirm(ctx, confirmQ)
	if err != nil {
		return domain.RejoinInstruction{}, fmt.Errorf("deviation: Confirm failed: %w", err)
	}

	var hitlOverride *bool
	if confirmAns.Confirm {
		textQ := interaction.TextQuestion{
			Question: interaction.Question{
				Title:  "HITL Value",
				Prompt: "New HITL value (true/false):",
			},
		}
		textAns, err := m.Interact.AskText(ctx, textQ)
		if err != nil {
			return domain.RejoinInstruction{}, fmt.Errorf("deviation: AskText failed: %w", err)
		}
		hitlVal := textAns.Text == "true"
		hitlOverride = &hitlVal
	}

	return domain.RejoinInstruction{
		Rejoin: &domain.RejoinAtRow{
			RowIndex:     rowIdx,
			HITLOverride: hitlOverride,
		},
	}, nil
}
