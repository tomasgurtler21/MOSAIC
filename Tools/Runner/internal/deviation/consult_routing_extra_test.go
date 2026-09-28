package deviation_test

// Additional ConsultRouting tests addressing row index resolution and
// empty-string treatment of required fields.

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"mosaic-run/internal/domain"
)

// TestOrchestratorConsultant_DispatchRowIndexResolved verifies that
// ConsultRouting resolves the agent name to its routing table index and records
// it in DispatchInstruction.RowIndex. agent-a is row 0 and agent-b is row 1 in
// the two-row fixture.
func TestOrchestratorConsultant_DispatchRowIndexResolved(t *testing.T) {
	cases := []struct {
		agent   string
		wantRow int
	}{
		{"agent-a", 0},
		{"agent-b", 1},
	}

	for _, tc := range cases {
		t.Run(tc.agent, func(t *testing.T) {
			table := mustParseTable(t)
			reply := []byte(fmt.Sprintf(`{"action":"dispatch","agent":%q,"task_description":"do the thing"}`, tc.agent))
			fake := &fakeRawInvoker{reply: reply}
			c := newTestOrchestratorConsultant(fake, table)

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

// TestOrchestratorConsultant_MissingAgentField_EmptyString verifies that a
// dispatch response with agent present but set to "" produces a *ConsultationError
// with ConsultFailMissingField. A field that is present-but-empty must be treated
// the same as absent.
func TestOrchestratorConsultant_MissingAgentField_EmptyString(t *testing.T) {
	table := mustParseTable(t)
	reply := []byte(`{"action":"dispatch","agent":"","task_description":"do the thing"}`)
	fake := &fakeRawInvoker{reply: reply}
	c := newTestOrchestratorConsultant(fake, table)

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
	c := newTestOrchestratorConsultant(fake, table)

	_, err := c.ConsultRouting(context.Background(), validRoutingRequest("Orchestration-abc/Orchestration.md"))

	ce := assertConsultationError(t, err, domain.ConsultFailMissingField)
	if !strings.Contains(ce.Detail, "task_description") {
		t.Errorf("want ConsultationError.Detail to contain %q (field name), got %q", "task_description", ce.Detail)
	}
}
