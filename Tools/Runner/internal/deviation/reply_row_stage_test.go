package deviation_test

// Tests for the row and stage a routing dispatch reply must name: a valid
// reply resolves to exactly that row and stage; an invalid one is retried with
// the malformed-JSON budget and never returned as a dispatch.

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"mosaic-run/internal/domain"
)

// stagedRoutingTable returns a four-row table (1-based row numbers):
//
//	1 researcher (RESEARCH, non-staged)
//	2 dev        (EXECUTION.Test, staged)
//	3 dev        (EXECUTION.Implementation, staged)
//	4 reviewer   (REVIEW, non-staged)
//
// The agent "dev" fills two rows, so the agent name alone is ambiguous.
func stagedRoutingTable() domain.RoutingTable {
	return domain.RoutingTable{Rows: []domain.RoutingRow{
		{Index: 0, Phase: "RESEARCH", Agent: "researcher", PhaseParsed: domain.PhaseParsed{Name: "RESEARCH"}},
		{Index: 1, Phase: "EXECUTION.Test.[StageNumber]", Agent: "dev",
			PhaseParsed: domain.PhaseParsed{Name: "EXECUTION", IsStaged: true, Group: "Test"}},
		{Index: 2, Phase: "EXECUTION.Implementation.[StageNumber]", Agent: "dev",
			PhaseParsed: domain.PhaseParsed{Name: "EXECUTION", IsStaged: true, Group: "Implementation"}},
		{Index: 3, Phase: "REVIEW", Agent: "reviewer", PhaseParsed: domain.PhaseParsed{Name: "REVIEW"}},
	}}
}

// stagesUpTo returns a stage set holding stages 1..n.
func stagesUpTo(n int) *domain.StageSet {
	set := &domain.StageSet{}
	for i := 1; i <= n; i++ {
		set.Entries = append(set.Entries, domain.StageEntry{Number: domain.StageNumber(i)})
	}
	return set
}

// rowStageRequest is a routing request carrying the current stage set.
func rowStageRequest(stages *domain.StageSet) domain.ConsultationRequest {
	req := validRoutingRequest("Orchestration-abc/Orchestration.md")
	req.Stages = stages
	return req
}

// dispatchReply builds a dispatch reply for agent with extra raw JSON members
// (for example `"row":3,"stage":2`) between the agent and the task.
func dispatchReply(agent, extra string) []byte {
	if extra != "" {
		extra += ","
	}
	return []byte(fmt.Sprintf(`{"action":"dispatch","agent":%q,%s"task_description":"do the thing"}`, agent, extra))
}

func TestConsultRouting_ValidRowAndStage_ResolveToTheNamedRowAndStage(t *testing.T) {
	cases := []struct {
		name      string
		agent     string
		extra     string
		wantIndex int
		wantStage domain.StageNumber
	}{
		{"non-staged row", "reviewer", `"row":4`, 3, 0},
		{"first row of a multi-row agent", "dev", `"row":2,"stage":1`, 1, 1},
		{"second row of a multi-row agent", "dev", `"row":3,"stage":2`, 2, 2},
		{"explicit null stage on non-staged row", "researcher", `"row":1,"stage":null`, 0, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fake := &fakeRawInvoker{reply: dispatchReply(tc.agent, tc.extra)}
			c := newTestOrchestratorConsultant(fake, stagedRoutingTable())

			instr, err := c.ConsultRouting(context.Background(), rowStageRequest(stagesUpTo(3)))

			if err != nil {
				t.Fatalf("want no error, got %v", err)
			}
			if instr.Dispatch == nil {
				t.Fatal("want Dispatch instruction, got nil")
			}
			if instr.Dispatch.RowIndex != tc.wantIndex {
				t.Errorf("want RowIndex=%d (the named row), got %d", tc.wantIndex, instr.Dispatch.RowIndex)
			}
			if instr.Dispatch.Stage != tc.wantStage {
				t.Errorf("want Stage=%d, got %d", tc.wantStage, instr.Dispatch.Stage)
			}
		})
	}
}

// Row and stage may arrive as decimal strings and the stage in the log's
// group form; all decode to the same target.
func TestConsultRouting_LenientRowAndStageForms_DecodeToTheSameTarget(t *testing.T) {
	cases := []struct {
		name  string
		extra string
	}{
		{"row as string", `"row":"3","stage":2`},
		{"stage as string", `"row":3,"stage":"2"`},
		{"stage in group form", `"row":3,"stage":"Implementation.2"`},
		{"row and stage as strings", `"row":"3","stage":"2"`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fake := &fakeRawInvoker{reply: dispatchReply("dev", tc.extra)}
			c := newTestOrchestratorConsultant(fake, stagedRoutingTable())

			instr, err := c.ConsultRouting(context.Background(), rowStageRequest(stagesUpTo(3)))

			if err != nil {
				t.Fatalf("want no error, got %v", err)
			}
			if instr.Dispatch == nil || instr.Dispatch.RowIndex != 2 || instr.Dispatch.Stage != 2 {
				t.Fatalf("want dispatch at RowIndex=2 Stage=2, got %+v", instr.Dispatch)
			}
		})
	}
}

// Every invalid reply consumes the whole attempt budget (3 invocations), ends
// with the malformed-JSON class and never yields a dispatch.
func TestConsultRouting_InvalidRowOrStage_RetriedThenMalformedJSON(t *testing.T) {
	cases := []struct {
		name  string
		agent string
		extra string
	}{
		{"missing row", "reviewer", ``},
		{"null row", "reviewer", `"row":null`},
		{"row beyond the table", "reviewer", `"row":9`},
		{"row zero", "reviewer", `"row":0`},
		{"negative row", "reviewer", `"row":-1`},
		{"stage zero on a staged row", "dev", `"row":3,"stage":0`},
		{"negative stage on a staged row", "dev", `"row":3,"stage":-1`},
		{"row of another agent", "reviewer", `"row":1`},
		{"staged row without stage", "dev", `"row":3`},
		{"staged row with null stage", "dev", `"row":3,"stage":null`},
		{"stage outside the current set", "dev", `"row":3,"stage":4`},
		{"stage on a non-staged row", "reviewer", `"row":4,"stage":1`},
		{"stage group of another group", "dev", `"row":3,"stage":"Test.1"`},
		{"row as boolean", "reviewer", `"row":true`},
		{"row as fraction", "reviewer", `"row":1.5`},
		{"row as non-numeric string", "reviewer", `"row":"abc"`},
		{"stage as boolean", "dev", `"row":3,"stage":true`},
		{"stage as non-numeric string", "dev", `"row":3,"stage":"abc"`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			bad := sequencedReply{reply: dispatchReply(tc.agent, tc.extra)}
			invoker := &sequencedRawInvoker{replies: []sequencedReply{bad, bad, bad, bad}}
			c := newTestOrchestratorConsultant(invoker, stagedRoutingTable())

			instr, err := c.ConsultRouting(context.Background(), rowStageRequest(stagesUpTo(3)))

			assertConsultationError(t, err, domain.ConsultFailMalformedJSON)
			if instr.Dispatch != nil {
				t.Errorf("want no dispatch for an invalid reply, got %+v", instr.Dispatch)
			}
			if invoker.pos != 3 {
				t.Errorf("want 3 InvokeRaw calls (same budget as malformed JSON), got %d", invoker.pos)
			}
		})
	}
}

func TestConsultRouting_InvalidRowThenValidReply_SucceedsOnSecondAttempt(t *testing.T) {
	invoker := &sequencedRawInvoker{replies: []sequencedReply{
		{reply: dispatchReply("dev", `"row":3`)}, // staged row, no stage
		{reply: dispatchReply("dev", `"row":3,"stage":2`)},
	}}
	c := newTestOrchestratorConsultant(invoker, stagedRoutingTable())

	instr, err := c.ConsultRouting(context.Background(), rowStageRequest(stagesUpTo(3)))

	if err != nil {
		t.Fatalf("want success on the retry, got %v", err)
	}
	if instr.Dispatch == nil || instr.Dispatch.RowIndex != 2 || instr.Dispatch.Stage != 2 {
		t.Fatalf("want dispatch at RowIndex=2 Stage=2, got %+v", instr.Dispatch)
	}
	if invoker.pos != 2 {
		t.Errorf("want 2 InvokeRaw calls, got %d", invoker.pos)
	}
}

// Invalid replies and malformed JSON share one budget of three attempts.
func TestConsultRouting_InvalidAndMalformedReplies_ShareOneRetryBudget(t *testing.T) {
	invoker := &sequencedRawInvoker{replies: []sequencedReply{
		malformedSchemaReply(),
		{reply: dispatchReply("reviewer", ``)},
		{reply: dispatchReply("dev", `"row":3`)},
		{reply: dispatchReply("dev", `"row":3,"stage":2`)}, // would be valid, but the budget is spent
	}}
	c := newTestOrchestratorConsultant(invoker, stagedRoutingTable())

	instr, err := c.ConsultRouting(context.Background(), rowStageRequest(stagesUpTo(3)))

	assertConsultationError(t, err, domain.ConsultFailMalformedJSON)
	if instr.Dispatch != nil {
		t.Errorf("want no dispatch once the budget is spent, got %+v", instr.Dispatch)
	}
	if invoker.pos != 3 {
		t.Errorf("want 3 InvokeRaw calls, got %d", invoker.pos)
	}
}

// The stage set that counts is the one on the request: a stage added mid-run
// is accepted, and the same reply is invalid against the earlier set.
func TestConsultRouting_StageValidatedAgainstTheRequestsCurrentStageSet(t *testing.T) {
	reply := dispatchReply("dev", `"row":3,"stage":3`)

	grown := &fakeRawInvoker{reply: reply}
	instr, err := newTestOrchestratorConsultant(grown, stagedRoutingTable()).
		ConsultRouting(context.Background(), rowStageRequest(stagesUpTo(3)))
	if err != nil {
		t.Fatalf("want stage 3 accepted when the request's stage set holds it, got %v", err)
	}
	if instr.Dispatch == nil || instr.Dispatch.Stage != 3 {
		t.Fatalf("want dispatch at Stage=3, got %+v", instr.Dispatch)
	}

	stale := &sequencedRawInvoker{replies: []sequencedReply{{reply: reply}, {reply: reply}, {reply: reply}}}
	_, err = newTestOrchestratorConsultant(stale, stagedRoutingTable()).
		ConsultRouting(context.Background(), rowStageRequest(stagesUpTo(2)))
	assertConsultationError(t, err, domain.ConsultFailMalformedJSON)
}

func TestConsultRouting_StagedRowWithoutAnyStageSet_IsInvalid(t *testing.T) {
	reply := sequencedReply{reply: dispatchReply("dev", `"row":3,"stage":1`)}
	invoker := &sequencedRawInvoker{replies: []sequencedReply{reply, reply, reply}}
	c := newTestOrchestratorConsultant(invoker, stagedRoutingTable())

	instr, err := c.ConsultRouting(context.Background(), rowStageRequest(nil))

	assertConsultationError(t, err, domain.ConsultFailMalformedJSON)
	if instr.Dispatch != nil {
		t.Errorf("want no dispatch without a stage set, got %+v", instr.Dispatch)
	}
}

// A stop reply needs neither row nor stage.
func TestConsultRouting_StopReplyNeedsNoRowOrStage(t *testing.T) {
	fake := &fakeRawInvoker{reply: routingStopReply("done")}
	c := newTestOrchestratorConsultant(fake, stagedRoutingTable())

	instr, err := c.ConsultRouting(context.Background(), rowStageRequest(stagesUpTo(1)))

	if err != nil {
		t.Fatalf("want no error for a stop reply, got %v", err)
	}
	if instr.Stop == nil {
		t.Fatal("want Stop instruction, got nil")
	}
}

// An unknown agent keeps its own failure class and is not retried, even when
// the reply carries a row.
func TestConsultRouting_UnknownAgentWithRow_StaysUnknownAgentWithoutRetry(t *testing.T) {
	bad := sequencedReply{reply: dispatchReply("nobody", `"row":1`)}
	invoker := &sequencedRawInvoker{replies: []sequencedReply{bad, bad, bad}}
	c := newTestOrchestratorConsultant(invoker, stagedRoutingTable())

	_, err := c.ConsultRouting(context.Background(), rowStageRequest(stagesUpTo(1)))

	assertConsultationError(t, err, domain.ConsultFailUnknownAgent)
	if invoker.pos != 1 {
		t.Errorf("want 1 InvokeRaw call (no retry), got %d", invoker.pos)
	}
}

// An unknown agent without any row stays an unknown-agent failure (no retry):
// agent and task checks come before row validation.
func TestConsultRouting_UnknownAgentWithoutRow_StaysUnknownAgentWithoutRetry(t *testing.T) {
	fake := &fakeRawInvoker{reply: dispatchReply("not-in-table", ``)}
	c := newTestOrchestratorConsultant(fake, stagedRoutingTable())

	_, err := c.ConsultRouting(context.Background(), rowStageRequest(stagesUpTo(3)))

	assertConsultationError(t, err, domain.ConsultFailUnknownAgent)
}

// The stage set is session context for validation only; it must not appear in
// the payload sent to the orchestrator.
func TestConsultRouting_StageSetIsNotSerialisedOntoTheWire(t *testing.T) {
	fake := &fakeRawInvoker{reply: dispatchReply("dev", `"row":3,"stage":2`)}
	c := newTestOrchestratorConsultant(fake, stagedRoutingTable())

	if _, err := c.ConsultRouting(context.Background(), rowStageRequest(stagesUpTo(3))); err != nil {
		t.Fatalf("want no error, got %v", err)
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(fake.sent, &raw); err != nil {
		t.Fatalf("want valid JSON wire payload, got %v", err)
	}
	for key := range raw {
		if strings.Contains(strings.ToLower(key), "stage") {
			t.Errorf("want no stage-set field in the wire payload, found key %q", key)
		}
	}
}
