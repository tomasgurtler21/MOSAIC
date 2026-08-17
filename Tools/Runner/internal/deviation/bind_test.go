package deviation_test

// Tests for the RunContextBinder implementation on OrchestratorConsultant and
// ManualResolver (domain.RunContextBinder).
//
// Coverage:
//
//   OrchestratorConsultant — accepts dispatch after binding a populated table:
//   - A consultant constructed with empty fields, then bound via BindRunContext
//     with a RunContext carrying a populated routing table and orchestrator
//     reference, accepts a dispatch instruction naming a routing-table agent.
//   - The returned RoutingInstruction has a non-nil Dispatch with the agent
//     identifier, task description, and the correct row index from the table.
//
//   OrchestratorConsultant — rejects unknown agent after binding:
//   - A dispatch instruction naming an agent absent from the bound routing
//     table is rejected with *ConsultationError{Failure: ConsultFailUnknownAgent}.
//   - The error's Agents list contains every agent identifier from the bound
//     routing table, so the caller can surface available options.
//
//   ManualResolver — option count after binding:
//   - A resolver constructed with an empty table, then bound via BindRunContext
//     with a RunContext carrying a two-row routing table, presents
//     Interaction.SelectOne with exactly len(table.Rows)+1 options.
//   - The stop option (ID "stop") is present in those options.
//   - One option per routing table row is present (agent identifiers as IDs).

import (
	"context"
	"testing"

	"mosaic-run/internal/deviation"
	"mosaic-run/internal/domain"
)

// boundOrchestratorRef returns the orchestrator AgentReference used in bind tests.
func boundOrchestratorRef() domain.AgentReference {
	return domain.AgentReference{
		Identifier:     "orchestrator",
		DefinitionPath: "/agents/orchestrator.md",
		InvocationKind: domain.InvocationOrchestrator,
	}
}

// consultReq returns a minimal ConsultationRequest for routing.
func consultReq() domain.ConsultationRequest {
	return domain.ConsultationRequest{
		OrchestrationArtifact: "Orchestration-abc/Orchestration.md",
		Context:               domain.ConsultContextRouting,
	}
}

// ===== OrchestratorConsultant: dispatch accepted after binding =====

// TestOrchestratorConsultant_AcceptsDispatch_AfterBindRunContext verifies that
// a consultant constructed with empty fields accepts a dispatch instruction for
// a routing-table agent once BindRunContext supplies the populated routing table.
func TestOrchestratorConsultant_AcceptsDispatch_AfterBindRunContext(t *testing.T) {
	table := mustParseTable(t)
	fake := &fakeRawInvoker{
		reply: []byte(`{"action":"dispatch","agent":"agent-a","task_description":"do the thing"}`),
	}
	// Construct with no table or orchestrator.
	c := &deviation.OrchestratorConsultant{Invoker: fake}

	// Bind the run context (routing table + orchestrator reference).
	c.BindRunContext(domain.RunContext{Orchestrator: boundOrchestratorRef(), Table: table})

	instr, err := c.ConsultRouting(context.Background(), consultReq())

	// After binding, a dispatch for a known agent must succeed.
	if err != nil {
		t.Fatalf("want no error after BindRunContext + valid dispatch, got: %v", err)
	}
	if instr.Dispatch == nil {
		t.Fatal("want non-nil Dispatch in RoutingInstruction, got nil")
	}
	if instr.Dispatch.Agent != "agent-a" {
		t.Errorf("want Dispatch.Agent %q, got %q", "agent-a", instr.Dispatch.Agent)
	}
	if instr.Dispatch.TaskDescription != "do the thing" {
		t.Errorf("want Dispatch.TaskDescription %q, got %q", "do the thing", instr.Dispatch.TaskDescription)
	}
}

// TestOrchestratorConsultant_DispatchRowIndex_AfterBindRunContext verifies that
// after binding, the row index in the Dispatch corresponds to the agent's
// position in the bound routing table, not a default or sentinel value.
func TestOrchestratorConsultant_DispatchRowIndex_AfterBindRunContext(t *testing.T) {
	table := mustParseTable(t)
	fake := &fakeRawInvoker{
		// agent-b is row index 1 in the two-row simpleLinearContent fixture.
		reply: []byte(`{"action":"dispatch","agent":"agent-b","task_description":"plan it"}`),
	}
	c := &deviation.OrchestratorConsultant{Invoker: fake}
	c.BindRunContext(domain.RunContext{Orchestrator: boundOrchestratorRef(), Table: table})

	instr, err := c.ConsultRouting(context.Background(), consultReq())

	if err != nil {
		t.Fatalf("want no error, got: %v", err)
	}
	if instr.Dispatch == nil {
		t.Fatal("want non-nil Dispatch")
	}
	if instr.Dispatch.RowIndex != 1 {
		t.Errorf("want RowIndex 1 for agent-b (second row), got %d", instr.Dispatch.RowIndex)
	}
}

// ===== OrchestratorConsultant: unknown agent rejected after binding =====

// TestOrchestratorConsultant_RejectsUnknownAgent_AfterBindRunContext verifies
// that a dispatch instruction naming an agent absent from the bound routing
// table is rejected with ConsultFailUnknownAgent.
func TestOrchestratorConsultant_RejectsUnknownAgent_AfterBindRunContext(t *testing.T) {
	table := mustParseTable(t)
	fake := &fakeRawInvoker{
		reply: []byte(`{"action":"dispatch","agent":"not-in-table","task_description":"do it"}`),
	}
	c := &deviation.OrchestratorConsultant{Invoker: fake}
	c.BindRunContext(domain.RunContext{Orchestrator: boundOrchestratorRef(), Table: table})

	_, err := c.ConsultRouting(context.Background(), consultReq())

	assertConsultationError(t, err, domain.ConsultFailUnknownAgent)
}

// TestOrchestratorConsultant_UnknownAgentError_ListsAllBoundTableAgents verifies
// that the ConsultFailUnknownAgent error names every agent identifier from the
// bound routing table. The caller uses this list to surface available options.
func TestOrchestratorConsultant_UnknownAgentError_ListsAllBoundTableAgents(t *testing.T) {
	table := mustParseTable(t)
	fake := &fakeRawInvoker{
		reply: []byte(`{"action":"dispatch","agent":"not-in-table","task_description":"do it"}`),
	}
	c := &deviation.OrchestratorConsultant{Invoker: fake}
	c.BindRunContext(domain.RunContext{Orchestrator: boundOrchestratorRef(), Table: table})

	_, err := c.ConsultRouting(context.Background(), consultReq())

	ce := assertConsultationError(t, err, domain.ConsultFailUnknownAgent)

	// Verify the Agents list is non-empty and contains all table agents.
	if len(ce.Agents) == 0 {
		t.Error("want non-empty Agents list so caller can show available options; got empty")
		return
	}
	wantAgents := map[string]bool{}
	for _, row := range table.Rows {
		wantAgents[row.Agent] = true
	}
	for want := range wantAgents {
		found := false
		for _, got := range ce.Agents {
			if got == want {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("want agent %q in Agents list; got %v", want, ce.Agents)
		}
	}
}

// ===== ManualResolver: option count after binding =====

// TestManualResolver_OptionsCount_AfterBindRunContext verifies that a resolver
// constructed with an empty table, then bound via BindRunContext with a two-row
// table, presents Interaction.SelectOne with exactly three options: one per
// routing table row plus the mandatory stop option.
func TestManualResolver_OptionsCount_AfterBindRunContext(t *testing.T) {
	table := mustParseTable(t)
	// Use scriptedInteraction (defined in deviation_test.go) to capture calls.
	// Default SelectOneResult returns q.Options[0]; we only care about recording.
	interact := &scriptedInteraction{}
	m := &deviation.ManualResolver{Interact: interact}

	// Bind the run context (supplies the two-row table).
	m.BindRunContext(domain.RunContext{Table: table})

	m.ConsultRouting(context.Background(), consultReq()) //nolint:errcheck

	if len(interact.SelectOneCalls) == 0 {
		t.Fatal("want ManualResolver to call Interaction.SelectOne, got zero calls")
	}
	q := interact.SelectOneCalls[0]
	wantCount := len(table.Rows) + 1 // one per row + stop
	if len(q.Options) != wantCount {
		t.Errorf("want %d options (%d rows + 1 stop), got %d: %v",
			wantCount, len(table.Rows), len(q.Options), q.Options)
	}
}

// TestManualResolver_StopOptionPresent_AfterBindRunContext verifies that after
// binding, the stop option with ID "stop" is present in the SelectOne options.
func TestManualResolver_StopOptionPresent_AfterBindRunContext(t *testing.T) {
	table := mustParseTable(t)
	interact := &scriptedInteraction{}
	m := &deviation.ManualResolver{Interact: interact}
	m.BindRunContext(domain.RunContext{Table: table})

	m.ConsultRouting(context.Background(), consultReq()) //nolint:errcheck

	if len(interact.SelectOneCalls) == 0 {
		t.Fatal("want SelectOne to be called, got zero calls")
	}
	q := interact.SelectOneCalls[0]
	for _, opt := range q.Options {
		if opt.ID == "stop" {
			return // found
		}
	}
	t.Errorf("want stop option with ID %q in SelectOne options; got: %v", "stop", q.Options)
}

// TestManualResolver_RowOptionsPresent_AfterBindRunContext verifies that after
// binding, one option per routing table row is present in the SelectOne options,
// identified by the row's agent identifier.
func TestManualResolver_RowOptionsPresent_AfterBindRunContext(t *testing.T) {
	table := mustParseTable(t)
	interact := &scriptedInteraction{}
	m := &deviation.ManualResolver{Interact: interact}
	m.BindRunContext(domain.RunContext{Table: table})

	m.ConsultRouting(context.Background(), consultReq()) //nolint:errcheck

	if len(interact.SelectOneCalls) == 0 {
		t.Fatal("want SelectOne to be called, got zero calls")
	}
	q := interact.SelectOneCalls[0]

	optionIDs := make(map[string]bool, len(q.Options))
	for _, opt := range q.Options {
		optionIDs[opt.ID] = true
	}

	for _, row := range table.Rows {
		if !optionIDs[row.Agent] {
			t.Errorf("want option for agent %q (row %d); got options: %v",
				row.Agent, row.Index, q.Options)
		}
	}
}
