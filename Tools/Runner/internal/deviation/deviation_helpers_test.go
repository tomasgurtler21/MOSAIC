package deviation_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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

func newTestOrchestratorConsultant(invoker domain.RawInvoker, table domain.RoutingTable) *deviation.OrchestratorConsultant {
	return &deviation.OrchestratorConsultant{
		Invoker:      invoker,
		Orchestrator: orchestratorRef(),
		Table:        table,
	}
}

func strptr(s string) *string { return &s }

// routingStopReply returns a JSON routing response with the given reason.
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
