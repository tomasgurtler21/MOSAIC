package deviation_test

// Tests for bounded consultation retry in ConsultRouting — up to 3 attempts on
// ConsultFailMalformedJSON, no retry for other failure classes.

import (
	"context"
	"errors"
	"testing"

	"mosaic-run/internal/deviation"
	"mosaic-run/internal/domain"
)

// ---- sequencedRawInvoker ----

// sequencedReply holds one (reply, error) pair for sequencedRawInvoker.
type sequencedReply struct {
	reply []byte
	err   error
}

// sequencedRawInvoker is a test double for domain.RawInvoker that returns a
// pre-configured sequence of (reply, error) pairs, one per InvokeRaw call.
// After the sequence is exhausted, subsequent calls return an error so tests
// can detect unexpected extra invocations.
type sequencedRawInvoker struct {
	replies []sequencedReply
	pos     int
	// sent records the payload from each InvokeRaw call, in call order.
	sent [][]byte
}

func (s *sequencedRawInvoker) InvokeRaw(_ context.Context, _ domain.AgentReference, payload []byte) ([]byte, error) {
	s.sent = append(s.sent, payload)
	if s.pos >= len(s.replies) {
		return nil, errors.New("sequencedRawInvoker: reply sequence exhausted")
	}
	r := s.replies[s.pos]
	s.pos++
	return r.reply, r.err
}

// malformedSchemaReply returns a sequencedReply whose body is a valid JSON
// object that ExtractJSONObject locates successfully but
// unmarshalRoutingResponseLenient rejects because the action field is a JSON
// array rather than a string. This produces ConsultFailMalformedJSON, which is
// the only failure class the retry loop retries.
func malformedSchemaReply() sequencedReply {
	return sequencedReply{reply: []byte(`{"action": ["not","a","string"]}`)}
}

// ===== Bounded Consultation Retry =====

// TestConsultRouting_RetrySucceedsOnSecondAttempt_ValidDispatch verifies that
// when the first attempt produces ConsultFailMalformedJSON, the retry loop makes
// a second attempt, and a valid dispatch response on the second attempt returns
// a RoutingInstruction without error.
func TestConsultRouting_RetrySucceedsOnSecondAttempt_ValidDispatch(t *testing.T) {
	table := mustParseTable(t)
	validDispatch := []byte(`{"action":"dispatch","agent":"agent-a","task_description":"do the thing"}`)
	invoker := &sequencedRawInvoker{replies: []sequencedReply{
		malformedSchemaReply(),
		{reply: validDispatch},
	}}
	c := newTestOrchestratorConsultant(invoker, table)

	instr, err := c.ConsultRouting(context.Background(), validRoutingRequest("Orchestration-abc/Orchestration.md"))

	if err != nil {
		t.Fatalf("want no error when retry succeeds on second attempt with dispatch, got %v", err)
	}
	if instr.Dispatch == nil {
		t.Fatal("want Dispatch instruction on successful retry, got nil Dispatch")
	}
	if instr.Dispatch.Agent != "agent-a" {
		t.Errorf("want Agent=%q after retry, got %q", "agent-a", instr.Dispatch.Agent)
	}
	// Both attempts must have been made: first (malformed) then second (success).
	if invoker.pos != 2 {
		t.Errorf("want 2 InvokeRaw calls (1 malformed + 1 success), got %d", invoker.pos)
	}
}

// TestConsultRouting_RetrySucceedsOnSecondAttempt_ValidStop verifies that when
// the first attempt produces ConsultFailMalformedJSON and the second attempt
// returns a valid stop response, ConsultRouting returns a RoutingInstruction
// with a non-nil Stop and no error.
func TestConsultRouting_RetrySucceedsOnSecondAttempt_ValidStop(t *testing.T) {
	table := mustParseTable(t)
	wantReason := "orchestrator decided to stop after retry"
	invoker := &sequencedRawInvoker{replies: []sequencedReply{
		malformedSchemaReply(),
		{reply: routingStopReply(wantReason)},
	}}
	c := newTestOrchestratorConsultant(invoker, table)

	instr, err := c.ConsultRouting(context.Background(), validRoutingRequest("Orchestration-abc/Orchestration.md"))

	if err != nil {
		t.Fatalf("want no error when retry succeeds with stop response, got %v", err)
	}
	if instr.Stop == nil {
		t.Fatal("want Stop instruction on successful retry, got nil Stop")
	}
	if instr.Dispatch != nil {
		t.Error("want Dispatch=nil for stop response, got non-nil")
	}
	if instr.Stop.Reason != wantReason {
		t.Errorf("want Stop.Reason=%q after retry, got %q", wantReason, instr.Stop.Reason)
	}
	// Both attempts must have been made: first (malformed) then second (stop).
	if invoker.pos != 2 {
		t.Errorf("want 2 InvokeRaw calls (1 malformed + 1 stop success), got %d", invoker.pos)
	}
}

// TestConsultRouting_AllThreeAttemptsMalformed_FailsWithMalformedJSON verifies
// that after 3 consecutive ConsultFailMalformedJSON results, ConsultRouting
// returns *ConsultationError with ConsultFailMalformedJSON without making a
// fourth attempt. The terminal path is identical to a single-attempt failure:
// ConsultFailMalformedJSON, just reached after exhausting all 3 attempts.
func TestConsultRouting_AllThreeAttemptsMalformed_FailsWithMalformedJSON(t *testing.T) {
	table := mustParseTable(t)
	invoker := &sequencedRawInvoker{replies: []sequencedReply{
		malformedSchemaReply(),
		malformedSchemaReply(),
		malformedSchemaReply(),
	}}
	c := newTestOrchestratorConsultant(invoker, table)

	_, err := c.ConsultRouting(context.Background(), validRoutingRequest("Orchestration-abc/Orchestration.md"))

	assertConsultationError(t, err, domain.ConsultFailMalformedJSON)
	// All 3 attempts must have been made before giving up.
	if invoker.pos != 3 {
		t.Errorf("want 3 InvokeRaw calls (all 3 malformed attempts exhausted), got %d", invoker.pos)
	}
}

// TestConsultRouting_NonMalformedFailureIsNotRetried verifies that failure
// classes other than ConsultFailMalformedJSON are never retried. For each case
// the invoker must be called exactly once: the retry loop must not make a
// second attempt when the failure class is not ConsultFailMalformedJSON.
func TestConsultRouting_NonMalformedFailureIsNotRetried(t *testing.T) {
	cases := []struct {
		name        string
		replies     []sequencedReply
		wantFailure domain.ConsultationFailure
	}{
		{
			name:        "transport error (ConsultFailTransport)",
			replies:     []sequencedReply{{err: errors.New("harness: timeout")}},
			wantFailure: domain.ConsultFailTransport,
		},
		{
			name:        "no JSON object in reply (ConsultFailNoInstruction)",
			replies:     []sequencedReply{{reply: []byte("no json object anywhere in this reply")}},
			wantFailure: domain.ConsultFailNoInstruction,
		},
		{
			name:        "missing required agent field (ConsultFailMissingField)",
			replies:     []sequencedReply{{reply: []byte(`{"action":"dispatch","task_description":"do the thing"}`)}},
			wantFailure: domain.ConsultFailMissingField,
		},
		{
			name:        "unknown action value (ConsultFailUnknownAction)",
			replies:     []sequencedReply{{reply: []byte(`{"action":"proceed"}`)}},
			wantFailure: domain.ConsultFailUnknownAction,
		},
		{
			name:        "agent not in routing table (ConsultFailUnknownAgent)",
			replies:     []sequencedReply{{reply: []byte(`{"action":"dispatch","agent":"no-such-agent","task_description":"do the thing"}`)}},
			wantFailure: domain.ConsultFailUnknownAgent,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			table := mustParseTable(t)
			// Give the invoker exactly one reply. If the implementation incorrectly
			// retries, the second call will return "sequence exhausted" — but we also
			// assert pos==1 to catch incorrect retry without relying on that error.
			invoker := &sequencedRawInvoker{replies: tc.replies}
			c := newTestOrchestratorConsultant(invoker, table)

			_, err := c.ConsultRouting(context.Background(), validRoutingRequest("Orchestration-abc/Orchestration.md"))

			assertConsultationError(t, err, tc.wantFailure)
			if invoker.pos != 1 {
				t.Errorf("want exactly 1 InvokeRaw call for failure class %q (no retry expected), got %d", tc.wantFailure, invoker.pos)
			}
		})
	}
}

// TestConsultRouting_RetryLogsDistinctConsultationIDs verifies that each attempt
// in a retry sequence produces its own dispatch-log entries with a unique
// consultationInstanceID. For 2 malformed attempts followed by a successful
// attempt, the DispatchLogger must record 3 LogRequest calls with 3 distinct
// AgentInstanceIDs and 2 LogError calls (one per malformed attempt).
func TestConsultRouting_RetryLogsDistinctConsultationIDs(t *testing.T) {
	table := mustParseTable(t)
	validDispatch := []byte(`{"action":"dispatch","agent":"agent-a","task_description":"do the thing"}`)
	invoker := &sequencedRawInvoker{replies: []sequencedReply{
		malformedSchemaReply(),
		malformedSchemaReply(),
		{reply: validDispatch},
	}}
	var events []string
	logger := &recordingDispatchLogger{events: &events}
	c := &deviation.OrchestratorConsultant{
		Invoker:        invoker,
		Orchestrator:   orchestratorRef(),
		Table:          table,
		DispatchLogger: logger,
	}

	_, err := c.ConsultRouting(context.Background(), validRoutingRequest("Orchestration-abc/Orchestration.md"))

	if err != nil {
		t.Fatalf("want no error when retry succeeds on third attempt, got %v", err)
	}

	// Each of the 3 attempts must produce its own LogRequest.
	if len(logger.requests) != 3 {
		t.Errorf("want 3 LogRequest calls (one per attempt), got %d", len(logger.requests))
	}

	// All 3 consultationInstanceIDs must be distinct.
	seen := make(map[string]int)
	for i, req := range logger.requests {
		seen[req.AgentInstanceID] = i
	}
	if len(seen) != len(logger.requests) {
		ids := make([]string, 0, len(logger.requests))
		for _, req := range logger.requests {
			ids = append(ids, req.AgentInstanceID)
		}
		t.Errorf("want distinct consultationInstanceID per attempt, got duplicate IDs: %v", ids)
	}

	// The 2 malformed attempts each produce a LogError; the successful attempt does not.
	if len(logger.errs) != 2 {
		t.Errorf("want 2 LogError calls (one per malformed attempt), got %d", len(logger.errs))
	}
}
