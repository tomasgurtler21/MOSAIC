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
	"errors"
	"fmt"
	"sync/atomic"

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

// wireRoutingRaw is the intermediate representation used by
// unmarshalRoutingResponseLenient. The three fields that an LLM may emit with
// a wrong JSON type (input_artifacts, output_artifacts, hitl_override) are
// captured as raw bytes so the coercion step can inspect and convert them.
// All other fields unmarshal directly into their target types.
type wireRoutingRaw struct {
	Action          string          `json:"action"`
	Agent           string          `json:"agent"`
	TaskDescription string          `json:"task_description"`
	Constraints     *string         `json:"constraints"`
	InputArtifacts  json.RawMessage `json:"input_artifacts"`
	OutputArtifacts json.RawMessage `json:"output_artifacts"`
	HITLOverride    json.RawMessage `json:"hitl_override"`
	Reason          string          `json:"reason"`
}

// unmarshalRoutingResponseLenient decodes the extracted JSON object into a
// wireRoutingResponse, applying type coercion for three fields where an LLM
// may produce a type mismatch:
//   - input_artifacts / output_artifacts: bare JSON string coerced to a
//     single-element []string; any other non-array value is an error.
//   - hitl_override: JSON string "true" / "false" coerced to bool; any other
//     non-boolean value is an error.
//
// Absent and null fields in the three special positions remain nil pointers,
// preserving existing fallback semantics. Genuinely uncoercible values (e.g. a
// JSON number where an array is expected) return ConsultFailMalformedJSON.
func unmarshalRoutingResponseLenient(data []byte) (wireRoutingResponse, error) {
	var raw wireRoutingRaw
	if err := json.Unmarshal(data, &raw); err != nil {
		return wireRoutingResponse{}, &domain.ConsultationError{
			Failure: domain.ConsultFailMalformedJSON,
			Detail:  fmt.Sprintf("malformed routing response JSON: %v", err),
			Err:     err,
		}
	}

	resp := wireRoutingResponse{
		Action:          raw.Action,
		Agent:           raw.Agent,
		TaskDescription: raw.TaskDescription,
		Constraints:     raw.Constraints,
		Reason:          raw.Reason,
	}

	var err error
	resp.InputArtifacts, err = coerceStringSliceField("input_artifacts", raw.InputArtifacts)
	if err != nil {
		return wireRoutingResponse{}, err
	}
	resp.OutputArtifacts, err = coerceStringSliceField("output_artifacts", raw.OutputArtifacts)
	if err != nil {
		return wireRoutingResponse{}, err
	}
	resp.HITLOverride, err = coerceBoolField("hitl_override", raw.HITLOverride)
	if err != nil {
		return wireRoutingResponse{}, err
	}

	return resp, nil
}

// coerceStringSliceField parses raw JSON for a *[]string field. Absent/null
// raw values yield a nil pointer. A JSON array is decoded normally. A JSON
// string is coerced to a single-element slice. Any other JSON type returns
// ConsultFailMalformedJSON.
func coerceStringSliceField(fieldName string, raw json.RawMessage) (*[]string, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	// JSON array — standard decode.
	if raw[0] == '[' {
		var s []string
		if err := json.Unmarshal(raw, &s); err != nil {
			return nil, &domain.ConsultationError{
				Failure: domain.ConsultFailMalformedJSON,
				Detail:  fmt.Sprintf("malformed routing response: cannot parse %s as string array: %v", fieldName, err),
				Err:     err,
			}
		}
		return &s, nil
	}
	// JSON string — coerce to single-element slice.
	if raw[0] == '"' {
		var s string
		if err := json.Unmarshal(raw, &s); err != nil {
			return nil, &domain.ConsultationError{
				Failure: domain.ConsultFailMalformedJSON,
				Detail:  fmt.Sprintf("malformed routing response: cannot parse %s as string: %v", fieldName, err),
				Err:     err,
			}
		}
		result := []string{s}
		return &result, nil
	}
	// Any other JSON type (number, bool, object) cannot be coerced.
	return nil, &domain.ConsultationError{
		Failure: domain.ConsultFailMalformedJSON,
		Detail:  fmt.Sprintf("malformed routing response: %s must be a string array or a bare string, got: %s", fieldName, string(raw)),
	}
}

// coerceBoolField parses raw JSON for a *bool field. Absent/null raw values
// yield a nil pointer. A JSON boolean is decoded normally. The JSON strings
// "true" and "false" are coerced to the corresponding bool. Any other value
// returns ConsultFailMalformedJSON.
func coerceBoolField(fieldName string, raw json.RawMessage) (*bool, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	// JSON boolean — standard decode.
	if string(raw) == "true" || string(raw) == "false" {
		var b bool
		if err := json.Unmarshal(raw, &b); err != nil {
			return nil, &domain.ConsultationError{
				Failure: domain.ConsultFailMalformedJSON,
				Detail:  fmt.Sprintf("malformed routing response: cannot parse %s as bool: %v", fieldName, err),
				Err:     err,
			}
		}
		return &b, nil
	}
	// JSON string — coerce only "true" and "false".
	if raw[0] == '"' {
		var s string
		if err := json.Unmarshal(raw, &s); err != nil {
			return nil, &domain.ConsultationError{
				Failure: domain.ConsultFailMalformedJSON,
				Detail:  fmt.Sprintf("malformed routing response: cannot parse %s as string: %v", fieldName, err),
				Err:     err,
			}
		}
		switch s {
		case "true":
			b := true
			return &b, nil
		case "false":
			b := false
			return &b, nil
		default:
			return nil, &domain.ConsultationError{
				Failure: domain.ConsultFailMalformedJSON,
				Detail:  fmt.Sprintf("malformed routing response: %s string value must be \"true\" or \"false\", got: %q", fieldName, s),
			}
		}
	}
	// Any other JSON type (number, object, array) cannot be coerced.
	return nil, &domain.ConsultationError{
		Failure: domain.ConsultFailMalformedJSON,
		Detail:  fmt.Sprintf("malformed routing response: %s must be a bool or a string \"true\"/\"false\", got: %s", fieldName, string(raw)),
	}
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
	// DispatchLogger records each consultation invocation in the same dispatch
	// log that invokeAndLog uses for subagent dispatches. When nil, no logging
	// is performed. The field is set by buildDeps alongside the subagent logger.
	DispatchLogger domain.DispatchLogger

	// callSeq is a per-instance monotonic counter that gives every ConsultRouting
	// and PreConsult call a unique dispatch-log identifier. Zero is the valid
	// unstarted state; no constructor is needed.
	callSeq uint64
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

	const maxAttempts = 3
	var lastErr error
	for attempt := 0; attempt < maxAttempts; attempt++ {
		if ctx.Err() != nil {
			return domain.RoutingInstruction{}, &domain.ConsultationError{
				Failure: domain.ConsultFailTransport,
				Detail:  fmt.Sprintf("context cancelled: %v", ctx.Err()),
				Err:     ctx.Err(),
			}
		}

		seq := atomic.AddUint64(&c.callSeq, 1)
		consultationInstanceID := fmt.Sprintf("%s#%s#%d", c.Orchestrator.Identifier, req.Context, seq)
		if c.DispatchLogger != nil {
			c.DispatchLogger.LogRequest(domain.ProtocolRequest{
				AgentInstanceID: consultationInstanceID,
				TaskDescription: string(payload),
			})
		}

		reply, err := c.Invoker.InvokeRaw(ctx, c.Orchestrator, payload)
		if err != nil {
			if c.DispatchLogger != nil {
				c.DispatchLogger.LogError(consultationInstanceID, err.Error())
			}
			return domain.RoutingInstruction{}, &domain.ConsultationError{
				Failure: domain.ConsultFailTransport,
				Detail:  fmt.Sprintf("InvokeRaw failed: %v", err),
				Err:     err,
			}
		}
		if c.DispatchLogger != nil {
			c.DispatchLogger.LogResponse(domain.ProtocolResponse{
				AgentInstanceID: consultationInstanceID,
				StatusMessage:   string(reply),
			})
		}

		extracted, extractErr := ExtractJSONObject(reply)
		if extractErr != nil {
			return domain.RoutingInstruction{}, extractErr
		}
		resp, parseErr := unmarshalRoutingResponseLenient(extracted)
		if parseErr != nil {
			var ce *domain.ConsultationError
			if errors.As(parseErr, &ce) && ce.Failure == domain.ConsultFailMalformedJSON {
				if c.DispatchLogger != nil {
					c.DispatchLogger.LogError(consultationInstanceID, parseErr.Error())
				}
				lastErr = parseErr
				continue
			}
			return domain.RoutingInstruction{}, parseErr
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
	return domain.RoutingInstruction{}, lastErr
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

	seq := atomic.AddUint64(&c.callSeq, 1)
	consultationInstanceID := fmt.Sprintf("%s#%s#%d", c.Orchestrator.Identifier, string(domain.ConsultContextPreConsultation), seq)
	if c.DispatchLogger != nil {
		c.DispatchLogger.LogRequest(domain.ProtocolRequest{
			AgentInstanceID: consultationInstanceID,
			TaskDescription: string(payload),
		})
	}

	reply, err := c.Invoker.InvokeRaw(ctx, c.Orchestrator, payload)
	if err != nil {
		if c.DispatchLogger != nil {
			c.DispatchLogger.LogError(consultationInstanceID, err.Error())
		}
		return domain.PreConsultationAdvice{}, &domain.ConsultationError{
			Failure: domain.ConsultFailTransport,
			Detail:  fmt.Sprintf("InvokeRaw failed: %v", err),
			Err:     err,
		}
	}
	if c.DispatchLogger != nil {
		c.DispatchLogger.LogResponse(domain.ProtocolResponse{
			AgentInstanceID: consultationInstanceID,
			StatusMessage:   string(reply),
		})
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

