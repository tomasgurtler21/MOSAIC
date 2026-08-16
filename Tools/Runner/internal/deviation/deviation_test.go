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
| PLANNING | agent-a | ❌ | agent-b | - | - | plan.md |
| PLANNING | agent-b | ❌ | COMPLETE | - | plan.md | result.md |
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
	return domain.AgentReference{
		Identifier:     "orchestrator-script",
		DefinitionPath: "/agents/orchestrator-script.md",
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

// TestOrchestratorConsultant_MalformedJSONResponse verifies that a malformed
// (non-parseable) JSON reply produces a *ConsultationError with
// ConsultFailMalformedJSON, naming the condition.
func TestOrchestratorConsultant_MalformedJSONResponse(t *testing.T) {
	table := mustParseTable(t)
	fake := &fakeRawInvoker{reply: []byte(`{not valid json at all`)}
	c := makeOrchestratorConsultant(fake, table)

	_, err := c.ConsultRouting(context.Background(), validRoutingRequest("Orchestration-abc/Orchestration.md"))

	assertConsultationError(t, err, domain.ConsultFailMalformedJSON)
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

// TestOrchestratorConsultant_PreConsultMalformedResponse verifies that a
// malformed response (not valid JSON) produces a *ConsultationError.
func TestOrchestratorConsultant_PreConsultMalformedResponse(t *testing.T) {
	table := mustParseTable(t)
	fake := &fakeRawInvoker{reply: []byte(`not json at all`)}
	c := makeOrchestratorConsultant(fake, table)

	req := domain.ConsultationRequest{
		OrchestrationArtifact: "Orchestration-abc/Orchestration.md",
		Context:               domain.ConsultContextPreConsultation,
	}
	_, err := c.PreConsult(context.Background(), req)

	assertConsultationError(t, err, domain.ConsultFailMalformedJSON)
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

// ---- small utilities ----

// Ensure json is imported (used in wireRequest struct tags and assertions above).
var _ = json.Marshal
