package domain

import "time"

// EventType is the wire discriminator. Unrecognised values decode as EventOther
// with Raw preserved, never as an error.
type EventType string

const (
	EventRunStart        EventType = "run_start"
	EventRunEnd          EventType = "run_end"
	EventSessionStart    EventType = "session_start"
	EventSessionEnd      EventType = "session_end"
	EventInvocationStart EventType = "invocation_start"
	EventInvocationEnd   EventType = "invocation_end"
	EventTurn            EventType = "turn"
	EventOther           EventType = "" // any type this tool does not consume
)

// SchemaVersionSupported is the schema version this tool was written against.
const SchemaVersionSupported = "1.0.0"

// Event is one decoded log line. Only the envelope and the field blocks this tool
// consumes are modelled. Large payload bodies (prompt, response, content,
// tool_input, tool_output) are deliberately NOT decoded.
type Event struct {
	// Envelope
	SchemaVersion string
	Type          EventType
	RawType       string  // the wire "event" value, retained for EventOther
	Timestamp     time.Time
	Harness       Harness
	SessionID     string // "" when absent
	RunID         string // "" when absent

	// Field blocks; exactly one is non-nil for a recognised type, all nil for EventOther.
	InvocationStart *InvocationStartFields
	InvocationEnd   *InvocationEndFields
	Turn            *TurnFields
}

// InvocationStartFields holds the fields from an invocation_start event.
type InvocationStartFields struct {
	AgentInstanceID AgentInstanceID
	AgentType       string // "" when absent
}

// InvocationEndFields holds the fields from an invocation_end event.
type InvocationEndFields struct {
	AgentInstanceID AgentInstanceID
	StatusCode      string // "" when absent
	Model           ModelID
	Usage           TokenUsage // all-absent when token_usage was absent
	HasUsage        bool       // false when the token_usage key was absent entirely
}

// TurnRole is the role (user or assistant) of a turn event.
type TurnRole string

const (
	TurnUser      TurnRole = "user"
	TurnAssistant TurnRole = "assistant"
)

// TurnFields holds the fields from a turn event.
type TurnFields struct {
	Role     TurnRole
	Model    ModelID
	Usage    TokenUsage
	HasUsage bool
}
