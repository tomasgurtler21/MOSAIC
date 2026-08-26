package deviation_test

// Tests for the deviation package — two-action orchestrator contract and client.
//
// Coverage:
//
//   OrchestratorConsultant — routing request construction (T3.1):
//   - Sends a wire request with all three required fields: orchestration_artifact,
//     context, and last_status_message.
//   - Encodes last_status_message as JSON null (not absent) when the
//     ConsultationRequest carries a nil pointer (first step of a new run).
//   - Uses exactly the orchestration_artifact path from the ConsultationRequest.
//
//   OrchestratorConsultant — routing response parsing (T3.2):
//   - Parses a dispatch response with every optional field present; all pointer
//     fields are non-nil and carry the given values.
//   - Parses a dispatch response with optional fields set to JSON null; pointer
//     fields are nil (fall back to table row).
//   - Parses a dispatch response with optional fields absent; pointer fields are
//     nil (same fallback behaviour as null).
//   - Parses a dispatch response with input_artifacts as an explicit empty array;
//     InputArtifacts is a non-nil pointer to an empty slice — distinct from nil
//     (AC3.2: "dispatch with no input artifacts" is different from "fall back").
//   - Parses a stop response; returns a StopInstruction with the given reason.
//   - Malformed JSON reply → *ConsultationError with ConsultFailMalformedJSON.
//   - action="dispatch" with agent field absent or empty → *ConsultationError
//     with ConsultFailMissingField naming the "agent" field.
//   - action="dispatch" with task_description absent or empty → *ConsultationError
//     with ConsultFailMissingField naming the "task_description" field.
//   - Unknown action value → *ConsultationError with ConsultFailUnknownAction.
//   - agent identifier not in routing table → *ConsultationError with
//     ConsultFailUnknownAgent; the error lists the available routing table agents.
//   - Unknown extra fields in an otherwise valid response are ignored (AC3.4).
//
//   OrchestratorConsultant — pre-consultation (T3.3):
//   - Sends context=pre_consultation and last_status_message=null on the wire.
//   - Parses a response with both task_description and constraints present.
//   - Parses a response with only task_description; constraints is empty.
//   - Parses a response with only constraints; task_description is empty.
//   - Parses a response with both fields absent; success with empty advice.
//   - Malformed response (not valid JSON) → *ConsultationError.
//
//   ManualResolver — two-action instruction shape (T3.4):
//   - Calls Interaction.SelectOne with one option per routing table row plus a
//     stop option whose ID is "stop".
//   - When the user selects an agent row and supplies a task description, returns
//     a RoutingInstruction with a non-nil Dispatch carrying the agent identifier
//     and the supplied task description.
//   - When the user selects the stop option, returns a RoutingInstruction with a
//     non-nil Stop (and nil Dispatch).

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"mosaic-common/interaction"
	"mosaic-run/internal/deviation"
	"mosaic-run/internal/domain"
	"mosaic-run/internal/workflow"
)

// ---- test workflow fixture ----

// simpleLinearContent is a minimal two-row linear workflow used to build
// routing table fixtures. Row 0: agent-a, Row 1: agent-b.
const simpleLinearContent = `## Simple Linear

| Phase | Subagent | HITL | On Success | On Findings | Input | Output |
|-------|----------|:----:|------------|-------------|-------|--------|
| PLANNING | agent-a | FALSE | agent-b | - | - | plan.md |
| PLANNING | agent-b | FALSE | COMPLETE | - | plan.md | result.md |
`

func mustParseTable(t *testing.T) domain.RoutingTable {
	t.Helper()
	info := domain.WorkflowInfo{ID: "linear", Version: "1.0"}
	table, err := workflow.Parse([]byte(simpleLinearContent), info)
	if err != nil {
		t.Fatalf("workflow.Parse: %v", err)
	}
	return table
}

// ---- fakeRawInvoker ----

// fakeRawInvoker is a test double for domain.RawInvoker. It captures the
// payload sent to InvokeRaw and returns a configured reply or error.
type fakeRawInvoker struct {
	reply []byte
	err   error
	// sent is populated with the payload from the last InvokeRaw call.
	sent []byte
}

func (f *fakeRawInvoker) InvokeRaw(_ context.Context, _ domain.AgentReference, payload []byte) ([]byte, error) {
	f.sent = payload
	if f.err != nil {
		return nil, f.err
	}
	if len(f.reply) == 0 {
		return nil, errors.New("fakeRawInvoker: no reply configured")
	}
	return f.reply, nil
}

// ---- scriptedInteraction ----

// scriptedInteraction is a test double for the Interaction port.
type scriptedInteraction struct {
	SelectOneResult  func(q interaction.ChoiceQuestion) (interaction.ChoiceAnswer, error)
	AskTextResult    func(q interaction.TextQuestion) (interaction.TextAnswer, error)
	ConfirmResult    func(q interaction.Question) (interaction.ConfirmAnswer, error)
	SelectManyResult func(q interaction.ChoiceQuestion) (interaction.MultiChoiceAnswer, error)

	SelectOneCalls []interaction.ChoiceQuestion
	AskTextCalls   []interaction.TextQuestion
}

func (s *scriptedInteraction) SelectOne(_ context.Context, q interaction.ChoiceQuestion) (interaction.ChoiceAnswer, error) {
	s.SelectOneCalls = append(s.SelectOneCalls, q)
	if s.SelectOneResult != nil {
		return s.SelectOneResult(q)
	}
	if len(q.Options) > 0 {
		return interaction.ChoiceAnswer{Status: interaction.Answered, OptionID: q.Options[0].ID}, nil
	}
	return interaction.ChoiceAnswer{Status: interaction.Answered}, nil
}

func (s *scriptedInteraction) SelectMany(_ context.Context, q interaction.ChoiceQuestion) (interaction.MultiChoiceAnswer, error) {
	if s.SelectManyResult != nil {
		return s.SelectManyResult(q)
	}
	return interaction.MultiChoiceAnswer{Status: interaction.Answered}, nil
}

func (s *scriptedInteraction) AskText(_ context.Context, q interaction.TextQuestion) (interaction.TextAnswer, error) {
	s.AskTextCalls = append(s.AskTextCalls, q)
	if s.AskTextResult != nil {
		return s.AskTextResult(q)
	}
	return interaction.TextAnswer{Status: interaction.Answered, Text: ""}, nil
}

func (s *scriptedInteraction) Confirm(_ context.Context, q interaction.Question) (interaction.ConfirmAnswer, error) {
	if s.ConfirmResult != nil {
		return s.ConfirmResult(q)
	}
	return interaction.ConfirmAnswer{Status: interaction.Answered, Confirm: false}, nil
}

func (s *scriptedInteraction) Notify(_ context.Context, _ interaction.Notice)          {}
func (s *scriptedInteraction) Progress(_ context.Context, _ interaction.ProgressEvent) {}

// ---- helpers ----

func orchestratorRef() domain.AgentReference {
	// Use a generic test identifier (not "orchestrator-script") so these tests
	// are not accidentally coupled to any hardcoded name in the implementation.
	return domain.AgentReference{
		Identifier:     "test-orchestrator",
		DefinitionPath: "/agents/test-orchestrator.md",
		InvocationKind: domain.InvocationOrchestrator,
	}
}

func makeOrchestratorConsultant(invoker domain.RawInvoker, table domain.RoutingTable) *deviation.OrchestratorConsultant {
	return &deviation.OrchestratorConsultant{
		Invoker:      invoker,
		Orchestrator: orchestratorRef(),
		Table:        table,
	}
}

func strptr(s string) *string { return &s }

// routingReply returns a JSON routing response with the given action and extra fields.
func routingStopReply(reason string) []byte {
	return []byte(fmt.Sprintf(`{"action":"stop","reason":%q}`, reason))
}

func validRoutingRequest(artifact string) domain.ConsultationRequest {
	msg := "agent returned BLOCKED"
	return domain.ConsultationRequest{
		OrchestrationArtifact: artifact,
		Context:               domain.ConsultContextRouting,
		LastStatusMessage:     &msg,
	}
}

// wireRequest mirrors the expected wire shape for assertion purposes.
type wireRequest struct {
	OrchestrationArtifact string  `json:"orchestration_artifact"`
	Context               string  `json:"context"`
	LastStatusMessage     *string `json:"last_status_message"`
}

// mustUnmarshalWireRequest parses a captured payload into wireRequest.
func mustUnmarshalWireRequest(t *testing.T, data []byte) wireRequest {
	t.Helper()
	var w wireRequest
	if err := json.Unmarshal(data, &w); err != nil {
		t.Fatalf("want valid JSON wire request, got unmarshal error: %v", err)
	}
	return w
}

// assertConsultationError checks that err is a *ConsultationError with the
// expected failure classification. Returns the error for further assertions.
func assertConsultationError(t *testing.T, err error, want domain.ConsultationFailure) *domain.ConsultationError {
	t.Helper()
	if err == nil {
		t.Fatalf("want *ConsultationError with failure=%q, got nil error", want)
	}
	var ce *domain.ConsultationError
	if !errors.As(err, &ce) {
		t.Fatalf("want *ConsultationError, got %T: %v", err, err)
	}
	if ce.Failure != want {
		t.Errorf("want ConsultationError.Failure=%q, got %q", want, ce.Failure)
	}
	return ce
}

// ===== T3.1: Routing consultation request construction =====

// TestOrchestratorConsultant_RequestContainsAllThreeFields verifies that
// ConsultRouting sends the orchestrator a wire request with all three required
// fields: orchestration_artifact, context, and last_status_message.
func TestOrchestratorConsultant_RequestContainsAllThreeFields(t *testing.T) {
	table := mustParseTable(t)
	fake := &fakeRawInvoker{reply: routingStopReply("done")}
	c := makeOrchestratorConsultant(fake, table)

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
	c := makeOrchestratorConsultant(fake, table)

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
	c := makeOrchestratorConsultant(fake, table)

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
		"task_description": "do the thing",
		"constraints": "run fast",
		"input_artifacts": ["in.md"],
		"output_artifacts": ["out.md"],
		"hitl_override": true
	}`)
	fake := &fakeRawInvoker{reply: reply}
	c := makeOrchestratorConsultant(fake, table)

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
		"task_description": "do the thing",
		"constraints": null,
		"input_artifacts": null,
		"output_artifacts": null,
		"hitl_override": null
	}`)
	fake := &fakeRawInvoker{reply: reply}
	c := makeOrchestratorConsultant(fake, table)

	instr, err := c.ConsultRouting(context.Background(), validRoutingRequest("Orchestration-abc/Orchestration.md"))

	if err != nil {
		t.Fatalf("want no error for dispatch with null optional fields, got %v", err)
	}
	if instr.Dispatch == nil {
		t.Fatal("want Dispatch instruction, got nil")
	}
	d := instr.Dispatch
	if d.Constraints != nil {
		t.Errorf("want Constraints=nil (JSON null → fall back to table row), got non-nil: %q", *d.Constraints)
	}
	if d.InputArtifacts != nil {
		t.Errorf("want InputArtifacts=nil (JSON null → fall back to table row), got non-nil")
	}
	if d.OutputArtifacts != nil {
		t.Errorf("want OutputArtifacts=nil (JSON null → fall back to table row), got non-nil")
	}
	if d.HITLOverride != nil {
		t.Errorf("want HITLOverride=nil (JSON null → fall back to table row), got non-nil: %v", *d.HITLOverride)
	}
}

// TestOrchestratorConsultant_DispatchAbsentOptionalFields verifies that when
// optional fields are absent from the JSON response, the corresponding pointer
// fields in DispatchInstruction are nil (same fallback behaviour as JSON null).
func TestOrchestratorConsultant_DispatchAbsentOptionalFields(t *testing.T) {
	table := mustParseTable(t)
	// Only the required fields; all optional fields absent.
	reply := []byte(`{"action":"dispatch","agent":"agent-a","task_description":"do the thing"}`)
	fake := &fakeRawInvoker{reply: reply}
	c := makeOrchestratorConsultant(fake, table)

	instr, err := c.ConsultRouting(context.Background(), validRoutingRequest("Orchestration-abc/Orchestration.md"))

	if err != nil {
		t.Fatalf("want no error for dispatch with absent optional fields, got %v", err)
	}
	if instr.Dispatch == nil {
		t.Fatal("want Dispatch instruction, got nil")
	}
	d := instr.Dispatch
	if d.Constraints != nil {
		t.Errorf("want Constraints=nil (absent → fall back to table row), got non-nil")
	}
	if d.InputArtifacts != nil {
		t.Errorf("want InputArtifacts=nil (absent → fall back to table row), got non-nil")
	}
	if d.OutputArtifacts != nil {
		t.Errorf("want OutputArtifacts=nil (absent → fall back to table row), got non-nil")
	}
	if d.HITLOverride != nil {
		t.Errorf("want HITLOverride=nil (absent → fall back to table row), got non-nil")
	}
}

// TestOrchestratorConsultant_DispatchExplicitlyEmptyInputArtifacts verifies the
// AC3.2 distinction: an explicit empty array [] decodes to a non-nil pointer to
// an empty slice — "dispatch with no input artifacts" — which is distinct from nil
// (which means "fall back to the table row's Input column").
func TestOrchestratorConsultant_DispatchExplicitlyEmptyInputArtifacts(t *testing.T) {
	table := mustParseTable(t)
	reply := []byte(`{"action":"dispatch","agent":"agent-a","task_description":"do the thing","input_artifacts":[]}`)
	fake := &fakeRawInvoker{reply: reply}
	c := makeOrchestratorConsultant(fake, table)

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
	c := makeOrchestratorConsultant(fake, table)

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
	c := makeOrchestratorConsultant(fake, table)

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
	c := makeOrchestratorConsultant(fake, table)

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
	c := makeOrchestratorConsultant(fake, table)

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
			c := makeOrchestratorConsultant(fake, table)

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
	c := makeOrchestratorConsultant(fake, table)

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
	c := makeOrchestratorConsultant(fake, table)

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
	c := makeOrchestratorConsultant(fake, table)

	_, err := c.ConsultRouting(context.Background(), validRoutingRequest("Orchestration-abc/Orchestration.md"))

	ce := assertConsultationError(t, err, domain.ConsultFailTransport)
	// ce.Err must wrap the original invoker error so callers can unwrap to the cause.
	if !errors.Is(ce.Err, invokerErr) {
		t.Errorf("want ce.Err to wrap the original invoker error via errors.Is, got ce.Err=%v", ce.Err)
	}
}

// ===== T3.3: Pre-consultation request and response parsing =====

// TestOrchestratorConsultant_PreConsultSendsCorrectContext verifies that
// PreConsult sends context=pre_consultation and last_status_message=null on
// the wire (pre-consultation always carries null, regardless of the request).
func TestOrchestratorConsultant_PreConsultSendsCorrectContext(t *testing.T) {
	table := mustParseTable(t)
	fake := &fakeRawInvoker{reply: []byte(`{}`)} // empty object = valid empty advice
	c := makeOrchestratorConsultant(fake, table)

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
	c := makeOrchestratorConsultant(fake, table)

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
	c := makeOrchestratorConsultant(fake, table)

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
	c := makeOrchestratorConsultant(fake, table)

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
	c := makeOrchestratorConsultant(fake, table)

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
	c := makeOrchestratorConsultant(fake, table)

	req := domain.ConsultationRequest{
		OrchestrationArtifact: "Orchestration-abc/Orchestration.md",
		Context:               domain.ConsultContextPreConsultation,
	}
	_, err := c.PreConsult(context.Background(), req)

	assertConsultationError(t, err, domain.ConsultFailNoInstruction)
}

// ===== T3.4: Manual resolver — two-action instruction shape =====

// TestManualResolver_PresentsBothRowOptionsAndStopOption verifies that
// ConsultRouting calls Interaction.SelectOne with one option per routing table
// row and a mandatory stop option whose ID is "stop".
func TestManualResolver_PresentsBothRowOptionsAndStopOption(t *testing.T) {
	table := mustParseTable(t)
	interact := &scriptedInteraction{
		SelectOneResult: func(q interaction.ChoiceQuestion) (interaction.ChoiceAnswer, error) {
			// Find the stop option to exercise that path.
			for _, opt := range q.Options {
				if opt.ID == "stop" {
					return interaction.ChoiceAnswer{Status: interaction.Answered, OptionID: opt.ID}, nil
				}
			}
			// Fall back to the first option if stop was not found (the assertion below will catch it).
			if len(q.Options) > 0 {
				return interaction.ChoiceAnswer{Status: interaction.Answered, OptionID: q.Options[0].ID}, nil
			}
			return interaction.ChoiceAnswer{Status: interaction.Answered}, nil
		},
	}

	m := &deviation.ManualResolver{
		Interact: interact,
		Table:    table,
	}
	req := domain.ConsultationRequest{
		OrchestrationArtifact: "Orchestration-abc/Orchestration.md",
		Context:               domain.ConsultContextRouting,
	}

	m.ConsultRouting(context.Background(), req) //nolint:errcheck

	if len(interact.SelectOneCalls) == 0 {
		t.Fatal("want ManualResolver to call Interaction.SelectOne, got zero calls")
	}

	q := interact.SelectOneCalls[0]

	// Expect one option per routing table row, plus the stop option.
	wantMinOptions := len(table.Rows) + 1
	if len(q.Options) < wantMinOptions {
		t.Errorf("want at least %d options (rows + stop), got %d", wantMinOptions, len(q.Options))
	}

	// The stop option must have ID "stop".
	hasStop := false
	for _, opt := range q.Options {
		if opt.ID == "stop" {
			hasStop = true
			break
		}
	}
	if !hasStop {
		t.Errorf("want stop option with ID=%q in SelectOne options, got none — options: %v", "stop", q.Options)
	}
}

// TestManualResolver_ConsultRoutingReturnsDispatchForSelectedAgent verifies that
// when the user selects an agent row and provides a task description, ConsultRouting
// returns a RoutingInstruction with a non-nil Dispatch carrying the agent identifier
// and the user-supplied task description.
func TestManualResolver_ConsultRoutingReturnsDispatchForSelectedAgent(t *testing.T) {
	table := mustParseTable(t)
	wantTaskDesc := "implement the planned feature"
	// User selects the first non-stop option.
	interact := &scriptedInteraction{
		SelectOneResult: func(q interaction.ChoiceQuestion) (interaction.ChoiceAnswer, error) {
			for _, opt := range q.Options {
				if opt.ID != "stop" {
					return interaction.ChoiceAnswer{Status: interaction.Answered, OptionID: opt.ID}, nil
				}
			}
			return interaction.ChoiceAnswer{Status: interaction.Answered}, nil
		},
		AskTextResult: func(_ interaction.TextQuestion) (interaction.TextAnswer, error) {
			return interaction.TextAnswer{Status: interaction.Answered, Text: wantTaskDesc}, nil
		},
	}

	m := &deviation.ManualResolver{
		Interact: interact,
		Table:    table,
	}
	req := domain.ConsultationRequest{
		OrchestrationArtifact: "Orchestration-abc/Orchestration.md",
		Context:               domain.ConsultContextRouting,
	}

	instr, err := m.ConsultRouting(context.Background(), req)

	if err != nil {
		t.Fatalf("want no error, got %v", err)
	}
	if instr.Dispatch == nil {
		t.Fatal("want Dispatch instruction when user selects an agent, got nil Dispatch")
	}
	if instr.Stop != nil {
		t.Error("want Stop=nil when user selects an agent, got non-nil Stop")
	}
	if instr.Dispatch.Agent == "" {
		t.Error("want non-empty Dispatch.Agent")
	}
	if instr.Dispatch.TaskDescription != wantTaskDesc {
		t.Errorf("want TaskDescription=%q, got %q", wantTaskDesc, instr.Dispatch.TaskDescription)
	}
	// The first non-stop option is agent-a at row 0 in the two-row fixture.
	if instr.Dispatch.RowIndex != 0 {
		t.Errorf("want RowIndex=0 for first agent (agent-a, row 0 in fixture), got %d", instr.Dispatch.RowIndex)
	}
	// ManualResolver never originates a HITL override; HITLOverride is set only
	// when the user explicitly asks for one. The resolver itself must leave it nil.
	if instr.Dispatch.HITLOverride != nil {
		t.Errorf("want HITLOverride=nil (ManualResolver never originates a HITL override), got non-nil: %v", *instr.Dispatch.HITLOverride)
	}
}

// TestManualResolver_ConsultRoutingReturnsStopWhenUserChoosesStop verifies that
// when the user selects the stop option, ConsultRouting returns a RoutingInstruction
// with a non-nil Stop and nil Dispatch.
func TestManualResolver_ConsultRoutingReturnsStopWhenUserChoosesStop(t *testing.T) {
	table := mustParseTable(t)
	interact := &scriptedInteraction{
		SelectOneResult: func(q interaction.ChoiceQuestion) (interaction.ChoiceAnswer, error) {
			for _, opt := range q.Options {
				if opt.ID == "stop" {
					return interaction.ChoiceAnswer{Status: interaction.Answered, OptionID: opt.ID}, nil
				}
			}
			// stop option not found — the assertion will surface this.
			t.Errorf("want stop option with ID=%q in SelectOne options, got none — options: %v", "stop", q.Options)
			return interaction.ChoiceAnswer{Status: interaction.Answered}, nil
		},
	}

	m := &deviation.ManualResolver{
		Interact: interact,
		Table:    table,
	}
	req := domain.ConsultationRequest{
		OrchestrationArtifact: "Orchestration-abc/Orchestration.md",
		Context:               domain.ConsultContextRouting,
	}

	instr, err := m.ConsultRouting(context.Background(), req)

	if err != nil {
		t.Fatalf("want no error when user chooses stop, got %v", err)
	}
	if instr.Stop == nil {
		t.Fatal("want Stop instruction when user selects stop, got nil Stop")
	}
	if instr.Dispatch != nil {
		t.Error("want Dispatch=nil when user selects stop, got non-nil Dispatch")
	}
	if instr.Stop.Reason == "" {
		t.Error("want non-empty Stop.Reason (human-readable and non-empty per design), got empty string")
	}
}

// ===== Additional tests addressing review findings =====

// TestOrchestratorConsultant_DispatchRowIndexResolved verifies that
// ConsultRouting resolves the agent name to its routing table index and records
// it in DispatchInstruction.RowIndex. agent-a is row 0 and agent-b is row 1 in
// the two-row fixture.
func TestOrchestratorConsultant_DispatchRowIndexResolved(t *testing.T) {
	cases := []struct {
		agent    string
		wantRow  int
	}{
		{"agent-a", 0},
		{"agent-b", 1},
	}

	for _, tc := range cases {
		t.Run(tc.agent, func(t *testing.T) {
			table := mustParseTable(t)
			reply := []byte(fmt.Sprintf(`{"action":"dispatch","agent":%q,"task_description":"do the thing"}`, tc.agent))
			fake := &fakeRawInvoker{reply: reply}
			c := makeOrchestratorConsultant(fake, table)

			instr, err := c.ConsultRouting(context.Background(), validRoutingRequest("Orchestration-abc/Orchestration.md"))

			if err != nil {
				t.Fatalf("want no error for valid dispatch response, got %v", err)
			}
			if instr.Dispatch == nil {
				t.Fatal("want Dispatch instruction, got nil")
			}
			if instr.Dispatch.RowIndex != tc.wantRow {
				t.Errorf("want RowIndex=%d for agent %q, got %d", tc.wantRow, tc.agent, instr.Dispatch.RowIndex)
			}
		})
	}
}

// TestManualResolver_ConsultRoutingRowIndexSet verifies that when the user
// selects a routing table row by agent, the returned DispatchInstruction carries
// the correct RowIndex matching the row's position in the table. Row 1 (agent-b)
// is selected by choosing the second non-stop option.
func TestManualResolver_ConsultRoutingRowIndexSet(t *testing.T) {
	table := mustParseTable(t)
	// Select the second non-stop option (agent-b, row 1) by skipping the first.
	firstNonStopSeen := false
	interact := &scriptedInteraction{
		SelectOneResult: func(q interaction.ChoiceQuestion) (interaction.ChoiceAnswer, error) {
			for _, opt := range q.Options {
				if opt.ID == "stop" {
					continue
				}
				if !firstNonStopSeen {
					firstNonStopSeen = true
					continue
				}
				// Second non-stop option found.
				return interaction.ChoiceAnswer{Status: interaction.Answered, OptionID: opt.ID}, nil
			}
			// Fall back to any non-stop option if fewer than two agent rows.
			for _, opt := range q.Options {
				if opt.ID != "stop" {
					return interaction.ChoiceAnswer{Status: interaction.Answered, OptionID: opt.ID}, nil
				}
			}
			return interaction.ChoiceAnswer{Status: interaction.Answered}, nil
		},
		AskTextResult: func(_ interaction.TextQuestion) (interaction.TextAnswer, error) {
			return interaction.TextAnswer{Status: interaction.Answered, Text: "implement feature"}, nil
		},
	}

	m := &deviation.ManualResolver{
		Interact: interact,
		Table:    table,
	}
	req := domain.ConsultationRequest{
		OrchestrationArtifact: "Orchestration-abc/Orchestration.md",
		Context:               domain.ConsultContextRouting,
	}

	instr, err := m.ConsultRouting(context.Background(), req)

	if err != nil {
		t.Fatalf("want no error, got %v", err)
	}
	if instr.Dispatch == nil {
		t.Fatal("want Dispatch instruction, got nil")
	}
	// The two-row fixture has agent-a at row 0 and agent-b at row 1.
	// The selected agent must match its actual row index.
	wantRow := -1
	for _, row := range table.Rows {
		if row.Agent == instr.Dispatch.Agent {
			wantRow = row.Index
			break
		}
	}
	if wantRow == -1 {
		t.Fatalf("selected agent %q not found in routing table", instr.Dispatch.Agent)
	}
	if instr.Dispatch.RowIndex != wantRow {
		t.Errorf("want RowIndex=%d for agent %q, got %d", wantRow, instr.Dispatch.Agent, instr.Dispatch.RowIndex)
	}
}

// TestOrchestratorConsultant_MissingAgentField_EmptyString verifies that a
// dispatch response with agent present but set to "" produces a *ConsultationError
// with ConsultFailMissingField. A field that is present-but-empty must be treated
// the same as absent.
func TestOrchestratorConsultant_MissingAgentField_EmptyString(t *testing.T) {
	table := mustParseTable(t)
	reply := []byte(`{"action":"dispatch","agent":"","task_description":"do the thing"}`)
	fake := &fakeRawInvoker{reply: reply}
	c := makeOrchestratorConsultant(fake, table)

	_, err := c.ConsultRouting(context.Background(), validRoutingRequest("Orchestration-abc/Orchestration.md"))

	ce := assertConsultationError(t, err, domain.ConsultFailMissingField)
	if !strings.Contains(ce.Detail, "agent") {
		t.Errorf("want ConsultationError.Detail to contain %q (field name), got %q", "agent", ce.Detail)
	}
}

// TestOrchestratorConsultant_MissingTaskDescriptionField_EmptyString verifies
// that a dispatch response with task_description present but set to "" produces
// a *ConsultationError with ConsultFailMissingField. A field that is present-but-empty
// must be treated the same as absent.
func TestOrchestratorConsultant_MissingTaskDescriptionField_EmptyString(t *testing.T) {
	table := mustParseTable(t)
	reply := []byte(`{"action":"dispatch","agent":"agent-a","task_description":""}`)
	fake := &fakeRawInvoker{reply: reply}
	c := makeOrchestratorConsultant(fake, table)

	_, err := c.ConsultRouting(context.Background(), validRoutingRequest("Orchestration-abc/Orchestration.md"))

	ce := assertConsultationError(t, err, domain.ConsultFailMissingField)
	if !strings.Contains(ce.Detail, "task_description") {
		t.Errorf("want ConsultationError.Detail to contain %q (field name), got %q", "task_description", ce.Detail)
	}
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
	c := makeOrchestratorConsultant(fake, table)

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

// ===== T4.1: ExtractJSONObject — reply shapes =====

// TestExtractJSONObject_BareObject verifies that when the reply is exactly a
// well-formed JSON object (with optional surrounding whitespace), ExtractJSONObject
// returns the object's bytes and a nil error.
func TestExtractJSONObject_BareObject(t *testing.T) {
	reply := []byte(`{"action":"stop","reason":"orchestration complete"}`)

	got, err := deviation.ExtractJSONObject(reply)

	if err != nil {
		t.Fatalf("want no error for bare JSON object reply, got %v", err)
	}
	if len(got) == 0 {
		t.Fatal("want non-empty bytes for bare JSON object reply, got empty")
	}
	// The returned bytes must be valid JSON.
	var parsed map[string]interface{}
	if jsonErr := json.Unmarshal(got, &parsed); jsonErr != nil {
		t.Errorf("want returned bytes to be valid JSON, got unmarshal error: %v", jsonErr)
	}
}

// TestExtractJSONObject_BareObject_WithWhitespace verifies that surrounding
// whitespace (newlines, spaces) does not prevent extraction of a bare object.
func TestExtractJSONObject_BareObject_WithWhitespace(t *testing.T) {
	reply := []byte("\n  {\"action\":\"stop\",\"reason\":\"done\"}  \n")

	got, err := deviation.ExtractJSONObject(reply)

	if err != nil {
		t.Fatalf("want no error for bare JSON object with surrounding whitespace, got %v", err)
	}
	if len(got) == 0 {
		t.Fatal("want non-empty bytes, got empty")
	}
	var parsed map[string]interface{}
	if jsonErr := json.Unmarshal(got, &parsed); jsonErr != nil {
		t.Errorf("want returned bytes to be valid JSON, got %v", jsonErr)
	}
}

// TestExtractJSONObject_ProseSurrounded verifies that ExtractJSONObject locates
// and returns the JSON object when it appears inside prose, ignoring the
// surrounding text.
func TestExtractJSONObject_ProseSurrounded(t *testing.T) {
	const object = `{"action":"stop","reason":"orchestration complete"}`
	reply := []byte("Based on my analysis of the orchestration artifact:\n\n" + object + "\n\nThis concludes my recommendation.")

	got, err := deviation.ExtractJSONObject(reply)

	if err != nil {
		t.Fatalf("want no error for prose-surrounded object, got %v", err)
	}
	if len(got) == 0 {
		t.Fatal("want non-empty bytes for prose-surrounded object, got empty")
	}
	var parsed map[string]interface{}
	if jsonErr := json.Unmarshal(got, &parsed); jsonErr != nil {
		t.Errorf("want returned bytes to be valid JSON, got %v", jsonErr)
	}
}

// TestExtractJSONObject_FencedWithLanguageTag verifies that ExtractJSONObject
// extracts the JSON object from a fenced code block carrying a language tag
// (e.g. ```json).
func TestExtractJSONObject_FencedWithLanguageTag(t *testing.T) {
	const object = `{"action":"stop","reason":"orchestration complete"}`
	reply := []byte("Here is my routing decision:\n\n```json\n" + object + "\n```\n")

	got, err := deviation.ExtractJSONObject(reply)

	if err != nil {
		t.Fatalf("want no error for fenced-with-language-tag object, got %v", err)
	}
	if len(got) == 0 {
		t.Fatal("want non-empty bytes for fenced object, got empty")
	}
	var parsed map[string]interface{}
	if jsonErr := json.Unmarshal(got, &parsed); jsonErr != nil {
		t.Errorf("want returned bytes to be valid JSON, got %v", jsonErr)
	}
}

// TestExtractJSONObject_FencedWithoutLanguageTag verifies that ExtractJSONObject
// extracts the JSON object from a fenced code block that carries no language tag.
func TestExtractJSONObject_FencedWithoutLanguageTag(t *testing.T) {
	const object = `{"action":"stop","reason":"orchestration complete"}`
	reply := []byte("Here is my routing decision:\n\n```\n" + object + "\n```\n")

	got, err := deviation.ExtractJSONObject(reply)

	if err != nil {
		t.Fatalf("want no error for fenced-without-language-tag object, got %v", err)
	}
	if len(got) == 0 {
		t.Fatal("want non-empty bytes for fenced object, got empty")
	}
	var parsed map[string]interface{}
	if jsonErr := json.Unmarshal(got, &parsed); jsonErr != nil {
		t.Errorf("want returned bytes to be valid JSON, got %v", jsonErr)
	}
}

// TestExtractJSONObject_FencedBlockPrecedesProseObject verifies that when both
// a fenced code block and a different JSON object appear in the prose, the
// fenced block's object is returned. Per the design, a fenced block is searched
// before the surrounding text.
func TestExtractJSONObject_FencedBlockPrecedesProseObject(t *testing.T) {
	// The prose contains one JSON object; the fenced block contains a different one.
	// The fenced object must win because fenced blocks are searched first.
	proseObject := `{"action":"stop","reason":"from-prose"}`
	fencedObject := `{"action":"stop","reason":"from-fence"}`
	reply := []byte(proseObject + "\n\n```json\n" + fencedObject + "\n```\n")

	got, err := deviation.ExtractJSONObject(reply)

	if err != nil {
		t.Fatalf("want no error when both prose and fenced objects are present, got %v", err)
	}
	if len(got) == 0 {
		t.Fatal("want non-empty bytes, got empty")
	}
	var parsed map[string]interface{}
	if jsonErr := json.Unmarshal(got, &parsed); jsonErr != nil {
		t.Fatalf("want valid JSON, got %v", jsonErr)
	}
	// The fenced object must be preferred.
	if reason, _ := parsed["reason"].(string); reason != "from-fence" {
		t.Errorf("want fenced object (reason=%q) to be preferred over prose object (reason=%q), got reason=%q",
			"from-fence", "from-prose", reason)
	}
}

// TestExtractJSONObject_NoObject verifies that a reply containing no JSON object
// at all returns *ConsultationError with ConsultFailNoInstruction.
func TestExtractJSONObject_NoObject(t *testing.T) {
	reply := []byte("The orchestrator has reviewed the run state and will advise shortly. Please stand by.")

	_, err := deviation.ExtractJSONObject(reply)

	assertConsultationError(t, err, domain.ConsultFailNoInstruction)
}

// TestExtractJSONObject_EmptyReply verifies that an empty reply returns
// *ConsultationError with ConsultFailNoInstruction.
func TestExtractJSONObject_EmptyReply(t *testing.T) {
	_, err := deviation.ExtractJSONObject([]byte{})

	assertConsultationError(t, err, domain.ConsultFailNoInstruction)
}

// TestExtractJSONObject_BraceBearingProse verifies that prose containing brace
// characters that do not form a valid JSON object does not produce a false
// extraction. The extractor must use balanced-brace logic, not a naive
// first-brace scan.
func TestExtractJSONObject_BraceBearingProse(t *testing.T) {
	// These brace fragments are not valid JSON objects (unquoted keys, invalid syntax).
	reply := []byte("The routing table has entries {agent-a, agent-b}. Status: {BLOCKED, reason: timeout}. No valid instruction follows.")

	_, err := deviation.ExtractJSONObject(reply)

	assertConsultationError(t, err, domain.ConsultFailNoInstruction)
}

// TestExtractJSONObject_SchemaAgnostic verifies that ExtractJSONObject returns
// the bytes of a syntactically valid JSON object even when that object does not
// conform to any consultation schema. Extraction and schema validation are
// strictly separate steps — a schema mismatch is never reported by this function.
func TestExtractJSONObject_SchemaAgnostic(t *testing.T) {
	// A valid JSON object that matches no consultation wire schema.
	reply := []byte(`{"unknown_field": "some_value", "also_unknown": 42}`)

	got, err := deviation.ExtractJSONObject(reply)

	if err != nil {
		t.Fatalf("want no error for syntactically valid JSON object (even if schema-invalid), got %v — extraction and schema validation must be strictly separate steps", err)
	}
	if len(got) == 0 {
		t.Fatal("want non-empty bytes for syntactically valid JSON object, got empty")
	}
	var parsed interface{}
	if jsonErr := json.Unmarshal(got, &parsed); jsonErr != nil {
		t.Errorf("want returned bytes to be valid JSON, got unmarshal error: %v", jsonErr)
	}
}

// TestExtractJSONObject_NoObjectDetailDoesNotEmbedFullReply verifies that the
// ConsultationError's Detail names the condition without embedding the entire
// reply, per the design contract.
func TestExtractJSONObject_NoObjectDetailDoesNotEmbedFullReply(t *testing.T) {
	const distinctPhrase = "substantial-analysis-is-required-marker"
	reply := []byte("The orchestrator has reviewed the run state. " + distinctPhrase + " and needs more time before deciding.")

	_, err := deviation.ExtractJSONObject(reply)

	if err == nil {
		t.Fatal("want error for reply with no JSON object, got nil")
	}
	var ce *domain.ConsultationError
	if !errors.As(err, &ce) {
		t.Fatalf("want *ConsultationError, got %T: %v", err, err)
	}
	// Detail must name the condition without embedding the full reply.
	if strings.Contains(ce.Detail, distinctPhrase) {
		t.Errorf("want Detail to not embed the full reply, got Detail=%q", ce.Detail)
	}
}

// ===== T4.2: Routing consultation — reply shape acceptance and distinguishable failure classes =====

// TestConsultRouting_AcceptsVariousReplyShapes verifies that ConsultRouting
// accepts a dispatch reply in any of the supported formats — bare object,
// prose-surrounded, fenced with language tag, fenced without language tag —
// and yields the same RoutingInstruction from all of them.
func TestConsultRouting_AcceptsVariousReplyShapes(t *testing.T) {
	table := mustParseTable(t)

	const bareJSON = `{"action":"stop","reason":"orchestration complete"}`

	cases := []struct {
		name  string
		reply string
	}{
		{
			name:  "bare object",
			reply: bareJSON,
		},
		{
			name:  "object surrounded by prose",
			reply: "Based on my analysis of the current run state:\n\n" + bareJSON + "\n\nThis is my final recommendation.",
		},
		{
			name:  "object in fenced block with language tag",
			reply: "My routing decision:\n\n```json\n" + bareJSON + "\n```\n",
		},
		{
			name:  "object in fenced block without language tag",
			reply: "My routing decision:\n\n```\n" + bareJSON + "\n```\n",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fake := &fakeRawInvoker{reply: []byte(tc.reply)}
			c := makeOrchestratorConsultant(fake, table)

			instr, err := c.ConsultRouting(context.Background(), validRoutingRequest("Orchestration-abc/Orchestration.md"))

			if err != nil {
				t.Fatalf("want no error for %s, got %v — AC4.2 violated", tc.name, err)
			}
			if instr.Stop == nil {
				t.Fatalf("want Stop instruction for %s, got nil Stop", tc.name)
			}
			if instr.Stop.Reason != "orchestration complete" {
				t.Errorf("want Reason=%q for %s, got %q", "orchestration complete", tc.name, instr.Stop.Reason)
			}
		})
	}
}

// TestConsultRouting_DispatchReplyShapesYieldSameInstruction verifies that a
// dispatch reply is parsed correctly from all supported formats, yielding the
// same DispatchInstruction regardless of wrapping.
func TestConsultRouting_DispatchReplyShapesYieldSameInstruction(t *testing.T) {
	table := mustParseTable(t)

	const bareJSON = `{"action":"dispatch","agent":"agent-a","task_description":"implement the plan"}`

	cases := []struct {
		name  string
		reply string
	}{
		{
			name:  "bare dispatch object",
			reply: bareJSON,
		},
		{
			name:  "dispatch in prose",
			reply: "After reviewing the orchestration artifact, I recommend dispatching:\n\n" + bareJSON + "\n\nPlease proceed.",
		},
		{
			name:  "dispatch in fenced block",
			reply: "```json\n" + bareJSON + "\n```",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fake := &fakeRawInvoker{reply: []byte(tc.reply)}
			c := makeOrchestratorConsultant(fake, table)

			instr, err := c.ConsultRouting(context.Background(), validRoutingRequest("Orchestration-abc/Orchestration.md"))

			if err != nil {
				t.Fatalf("want no error for %s, got %v", tc.name, err)
			}
			if instr.Dispatch == nil {
				t.Fatalf("want Dispatch instruction for %s, got nil Dispatch", tc.name)
			}
			if instr.Dispatch.Agent != "agent-a" {
				t.Errorf("want Agent=%q for %s, got %q", "agent-a", tc.name, instr.Dispatch.Agent)
			}
			if instr.Dispatch.TaskDescription != "implement the plan" {
				t.Errorf("want TaskDescription=%q for %s, got %q", "implement the plan", tc.name, instr.Dispatch.TaskDescription)
			}
			// agent-a is row 0 in the two-row fixture.
			if instr.Dispatch.RowIndex != 0 {
				t.Errorf("want RowIndex=0 for agent-a for %s, got %d", tc.name, instr.Dispatch.RowIndex)
			}
		})
	}
}

// TestConsultRouting_NoObjectReply_FailsWithConsultFailNoInstruction verifies
// that when the orchestrator's reply contains no JSON object at all, ConsultRouting
// returns *ConsultationError with ConsultFailNoInstruction — not ConsultFailMalformedJSON.
// This is the "no instruction was found" class; AC4.3 requires it to be distinct
// from "instruction present but invalid".
func TestConsultRouting_NoObjectReply_FailsWithConsultFailNoInstruction(t *testing.T) {
	table := mustParseTable(t)
	// Pure prose, no JSON object anywhere in the reply.
	fake := &fakeRawInvoker{reply: []byte("The run is progressing well. I will advise you to continue to the next agent.")}
	c := makeOrchestratorConsultant(fake, table)

	_, err := c.ConsultRouting(context.Background(), validRoutingRequest("Orchestration-abc/Orchestration.md"))

	// Must be ConsultFailNoInstruction, not ConsultFailMalformedJSON — AC4.3.
	assertConsultationError(t, err, domain.ConsultFailNoInstruction)
}

// TestConsultRouting_FailureClassesAreDistinguishable verifies that each distinct
// failure condition produces a different ConsultationFailure value, so a caller
// switching on ConsultationError.Failure can always identify the condition without
// matching on message text.
func TestConsultRouting_FailureClassesAreDistinguishable(t *testing.T) {
	table := mustParseTable(t)

	cases := []struct {
		name        string
		reply       []byte
		wantFailure domain.ConsultationFailure
	}{
		{
			name:        "no object in reply",
			reply:       []byte("Pure prose, no JSON object anywhere."),
			wantFailure: domain.ConsultFailNoInstruction,
		},
		{
			name:        "object with unknown action",
			reply:       []byte(`{"action":"proceed"}`),
			wantFailure: domain.ConsultFailUnknownAction,
		},
		{
			name:        "dispatch with missing agent field",
			reply:       []byte(`{"action":"dispatch","task_description":"do the thing"}`),
			wantFailure: domain.ConsultFailMissingField,
		},
		{
			name:        "dispatch with unknown agent",
			reply:       []byte(`{"action":"dispatch","agent":"no-such-agent","task_description":"do the thing"}`),
			wantFailure: domain.ConsultFailUnknownAgent,
		},
	}

	// Collect the failure class each condition produces, then assert they are all distinct.
	seen := make(map[domain.ConsultationFailure]string)
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fake := &fakeRawInvoker{reply: tc.reply}
			c := makeOrchestratorConsultant(fake, table)

			_, err := c.ConsultRouting(context.Background(), validRoutingRequest("Orchestration-abc/Orchestration.md"))

			ce := assertConsultationError(t, err, tc.wantFailure)
			if existing, conflict := seen[ce.Failure]; conflict {
				t.Errorf("failure class %q is shared by conditions %q and %q — failure classes must be distinguishable", ce.Failure, existing, tc.name)
			}
			seen[ce.Failure] = tc.name
		})
	}
}

// TestConsultRouting_ObjectFoundButInvalidSchema_FailsWithMalformedJSON verifies
// that when an object IS found in the reply but does not unmarshal into a valid
// routing response (wrong schema), the failure is ConsultFailMalformedJSON — not
// ConsultFailNoInstruction. This is the "found but invalid" class, distinct from
// "not found", satisfying AC4.3.
//
// Note: "wrong schema" here means a JSON array (not an object) at the top level;
// a plain extra-field object is tolerated by the routing response (AC3.4 from
// Stage 3). A true schema mismatch that json.Unmarshal cannot survive is needed.
func TestConsultRouting_ObjectFoundButFailsUnmarshal_FailsWithMalformedJSON(t *testing.T) {
	table := mustParseTable(t)
	// Valid JSON but a JSON array, not an object: ExtractJSONObject will not
	// find a JSON object, so this actually tests the NoInstruction path.
	// To test "object found but schema-invalid", we need the per-context
	// unmarshal to fail after extraction. Since routing's wireRoutingResponse
	// is permissive (ignores unknown fields), the only true mismatch is an
	// object whose required top-level value is the wrong type — e.g. action
	// as a non-string. This verifies that a located-but-unmarshalable object
	// produces ConsultFailMalformedJSON, not ConsultFailNoInstruction.
	fake := &fakeRawInvoker{reply: []byte(`{"action": ["not","a","string"]}`) }
	c := makeOrchestratorConsultant(fake, table)

	_, err := c.ConsultRouting(context.Background(), validRoutingRequest("Orchestration-abc/Orchestration.md"))

	// The object was located (valid JSON object found) but the schema is violated
	// (action is an array, not a string). This must be ConsultFailMalformedJSON.
	assertConsultationError(t, err, domain.ConsultFailMalformedJSON)
}

// ===== T4.3: Pre-consultation — reply shape acceptance =====

// TestPreConsult_AcceptsVariousReplyShapes verifies that PreConsult extracts
// the JSON object using the same mechanism as ConsultRouting, accepting bare,
// prose-surrounded, and fenced reply formats.
func TestPreConsult_AcceptsVariousReplyShapes(t *testing.T) {
	table := mustParseTable(t)

	const bareJSON = `{"task_description":"run stage 1 first","constraints":"use python only"}`

	cases := []struct {
		name  string
		reply string
	}{
		{
			name:  "bare object",
			reply: bareJSON,
		},
		{
			name:  "object surrounded by prose",
			reply: "Before the run starts, please note:\n\n" + bareJSON + "\n\nGood luck with the execution.",
		},
		{
			name:  "object in fenced block with language tag",
			reply: "Pre-consultation advice:\n\n```json\n" + bareJSON + "\n```\n",
		},
		{
			name:  "object in fenced block without language tag",
			reply: "Pre-consultation advice:\n\n```\n" + bareJSON + "\n```\n",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fake := &fakeRawInvoker{reply: []byte(tc.reply)}
			c := makeOrchestratorConsultant(fake, table)

			req := domain.ConsultationRequest{
				OrchestrationArtifact: "Orchestration-abc/Orchestration.md",
				Context:               domain.ConsultContextPreConsultation,
			}
			advice, err := c.PreConsult(context.Background(), req)

			if err != nil {
				t.Fatalf("want no error for %s, got %v", tc.name, err)
			}
			if advice.TaskDescription != "run stage 1 first" {
				t.Errorf("want TaskDescription=%q for %s, got %q", "run stage 1 first", tc.name, advice.TaskDescription)
			}
			if advice.Constraints != "use python only" {
				t.Errorf("want Constraints=%q for %s, got %q", "use python only", tc.name, advice.Constraints)
			}
		})
	}
}

// TestPreConsult_NoObjectReply_FailsWithConsultFailNoInstruction verifies that
// when the orchestrator's reply contains no JSON object, PreConsult returns
// *ConsultationError with ConsultFailNoInstruction. The pre-consultation context
// uses the same extraction mechanism as routing (AC4.4).
func TestPreConsult_NoObjectReply_FailsWithConsultFailNoInstruction(t *testing.T) {
	table := mustParseTable(t)
	fake := &fakeRawInvoker{reply: []byte("The run environment is healthy. No specific advice at this time.")}
	c := makeOrchestratorConsultant(fake, table)

	req := domain.ConsultationRequest{
		OrchestrationArtifact: "Orchestration-abc/Orchestration.md",
		Context:               domain.ConsultContextPreConsultation,
	}
	_, err := c.PreConsult(context.Background(), req)

	assertConsultationError(t, err, domain.ConsultFailNoInstruction)
}

// TestPreConsult_ValidatesOwnSchema verifies that pre-consultation still applies
// its own schema validation after extraction: an object that is extracted
// successfully but does not match the pre-consultation wire schema (e.g. because
// a field has the wrong type) produces ConsultFailMalformedJSON — not
// ConsultFailNoInstruction. This confirms that extraction and schema validation
// are separate steps (AC4.4), and that schema validation still occurs.
func TestPreConsult_ValidatesOwnSchema(t *testing.T) {
	table := mustParseTable(t)
	// task_description must be a string; passing an integer causes schema failure.
	fake := &fakeRawInvoker{reply: []byte(`{"task_description": 42}`)}
	c := makeOrchestratorConsultant(fake, table)

	req := domain.ConsultationRequest{
		OrchestrationArtifact: "Orchestration-abc/Orchestration.md",
		Context:               domain.ConsultContextPreConsultation,
	}
	_, err := c.PreConsult(context.Background(), req)

	// The object was found but its field type is wrong for the pre-consultation schema.
	assertConsultationError(t, err, domain.ConsultFailMalformedJSON)
}

// ---- small utilities ----

// Ensure json is imported (used in wireRequest struct tags and assertions above).
var _ = json.Marshal
