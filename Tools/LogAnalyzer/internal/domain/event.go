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
	EventUsageRecord     EventType = "usage_record"
	EventOther           EventType = "" // any type this tool does not consume
)

// SchemaVersionCurrent is the version this tool is written against.
const SchemaVersionCurrent = "1.1.0"

// supportedSchemaVersions is the backing slice for SupportedSchemaVersions,
// newest first. Prior versions remain supported so older logs keep working.
var supportedSchemaVersions = []string{"1.1.0", "1.0.0"}

// SupportedSchemaVersions returns every schema version this tool reads without
// a finding, newest first. Prior versions remain supported so older logs keep
// working; a version outside this set still yields exactly one finding per file.
func SupportedSchemaVersions() []string {
	out := make([]string, len(supportedSchemaVersions))
	copy(out, supportedSchemaVersions)
	return out
}

// IsSupportedSchemaVersion reports whether v is in the supported set.
func IsSupportedSchemaVersion(v string) bool {
	for _, s := range supportedSchemaVersions {
		if s == v {
			return true
		}
	}
	return false
}

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
	UsageRecord     *UsageRecordFields
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

// UsageRecordFields holds the fields from a usage_record event: the usage of
// exactly one assistant record (one API call) observed in a transcript.
//
// The adapter deliberately does not deduplicate. The same record is emitted
// once per hook firing that observed it, so RecordID is the key by which the
// aggregator counts it exactly once.
type UsageRecordFields struct {
	AgentInstanceID AgentInstanceID // "" on the orchestrator stream
	RecordID        string          // "" only when the adapter could not derive one
	RecordIndex     int64
	HasRecordIndex  bool
	Source          string // "orchestrator_transcript" | "agent_transcript"; forensic only
	Model           ModelID
	ServiceTier     string // "" when absent; descriptive only, never priced
	Usage           TokenUsage
	HasUsage        bool // false when the token_usage key was absent entirely
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
