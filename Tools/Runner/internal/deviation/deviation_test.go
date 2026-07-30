package deviation_test

// Tests for the deviation package.
//
// Coverage:
//
//   OrchestratorDelegate — request assembly:
//   - Assembles a protocol request with the correct task_description format
//     ("Resolve deviation: {agent} returned {status} at row {index} in
//     phase {phase}"), input_artifacts=["Orchestration.md"],
//     output_artifacts=["Orchestration.md"], include_result_summary=true,
//     human_in_the_loop=false.
//
//   OrchestratorDelegate — action parsing:
//   - Parses "rejoin" action from orchestrator result_data → RejoinAtRow.
//   - Parses "stop" action from orchestrator result_data → StopRun.
//   - Parses "custom" action with well-formed custom_request → Custom field.
//   - "custom" without custom_request field → error.
//
//   OrchestratorDelegate — HITL override (never originates, only carries):
//   - Carries explicit hitl_override=false from orchestrator instruction →
//     HITLOverride=&false in RejoinAtRow (E503 re-invocation path).
//   - Carries explicit hitl_override=true from orchestrator instruction →
//     HITLOverride=&true in RejoinAtRow (distinct JSON nullable-bool path).
//   - Does NOT set HITLOverride when orchestrator instruction omits hitl_override.
//
//   OrchestratorDelegate — error cases:
//   - Orchestrator returns non-SUCCESS status → error.
//   - Orchestrator result_data missing → error.
//   - Orchestrator result_data is malformed JSON → error.
//   - rejoin_agent names an identifier not in the routing table → error.
//
//   ManualResolver:
//   - Calls Interaction.SelectOne to offer the user a row to rejoin at.
//   - Returns RejoinAtRow with the user-selected row index.
//   - Carries HITLOverride when the user explicitly confirms a HITL change.
//   - Does NOT set HITLOverride when the user does not choose to change HITL.
//   - Stop option ID is "stop" (convention enforced by the test); when user
//     chooses it, returns StopRun.

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"mosaic-common/interaction"
	"mosaic-run/internal/deviation"
	"mosaic-run/internal/domain"
	"mosaic-run/internal/harness"
	"mosaic-run/internal/workflow"
)

// ---- test workflow fixture ----

// simpleLinearContent is a minimal two-row linear workflow used to build
// routing table fixtures. Row 0: agent-a, Row 1: agent-b.
const simpleLinearContent = `<!-- workflow-version: 1.0 -->
## Simple Linear

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

// ---- scripted Interaction ----

// scriptedInteraction is a test double for the Interaction port.
// SelectOne always returns the first option unless overridden via SelectOneResult.
type scriptedInteraction struct {
	SelectOneResult  func(q interaction.ChoiceQuestion) (interaction.ChoiceAnswer, error)
	ConfirmResult    func(q interaction.Question) (interaction.ConfirmAnswer, error)
	SelectManyResult func(q interaction.ChoiceQuestion) (interaction.MultiChoiceAnswer, error)
	AskTextResult    func(q interaction.TextQuestion) (interaction.TextAnswer, error)

	SelectOneCalls []interaction.ChoiceQuestion
	ConfirmCalls   []interaction.Question
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
	if s.AskTextResult != nil {
		return s.AskTextResult(q)
	}
	return interaction.TextAnswer{Status: interaction.Answered, Text: ""}, nil
}

func (s *scriptedInteraction) Confirm(_ context.Context, q interaction.Question) (interaction.ConfirmAnswer, error) {
	s.ConfirmCalls = append(s.ConfirmCalls, q)
	if s.ConfirmResult != nil {
		return s.ConfirmResult(q)
	}
	return interaction.ConfirmAnswer{Status: interaction.Answered, Confirm: false}, nil
}

func (s *scriptedInteraction) Notify(_ context.Context, _ interaction.Notice)             {}
func (s *scriptedInteraction) Progress(_ context.Context, _ interaction.ProgressEvent)    {}

// ---- deviation fixtures ----

func makeDeviationInfo(table domain.RoutingTable) domain.DeviationInfo {
	return domain.DeviationInfo{
		Kind: domain.DeviationNonSuccess,
		Response: domain.ProtocolResponse{
			AgentInstanceID: "agent-a#1",
			StatusCode:      domain.StatusBLOCKED,
			StatusMessage:   "blocked on missing input",
			ErrorCode:       domain.ErrorINPUT_NOT_FOUND,
		},
		CurrentRow:   0,
		CurrentPhase: "PLANNING",
		CurrentStage: "",
		PlannedContinuation: &domain.DispatchStep{
			RowIndex: 1,
			Agent:    domain.AgentReference{Identifier: "agent-b", InvocationKind: domain.InvocationOrdinary},
		},
		ArtifactState: domain.ArtifactState{},
	}
}

func orchestratorRef() domain.AgentReference {
	return domain.AgentReference{
		Identifier:     "orchestrator-script",
		DefinitionPath: "/agents/orchestrator-script.md",
		InvocationKind: domain.InvocationOrdinary,
	}
}

func makeOrchestrator(t *testing.T, f *harness.FakeAdapter, table domain.RoutingTable) *deviation.OrchestratorDelegate {
	t.Helper()
	seq := 0
	return &deviation.OrchestratorDelegate{
		Harness:      f,
		Orchestrator: orchestratorRef(),
		Table:        table,
		ArtifactPath: "Orchestration.md",
		NextSeq: func() int {
			seq++
			return seq
		},
	}
}

// orchestratorSuccess queues a successful orchestrator response with the given
// result_data JSON.
func orchestratorSuccess(f *harness.FakeAdapter, resultData string) {
	f.Queue("orchestrator-script", harness.ScriptedEntry{
		Response: &domain.ProtocolResponse{
			AgentInstanceID: "orchestrator-script#1",
			StatusCode:      domain.StatusSUCCESS,
			StatusMessage:   "deviation resolved",
			ResultData:      resultData,
		},
	})
}

// ===== OrchestratorDelegate: protocol request assembly =====

// TestOrchestratorDelegate_AssemblesCorrectRequest verifies that the delegate
// invokes the orchestrator agent with a protocol request that names the
// deviating agent, status, row, and phase in the task_description, and sets
// input_artifacts and output_artifacts to the artifact path with
// include_result_summary=true and human_in_the_loop=false.
func TestOrchestratorDelegate_AssemblesCorrectRequest(t *testing.T) {
	table := mustParseTable(t)
	f := harness.NewFakeAdapter()
	orchestratorSuccess(f, `{"action":"stop","reason":"test"}`)

	d := makeOrchestrator(t, f, table)
	info := makeDeviationInfo(table)

	d.Resolve(context.Background(), info) //nolint:errcheck

	invs := f.Invocations()
	if len(invs) == 0 {
		t.Fatal("want orchestrator invoked, got zero invocations")
	}
	req := invs[0].Request

	wantDesc := fmt.Sprintf("Resolve deviation: %s returned %s at row %d in phase %s",
		info.Response.AgentInstanceID, info.Response.StatusCode, info.CurrentRow, info.CurrentPhase)
	if req.TaskDescription != wantDesc {
		t.Errorf("want TaskDescription=%q\ngot %q", wantDesc, req.TaskDescription)
	}
	if len(req.InputArtifacts) != 1 || req.InputArtifacts[0] != "Orchestration.md" {
		t.Errorf("want InputArtifacts=[Orchestration.md], got %v", req.InputArtifacts)
	}
	if len(req.OutputArtifacts) != 1 || req.OutputArtifacts[0] != "Orchestration.md" {
		t.Errorf("want OutputArtifacts=[Orchestration.md], got %v", req.OutputArtifacts)
	}
	if !req.IncludeResultSummary {
		t.Error("want IncludeResultSummary=true")
	}
	if req.HumanInTheLoop {
		t.Error("want HumanInTheLoop=false for orchestrator invocation")
	}
}

// ===== OrchestratorDelegate: rejoin action parsing =====

// TestOrchestratorDelegate_ParsesRejoinAction verifies that a "rejoin"
// instruction in the orchestrator result_data returns a RejoinAtRow with the
// row index that corresponds to the named agent.
func TestOrchestratorDelegate_ParsesRejoinAction(t *testing.T) {
	table := mustParseTable(t)
	f := harness.NewFakeAdapter()
	// agent-b is row 1 in the simple linear table.
	orchestratorSuccess(f, `{"action":"rejoin","rejoin_agent":"agent-b"}`)

	d := makeOrchestrator(t, f, table)
	info := makeDeviationInfo(table)

	instr, err := d.Resolve(context.Background(), info)

	if err != nil {
		t.Fatalf("want no error, got %v", err)
	}
	if instr.Rejoin == nil {
		t.Fatal("want RejoinAtRow instruction, got nil")
	}
	// agent-b is row index 1.
	if instr.Rejoin.RowIndex != 1 {
		t.Errorf("want RowIndex=1 (agent-b), got %d", instr.Rejoin.RowIndex)
	}
}

// TestOrchestratorDelegate_ParsesStopAction verifies that a "stop"
// instruction in the orchestrator result_data returns a StopRun with the
// reason from the instruction.
func TestOrchestratorDelegate_ParsesStopAction(t *testing.T) {
	table := mustParseTable(t)
	f := harness.NewFakeAdapter()
	orchestratorSuccess(f, `{"action":"stop","reason":"cannot recover"}`)

	d := makeOrchestrator(t, f, table)
	info := makeDeviationInfo(table)

	instr, err := d.Resolve(context.Background(), info)

	if err != nil {
		t.Fatalf("want no error, got %v", err)
	}
	if instr.Stop == nil {
		t.Fatal("want StopRun instruction, got nil")
	}
	if instr.Stop.Reason != "cannot recover" {
		t.Errorf("want Reason=%q, got %q", "cannot recover", instr.Stop.Reason)
	}
}

// ===== OrchestratorDelegate: HITL override (AC8.2) =====

// TestOrchestratorDelegate_CarriesExplicitHITLOverrideFalse verifies that
// when the orchestrator instruction includes hitl_override=false (the E503
// resolution path: re-invoke with HITL lowered), the delegate carries it
// through to the RejoinInstruction.HITLOverride = &false.
// This is the ONLY way the resolver can produce a HITL-lowering instruction:
// it carries an explicit instruction, it does not originate one.
func TestOrchestratorDelegate_CarriesExplicitHITLOverrideFalse(t *testing.T) {
	table := mustParseTable(t)
	f := harness.NewFakeAdapter()
	// hitl_override=false: orchestrator explicitly instructs HITL lowering.
	orchestratorSuccess(f, `{"action":"rejoin","rejoin_agent":"agent-b","hitl_override":false}`)

	d := makeOrchestrator(t, f, table)
	info := makeDeviationInfo(table)

	instr, err := d.Resolve(context.Background(), info)

	if err != nil {
		t.Fatalf("want no error, got %v", err)
	}
	if instr.Rejoin == nil {
		t.Fatal("want RejoinAtRow, got nil")
	}
	if instr.Rejoin.HITLOverride == nil {
		t.Fatal("want HITLOverride set, got nil (explicit instruction not carried through)")
	}
	if *instr.Rejoin.HITLOverride != false {
		t.Errorf("want HITLOverride=false, got %v", *instr.Rejoin.HITLOverride)
	}
}

// TestOrchestratorDelegate_CarriesExplicitHITLOverrideTrue verifies that when
// the orchestrator instruction includes hitl_override=true (raising HITL for
// the re-invocation), the delegate carries it through to
// RejoinInstruction.HITLOverride = &true.
// This exercises the nullable-boolean JSON parsing path for the true case,
// which is a distinct code path from the false case.
func TestOrchestratorDelegate_CarriesExplicitHITLOverrideTrue(t *testing.T) {
	table := mustParseTable(t)
	f := harness.NewFakeAdapter()
	// hitl_override=true: orchestrator explicitly instructs HITL raising.
	orchestratorSuccess(f, `{"action":"rejoin","rejoin_agent":"agent-b","hitl_override":true}`)

	d := makeOrchestrator(t, f, table)
	info := makeDeviationInfo(table)

	instr, err := d.Resolve(context.Background(), info)

	if err != nil {
		t.Fatalf("want no error, got %v", err)
	}
	if instr.Rejoin == nil {
		t.Fatal("want RejoinAtRow, got nil")
	}
	if instr.Rejoin.HITLOverride == nil {
		t.Fatal("want HITLOverride set, got nil (explicit hitl_override=true not carried through)")
	}
	if *instr.Rejoin.HITLOverride != true {
		t.Errorf("want HITLOverride=true, got %v", *instr.Rejoin.HITLOverride)
	}
}

// TestOrchestratorDelegate_DoesNotOriginateHITLLowering verifies that when
// the orchestrator instruction omits hitl_override (null / absent), the
// delegate does NOT set HITLOverride in the RejoinInstruction.
// This is the AC8.2 invariant: the resolver never originates HITL lowering;
// it only carries an explicit instruction.
func TestOrchestratorDelegate_DoesNotOriginateHITLLowering(t *testing.T) {
	table := mustParseTable(t)
	f := harness.NewFakeAdapter()
	// hitl_override absent: orchestrator does not instruct any HITL change.
	orchestratorSuccess(f, `{"action":"rejoin","rejoin_agent":"agent-b"}`)

	d := makeOrchestrator(t, f, table)
	info := makeDeviationInfo(table)

	instr, err := d.Resolve(context.Background(), info)

	if err != nil {
		t.Fatalf("want no error, got %v", err)
	}
	if instr.Rejoin == nil {
		t.Fatal("want RejoinAtRow, got nil")
	}
	if instr.Rejoin.HITLOverride != nil {
		t.Errorf("want HITLOverride=nil when not in instruction, got %v", *instr.Rejoin.HITLOverride)
	}
}

// ===== OrchestratorDelegate: error cases =====

// TestOrchestratorDelegate_NonSuccessResponse_ReturnsError verifies that when
// the orchestrator returns a non-SUCCESS status code, the delegate returns an
// error (the deviation is unresolvable; the run should stop cleanly).
func TestOrchestratorDelegate_NonSuccessResponse_ReturnsError(t *testing.T) {
	table := mustParseTable(t)
	f := harness.NewFakeAdapter()
	f.Queue("orchestrator-script", harness.ScriptedEntry{
		Response: &domain.ProtocolResponse{
			AgentInstanceID: "orchestrator-script#1",
			StatusCode:      domain.StatusBLOCKED,
			StatusMessage:   "orchestrator also blocked",
		},
	})

	d := makeOrchestrator(t, f, table)
	info := makeDeviationInfo(table)

	_, err := d.Resolve(context.Background(), info)

	if err == nil {
		t.Fatal("want error for non-SUCCESS orchestrator response, got nil")
	}
	// Verify the orchestrator was actually invoked; if it wasn't, the error
	// is from the stub ("not implemented") rather than from parsing the response.
	invs := f.Invocations()
	if len(invs) == 0 {
		t.Error("want orchestrator invoked before error, but zero invocations recorded")
	}
}

// TestOrchestratorDelegate_MissingResultData_ReturnsError verifies that when
// the orchestrator returns SUCCESS but result_data is empty or missing, the
// delegate returns an error (cannot parse a RejoinInstruction).
func TestOrchestratorDelegate_MissingResultData_ReturnsError(t *testing.T) {
	table := mustParseTable(t)
	f := harness.NewFakeAdapter()
	f.Queue("orchestrator-script", harness.ScriptedEntry{
		Response: &domain.ProtocolResponse{
			AgentInstanceID: "orchestrator-script#1",
			StatusCode:      domain.StatusSUCCESS,
			StatusMessage:   "done",
			ResultData:      "", // empty
		},
	})

	d := makeOrchestrator(t, f, table)
	info := makeDeviationInfo(table)

	_, err := d.Resolve(context.Background(), info)

	if err == nil {
		t.Fatal("want error when result_data is empty, got nil")
	}
	// Verify the orchestrator was actually invoked; if it wasn't, the error
	// is from the stub ("not implemented") rather than from parsing the response.
	invs := f.Invocations()
	if len(invs) == 0 {
		t.Error("want orchestrator invoked before error, but zero invocations recorded")
	}
}

// TestOrchestratorDelegate_MalformedResultData_ReturnsError verifies that
// malformed JSON in result_data returns an error rather than panicking.
func TestOrchestratorDelegate_MalformedResultData_ReturnsError(t *testing.T) {
	table := mustParseTable(t)
	f := harness.NewFakeAdapter()
	orchestratorSuccess(f, `{not valid json}`)

	d := makeOrchestrator(t, f, table)
	info := makeDeviationInfo(table)

	_, err := d.Resolve(context.Background(), info)

	if err == nil {
		t.Fatal("want error for malformed result_data JSON, got nil")
	}
	// Verify the orchestrator was actually invoked; if it wasn't, the error
	// is from the stub ("not implemented") rather than from the JSON parse failure.
	invs := f.Invocations()
	if len(invs) == 0 {
		t.Error("want orchestrator invoked before error, but zero invocations recorded")
	}
}

// ===== OrchestratorDelegate: action="custom" =====

// TestOrchestratorDelegate_CustomDispatch_PopulatesCustomField verifies that
// when the orchestrator instruction has action="custom" with a well-formed
// custom_request and rejoin_after_custom, the delegate returns a
// RejoinInstruction with the Custom field populated correctly.
//
// The custom_request is embedded as-is in the instruction. rejoin_after_custom
// names the agent to rejoin after the custom invocation completes; the delegate
// must resolve this name to a row index in the routing table.
func TestOrchestratorDelegate_CustomDispatch_PopulatesCustomField(t *testing.T) {
	table := mustParseTable(t)
	f := harness.NewFakeAdapter()
	// action="custom" with a minimal custom request; rejoin at agent-b (row 1).
	orchestratorSuccess(f, `{
		"action": "custom",
		"custom_request": {
			"agent_instance_id": "infra#99",
			"task_description": "run infrastructure check",
			"input_artifacts": ["Orchestration.md"],
			"output_artifacts": [],
			"include_result_summary": false,
			"human_in_the_loop": false
		},
		"rejoin_after_custom": "agent-b"
	}`)

	d := makeOrchestrator(t, f, table)
	info := makeDeviationInfo(table)

	instr, err := d.Resolve(context.Background(), info)

	if err != nil {
		t.Fatalf("want no error for well-formed custom dispatch, got %v", err)
	}
	if instr.Custom == nil {
		t.Fatal("want RejoinInstruction.Custom populated, got nil (action=custom not handled)")
	}
	// The custom request's task_description must be carried through unchanged.
	if instr.Custom.Request.TaskDescription != "run infrastructure check" {
		t.Errorf("want Custom.Request.TaskDescription=%q, got %q",
			"run infrastructure check", instr.Custom.Request.TaskDescription)
	}
	// rejoin_after_custom="agent-b" resolves to row index 1 in the simple linear table.
	if instr.Custom.RejoinRow != 1 {
		t.Errorf("want Custom.RejoinRow=1 (agent-b), got %d", instr.Custom.RejoinRow)
	}
}

// TestOrchestratorDelegate_CustomDispatch_MissingCustomRequest_ReturnsError
// verifies that action="custom" without a custom_request field returns an
// error rather than producing a zero-valued Custom dispatch instruction.
// The custom_request is the payload of the custom invocation; without it
// the instruction is malformed and the deviation is unresolvable.
func TestOrchestratorDelegate_CustomDispatch_MissingCustomRequest_ReturnsError(t *testing.T) {
	table := mustParseTable(t)
	f := harness.NewFakeAdapter()
	// action="custom" but custom_request is absent (null / missing field).
	orchestratorSuccess(f, `{"action":"custom","rejoin_after_custom":"agent-b"}`)

	d := makeOrchestrator(t, f, table)
	info := makeDeviationInfo(table)

	_, err := d.Resolve(context.Background(), info)

	if err == nil {
		t.Fatal("want error for action=custom with missing custom_request, got nil")
	}
	// Verify the orchestrator was actually invoked; if it wasn't, the error
	// is from the stub ("not implemented") rather than from the missing field.
	invs := f.Invocations()
	if len(invs) == 0 {
		t.Error("want orchestrator invoked before error, but zero invocations recorded")
	}
}

// ===== OrchestratorDelegate: rejoin_agent not found =====

// TestOrchestratorDelegate_RejoinAgentNotFound_ReturnsError verifies that
// when the orchestrator instruction names a rejoin_agent that is not present
// in the routing table, the delegate returns an error rather than silently
// producing a zero row index or undefined row index.
//
// An unrecognized agent identifier indicates the orchestrator produced an
// instruction that does not match the current workflow. The delegate must
// detect this and surface it as an unresolvable deviation.
func TestOrchestratorDelegate_RejoinAgentNotFound_ReturnsError(t *testing.T) {
	table := mustParseTable(t)
	f := harness.NewFakeAdapter()
	// rejoin_agent names an agent that does not appear in the routing table.
	orchestratorSuccess(f, `{"action":"rejoin","rejoin_agent":"nonexistent-agent"}`)

	d := makeOrchestrator(t, f, table)
	info := makeDeviationInfo(table)

	_, err := d.Resolve(context.Background(), info)

	if err == nil {
		t.Fatal("want error when rejoin_agent is not found in routing table, got nil")
	}
	// Verify the orchestrator was actually invoked; if it wasn't, the error
	// is from the stub ("not implemented") rather than from the lookup failure.
	invs := f.Invocations()
	if len(invs) == 0 {
		t.Error("want orchestrator invoked before error, but zero invocations recorded")
	}
}

// ===== ManualResolver =====

// TestManualResolver_CallsSelectOne verifies that ManualResolver.Resolve
// consults the Interaction port (at minimum, SelectOne) to obtain a row
// selection from the user.
func TestManualResolver_CallsSelectOne(t *testing.T) {
	table := mustParseTable(t)
	interact := &scriptedInteraction{
		SelectOneResult: func(q interaction.ChoiceQuestion) (interaction.ChoiceAnswer, error) {
			// Select the first option (agent-a, row 0) and no stop.
			if len(q.Options) > 0 {
				return interaction.ChoiceAnswer{Status: interaction.Answered, OptionID: q.Options[0].ID}, nil
			}
			return interaction.ChoiceAnswer{Status: interaction.Answered}, nil
		},
	}

	r := &deviation.ManualResolver{
		Interact: interact,
		Table:    table,
	}
	info := makeDeviationInfo(table)

	r.Resolve(context.Background(), info) //nolint:errcheck

	if len(interact.SelectOneCalls) == 0 {
		t.Error("want Interaction.SelectOne called at least once, got zero calls")
	}
}

// TestManualResolver_ReturnsRejoinAtRow verifies that when the user selects a
// row to rejoin at, ManualResolver returns a RejoinInstruction with RejoinAtRow
// pointing to the selected row index.
func TestManualResolver_ReturnsRejoinAtRow(t *testing.T) {
	table := mustParseTable(t)
	// User selects agent-b (row 1) -- we fake this by returning its ID.
	interact := &scriptedInteraction{
		SelectOneResult: func(q interaction.ChoiceQuestion) (interaction.ChoiceAnswer, error) {
			// Look for the option whose ID corresponds to agent-b (row 1).
			for _, opt := range q.Options {
				if opt.ID == "1" {
					return interaction.ChoiceAnswer{Status: interaction.Answered, OptionID: opt.ID}, nil
				}
			}
			return interaction.ChoiceAnswer{Status: interaction.Answered, OptionID: "0"}, nil
		},
		ConfirmResult: func(_ interaction.Question) (interaction.ConfirmAnswer, error) {
			// No HITL override.
			return interaction.ConfirmAnswer{Status: interaction.Answered, Confirm: false}, nil
		},
	}

	r := &deviation.ManualResolver{
		Interact: interact,
		Table:    table,
	}
	info := makeDeviationInfo(table)

	instr, err := r.Resolve(context.Background(), info)

	if err != nil {
		t.Fatalf("want no error, got %v", err)
	}
	if instr.Rejoin == nil {
		t.Fatal("want RejoinAtRow instruction, got nil")
	}
	if instr.Rejoin.RowIndex != 1 {
		t.Errorf("want RowIndex=1 (agent-b), got %d", instr.Rejoin.RowIndex)
	}
}

// TestManualResolver_CarriesHITLOverrideWhenUserRequests verifies that when
// the user explicitly confirms they want to override HITL, the ManualResolver
// carries the override in the RejoinInstruction (and does not originate it
// silently).
func TestManualResolver_CarriesHITLOverrideWhenUserRequests(t *testing.T) {
	table := mustParseTable(t)
	hitlOverrideValue := false // user wants HITL lowered
	interact := &scriptedInteraction{
		SelectOneResult: func(q interaction.ChoiceQuestion) (interaction.ChoiceAnswer, error) {
			return interaction.ChoiceAnswer{Status: interaction.Answered, OptionID: "0"}, nil
		},
		ConfirmResult: func(_ interaction.Question) (interaction.ConfirmAnswer, error) {
			// User explicitly confirms they want to change HITL.
			return interaction.ConfirmAnswer{Status: interaction.Answered, Confirm: true}, nil
		},
		AskTextResult: func(_ interaction.TextQuestion) (interaction.TextAnswer, error) {
			// When asked the new HITL value, user says "false" (lower it).
			_ = hitlOverrideValue
			return interaction.TextAnswer{Status: interaction.Answered, Text: "false"}, nil
		},
	}

	r := &deviation.ManualResolver{
		Interact: interact,
		Table:    table,
	}
	info := makeDeviationInfo(table)

	instr, err := r.Resolve(context.Background(), info)

	if err != nil {
		t.Fatalf("want no error, got %v", err)
	}
	if instr.Rejoin == nil {
		t.Fatal("want RejoinAtRow, got nil")
	}
	if instr.Rejoin.HITLOverride == nil {
		t.Fatal("want HITLOverride set when user explicitly requested it, got nil")
	}
	if *instr.Rejoin.HITLOverride != hitlOverrideValue {
		t.Errorf("want HITLOverride=%v, got %v", hitlOverrideValue, *instr.Rejoin.HITLOverride)
	}
}

// TestManualResolver_DoesNotOriginateHITLLowering verifies that when the user
// does not explicitly request a HITL change, ManualResolver sets HITLOverride=nil.
// This is the AC8.2 invariant: HITL lowering is never originated by the resolver.
func TestManualResolver_DoesNotOriginateHITLLowering(t *testing.T) {
	table := mustParseTable(t)
	interact := &scriptedInteraction{
		SelectOneResult: func(q interaction.ChoiceQuestion) (interaction.ChoiceAnswer, error) {
			return interaction.ChoiceAnswer{Status: interaction.Answered, OptionID: "0"}, nil
		},
		ConfirmResult: func(_ interaction.Question) (interaction.ConfirmAnswer, error) {
			// User does NOT confirm HITL change.
			return interaction.ConfirmAnswer{Status: interaction.Answered, Confirm: false}, nil
		},
	}

	r := &deviation.ManualResolver{
		Interact: interact,
		Table:    table,
	}
	info := makeDeviationInfo(table)

	instr, err := r.Resolve(context.Background(), info)

	if err != nil {
		t.Fatalf("want no error, got %v", err)
	}
	if instr.Rejoin == nil {
		// May return Stop if user chose stop option; that's fine for this test.
		if instr.Stop != nil {
			return
		}
		t.Fatal("want RejoinAtRow or StopRun, got nil instruction")
	}
	if instr.Rejoin.HITLOverride != nil {
		t.Errorf("want HITLOverride=nil when user did not request change, got %v", *instr.Rejoin.HITLOverride)
	}
}

// TestManualResolver_UserChoosesStop_ReturnsStopRun verifies that when the
// user selects the stop option, ManualResolver returns a StopRun instruction.
//
// Convention: the ManualResolver must include a stop option with ID "stop"
// in the SelectOne question. The test enforces this convention by failing
// with a clear message if no option with ID "stop" is found, rather than
// falling back to a different option (which would produce a false positive).
func TestManualResolver_UserChoosesStop_ReturnsStopRun(t *testing.T) {
	table := mustParseTable(t)
	interact := &scriptedInteraction{
		SelectOneResult: func(q interaction.ChoiceQuestion) (interaction.ChoiceAnswer, error) {
			// Look for the stop option; its ID must be "stop" by convention.
			// No fallback: if the convention is broken the test fails clearly,
			// not silently via an unintended option selection.
			for _, opt := range q.Options {
				if opt.ID == "stop" {
					return interaction.ChoiceAnswer{Status: interaction.Answered, OptionID: opt.ID}, nil
				}
			}
			t.Errorf("want a stop option with ID='stop' in SelectOne question options, got %v (implementation must include a stop option with this ID)", q.Options)
			return interaction.ChoiceAnswer{Status: interaction.Answered}, nil
		},
	}

	r := &deviation.ManualResolver{
		Interact: interact,
		Table:    table,
	}
	info := makeDeviationInfo(table)

	instr, err := r.Resolve(context.Background(), info)

	if err != nil {
		t.Fatalf("want no error, got %v", err)
	}
	if instr.Stop == nil {
		t.Fatal("want StopRun when user chooses stop, got nil Stop")
	}
}

// ---- small utilities ----

// Ensure json is imported (used in orchestratorInstruction struct tags
// referenced by the test's conceptual coverage).
var _ = json.Marshal
