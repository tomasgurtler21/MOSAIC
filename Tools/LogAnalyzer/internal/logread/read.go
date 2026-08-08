package logread

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"strings"
	"time"

	"mosaic-log-analyzer/internal/domain"
)

// MaxLineBytes is the per-line limit for the streaming scanner. It is sized well
// above realistic turn-content lengths because a turn event carries full message
// text; the default bufio scanner limit is far too small and would misreport long
// valid lines as malformed.
const MaxLineBytes = 64 << 20 // 64 MiB

// Reader is a tolerant, streaming JSONL decoder.
//
// Binding tolerance rules:
//   - Per-line containment: an unparseable, truncated or otherwise unusable line
//     is skipped, recorded as a Finding naming the file and 1-based line number,
//     and reading continues with the next line.
//   - Never silent: every skip produces exactly one Finding.
//   - Unknown is not invalid: unknown event types, unknown fields and absent
//     optional fields decode normally and produce NO finding.
//   - Blank lines are skipped and produce no finding.
//   - Streaming only: the file is never read whole into memory.
//   - The envelope (harness, schema_version) is extracted from every event; an
//     unrecognised schema_version yields one finding per file, not per line, and
//     the events are still returned.
type Reader struct{}

// NewReader returns a new Reader.
func NewReader() *Reader {
	return &Reader{}
}

// wireEvent is the JSON wire shape for any JSONL event line. All fields are
// optional at the wire level; absent fields decode to their zero values.
// TokenUsage uses a pointer so that "key present" vs "key absent" is
// distinguishable (nil pointer = key absent; non-nil = key present).
type wireEvent struct {
	SchemaVersion   string           `json:"schema_version"`
	Event           string           `json:"event"`
	Timestamp       string           `json:"timestamp"`
	Harness         string           `json:"harness"`
	SessionID       string           `json:"session_id"`
	RunID           string           `json:"run_id"`
	AgentInstanceID string           `json:"agent_instance_id"`
	AgentType       string           `json:"agent_type"`
	StatusCode      string           `json:"status_code"`
	Model           string           `json:"model"`
	Role            string           `json:"role"`
	TokenUsage      *json.RawMessage `json:"token_usage"`
	RecordID        string           `json:"record_id"`
	RecordIndex     *int64           `json:"record_index"`
	Source          string           `json:"source"`
	ServiceTier     string           `json:"service_tier"`
}

// wireUsage is the JSON wire shape for the token_usage sub-object.
// Pointer fields distinguish "token category present" from "token category absent".
type wireUsage struct {
	Input         *int64 `json:"input_tokens"`
	Output        *int64 `json:"output_tokens"`
	CacheRead     *int64 `json:"cache_read_tokens"`
	CacheCreation *int64 `json:"cache_creation_tokens"`
}

// Read decodes path line by line, calling visit for each decoded event in
// file order. The whole file is never buffered in memory.
//
// Returns an error ONLY when visit returns one (returned unchanged) or ctx is
// cancelled. An unopenable file, a malformed line, a truncated tail and an
// unrecognised schema version all yield Findings and a nil error.
func (r *Reader) Read(ctx context.Context, path string, visit func(domain.Event) error) ([]domain.Finding, error) {
	f, err := os.Open(path)
	if err != nil {
		return []domain.Finding{{
			Kind:     domain.FindingUnreadableFile,
			Severity: domain.SeverityWarning,
			Path:     path,
			Detail:   err.Error(),
		}}, nil
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	buf := make([]byte, MaxLineBytes)
	scanner.Buffer(buf, MaxLineBytes)

	var findings []domain.Finding
	schemaFindingEmitted := false
	lineNum := 0

	for scanner.Scan() {
		// Check for context cancellation on every iteration.
		if ctxErr := ctx.Err(); ctxErr != nil {
			return findings, ctxErr
		}

		lineNum++
		raw := scanner.Text()
		trimmed := strings.TrimSpace(raw)
		if trimmed == "" {
			// Blank lines are silently skipped.
			continue
		}

		var wire wireEvent
		if jsonErr := json.Unmarshal([]byte(trimmed), &wire); jsonErr != nil {
			// Distinguish a truncated JSON object from clearly non-JSON garbage.
			kind := domain.FindingMalformedLine
			if strings.HasPrefix(trimmed, "{") {
				kind = domain.FindingTruncatedLine
			}
			findings = append(findings, domain.Finding{
				Kind:     kind,
				Severity: domain.SeverityWarning,
				Path:     path,
				Line:     lineNum,
				Detail:   jsonErr.Error(),
			})
			continue
		}

		// Emit a schema-version finding at most once per file.
		if !domain.IsSupportedSchemaVersion(wire.SchemaVersion) && !schemaFindingEmitted {
			schemaFindingEmitted = true
			findings = append(findings, domain.Finding{
				Kind:     domain.FindingUnknownSchema,
				Severity: domain.SeverityWarning,
				Path:     path,
				Detail:   "schema_version " + wire.SchemaVersion + " is not supported; supported versions: " + strings.Join(domain.SupportedSchemaVersions(), ", "),
			})
		}

		ev := buildEvent(wire)
		if visitErr := visit(ev); visitErr != nil {
			return findings, visitErr
		}
	}

	if scanErr := scanner.Err(); scanErr != nil {
		// Scanner-level errors (e.g. token too long if somehow MaxLineBytes is
		// exceeded) — record as a malformed line.
		lineNum++
		findings = append(findings, domain.Finding{
			Kind:     domain.FindingMalformedLine,
			Severity: domain.SeverityWarning,
			Path:     path,
			Line:     lineNum,
			Detail:   scanErr.Error(),
		})
	}

	return findings, nil
}

// ReadAll is a convenience wrapper collecting events into a slice, for callers
// (and tests) that know the file is small. Production flows use Read.
func (r *Reader) ReadAll(ctx context.Context, path string) ([]domain.Event, []domain.Finding, error) {
	var events []domain.Event
	findings, err := r.Read(ctx, path, func(ev domain.Event) error {
		events = append(events, ev)
		return nil
	})
	return events, findings, err
}

// buildEvent converts a decoded wire event into the domain.Event representation.
func buildEvent(wire wireEvent) domain.Event {
	ev := domain.Event{
		SchemaVersion: wire.SchemaVersion,
		RawType:       wire.Event,
		Timestamp:     parseTimestamp(wire.Timestamp),
		Harness:       domain.Harness(wire.Harness),
		SessionID:     wire.SessionID,
		RunID:         wire.RunID,
	}

	switch domain.EventType(wire.Event) {
	case domain.EventRunStart:
		ev.Type = domain.EventRunStart
	case domain.EventRunEnd:
		ev.Type = domain.EventRunEnd
	case domain.EventSessionStart:
		ev.Type = domain.EventSessionStart
	case domain.EventSessionEnd:
		ev.Type = domain.EventSessionEnd
	case domain.EventInvocationStart:
		ev.Type = domain.EventInvocationStart
		ev.InvocationStart = &domain.InvocationStartFields{
			AgentInstanceID: domain.AgentInstanceID(wire.AgentInstanceID),
			AgentType:       wire.AgentType,
		}
	case domain.EventInvocationEnd:
		ev.Type = domain.EventInvocationEnd
		fields := &domain.InvocationEndFields{
			AgentInstanceID: domain.AgentInstanceID(wire.AgentInstanceID),
			StatusCode:      wire.StatusCode,
			Model:           domain.ModelID(wire.Model),
		}
		if wire.TokenUsage != nil {
			fields.HasUsage = true
			var wu wireUsage
			if json.Unmarshal(*wire.TokenUsage, &wu) == nil {
				fields.Usage = decodeUsage(wu)
			}
		}
		ev.InvocationEnd = fields
	case domain.EventTurn:
		ev.Type = domain.EventTurn
		fields := &domain.TurnFields{
			Role:  domain.TurnRole(wire.Role),
			Model: domain.ModelID(wire.Model),
		}
		if wire.TokenUsage != nil {
			fields.HasUsage = true
			var wu wireUsage
			if json.Unmarshal(*wire.TokenUsage, &wu) == nil {
				fields.Usage = decodeUsage(wu)
			}
		}
		ev.Turn = fields
	case domain.EventUsageRecord:
		ev.Type = domain.EventUsageRecord
		fields := &domain.UsageRecordFields{
			AgentInstanceID: domain.AgentInstanceID(wire.AgentInstanceID),
			RecordID:        wire.RecordID,
			Source:          wire.Source,
			Model:           domain.ModelID(wire.Model),
			ServiceTier:     wire.ServiceTier,
		}
		if wire.RecordIndex != nil {
			fields.RecordIndex, fields.HasRecordIndex = *wire.RecordIndex, true
		}
		if wire.TokenUsage != nil {
			fields.HasUsage = true
			var wu wireUsage
			if json.Unmarshal(*wire.TokenUsage, &wu) == nil {
				fields.Usage = decodeUsage(wu)
			}
		}
		ev.UsageRecord = fields
	default:
		// Unknown event type — decoded as EventOther with RawType preserved.
		ev.Type = domain.EventOther
	}

	return ev
}

// decodeUsage converts the wire usage shape into the domain TokenUsage type.
// Absent categories (nil pointer) become AbsentTokens; present categories
// (non-nil pointer) become Tokens with the given value.
func decodeUsage(wu wireUsage) domain.TokenUsage {
	var u domain.TokenUsage
	if wu.Input != nil {
		u.Input = domain.Tokens(*wu.Input)
	}
	if wu.Output != nil {
		u.Output = domain.Tokens(*wu.Output)
	}
	if wu.CacheRead != nil {
		u.CacheRead = domain.Tokens(*wu.CacheRead)
	}
	if wu.CacheCreation != nil {
		u.CacheCreation = domain.Tokens(*wu.CacheCreation)
	}
	return u
}

// parseTimestamp parses a timestamp string in RFC3339 or RFC3339Nano format.
// Returns the zero time if parsing fails.
func parseTimestamp(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		t, _ = time.Parse(time.RFC3339, s)
	}
	return t
}
