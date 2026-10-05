package deviation_test

// Tests for OrchestratorConsultant dispatch-logger coverage — ConsultRouting and
// PreConsult logging of request, response, and error events.

import (
	"context"
	"errors"
	"testing"

	"mosaic-run/internal/deviation"
	"mosaic-run/internal/domain"
)

// ---- recordingDispatchLogger ----

// recordingDispatchLogger is a test double for domain.DispatchLogger that
// records every LogRequest, LogResponse, and LogError call. It also appends a
// named event string to a shared events slice so that ordering can be verified
// against a companion orderingRawInvoker.
type recordingDispatchLogger struct {
	events    *[]string // shared with orderingRawInvoker to verify call order
	requests  []domain.ProtocolRequest
	responses []domain.ProtocolResponse
	errs      []struct{ id, text string }
}

func (l *recordingDispatchLogger) LogRequest(req domain.ProtocolRequest) {
	*l.events = append(*l.events, "log-request")
	l.requests = append(l.requests, req)
}

func (l *recordingDispatchLogger) LogResponse(resp domain.ProtocolResponse) {
	*l.events = append(*l.events, "log-response")
	l.responses = append(l.responses, resp)
}

func (l *recordingDispatchLogger) LogError(id, text string) {
	*l.events = append(*l.events, "log-error")
	l.errs = append(l.errs, struct{ id, text string }{id, text})
}

func (l *recordingDispatchLogger) SetRunID(_ string) {}
func (l *recordingDispatchLogger) Close()             {}
func (l *recordingDispatchLogger) Path() string       { return "" }

// ---- orderingRawInvoker ----

// orderingRawInvoker wraps a fakeRawInvoker and appends "invoke-raw" to the
// shared events slice at the moment InvokeRaw is called. This lets tests verify
// that LogRequest fired before the invocation and LogResponse/LogError fired
// after, by inspecting the order of entries in events.
type orderingRawInvoker struct {
	inner  *fakeRawInvoker
	events *[]string
}

func (o *orderingRawInvoker) InvokeRaw(ctx context.Context, ref domain.AgentReference, payload []byte) ([]byte, error) {
	*o.events = append(*o.events, "invoke-raw")
	return o.inner.InvokeRaw(ctx, ref, payload)
}

// newTestConsultantWithLogger builds an OrchestratorConsultant with a
// recordingDispatchLogger injected, returning both so tests can inspect calls.
func newTestConsultantWithLogger(invoker *orderingRawInvoker, table domain.RoutingTable, logger *recordingDispatchLogger) *deviation.OrchestratorConsultant {
	return &deviation.OrchestratorConsultant{
		Invoker:        invoker,
		Orchestrator:   orchestratorRef(),
		Table:          table,
		DispatchLogger: logger,
	}
}

// newOrderingPair creates a matched fakeRawInvoker/orderingRawInvoker/
// recordingDispatchLogger triple sharing a single events slice. reply is
// returned by InvokeRaw on every call; err overrides reply when non-nil.
func newOrderingPair(reply []byte, err error) (invoker *orderingRawInvoker, logger *recordingDispatchLogger) {
	var events []string
	inner := &fakeRawInvoker{reply: reply, err: err}
	invoker = &orderingRawInvoker{inner: inner, events: &events}
	logger = &recordingDispatchLogger{events: &events}
	return invoker, logger
}

// ===== ConsultRouting logging =====

// TestOrchestratorConsultant_ConsultRouting_LogsRequestBeforeInvokeAndResponseAfter
// verifies that ConsultRouting calls DispatchLogger.LogRequest before the
// InvokeRaw call and DispatchLogger.LogResponse after a successful reply. The
// call order is observed via the shared events slice.
func TestOrchestratorConsultant_ConsultRouting_LogsRequestBeforeInvokeAndResponseAfter(t *testing.T) {
	table := mustParseTable(t)
	invoker, logger := newOrderingPair(routingStopReply("done"), nil)
	c := newTestConsultantWithLogger(invoker, table, logger)

	req := validRoutingRequest("Orchestration-abc/Orchestration.md")
	_, err := c.ConsultRouting(context.Background(), req)
	if err != nil {
		t.Fatalf("want no error for valid routing response, got %v", err)
	}

	// Arrange: verify the three events were recorded.
	events := *invoker.events
	if len(events) < 3 {
		t.Fatalf("want at least 3 events (log-request, invoke-raw, log-response), got %d: %v", len(events), events)
	}
	// Assert ordering: request logged before invocation, response logged after.
	if events[0] != "log-request" {
		t.Errorf("events[0] = %q, want %q (request must be logged before InvokeRaw)", events[0], "log-request")
	}
	if events[1] != "invoke-raw" {
		t.Errorf("events[1] = %q, want %q", events[1], "invoke-raw")
	}
	if events[2] != "log-response" {
		t.Errorf("events[2] = %q, want %q (response must be logged after InvokeRaw succeeds)", events[2], "log-response")
	}
	// Assert counts.
	if len(logger.requests) != 1 {
		t.Errorf("want 1 LogRequest call, got %d", len(logger.requests))
	}
	if len(logger.responses) != 1 {
		t.Errorf("want 1 LogResponse call for successful reply, got %d", len(logger.responses))
	}
	if len(logger.errs) != 0 {
		t.Errorf("want 0 LogError calls for successful reply, got %d", len(logger.errs))
	}
}

// TestOrchestratorConsultant_ConsultRouting_TransportError_LogsRequestAndError
// verifies that when InvokeRaw returns an error, ConsultRouting calls
// DispatchLogger.LogRequest before the invocation and DispatchLogger.LogError
// after — and does not call LogResponse.
func TestOrchestratorConsultant_ConsultRouting_TransportError_LogsRequestAndError(t *testing.T) {
	table := mustParseTable(t)
	invokerErr := errors.New("harness: transport failure")
	invoker, logger := newOrderingPair(nil, invokerErr)
	c := newTestConsultantWithLogger(invoker, table, logger)

	req := validRoutingRequest("Orchestration-abc/Orchestration.md")
	_, err := c.ConsultRouting(context.Background(), req)
	if err == nil {
		t.Fatal("want error from ConsultRouting when InvokeRaw fails, got nil")
	}

	events := *invoker.events
	if len(events) < 3 {
		t.Fatalf("want at least 3 events (log-request, invoke-raw, log-error), got %d: %v", len(events), events)
	}
	if events[0] != "log-request" {
		t.Errorf("events[0] = %q, want %q (request must be logged before InvokeRaw)", events[0], "log-request")
	}
	if events[1] != "invoke-raw" {
		t.Errorf("events[1] = %q, want %q", events[1], "invoke-raw")
	}
	if events[2] != "log-error" {
		t.Errorf("events[2] = %q, want %q (error must be logged when InvokeRaw fails, not response)", events[2], "log-error")
	}
	// Assert counts.
	if len(logger.requests) != 1 {
		t.Errorf("want 1 LogRequest call, got %d", len(logger.requests))
	}
	if len(logger.errs) != 1 {
		t.Errorf("want 1 LogError call when InvokeRaw fails, got %d", len(logger.errs))
	}
	if len(logger.responses) != 0 {
		t.Errorf("want 0 LogResponse calls when InvokeRaw fails, got %d", len(logger.responses))
	}
}

// ===== PreConsult logging =====

// TestOrchestratorConsultant_PreConsult_LogsRequestAndResponse verifies that
// PreConsult calls DispatchLogger.LogRequest before InvokeRaw and
// DispatchLogger.LogResponse after a successful reply, mirroring the
// ConsultRouting logging contract.
func TestOrchestratorConsultant_PreConsult_LogsRequestAndResponse(t *testing.T) {
	table := mustParseTable(t)
	invoker, logger := newOrderingPair([]byte(`{"task_description":"","constraints":""}`), nil)
	c := newTestConsultantWithLogger(invoker, table, logger)

	req := domain.ConsultationRequest{
		OrchestrationArtifact: "Orchestration-abc/Orchestration.md",
		Context:               domain.ConsultContextPreConsultation,
		LastStatusMessage:     nil,
	}
	_, err := c.PreConsult(context.Background(), req)
	if err != nil {
		t.Fatalf("want no error for valid pre-consultation response, got %v", err)
	}

	events := *invoker.events
	if len(events) < 3 {
		t.Fatalf("want at least 3 events (log-request, invoke-raw, log-response), got %d: %v", len(events), events)
	}
	if events[0] != "log-request" {
		t.Errorf("events[0] = %q, want %q", events[0], "log-request")
	}
	if events[1] != "invoke-raw" {
		t.Errorf("events[1] = %q, want %q", events[1], "invoke-raw")
	}
	if events[2] != "log-response" {
		t.Errorf("events[2] = %q, want %q", events[2], "log-response")
	}
	if len(logger.requests) != 1 {
		t.Errorf("want 1 LogRequest call, got %d", len(logger.requests))
	}
	if len(logger.responses) != 1 {
		t.Errorf("want 1 LogResponse call for successful pre-consultation, got %d", len(logger.responses))
	}
	if len(logger.errs) != 0 {
		t.Errorf("want 0 LogError calls for successful pre-consultation, got %d", len(logger.errs))
	}
}

// TestOrchestratorConsultant_PreConsult_TransportError_LogsRequestAndError
// verifies that when InvokeRaw returns an error during a pre-consultation call,
// PreConsult calls LogRequest before the invocation and LogError after — not
// LogResponse. Mirrors the ConsultRouting transport error logging test.
func TestOrchestratorConsultant_PreConsult_TransportError_LogsRequestAndError(t *testing.T) {
	table := mustParseTable(t)
	invokerErr := errors.New("harness: timeout")
	invoker, logger := newOrderingPair(nil, invokerErr)
	c := newTestConsultantWithLogger(invoker, table, logger)

	req := domain.ConsultationRequest{
		OrchestrationArtifact: "Orchestration-abc/Orchestration.md",
		Context:               domain.ConsultContextPreConsultation,
		LastStatusMessage:     nil,
	}
	_, err := c.PreConsult(context.Background(), req)
	if err == nil {
		t.Fatal("want error from PreConsult when InvokeRaw fails, got nil")
	}

	events := *invoker.events
	if len(events) < 3 {
		t.Fatalf("want at least 3 events (log-request, invoke-raw, log-error), got %d: %v", len(events), events)
	}
	if events[0] != "log-request" {
		t.Errorf("events[0] = %q, want %q", events[0], "log-request")
	}
	if events[1] != "invoke-raw" {
		t.Errorf("events[1] = %q, want %q", events[1], "invoke-raw")
	}
	if events[2] != "log-error" {
		t.Errorf("events[2] = %q, want %q", events[2], "log-error")
	}
	if len(logger.requests) != 1 {
		t.Errorf("want 1 LogRequest call, got %d", len(logger.requests))
	}
	if len(logger.errs) != 1 {
		t.Errorf("want 1 LogError call when InvokeRaw fails, got %d", len(logger.errs))
	}
	if len(logger.responses) != 0 {
		t.Errorf("want 0 LogResponse calls when InvokeRaw fails, got %d", len(logger.responses))
	}
}

// TestOrchestratorConsultant_PreConsult_PreStart_LogsRequest verifies that
// the pre-start pre-consultation (context=pre_consultation, last_status_message=nil)
// is logged. This is the "even the pre-start one" requirement: the very first
// call to PreConsult (before the dispatch loop's first step) must record its
// request in the dispatch log. The test uses a nil LastStatusMessage to simulate
// the pre-start call's always-null last_status_message.
func TestOrchestratorConsultant_PreConsult_PreStart_LogsRequest(t *testing.T) {
	table := mustParseTable(t)
	invoker, logger := newOrderingPair([]byte(`{}`), nil)
	c := newTestConsultantWithLogger(invoker, table, logger)

	// Simulate the pre-start call: context=pre_consultation, no prior status.
	req := domain.ConsultationRequest{
		OrchestrationArtifact: "Orchestration-abc/Orchestration.md",
		Context:               domain.ConsultContextPreConsultation,
		LastStatusMessage:     nil,
	}
	_, err := c.PreConsult(context.Background(), req)
	if err != nil {
		t.Fatalf("want no error for pre-start pre-consultation, got %v", err)
	}

	// The pre-start call must be logged; zero LogRequest calls means the
	// pre-start consultation is invisible to the dispatch log.
	if len(logger.requests) == 0 {
		t.Error("want LogRequest called for the pre-start pre-consultation (context=pre_consultation), got zero calls; " +
			"the pre-start call must appear in the dispatch log alongside subagent invocations")
	}
}
