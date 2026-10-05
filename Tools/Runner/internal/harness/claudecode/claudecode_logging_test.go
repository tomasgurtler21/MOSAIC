package claudecode_test

// Tests for ClaudeCodeAdapter debug-log emission (recordingLogger-based).
// Uses the fake-CLI helper process defined in helperprocess_test.go.

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"mosaic-run/internal/domain"
	"mosaic-run/internal/harness/claudecode"
	"mosaic-run/internal/harness/cliexec"
)

// ---------------------------------------------------------------------------
// recordingLogger — fake domain.DebugLogger for logging tests (T5.1, T5.2, T5.6)
// ---------------------------------------------------------------------------

// harnessLogEntry is one captured call to recordingLogger.Log.
type harnessLogEntry struct {
	Event   string
	Message string
	Fields  []domain.DebugField
}

// recordingLogger is a thread-safe domain.DebugLogger that records every Log
// call. Tests assert on the recorded entries after invoking the adapter.
type recordingLogger struct {
	mu      sync.Mutex
	entries []harnessLogEntry
}

// Log implements domain.DebugLogger.
func (r *recordingLogger) Log(event string, message string, fields ...domain.DebugField) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.entries = append(r.entries, harnessLogEntry{
		Event:   event,
		Message: message,
		Fields:  append([]domain.DebugField{}, fields...),
	})
}

// eventLogged reports whether at least one entry with the given event name
// was recorded.
func (r *recordingLogger) eventLogged(event string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, e := range r.entries {
		if e.Event == event {
			return true
		}
	}
	return false
}

// fieldValue returns the value for the given field key in the first entry
// with the given event name. Returns ("", false) when not found.
func (r *recordingLogger) fieldValue(event, key string) (string, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, e := range r.entries {
		if e.Event != event {
			continue
		}
		for _, f := range e.Fields {
			if f.Key == key {
				return f.Value, true
			}
		}
	}
	return "", false
}

// allEvents returns the event names of all recorded entries in order.
func (r *recordingLogger) allEvents() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	names := make([]string, len(r.entries))
	for i, e := range r.entries {
		names[i] = e.Event
	}
	return names
}

// messageFor returns the message of the first recorded entry with the given
// event name. Returns ("", false) when no such entry was recorded.
func (r *recordingLogger) messageFor(event string) (string, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, e := range r.entries {
		if e.Event == event {
			return e.Message, true
		}
	}
	return "", false
}

// countEvent returns how many entries were recorded for the given event name.
func (r *recordingLogger) countEvent(event string) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	n := 0
	for _, e := range r.entries {
		if e.Event == event {
			n++
		}
	}
	return n
}

// ---------------------------------------------------------------------------
// T5.1 — adapter logs invocation start, raw stdout, raw stderr, and outcome
// ---------------------------------------------------------------------------

// TestClaudeCodeAdapterWithLogger_SuccessfulInvocation_LogsInvokeStart verifies
// that NewClaudeCodeAdapterWithLogger emits EventHarnessInvokeStart for each
// call to Invoke.
func TestClaudeCodeAdapterWithLogger_SuccessfulInvocation_LogsInvokeStart(t *testing.T) {
	setHelperEnv(t, "success")
	logger := &recordingLogger{}
	adapter := claudecode.NewClaudeCodeAdapterWithLogger(helperExe(t), 5*time.Second, logger)

	_, err := adapter.Invoke(context.Background(), ordinaryAgentRefCC(t), minimalClaudeRequest("test-agent#1"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !logger.eventLogged(domain.EventHarnessInvokeStart) {
		t.Errorf("want %s logged, got events: %v", domain.EventHarnessInvokeStart, logger.allEvents())
	}
}

// TestClaudeCodeAdapterWithLogger_SuccessfulInvocation_LogsRawStdout verifies
// that EventHarnessStdout is logged after the subprocess exits, capturing the
// raw stdout content.
func TestClaudeCodeAdapterWithLogger_SuccessfulInvocation_LogsRawStdout(t *testing.T) {
	setHelperEnv(t, "success")
	logger := &recordingLogger{}
	adapter := claudecode.NewClaudeCodeAdapterWithLogger(helperExe(t), 5*time.Second, logger)

	_, _ = adapter.Invoke(context.Background(), ordinaryAgentRefCC(t), minimalClaudeRequest("test-agent#1"))

	if !logger.eventLogged(domain.EventHarnessStdout) {
		t.Errorf("want %s logged after process exits, got events: %v", domain.EventHarnessStdout, logger.allEvents())
	}
}

// TestClaudeCodeAdapterWithLogger_SuccessfulInvocation_LogsRawStderr verifies
// that EventHarnessStderr is logged after the subprocess exits, even when
// stderr is empty (an empty log entry is still expected per invocation).
func TestClaudeCodeAdapterWithLogger_SuccessfulInvocation_LogsRawStderr(t *testing.T) {
	setHelperEnv(t, "success")
	logger := &recordingLogger{}
	adapter := claudecode.NewClaudeCodeAdapterWithLogger(helperExe(t), 5*time.Second, logger)

	_, _ = adapter.Invoke(context.Background(), ordinaryAgentRefCC(t), minimalClaudeRequest("test-agent#1"))

	if !logger.eventLogged(domain.EventHarnessStderr) {
		t.Errorf("want %s logged after process exits, got events: %v", domain.EventHarnessStderr, logger.allEvents())
	}
}

// TestClaudeCodeAdapterWithLogger_SuccessfulInvocation_LogsInvokeOK verifies
// that a successful invocation logs EventHarnessInvokeOK and does not log
// EventHarnessInvokeError.
func TestClaudeCodeAdapterWithLogger_SuccessfulInvocation_LogsInvokeOK(t *testing.T) {
	setHelperEnv(t, "success")
	logger := &recordingLogger{}
	adapter := claudecode.NewClaudeCodeAdapterWithLogger(helperExe(t), 5*time.Second, logger)

	_, err := adapter.Invoke(context.Background(), ordinaryAgentRefCC(t), minimalClaudeRequest("test-agent#1"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !logger.eventLogged(domain.EventHarnessInvokeOK) {
		t.Errorf("want %s logged on success, got events: %v", domain.EventHarnessInvokeOK, logger.allEvents())
	}
	if logger.eventLogged(domain.EventHarnessInvokeError) {
		t.Errorf("want no %s on success, but it was logged", domain.EventHarnessInvokeError)
	}
}

// TestClaudeCodeAdapterWithLogger_NonZeroExit_LogsInvokeError verifies that a
// non-zero subprocess exit logs EventHarnessInvokeError and does not log
// EventHarnessInvokeOK.
func TestClaudeCodeAdapterWithLogger_NonZeroExit_LogsInvokeError(t *testing.T) {
	setHelperEnv(t, "exit1")
	logger := &recordingLogger{}
	adapter := claudecode.NewClaudeCodeAdapterWithLogger(helperExe(t), 5*time.Second, logger)

	_, _ = adapter.Invoke(context.Background(), ordinaryAgentRefCC(t), minimalClaudeRequest("test-agent#1"))

	if !logger.eventLogged(domain.EventHarnessInvokeError) {
		t.Errorf("want %s logged on non-zero exit, got events: %v", domain.EventHarnessInvokeError, logger.allEvents())
	}
	if logger.eventLogged(domain.EventHarnessInvokeOK) {
		t.Errorf("want no %s on error, but it was logged", domain.EventHarnessInvokeOK)
	}
}

// TestClaudeCodeAdapterWithLogger_InvokeStart_CarriesAgentAndKindFields
// verifies that the EventHarnessInvokeStart entry carries the agent instance ID
// (from the request) and the invocation kind as structured fields.
func TestClaudeCodeAdapterWithLogger_InvokeStart_CarriesAgentAndKindFields(t *testing.T) {
	setHelperEnv(t, "success")
	logger := &recordingLogger{}
	adapter := claudecode.NewClaudeCodeAdapterWithLogger(helperExe(t), 5*time.Second, logger)

	_, _ = adapter.Invoke(context.Background(), ordinaryAgentRefCC(t), minimalClaudeRequest("test-agent#1"))

	agentVal, ok := logger.fieldValue(domain.EventHarnessInvokeStart, "agent")
	if !ok {
		t.Errorf("want 'agent' field on %s entry; events: %v",
			domain.EventHarnessInvokeStart, logger.allEvents())
	} else if agentVal != "test-agent#1" {
		t.Errorf("want agent=test-agent#1 on %s, got %q", domain.EventHarnessInvokeStart, agentVal)
	}

	kindVal, ok := logger.fieldValue(domain.EventHarnessInvokeStart, "kind")
	if !ok {
		t.Errorf("want 'kind' field on %s entry", domain.EventHarnessInvokeStart)
	} else if kindVal != "ordinary" {
		t.Errorf("want kind=ordinary on %s, got %q", domain.EventHarnessInvokeStart, kindVal)
	}
}

// ---------------------------------------------------------------------------
// T5.2 — adapter logs envelope parse failure and recovery
// ---------------------------------------------------------------------------

// TestClaudeCodeAdapterWithLogger_NonJSONOutput_LogsParseFailed verifies that
// when the CLI outputs non-JSON text and recovery also fails, the adapter logs
// EventHarnessParseFailed. The error return remains ErrMalformedJSON.
func TestClaudeCodeAdapterWithLogger_NonJSONOutput_LogsParseFailed(t *testing.T) {
	setHelperEnv(t, "bad-json")
	logger := &recordingLogger{}
	adapter := claudecode.NewClaudeCodeAdapterWithLogger(helperExe(t), 5*time.Second, logger)

	_, _ = adapter.Invoke(context.Background(), ordinaryAgentRefCC(t), minimalClaudeRequest("test-agent#1"))

	if !logger.eventLogged(domain.EventHarnessParseFailed) {
		t.Errorf("want %s logged on non-JSON output with failed recovery, got events: %v",
			domain.EventHarnessParseFailed, logger.allEvents())
	}
}

// TestClaudeCodeAdapterWithLogger_BareJSONRecovery_LogsParseRecovered verifies
// that when the CLI outputs a bare JSON object that is a valid protocol response
// and recovery succeeds, EventHarnessParseRecovered is logged.
func TestClaudeCodeAdapterWithLogger_BareJSONRecovery_LogsParseRecovered(t *testing.T) {
	setHelperEnv(t, "bare-json")
	logger := &recordingLogger{}
	adapter := claudecode.NewClaudeCodeAdapterWithLogger(helperExe(t), 5*time.Second, logger)

	_, err := adapter.Invoke(context.Background(), ordinaryAgentRefCC(t), minimalClaudeRequest("test-agent#1"))
	if err != nil {
		t.Fatalf("unexpected error (bare-JSON recovery should succeed): %v", err)
	}

	if !logger.eventLogged(domain.EventHarnessParseRecovered) {
		t.Errorf("want %s logged after successful bare-JSON recovery, got events: %v",
			domain.EventHarnessParseRecovered, logger.allEvents())
	}
}

// TestClaudeCodeAdapterWithLogger_EmbeddedJSONRecovery_LogsParseRecovered
// verifies that when a valid protocol response embedded in surrounding CLI text
// is recovered, EventHarnessParseRecovered is logged.
func TestClaudeCodeAdapterWithLogger_EmbeddedJSONRecovery_LogsParseRecovered(t *testing.T) {
	setHelperEnv(t, "embedded-json")
	logger := &recordingLogger{}
	adapter := claudecode.NewClaudeCodeAdapterWithLogger(helperExe(t), 5*time.Second, logger)

	_, err := adapter.Invoke(context.Background(), ordinaryAgentRefCC(t), minimalClaudeRequest("test-agent#1"))
	if err != nil {
		t.Fatalf("unexpected error (embedded-JSON recovery should succeed): %v", err)
	}

	if !logger.eventLogged(domain.EventHarnessParseRecovered) {
		t.Errorf("want %s logged after successful embedded-JSON recovery, got events: %v",
			domain.EventHarnessParseRecovered, logger.allEvents())
	}
}

// ---------------------------------------------------------------------------
// T5.6 — nil logger defaults to no-op; adapter behaviour unchanged
// ---------------------------------------------------------------------------

// TestClaudeCodeAdapterWithLogger_NilLogger_NormalisedToNop verifies that
// passing nil as the logger is safe: nil is normalised to domain.NopDebugLogger,
// Invoke does not panic, and the response is correct.
func TestClaudeCodeAdapterWithLogger_NilLogger_NormalisedToNop(t *testing.T) {
	setHelperEnv(t, "success")

	adapter := claudecode.NewClaudeCodeAdapterWithLogger(helperExe(t), 5*time.Second, nil)

	resp, err := adapter.Invoke(context.Background(), ordinaryAgentRefCC(t), minimalClaudeRequest("test-agent#1"))
	if err != nil {
		t.Fatalf("want no error with nil logger, got %v", err)
	}
	if resp.StatusCode != domain.StatusSUCCESS {
		t.Errorf("want SUCCESS with nil logger, got %q", resp.StatusCode)
	}
}

// TestClaudeCodeAdapter_BasicConstructor_UnchangedBehaviourAfterLoggerAdded
// verifies that the original two-argument NewClaudeCodeAdapter constructor
// continues to produce correct results after the logger field was introduced.
// This regression guard ensures all existing call sites keep working unchanged.
func TestClaudeCodeAdapter_BasicConstructor_UnchangedBehaviourAfterLoggerAdded(t *testing.T) {
	setHelperEnv(t, "success")
	adapter := claudecode.NewClaudeCodeAdapter(helperExe(t), 5*time.Second)

	resp, err := adapter.Invoke(context.Background(), ordinaryAgentRefCC(t), minimalClaudeRequest("test-agent#1"))
	if err != nil {
		t.Fatalf("want no error with basic constructor, got %v", err)
	}
	if resp.StatusCode != domain.StatusSUCCESS {
		t.Errorf("want SUCCESS with basic constructor, got %q", resp.StatusCode)
	}
}

// ---------------------------------------------------------------------------
// Debug log raw-content gap-fill
// ---------------------------------------------------------------------------

// TestClaudeCodeAdapterWithLogger_ProtocolDecodeFailure_LogsRawContent
// verifies that when the CLI output IS extractable as a Communication
// Protocol candidate but fails to decode into domain.ProtocolResponse, the
// adapter logs the raw content to the debug log rather than leaving only the
// short wrapped error message.
func TestClaudeCodeAdapterWithLogger_ProtocolDecodeFailure_LogsRawContent(t *testing.T) {
	setHelperEnv(t, "protocol-decode-fail")
	logger := &recordingLogger{}
	adapter := claudecode.NewClaudeCodeAdapterWithLogger(helperExe(t), 5*time.Second, logger)

	_, err := adapter.Invoke(context.Background(), ordinaryAgentRefCC(t), minimalClaudeRequest("test-agent#1"))

	if !errors.Is(err, cliexec.ErrMalformedOutput) {
		t.Fatalf("want ErrMalformedOutput, got %v", err)
	}

	msg, ok := logger.messageFor(domain.EventHarnessParseFailed)
	if !ok {
		t.Fatalf("want %s logged when the extracted protocol response fails to decode, got events: %v",
			domain.EventHarnessParseFailed, logger.allEvents())
	}
	if !strings.Contains(msg, `"error_code":12345`) {
		t.Errorf("want logged message to contain the raw undecodable content, got %q", msg)
	}
}

// TestClaudeCodeAdapterWithLogger_ProtocolNotExtractable_LogsRawContent
// verifies that when the CLI produces a valid envelope with no extractable
// Communication Protocol response, the adapter logs the raw content to the
// debug log. Before this behaviour exists, only a short wrapped error is
// logged under EventHarnessInvokeError and no raw content reaches the log.
func TestClaudeCodeAdapterWithLogger_ProtocolNotExtractable_LogsRawContent(t *testing.T) {
	setHelperEnv(t, "bad-envelope")
	logger := &recordingLogger{}
	adapter := claudecode.NewClaudeCodeAdapterWithLogger(helperExe(t), 5*time.Second, logger)

	_, err := adapter.Invoke(context.Background(), ordinaryAgentRefCC(t), minimalClaudeRequest("test-agent#1"))

	if !errors.Is(err, cliexec.ErrMalformedOutput) {
		t.Fatalf("want ErrMalformedOutput, got %v", err)
	}

	msg, ok := logger.messageFor(domain.EventHarnessParseFailed)
	if !ok {
		t.Fatalf("want %s logged when no protocol response is extractable, got events: %v",
			domain.EventHarnessParseFailed, logger.allEvents())
	}
	if !strings.Contains(msg, `"type":"assistant","message":"hello there"`) {
		t.Errorf("want logged message to contain the raw CLI output, got %q", msg)
	}
}

// TestClaudeCodeAdapterWithLogger_ProtocolNotExtractable_LargePayloadUntruncated
// verifies that raw content reaching the debug log for the
// ErrProtocolNotExtractable path is not bounded by the shared package's
// 2000-byte error-string cap: a payload well beyond that cap must reach the
// log in full.
func TestClaudeCodeAdapterWithLogger_ProtocolNotExtractable_LargePayloadUntruncated(t *testing.T) {
	setHelperEnv(t, "bad-envelope-large")
	logger := &recordingLogger{}
	adapter := claudecode.NewClaudeCodeAdapterWithLogger(helperExe(t), 5*time.Second, logger)

	_, err := adapter.Invoke(context.Background(), ordinaryAgentRefCC(t), minimalClaudeRequest("test-agent#1"))

	if !errors.Is(err, cliexec.ErrMalformedOutput) {
		t.Fatalf("want ErrMalformedOutput, got %v", err)
	}

	msg, ok := logger.messageFor(domain.EventHarnessParseFailed)
	if !ok {
		t.Fatalf("want %s logged for the large unrecoverable payload, got events: %v",
			domain.EventHarnessParseFailed, logger.allEvents())
	}
	if strings.Contains(msg, "[truncated") {
		t.Errorf("want no truncation indicator in the debug-log message, got %q", msg)
	}
	if !strings.Contains(msg, strings.Repeat("x", 3000)) {
		t.Errorf("want the full 3000-byte payload present in the debug-log message untruncated")
	}
}

// TestClaudeCodeAdapterWithLogger_MalformedJSON_NoDuplicateRawContentLog is a
// regression guard: the already-covered ErrMalformedJSON path must keep
// logging EventHarnessParseFailed exactly once per invocation. Gap-filling
// the two other unrecoverable paths must not introduce a second, duplicated
// raw-content log entry on this pre-existing path.
func TestClaudeCodeAdapterWithLogger_MalformedJSON_NoDuplicateRawContentLog(t *testing.T) {
	setHelperEnv(t, "bad-json")
	logger := &recordingLogger{}
	adapter := claudecode.NewClaudeCodeAdapterWithLogger(helperExe(t), 5*time.Second, logger)

	_, err := adapter.Invoke(context.Background(), ordinaryAgentRefCC(t), minimalClaudeRequest("test-agent#1"))

	if !errors.Is(err, cliexec.ErrMalformedJSON) {
		t.Fatalf("want ErrMalformedJSON, got %v", err)
	}

	if got := logger.countEvent(domain.EventHarnessParseFailed); got != 1 {
		t.Errorf("want exactly one %s entry, got %d: %v",
			domain.EventHarnessParseFailed, got, logger.allEvents())
	}
}

// TestClaudeCodeAdapterWithLogger_ObjectShapedEnvelope_NoParseRecoveredLogged
// verifies that a successful invocation returning a realistic object-shaped
// CLI envelope (the shape Claude Code actually emits: a single "type":"result"
// object, not a JSON array) is classified as a normal envelope, not a
// recovery: no EventHarnessParseRecovered entry is logged, and the response
// is still parsed correctly.
func TestClaudeCodeAdapterWithLogger_ObjectShapedEnvelope_NoParseRecoveredLogged(t *testing.T) {
	setHelperEnv(t, "object-envelope")
	logger := &recordingLogger{}
	adapter := claudecode.NewClaudeCodeAdapterWithLogger(helperExe(t), 5*time.Second, logger)

	resp, err := adapter.Invoke(context.Background(), ordinaryAgentRefCC(t), minimalClaudeRequest("test-agent#1"))

	if err != nil {
		t.Fatalf("want no error for a successful object-shaped envelope, got %v", err)
	}
	if resp.StatusCode != domain.StatusSUCCESS {
		t.Errorf("want StatusCode=SUCCESS, got %q", resp.StatusCode)
	}
	if resp.AgentInstanceID != "test-agent#1" {
		t.Errorf("want AgentInstanceID=test-agent#1, got %q", resp.AgentInstanceID)
	}
	if logger.eventLogged(domain.EventHarnessParseRecovered) {
		t.Errorf("want no %s for a genuine object-shaped envelope, got events: %v",
			domain.EventHarnessParseRecovered, logger.allEvents())
	}
}
