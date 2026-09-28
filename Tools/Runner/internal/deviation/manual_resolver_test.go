package deviation_test

// Tests for ManualResolver.ConsultRouting — two-action instruction shape (T3.4).

import (
	"context"
	"testing"

	"mosaic-common/interaction"
	"mosaic-run/internal/deviation"
	"mosaic-run/internal/domain"
)

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
