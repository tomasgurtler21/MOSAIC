package deviation_test

// Tests for ManualResolver.ConsultRouting keyed by workflow row: 1-based row
// options, a stage choice for staged rows, validation of the result, and the
// ending outcome (a consultation failure, never a dispatch) for a cancelled,
// unanswerable or repeatedly invalid result.

import (
	"context"
	"strings"
	"testing"

	"mosaic-common/interaction"
	"mosaic-run/internal/deviation"
	"mosaic-run/internal/domain"
)

// manualScript answers the row question, the optional stage question and the
// task text question of a ManualResolver run; the remaining steps (artifact
// lists, constraints, HITL) are answered with their defaults.
type manualScript struct {
	rowAnswer   interaction.ChoiceAnswer
	stageAnswer interaction.ChoiceAnswer
	textAnswer  interaction.TextAnswer
}

// interaction builds a scriptedInteraction answering by question ID.
func (s manualScript) interaction() *scriptedInteraction {
	si := &scriptedInteraction{}
	si.SelectOneResult = func(q interaction.ChoiceQuestion) (interaction.ChoiceAnswer, error) {
		switch q.ID {
		case domain.QuestionManualRow:
			return s.rowAnswer, nil
		case domain.QuestionManualStage:
			return s.stageAnswer, nil
		case domain.QuestionManualHITL:
			return answeredOption(domain.ManualHITLDefault), nil
		}
		return interaction.ChoiceAnswer{Status: interaction.Cancelled}, nil
	}
	si.AskTextResult = func(q interaction.TextQuestion) (interaction.TextAnswer, error) {
		if q.ID == domain.QuestionManualTask {
			return s.textAnswer, nil
		}
		return answeredText(""), nil
	}
	return si
}

// selectOneIDs returns the question IDs of the recorded SelectOne calls.
func selectOneIDs(si *scriptedInteraction) []interaction.QuestionID {
	var ids []interaction.QuestionID
	for _, q := range si.SelectOneCalls {
		ids = append(ids, q.ID)
	}
	return ids
}

// stageQuestion returns the recorded stage question, if one was asked.
func stageQuestion(si *scriptedInteraction) (interaction.ChoiceQuestion, bool) {
	for _, q := range si.SelectOneCalls {
		if q.ID == domain.QuestionManualStage {
			return q, true
		}
	}
	return interaction.ChoiceQuestion{}, false
}

func answeredOption(id string) interaction.ChoiceAnswer {
	return interaction.ChoiceAnswer{Status: interaction.Answered, OptionID: id}
}

func answeredText(text string) interaction.TextAnswer {
	return interaction.TextAnswer{Status: interaction.Answered, Text: text}
}

func runManual(t *testing.T, si *scriptedInteraction, stages *domain.StageSet) (domain.RoutingInstruction, error) {
	t.Helper()
	m := &deviation.ManualResolver{Interact: si, Table: stagedRoutingTable()}
	return m.ConsultRouting(context.Background(), rowStageRequest(stages))
}

func assertNoDispatch(t *testing.T, instr domain.RoutingInstruction) {
	t.Helper()
	if instr.Dispatch != nil {
		t.Errorf("want no dispatch instruction, got %+v", instr.Dispatch)
	}
}

func TestManualResolver_RowOptionsAreKeyedByRowWithOneBasedLabels(t *testing.T) {
	si := manualScript{rowAnswer: answeredOption("stop")}.interaction()

	runManual(t, si, stagesUpTo(2)) //nolint:errcheck

	if len(si.SelectOneCalls) == 0 {
		t.Fatal("want a row question, got no SelectOne call")
	}
	opts := si.SelectOneCalls[0].Options
	wantIDs := []string{"1", "2", "3", "4", "stop"}
	if len(opts) != len(wantIDs) {
		t.Fatalf("want %d options (4 rows + stop), got %d: %v", len(wantIDs), len(opts), opts)
	}
	for i, want := range wantIDs {
		if opts[i].ID != want {
			t.Errorf("option %d: want ID %q, got %q", i, want, opts[i].ID)
		}
	}
	// The agent "dev" fills rows 2 and 3: two distinct options, labelled by row.
	if !strings.Contains(opts[1].Label, "[2]") || !strings.Contains(opts[1].Label, "dev") {
		t.Errorf("want label of row 2 to show [2] and dev, got %q", opts[1].Label)
	}
	if !strings.Contains(opts[2].Label, "[3]") || !strings.Contains(opts[2].Label, "dev") {
		t.Errorf("want label of row 3 to show [3] and dev, got %q", opts[2].Label)
	}
	if !strings.Contains(opts[0].Label, "[1]") || strings.Contains(opts[0].Label, "[0]") {
		t.Errorf("want first row labelled [1] (1-based), got %q", opts[0].Label)
	}
	if opts[4].Label != "Stop the run" {
		t.Errorf("want stop option labelled %q, got %q", "Stop the run", opts[4].Label)
	}
}

func TestManualResolver_LaterRowOfMultiRowAgent_YieldsThatRowAndStage(t *testing.T) {
	si := manualScript{
		rowAnswer:   answeredOption("3"),
		stageAnswer: answeredOption("2"),
		textAnswer:  answeredText("implement it"),
	}.interaction()

	instr, err := runManual(t, si, stagesUpTo(3))

	if err != nil {
		t.Fatalf("want no error, got %v", err)
	}
	if instr.Dispatch == nil {
		t.Fatal("want Dispatch instruction, got nil")
	}
	d := instr.Dispatch
	if d.Agent != "dev" || d.RowIndex != 2 || d.Stage != 2 {
		t.Errorf("want dev at RowIndex=2 Stage=2 (the chosen later row), got agent %q RowIndex=%d Stage=%d",
			d.Agent, d.RowIndex, d.Stage)
	}
	if d.TaskDescription != "implement it" {
		t.Errorf("want the entered task text, got %q", d.TaskDescription)
	}
	// The result is a valid target per the shared validator.
	_, verr := domain.ValidateDispatchTarget(stagedRoutingTable(), stagesUpTo(3), domain.DispatchTarget{
		Agent: d.Agent, Row: domain.WorkflowRowFromIndex(d.RowIndex), Stage: d.Stage,
	})
	if verr != nil {
		t.Errorf("want the returned dispatch to pass ValidateDispatchTarget, got %v", verr)
	}
}

func TestManualResolver_StagedRow_AsksForAStageFromTheCurrentStageSet(t *testing.T) {
	si := manualScript{
		rowAnswer:   answeredOption("2"),
		stageAnswer: answeredOption("1"),
		textAnswer:  answeredText("write tests"),
	}.interaction()

	runManual(t, si, stagesUpTo(3)) //nolint:errcheck

	stageQ, asked := stageQuestion(si)
	if !asked {
		t.Fatalf("want a stage question after the row question, got SelectOne questions %v", selectOneIDs(si))
	}
	opts := stageQ.Options
	wantIDs := []string{"1", "2", "3"}
	if len(opts) != len(wantIDs) {
		t.Fatalf("want one option per current stage (%v), got %v", wantIDs, opts)
	}
	for i, want := range wantIDs {
		if opts[i].ID != want {
			t.Errorf("stage option %d: want ID %q, got %q", i, want, opts[i].ID)
		}
		if !strings.Contains(opts[i].Label, want) {
			t.Errorf("stage option %d: want label to show the stage number %s, got %q", i, want, opts[i].Label)
		}
	}
}

func TestManualResolver_NonStagedRow_AsksNoStageAndYieldsStageZero(t *testing.T) {
	si := manualScript{
		rowAnswer:  answeredOption("4"),
		textAnswer: answeredText("review it"),
	}.interaction()

	instr, err := runManual(t, si, stagesUpTo(3))

	if err != nil {
		t.Fatalf("want no error, got %v", err)
	}
	if _, asked := stageQuestion(si); asked {
		t.Errorf("want no stage question for a non-staged row, got SelectOne questions %v", selectOneIDs(si))
	}
	if instr.Dispatch == nil || instr.Dispatch.RowIndex != 3 || instr.Dispatch.Stage != 0 {
		t.Fatalf("want reviewer at RowIndex=3 Stage=0, got %+v", instr.Dispatch)
	}
}

func TestManualResolver_StageAddedMidRun_IsOfferedAndAccepted(t *testing.T) {
	si := manualScript{
		rowAnswer:   answeredOption("3"),
		stageAnswer: answeredOption("4"),
		textAnswer:  answeredText("late stage"),
	}.interaction()

	instr, err := runManual(t, si, stagesUpTo(4))

	if err != nil {
		t.Fatalf("want no error for a stage present in the current set, got %v", err)
	}
	if instr.Dispatch == nil || instr.Dispatch.Stage != 4 {
		t.Fatalf("want dispatch at Stage=4, got %+v", instr.Dispatch)
	}
}

func TestManualResolver_CancelledRowPick_IsUserAbandonedAndNeverDispatches(t *testing.T) {
	si := manualScript{rowAnswer: interaction.ChoiceAnswer{Status: interaction.Cancelled}}.interaction()

	instr, err := runManual(t, si, stagesUpTo(2))

	assertConsultationError(t, err, domain.ConsultFailUserAbandoned)
	assertNoDispatch(t, instr)
	if instr.Stop != nil {
		t.Errorf("want no stop instruction for a cancellation, got %+v", instr.Stop)
	}
}

func TestManualResolver_BlankTaskText_IsAnUnavailableInteractionAndNeverDispatches(t *testing.T) {
	si := manualScript{rowAnswer: answeredOption("4"), textAnswer: answeredText("   ")}.interaction()

	instr, err := runManual(t, si, stagesUpTo(2))

	assertConsultationError(t, err, domain.ConsultFailInteractionUnavailable)
	assertNoDispatch(t, instr)
}

// An interaction that keeps giving the same invalid answer is re-prompted up
// to the bound and then ends the routing; it never dispatches.
func TestManualResolver_RepeatedInvalidResult_EndsAsBoundExceededAndNeverDispatches(t *testing.T) {
	cases := []struct {
		name   string
		script manualScript
		stages *domain.StageSet
	}{
		{"row id that is not a table row", manualScript{
			rowAnswer: answeredOption("99"), textAnswer: answeredText("x"),
		}, stagesUpTo(2)},
		{"row id that is not a number", manualScript{
			rowAnswer: answeredOption("dev"), textAnswer: answeredText("x"),
		}, stagesUpTo(2)},
		{"stage outside the current set", manualScript{
			rowAnswer: answeredOption("3"), stageAnswer: answeredOption("7"), textAnswer: answeredText("x"),
		}, stagesUpTo(2)},
		{"staged row with no stage set", manualScript{
			rowAnswer: answeredOption("3"), stageAnswer: answeredOption("1"), textAnswer: answeredText("x"),
		}, nil},
		{"staged row with an empty stage set", manualScript{
			rowAnswer: answeredOption("3"), stageAnswer: answeredOption("1"), textAnswer: answeredText("x"),
		}, &domain.StageSet{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			instr, err := runManual(t, tc.script.interaction(), tc.stages)

			assertConsultationError(t, err, domain.ConsultFailManualBoundExceeded)
			assertNoDispatch(t, instr)
		})
	}
}
