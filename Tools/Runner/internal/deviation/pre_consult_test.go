package deviation_test

// Tests for OrchestratorConsultant.PreConsult — pre-consultation request
// construction and response parsing (T3.3).

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"mosaic-run/internal/domain"
)

// ===== T3.3: Pre-consultation request and response parsing =====

// TestOrchestratorConsultant_PreConsultSendsCorrectContext verifies that
// PreConsult sends context=pre_consultation and last_status_message=null on
// the wire (pre-consultation always carries null, regardless of the request).
func TestOrchestratorConsultant_PreConsultSendsCorrectContext(t *testing.T) {
	table := mustParseTable(t)
	fake := &fakeRawInvoker{reply: []byte(`{}`)} // empty object = valid empty advice
	c := newTestOrchestratorConsultant(fake, table)

	req := domain.ConsultationRequest{
		OrchestrationArtifact: "Orchestration-abc/Orchestration.md",
		Context:               domain.ConsultContextPreConsultation,
		LastStatusMessage:     nil,
	}

	c.PreConsult(context.Background(), req) //nolint:errcheck

	if len(fake.sent) == 0 {
		t.Fatal("want OrchestratorConsultant to call RawInvoker.InvokeRaw for pre-consultation, got no payload")
	}

	w := mustUnmarshalWireRequest(t, fake.sent)
	if w.Context != "pre_consultation" {
		t.Errorf("want context=%q in pre-consultation wire request, got %q", "pre_consultation", w.Context)
	}

	// last_status_message must be JSON null for pre-consultation.
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(fake.sent, &raw); err != nil {
		t.Fatalf("want valid JSON payload, got %v", err)
	}
	rawLSM, present := raw["last_status_message"]
	if !present {
		t.Fatal("want last_status_message field present in pre-consultation wire request (as null), got absent")
	}
	if string(rawLSM) != "null" {
		t.Errorf("want last_status_message=null for pre-consultation, got %s", rawLSM)
	}
}

// TestOrchestratorConsultant_PreConsultBothFieldsPresent verifies that when the
// orchestrator returns both task_description and constraints, PreConsult parses
// them into a PreConsultationAdvice with both fields set.
func TestOrchestratorConsultant_PreConsultBothFieldsPresent(t *testing.T) {
	table := mustParseTable(t)
	fake := &fakeRawInvoker{reply: []byte(`{
		"task_description": "run stage 1 first",
		"constraints": "use python only"
	}`)}
	c := newTestOrchestratorConsultant(fake, table)

	req := domain.ConsultationRequest{
		OrchestrationArtifact: "Orchestration-abc/Orchestration.md",
		Context:               domain.ConsultContextPreConsultation,
	}
	advice, err := c.PreConsult(context.Background(), req)

	if err != nil {
		t.Fatalf("want no error for valid pre-consultation response, got %v", err)
	}
	if advice.TaskDescription != "run stage 1 first" {
		t.Errorf("want TaskDescription=%q, got %q", "run stage 1 first", advice.TaskDescription)
	}
	if advice.Constraints != "use python only" {
		t.Errorf("want Constraints=%q, got %q", "use python only", advice.Constraints)
	}
}

// TestOrchestratorConsultant_PreConsultOnlyTaskDescription verifies that when
// only task_description is present, PreConsult returns advice with TaskDescription
// set and Constraints empty.
func TestOrchestratorConsultant_PreConsultOnlyTaskDescription(t *testing.T) {
	table := mustParseTable(t)
	fake := &fakeRawInvoker{reply: []byte(`{"task_description":"run stage 1 first"}`)}
	c := newTestOrchestratorConsultant(fake, table)

	req := domain.ConsultationRequest{
		OrchestrationArtifact: "Orchestration-abc/Orchestration.md",
		Context:               domain.ConsultContextPreConsultation,
	}
	advice, err := c.PreConsult(context.Background(), req)

	if err != nil {
		t.Fatalf("want no error, got %v", err)
	}
	if advice.TaskDescription != "run stage 1 first" {
		t.Errorf("want TaskDescription=%q, got %q", "run stage 1 first", advice.TaskDescription)
	}
	if advice.Constraints != "" {
		t.Errorf("want Constraints empty, got %q", advice.Constraints)
	}
}

// TestOrchestratorConsultant_PreConsultOnlyConstraints verifies that when only
// constraints is present, PreConsult returns advice with Constraints set and
// TaskDescription empty.
func TestOrchestratorConsultant_PreConsultOnlyConstraints(t *testing.T) {
	table := mustParseTable(t)
	fake := &fakeRawInvoker{reply: []byte(`{"constraints":"use python only"}`)}
	c := newTestOrchestratorConsultant(fake, table)

	req := domain.ConsultationRequest{
		OrchestrationArtifact: "Orchestration-abc/Orchestration.md",
		Context:               domain.ConsultContextPreConsultation,
	}
	advice, err := c.PreConsult(context.Background(), req)

	if err != nil {
		t.Fatalf("want no error, got %v", err)
	}
	if advice.TaskDescription != "" {
		t.Errorf("want TaskDescription empty, got %q", advice.TaskDescription)
	}
	if advice.Constraints != "use python only" {
		t.Errorf("want Constraints=%q, got %q", "use python only", advice.Constraints)
	}
}

// TestOrchestratorConsultant_PreConsultBothFieldsAbsent verifies that an empty
// JSON object {} — both fields absent — is a valid, successful pre-consultation
// returning a zero-valued PreConsultationAdvice. Absence of advice is not an
// error; the orchestrator simply has nothing to add.
func TestOrchestratorConsultant_PreConsultBothFieldsAbsent(t *testing.T) {
	table := mustParseTable(t)
	fake := &fakeRawInvoker{reply: []byte(`{}`)}
	c := newTestOrchestratorConsultant(fake, table)

	req := domain.ConsultationRequest{
		OrchestrationArtifact: "Orchestration-abc/Orchestration.md",
		Context:               domain.ConsultContextPreConsultation,
	}
	advice, err := c.PreConsult(context.Background(), req)

	if err != nil {
		t.Fatalf("want no error for empty pre-consultation response, got %v", err)
	}
	if advice.TaskDescription != "" || advice.Constraints != "" {
		t.Errorf("want empty PreConsultationAdvice, got TaskDescription=%q Constraints=%q",
			advice.TaskDescription, advice.Constraints)
	}
}

// TestOrchestratorConsultant_PreConsultMalformedResponse verifies that a reply
// containing no JSON object at all produces a *ConsultationError with
// ConsultFailNoInstruction — "no instruction was found". ConsultFailMalformedJSON
// is reserved for a located object whose content violates the expected schema.
func TestOrchestratorConsultant_PreConsultMalformedResponse(t *testing.T) {
	table := mustParseTable(t)
	fake := &fakeRawInvoker{reply: []byte(`not json at all`)}
	c := newTestOrchestratorConsultant(fake, table)

	req := domain.ConsultationRequest{
		OrchestrationArtifact: "Orchestration-abc/Orchestration.md",
		Context:               domain.ConsultContextPreConsultation,
	}
	_, err := c.PreConsult(context.Background(), req)

	assertConsultationError(t, err, domain.ConsultFailNoInstruction)
}

// TestOrchestratorConsultant_PreConsultTransportError verifies that when
// RawInvoker returns an error during a pre-consultation call, PreConsult returns
// a *ConsultationError with ConsultFailTransport and that ConsultationError.Err
// wraps the original invoker error so callers can errors.Is/errors.As against the
// underlying cause. Mirrors the equivalent ConsultRouting transport error test.
func TestOrchestratorConsultant_PreConsultTransportError(t *testing.T) {
	table := mustParseTable(t)
	invokerErr := errors.New("harness: timeout after 30s")
	fake := &fakeRawInvoker{err: invokerErr}
	c := newTestOrchestratorConsultant(fake, table)

	req := domain.ConsultationRequest{
		OrchestrationArtifact: "Orchestration-abc/Orchestration.md",
		Context:               domain.ConsultContextPreConsultation,
		LastStatusMessage:     nil,
	}
	_, err := c.PreConsult(context.Background(), req)

	ce := assertConsultationError(t, err, domain.ConsultFailTransport)
	// ce.Err must wrap the original invoker error so callers can unwrap to the cause.
	if !errors.Is(ce.Err, invokerErr) {
		t.Errorf("want ce.Err to wrap the original invoker error via errors.Is, got ce.Err=%v", ce.Err)
	}
}
