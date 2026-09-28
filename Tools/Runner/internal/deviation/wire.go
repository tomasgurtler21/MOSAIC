package deviation

import (
	"encoding/json"
	"fmt"

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
	Action          string    `json:"action"`           // "dispatch" | "stop"
	Agent           string    `json:"agent"`            // dispatch only; required
	TaskDescription string    `json:"task_description"` // dispatch only; required
	Constraints     *string   `json:"constraints"`      // optional; nil = fall back to table row
	InputArtifacts  *[]string `json:"input_artifacts"`  // optional; nil = fall back; non-nil empty = "no inputs"
	OutputArtifacts *[]string `json:"output_artifacts"` // optional; same semantics
	HITLOverride    *bool     `json:"hitl_override"`    // optional; nil = fall back
	Reason          string    `json:"reason"`           // stop only
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
