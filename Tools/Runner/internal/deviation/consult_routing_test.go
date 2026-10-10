package deviation_test

// Tests for OrchestratorConsultant.ConsultRouting — routing request construction
// (T3.1), response parsing (T3.2), and additional routing assertions.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"mosaic-run/internal/domain"
)

// ===== T3.1: Routing consultation request construction =====

// TestOrchestratorConsultant_RequestContainsAllThreeFields verifies that
// ConsultRouting sends the orchestrator a wire request with all three required
// fields: orchestration_artifact, context, and last_status_message.
func TestOrchestratorConsultant_RequestContainsAllThreeFields(t *testing.T) {
	table := mustParseTable(t)
	fake := &fakeRawInvoker{reply: routingStopReply("done")}
	c := newTestOrchestratorConsultant(fake, table)

	msg := "agent-a#1 returned BLOCKED"
	req := domain.ConsultationRequest{
		OrchestrationArtifact: "Orchestration-20260816T000724Z-3b68/Orchestration.md",
		Context:               domain.ConsultContextRouting,
		LastStatusMessage:     &msg,
	}

	c.ConsultRouting(context.Background(), req) //nolint:errcheck

	if len(fake.sent) == 0 {
		t.Fatal("want OrchestratorConsultant to call RawInvoker.InvokeRaw with a payload, got no payload sent")
	}

	w := mustUnmarshalWireRequest(t, fake.sent)

	if w.OrchestrationArtifact == "" {
		t.Error("want orchestration_artifact non-empty in wire request")
	}
	if w.Context != "routing" {
		t.Errorf("want context=%q in routing wire request, got %q", "routing", w.Context)
	}
	// last_status_message must be present; we check null-vs-string separately.
	// Here we just verify the field was sent (non-nil pointer decoded means it was present).
	if w.LastStatusMessage == nil {
		t.Error("want last_status_message present in wire request when ConsultationRequest has a non-nil pointer")
	}
}

// TestOrchestratorConsultant_LastStatusMessageIsNullWhenNil verifies that when
// ConsultationRequest.LastStatusMessage is nil (first step of a new run),
// ConsultRouting encodes it as JSON null — not as an absent field — in the wire
// request. The orchestrator contract requires the field to always be present.
func TestOrchestratorConsultant_LastStatusMessageIsNullWhenNil(t *testing.T) {
	table := mustParseTable(t)
	fake := &fakeRawInvoker{reply: routingStopReply("done")}
	c := newTestOrchestratorConsultant(fake, table)

	req := domain.ConsultationRequest{
		OrchestrationArtifact: "Orchestration-abc/Orchestration.md",
		Context:               domain.ConsultContextRouting,
		LastStatusMessage:     nil, // first step — no prior agent message
	}

	c.ConsultRouting(context.Background(), req) //nolint:errcheck

	if len(fake.sent) == 0 {
		t.Fatal("want OrchestratorConsultant to call RawInvoker.InvokeRaw, got no payload")
	}

	// Unmarshal into a map so we can distinguish absent from null.
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(fake.sent, &raw); err != nil {
		t.Fatalf("want valid JSON wire payload, got %v", err)
	}

	rawLSM, present := raw["last_status_message"]
	if !present {
		t.Fatal("want last_status_message field present in wire request (as null), got absent")
	}

	// "null" must be the encoded value — the field exists but its value is JSON null.
	if string(rawLSM) != "null" {
		t.Errorf("want last_status_message=null in wire request, got %s", rawLSM)
	}
}

// TestOrchestratorConsultant_ArtifactPathMatchesRequest verifies that the
// orchestration_artifact in the wire request is exactly the path supplied in
// the ConsultationRequest, without modification.
func TestOrchestratorConsultant_ArtifactPathMatchesRequest(t *testing.T) {
	table := mustParseTable(t)
	fake := &fakeRawInvoker{reply: routingStopReply("done")}
	c := newTestOrchestratorConsultant(fake, table)

	wantArtifact := "Orchestration-20260816T000724Z-3b68/Orchestration.md"
	req := domain.ConsultationRequest{
		OrchestrationArtifact: wantArtifact,
		Context:               domain.ConsultContextRouting,
		LastStatusMessage:     strptr("some message"),
	}

	c.ConsultRouting(context.Background(), req) //nolint:errcheck

	if len(fake.sent) == 0 {
		t.Fatal("want OrchestratorConsultant to call RawInvoker.InvokeRaw, got no payload")
	}

	w := mustUnmarshalWireRequest(t, fake.sent)
	if w.OrchestrationArtifact != wantArtifact {
		t.Errorf("want orchestration_artifact=%q, got %q", wantArtifact, w.OrchestrationArtifact)
	}
}

// ===== T3.2: Routing response parsing =====

// TestOrchestratorConsultant_DispatchAllOptionalFieldsPresent verifies that
// when the routing response carries all optional fields with non-null values,
// ConsultRouting returns a DispatchInstruction with all pointer fields non-nil
// and set to the given values.
func TestOrchestratorConsultant_DispatchAllOptionalFieldsPresent(t *testing.T) {
	table := mustParseTable(t)
	constraintVal := "run fast"
	reply := []byte(`{
		"action": "dispatch",
		"agent": "agent-a",
"row": 1,
		"task_description": "do the thing",
		"constraints": "run fast",
		"input_artifacts": ["in.md"],
		"output_artifacts": ["out.md"],
		"hitl_override": true
	}`)
	fake := &fakeRawInvoker{reply: reply}
	c := newTestOrchestratorConsultant(fake, table)

	instr, err := c.ConsultRouting(context.Background(), validRoutingRequest("Orchestration-abc/Orchestration.md"))

	if err != nil {
		t.Fatalf("want no error for valid dispatch response, got %v", err)
	}
	if instr.Dispatch == nil {
		t.Fatal("want Dispatch instruction, got nil Dispatch")
	}
	d := instr.Dispatch
	if d.Agent != "agent-a" {
		t.Errorf("want Agent=%q, got %q", "agent-a", d.Agent)
	}
	if d.TaskDescription != "do the thing" {
		t.Errorf("want TaskDescription=%q, got %q", "do the thing", d.TaskDescription)
	}
	if d.Constraints == nil {
		t.Fatal("want non-nil Constraints pointer, got nil")
	}
	if *d.Constraints != constraintVal {
		t.Errorf("want *Constraints=%q, got %q", constraintVal, *d.Constraints)
	}
	if d.InputArtifacts == nil {
		t.Fatal("want non-nil InputArtifacts pointer, got nil")
	}
	if len(*d.InputArtifacts) != 1 || (*d.InputArtifacts)[0] != "in.md" {
		t.Errorf("want *InputArtifacts=[in.md], got %v", *d.InputArtifacts)
	}
	if d.OutputArtifacts == nil {
		t.Fatal("want non-nil OutputArtifacts pointer, got nil")
	}
	if len(*d.OutputArtifacts) != 1 || (*d.OutputArtifacts)[0] != "out.md" {
		t.Errorf("want *OutputArtifacts=[out.md], got %v", *d.OutputArtifacts)
	}
	if d.HITLOverride == nil {
		t.Fatal("want non-nil HITLOverride pointer, got nil")
	}
	if !*d.HITLOverride {
		t.Error("want *HITLOverride=true, got false")
	}
	// agent-a is row 0 in the two-row fixture.
	if d.RowIndex != 0 {
		t.Errorf("want RowIndex=0 for agent-a (row 0 in fixture), got %d", d.RowIndex)
	}
}

// TestOrchestratorConsultant_DispatchNullOptionalFields verifies that when
// optional fields are explicitly set to JSON null, the corresponding pointer
// fields in DispatchInstruction are nil (fall back to the table row).
func TestOrchestratorConsultant_DispatchNullOptionalFields(t *testing.T) {
	table := mustParseTable(t)
	reply := []byte(`{
		"action": "dispatch",
		"agent": "agent-a",
"row": 1,
		"task_description": "do the thing",
		"constraints": null,
		"input_artifacts": null,
		"output_artifacts": null,
		"hitl_override": null
	}`)
	fake := &fakeRawInvoker{reply: reply}
	c := newTestOrchestratorConsultant(fake, table)

	instr, err := c.ConsultRouting(context.Background(), validRoutingRequest("Orchestration-abc/Orchestration.md"))

	if err != nil {
		t.Fatalf("want no error for dispatch with null optional fields, got %v", err)
	}
	if instr.Dispatch == nil {
		t.Fatal("want Dispatch instruction, got nil")
	}
	d := instr.Dispatch
	if d.Constraints != nil {
		t.Errorf("want Constraints=nil (JSON null -> fall back to table row), got non-nil: %q", *d.Constraints)
	}
	if d.InputArtifacts != nil {
		t.Errorf("want InputArtifacts=nil (JSON null -> fall back to table row), got non-nil")
	}
	if d.OutputArtifacts != nil {
		t.Errorf("want OutputArtifacts=nil (JSON null -> fall back to table row), got non-nil")
	}
	if d.HITLOverride != nil {
		t.Errorf("want HITLOverride=nil (JSON null -> fall back to table row), got non-nil: %v", *d.HITLOverride)
	}
}

// TestOrchestratorConsultant_DispatchAbsentOptionalFields verifies that when
// optional fields are absent from the JSON response, the corresponding pointer
// fields in DispatchInstruction are nil (same fallback behaviour as JSON null).
func TestOrchestratorConsultant_DispatchAbsentOptionalFields(t *testing.T) {
	table := mustParseTable(t)
	// Only the required fields; all optional fields absent.
	reply := []byte(`{"action":"dispatch","agent":"agent-a","row":1,"task_description":"do the thing"}`)
	fake := &fakeRawInvoker{reply: reply}
	c := newTestOrchestratorConsultant(fake, table)

	instr, err := c.ConsultRouting(context.Background(), validRoutingRequest("Orchestration-abc/Orchestration.md"))

	if err != nil {
		t.Fatalf("want no error for dispatch with absent optional fields, got %v", err)
	}
	if instr.Dispatch == nil {
		t.Fatal("want Dispatch instruction, got nil")
	}
	d := instr.Dispatch
	if d.Constraints != nil {
		t.Errorf("want Constraints=nil (absent -> fall back to table row), got non-nil")
	}
	if d.InputArtifacts != nil {
		t.Errorf("want InputArtifacts=nil (absent -> fall back to table row), got non-nil")
	}
	if d.OutputArtifacts != nil {
		t.Errorf("want OutputArtifacts=nil (absent -> fall back to table row), got non-nil")
	}
	if d.HITLOverride != nil {
		t.Errorf("want HITLOverride=nil (absent -> fall back to table row), got non-nil")
	}
}

// TestOrchestratorConsultant_DispatchExplicitlyEmptyInputArtifacts verifies the
// AC3.2 distinction: an explicit empty array [] decodes to a non-nil pointer to
// an empty slice — "dispatch with no input artifacts" — which is distinct from nil
// (which means "fall back to the table row's Input column").
func TestOrchestratorConsultant_DispatchExplicitlyEmptyInputArtifacts(t *testing.T) {
	table := mustParseTable(t)
	reply := []byte(`{"action":"dispatch","agent":"agent-a","row":1,"task_description":"do the thing","input_artifacts":[]}`)
	fake := &fakeRawInvoker{reply: reply}
	c := newTestOrchestratorConsultant(fake, table)

	instr, err := c.ConsultRouting(context.Background(), validRoutingRequest("Orchestration-abc/Orchestration.md"))

	if err != nil {
		t.Fatalf("want no error for dispatch with explicit empty input_artifacts, got %v", err)
	}
	if instr.Dispatch == nil {
		t.Fatal("want Dispatch instruction, got nil")
	}
	d := instr.Dispatch
	if d.InputArtifacts == nil {
		t.Fatal("want non-nil InputArtifacts pointer for explicit [] (distinct from absent/null), got nil — AC3.2 violated")
	}
	if len(*d.InputArtifacts) != 0 {
		t.Errorf("want empty InputArtifacts slice, got %v", *d.InputArtifacts)
	}
}

// TestOrchestratorConsultant_StopResponseParsed verifies that a stop routing
// response returns a RoutingInstruction with a non-nil Stop field carrying the
// reason verbatim.
func TestOrchestratorConsultant_StopResponseParsed(t *testing.T) {
	table := mustParseTable(t)
	wantReason := "orchestrator decided the run is complete"
	fake := &fakeRawInvoker{reply: routingStopReply(wantReason)}
	c := newTestOrchestratorConsultant(fake, table)

	instr, err := c.ConsultRouting(context.Background(), validRoutingRequest("Orchestration-abc/Orchestration.md"))

	if err != nil {
		t.Fatalf("want no error for valid stop response, got %v", err)
	}
	if instr.Stop == nil {
		t.Fatal("want Stop instruction, got nil Stop")
	}
	if instr.Dispatch != nil {
		t.Error("want Dispatch=nil for stop response, got non-nil")
	}
	if instr.Stop.Reason != wantReason {
		t.Errorf("want Stop.Reason=%q, got %q", wantReason, instr.Stop.Reason)
	}
}

// TestOrchestratorConsultant_MalformedJSONResponse verifies that a reply
// containing no parseable JSON object (an opening brace with no matching close,
// or otherwise unparseable content) produces a *ConsultationError with
// ConsultFailNoInstruction — "no instruction was found in the reply".
// ConsultFailMalformedJSON is reserved for the case where an object IS located
// but does not unmarshal into the expected wire schema.
func TestOrchestratorConsultant_MalformedJSONResponse(t *testing.T) {
	table := mustParseTable(t)
	fake := &fakeRawInvoker{reply: []byte(`{not valid json at all`)}
	c := newTestOrchestratorConsultant(fake, table)

	_, err := c.ConsultRouting(context.Background(), validRoutingRequest("Orchestration-abc/Orchestration.md"))

	assertConsultationError(t, err, domain.ConsultFailNoInstruction)
}

// TestOrchestratorConsultant_MissingAgentField verifies that a dispatch response
// with an absent or empty agent field produces a *ConsultationError with
// ConsultFailMissingField. The error must name "agent" as the missing field.
func TestOrchestratorConsultant_MissingAgentField(t *testing.T) {
	table := mustParseTable(t)
	// agent is absent
	reply := []byte(`{"action":"dispatch","task_description":"do the thing"}`)
	fake := &fakeRawInvoker{reply: reply}
	c := newTestOrchestratorConsultant(fake, table)

	_, err := c.ConsultRouting(context.Background(), validRoutingRequest("Orchestration-abc/Orchestration.md"))

	ce := assertConsultationError(t, err, domain.ConsultFailMissingField)
	// The error detail must name the missing field so the operator can diagnose it.
	if !strings.Contains(ce.Detail, "agent") {
		t.Errorf("want ConsultationError.Detail to contain %q (field name), got %q", "agent", ce.Detail)
	}
}

// TestOrchestratorConsultant_MissingTaskDescriptionField verifies that a dispatch
// response with an absent or empty task_description produces a *ConsultationError
// with ConsultFailMissingField, naming "task_description".
func TestOrchestratorConsultant_MissingTaskDescriptionField(t *testing.T) {
	table := mustParseTable(t)
	// task_description is absent
	reply := []byte(`{"action":"dispatch","agent":"agent-a"}`)
	fake := &fakeRawInvoker{reply: reply}
	c := newTestOrchestratorConsultant(fake, table)

	_, err := c.ConsultRouting(context.Background(), validRoutingRequest("Orchestration-abc/Orchestration.md"))

	ce := assertConsultationError(t, err, domain.ConsultFailMissingField)
	if !strings.Contains(ce.Detail, "task_description") {
		t.Errorf("want ConsultationError.Detail to contain %q (field name), got %q", "task_description", ce.Detail)
	}
}

// TestOrchestratorConsultant_UnknownActionValue verifies that an unrecognised
// action value (anything other than "dispatch" or "stop") produces a
// *ConsultationError with ConsultFailUnknownAction. Comparison must be exact and
// case-sensitive (e.g. "Dispatch" is unknown).
func TestOrchestratorConsultant_UnknownActionValue(t *testing.T) {
	cases := []struct {
		name   string
		action string
	}{
		{"rejoin (retired)", "rejoin"},
		{"custom (retired)", "custom"},
		{"capitalised", "Dispatch"},
		{"empty", ""},
		{"unknown", "proceed"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			table := mustParseTable(t)
			reply := []byte(fmt.Sprintf(`{"action":%q}`, tc.action))
			fake := &fakeRawInvoker{reply: reply}
			c := newTestOrchestratorConsultant(fake, table)

			_, err := c.ConsultRouting(context.Background(), validRoutingRequest("Orchestration-abc/Orchestration.md"))

			assertConsultationError(t, err, domain.ConsultFailUnknownAction)
		})
	}
}

// TestOrchestratorConsultant_AgentNotInRoutingTable verifies that when a dispatch
// response names an agent not present in the routing table, ConsultRouting returns
// a *ConsultationError with ConsultFailUnknownAgent. The error must list the
// available routing table agents so the operator knows what is valid (AC3.3).
func TestOrchestratorConsultant_AgentNotInRoutingTable(t *testing.T) {
	table := mustParseTable(t)
	reply := []byte(`{"action":"dispatch","agent":"nonexistent-agent","task_description":"do the thing"}`)
	fake := &fakeRawInvoker{reply: reply}
	c := newTestOrchestratorConsultant(fake, table)

	_, err := c.ConsultRouting(context.Background(), validRoutingRequest("Orchestration-abc/Orchestration.md"))

	ce := assertConsultationError(t, err, domain.ConsultFailUnknownAgent)
	if len(ce.Agents) == 0 {
		t.Error("want ConsultationError.Agents non-empty (lists available routing table agents), got empty — AC3.3 violated")
	}
}

// TestOrchestratorConsultant_UnknownExtraFieldsIgnored verifies that a routing
// response with extra unknown fields is still parsed successfully (AC3.4: forward
// compatibility). An unknown field must not cause a failure.
func TestOrchestratorConsultant_UnknownExtraFieldsIgnored(t *testing.T) {
	table := mustParseTable(t)
	reply := []byte(`{
		"action": "stop",
		"reason": "done",
		"unknown_future_field": "ignored",
		"another_unknown": 42
	}`)
	fake := &fakeRawInvoker{reply: reply}
	c := newTestOrchestratorConsultant(fake, table)

	instr, err := c.ConsultRouting(context.Background(), validRoutingRequest("Orchestration-abc/Orchestration.md"))

	if err != nil {
		t.Fatalf("want no error when response has extra unknown fields, got %v — AC3.4 violated", err)
	}
	if instr.Stop == nil {
		t.Fatal("want Stop instruction from response with extra fields, got nil")
	}
}

// TestOrchestratorConsultant_TransportError verifies that when RawInvoker returns
// an error (harness failure), ConsultRouting returns a *ConsultationError with
// ConsultFailTransport and that ConsultationError.Err wraps the original invoker
// error so callers can errors.Is/errors.As against the underlying cause.
func TestOrchestratorConsultant_TransportError(t *testing.T) {
	table := mustParseTable(t)
	invokerErr := errors.New("harness: timeout after 30s")
	fake := &fakeRawInvoker{err: invokerErr}
	c := newTestOrchestratorConsultant(fake, table)

	_, err := c.ConsultRouting(context.Background(), validRoutingRequest("Orchestration-abc/Orchestration.md"))

	ce := assertConsultationError(t, err, domain.ConsultFailTransport)
	// ce.Err must wrap the original invoker error so callers can unwrap to the cause.
	if !errors.Is(ce.Err, invokerErr) {
		t.Errorf("want ce.Err to wrap the original invoker error via errors.Is, got ce.Err=%v", ce.Err)
	}
}

