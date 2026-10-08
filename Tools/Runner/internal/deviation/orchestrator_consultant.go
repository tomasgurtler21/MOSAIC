package deviation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync/atomic"

	"mosaic-run/internal/domain"
)

// OrchestratorConsultant implements domain.RoutingConsultant and
// domain.PreConsultant by invoking the script-mode orchestrator agent over the
// raw transport and parsing the two-action contract.
type OrchestratorConsultant struct {
	// Invoker carries the raw JSON payload to the orchestrator.
	Invoker domain.RawInvoker
	// Orchestrator is the resolved orchestrator agent. Its InvocationKind must
	// be domain.InvocationOrchestrator.
	Orchestrator domain.AgentReference
	// Table validates a dispatch reply's agent, row and stage and supplies the
	// available-agent list for the unknown-agent error.
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
//
// A dispatch reply must name the row (1-based) and, for a staged row, the
// stage; it is validated against the table and req.Stages. An invalid reply is
// retried like a malformed one and, once the attempts are spent, fails with
// ConsultFailMalformedJSON.
func (c *OrchestratorConsultant) ConsultRouting(ctx context.Context, req domain.ConsultationRequest) (domain.RoutingInstruction, error) {
	wr := wireRequest{
		OrchestrationArtifact: req.OrchestrationArtifact,
		Context:               string(req.Context),
		LastStatusMessage:     req.LastStatusMessage,
		LastErrorReason:       req.LastErrorReason,
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
			dispatch, dispErr := c.dispatchFromReply(req, resp)
			if dispErr != nil {
				var ce *domain.ConsultationError
				if errors.As(dispErr, &ce) && ce.Failure == domain.ConsultFailMalformedJSON {
					if c.DispatchLogger != nil {
						c.DispatchLogger.LogError(consultationInstanceID, dispErr.Error())
					}
					lastErr = dispErr
					continue
				}
				return domain.RoutingInstruction{}, dispErr
			}
			return domain.RoutingInstruction{Dispatch: dispatch}, nil

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

// dispatchFromReply turns a parsed dispatch reply into a DispatchInstruction.
// A reply whose row or stage fails domain.ValidateDispatchTarget against the
// routing table and the request's current stage set is reported as
// ConsultFailMalformedJSON, so the caller retries it like any malformed reply.
// An unknown agent keeps its own failure class and is not retried.
func (c *OrchestratorConsultant) dispatchFromReply(req domain.ConsultationRequest, resp wireRoutingResponse) (*domain.DispatchInstruction, error) {
	if resp.Agent == "" {
		return nil, &domain.ConsultationError{
			Failure: domain.ConsultFailMissingField,
			Detail:  "missing required field: agent",
		}
	}
	if resp.TaskDescription == "" {
		return nil, &domain.ConsultationError{
			Failure: domain.ConsultFailMissingField,
			Detail:  "missing required field: task_description",
		}
	}
	agents := make([]string, 0, len(c.Table.Rows))
	known := false
	for _, row := range c.Table.Rows {
		agents = append(agents, row.Agent)
		if row.Agent == resp.Agent {
			known = true
		}
	}
	if !known {
		return nil, &domain.ConsultationError{
			Failure: domain.ConsultFailUnknownAgent,
			Detail:  fmt.Sprintf("agent %q not found in routing table; available: %v", resp.Agent, agents),
			Agents:  agents,
		}
	}
	resolved, err := domain.ValidateDispatchTarget(c.Table, req.Stages, domain.DispatchTarget{
		Agent:      resp.Agent,
		Row:        resp.Row,
		Stage:      resp.Stage,
		StageGroup: resp.StageGroup,
	})
	if err != nil {
		return nil, &domain.ConsultationError{
			Failure: domain.ConsultFailMalformedJSON,
			Detail:  describeTargetError(err),
			Err:     err,
		}
	}
	return &domain.DispatchInstruction{
		Agent:           resp.Agent,
		RowIndex:        resolved.Row.Index,
		Stage:           resolved.StageNumber,
		TaskDescription: resp.TaskDescription,
		Constraints:     resp.Constraints,
		InputArtifacts:  resp.InputArtifacts,
		OutputArtifacts: resp.OutputArtifacts,
		HITLOverride:    resp.HITLOverride,
	}, nil
}

// describeTargetError renders an invalid row/stage for the consultation error.
func describeTargetError(err error) string {
	var te *domain.DispatchTargetError
	if errors.As(err, &te) && te.Detail != "" {
		return fmt.Sprintf("invalid dispatch row/stage (%s): %s", te.Reason, te.Detail)
	}
	return fmt.Sprintf("invalid dispatch row/stage: %v", err)
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
		LastErrorReason:       nil, // always null for pre-consultation, per the contract
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
