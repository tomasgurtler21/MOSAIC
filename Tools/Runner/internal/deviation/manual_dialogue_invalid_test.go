package deviation_test

// Tests for invalid manual results: they are never dispatched, the user is
// shown the reason and taken back, and the re-prompting is bounded.

import (
	"errors"
	"slices"
	"testing"

	"mosaic-common/interaction"
	"mosaic-run/internal/deviation"
	"mosaic-run/internal/domain"
)

// requireReasonShown asserts the first invalid result was reported to the
// user: a warning notice, and the re-asked question carrying the same text.
func requireReasonShown(t *testing.T, d *dialogue, reasked interaction.QuestionID, occurrence int) {
	t.Helper()
	if len(d.notices) == 0 {
		t.Fatal("want the reason of the invalid result shown in a notice, got none")
	}
	n := d.notices[0]
	if n.Level != interaction.NoticeWarning || n.Title != "Manual Routing" || n.Message == "" {
		t.Errorf("want a non-empty warning titled %q, got %+v", "Manual Routing", n)
	}
	var detail string
	switch reasked {
	case domain.QuestionManualRow, domain.QuestionManualStage:
		qs := d.oneQs[reasked]
		if len(qs) <= occurrence {
			t.Fatalf("want question %s asked at least %d times, got %d", reasked, occurrence+1, len(qs))
		}
		detail = qs[occurrence].Detail
	default:
		t.Fatalf("unsupported question %s", reasked)
	}
	if detail != n.Message {
		t.Errorf("want the re-asked question to carry the shown reason %q, got %q", n.Message, detail)
	}
}

func TestManualDialogue_StageRemovedFromTheSetBeforeTheFinalCheck_ReasksTheStageWithTheReason(t *testing.T) {
	stages := stagesUpTo(3)
	req := manualRequest(stages)
	d := newDialogue(t).standard("3", "3")
	d.one[domain.QuestionManualStage] = []oneFn{pick("3"), pick("2")}
	removed := false
	d.one[domain.QuestionManualHITL] = []oneFn{func(interaction.ChoiceQuestion) interaction.ChoiceAnswer {
		if !removed {
			removed = true
			stages.Entries = stages.Entries[:2] // stage 3 disappears while the user answers
		}
		return interaction.ChoiceAnswer{Status: interaction.Answered, OptionID: domain.ManualHITLDefault}
	}}

	instr, err := consultManual(t, d, stagedRoutingTable(), req)

	disp := requireDispatch(t, instr, err)
	if disp.Stage != 2 {
		t.Errorf("want the corrected stage 2 dispatched, got %d", disp.Stage)
	}
	if got := questionAfterFirst(d.calls, domain.QuestionManualHITL); got != domain.QuestionManualStage {
		t.Errorf("want the stage asked again right after the rejected result, got %q (sequence %v)", got, d.calls)
	}
	requireReasonShown(t, d, domain.QuestionManualStage, 1)
	if want := []string{"1", "2"}; !slices.Equal(optionIDs(d.oneQ(domain.QuestionManualStage, 1)), want) {
		t.Errorf("want the re-asked stage question to offer the current set %v, got %v",
			want, optionIDs(d.oneQ(domain.QuestionManualStage, 1)))
	}
}

func TestManualDialogue_TypedStageOutsideTheSet_IsCaughtWhenTheDefaultsResolveAndAsksTheStageAgain(t *testing.T) {
	d := newDialogue(t).standard("3", "2")
	d.one[domain.QuestionManualStage] = []oneFn{typed("7"), pick("2")}

	instr, err := consultStaged(t, d, 3)

	disp := requireDispatch(t, instr, err)
	requireSequence(t, d,
		domain.QuestionManualRow, domain.QuestionManualStage, domain.QuestionManualTask,
		domain.QuestionManualStage, domain.QuestionManualTask, domain.QuestionManualInputs,
		domain.QuestionManualOutputs, domain.QuestionManualConstraints, domain.QuestionManualHITL)
	requireReasonShown(t, d, domain.QuestionManualStage, 1)
	if disp.Stage != 2 {
		t.Errorf("want the corrected stage 2, got %d", disp.Stage)
	}
}

func TestManualDialogue_InvalidAnswer_IsReportedAndTheStepAskedAgain(t *testing.T) {
	cases := []struct {
		name    string
		script  func(d *dialogue)
		stages  int
		reasked interaction.QuestionID
		wantSeq []interaction.QuestionID // prefix of the asked questions
	}{
		{"row that is not in the table", func(d *dialogue) { d.one[domain.QuestionManualRow] = []oneFn{pick("99"), pick("4")} }, 3,
			domain.QuestionManualRow, []interaction.QuestionID{domain.QuestionManualRow, domain.QuestionManualRow, domain.QuestionManualTask}},
		{"row that is not a number", func(d *dialogue) { d.one[domain.QuestionManualRow] = []oneFn{typed("abc"), pick("4")} }, 3,
			domain.QuestionManualRow, []interaction.QuestionID{domain.QuestionManualRow, domain.QuestionManualRow, domain.QuestionManualTask}},
		{"stage that is not a number", func(d *dialogue) { d.one[domain.QuestionManualStage] = []oneFn{typed("abc"), pick("2")} }, 3,
			domain.QuestionManualStage, []interaction.QuestionID{domain.QuestionManualRow, domain.QuestionManualStage, domain.QuestionManualStage, domain.QuestionManualTask}},
		{"stage zero", func(d *dialogue) { d.one[domain.QuestionManualStage] = []oneFn{typed("0"), pick("2")} }, 3,
			domain.QuestionManualStage, []interaction.QuestionID{domain.QuestionManualRow, domain.QuestionManualStage, domain.QuestionManualStage, domain.QuestionManualTask}},
		{"negative stage", func(d *dialogue) { d.one[domain.QuestionManualStage] = []oneFn{typed("-1"), pick("2")} }, 3,
			domain.QuestionManualStage, []interaction.QuestionID{domain.QuestionManualRow, domain.QuestionManualStage, domain.QuestionManualStage, domain.QuestionManualTask}},
		{"staged row while the stage set is empty returns to the row", func(d *dialogue) { d.one[domain.QuestionManualRow] = []oneFn{pick("3"), pick("4")} }, 0,
			domain.QuestionManualRow, []interaction.QuestionID{domain.QuestionManualRow, domain.QuestionManualRow, domain.QuestionManualTask}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d := newDialogue(t).standard("3", "2")
			tc.script(d)

			instr, err := consultStaged(t, d, tc.stages)

			requireDispatch(t, instr, err)
			if len(d.calls) < len(tc.wantSeq) || !slices.Equal(d.calls[:len(tc.wantSeq)], tc.wantSeq) {
				t.Errorf("question sequence:\n got %v\nwant prefix %v", d.calls, tc.wantSeq)
			}
			requireReasonShown(t, d, tc.reasked, 1)
		})
	}
}

func TestManualDialogue_UnresolvableRowDefaults_AreAnInvalidResultThatReturnsToTheRowStep(t *testing.T) {
	d := newDialogue(t).standard("4", "")
	d.one[domain.QuestionManualRow] = []oneFn{pick("4"), pick("1")}
	req := manualRequest(stagesUpTo(2))
	inner := req.RowDefaults
	req.RowDefaults = func(row domain.RoutingRow, stage domain.StageNumber) (domain.DispatchDefaults, error) {
		if row.Index == 3 {
			return domain.DispatchDefaults{}, errors.New("cannot resolve the defaults of this row")
		}
		return inner(row, stage)
	}

	instr, err := consultManual(t, d, stagedRoutingTable(), req)

	disp := requireDispatch(t, instr, err)
	requireSequence(t, d,
		domain.QuestionManualRow, domain.QuestionManualTask, domain.QuestionManualRow,
		domain.QuestionManualTask, domain.QuestionManualInputs, domain.QuestionManualOutputs,
		domain.QuestionManualConstraints, domain.QuestionManualHITL)
	requireReasonShown(t, d, domain.QuestionManualRow, 1)
	if disp.RowIndex != 0 {
		t.Errorf("want the corrected row 1 dispatched, got RowIndex=%d", disp.RowIndex)
	}
}

func TestManualDialogue_NoDefaultsFunction_MeansEmptyListsAndNoHITLDefault(t *testing.T) {
	d := newDialogue(t).standard("3", "2")
	req := rowStageRequest(stagesUpTo(3)) // RowDefaults left nil

	instr, err := consultManual(t, d, stagedRoutingTable(), req)

	disp := requireDispatch(t, instr, err)
	if got := d.manyQ(domain.QuestionManualInputs, 0).DefaultOptionIDs; len(got) != 0 {
		t.Errorf("want nothing pre-checked without a defaults function, got %v", got)
	}
	if disp.InputArtifacts != nil || disp.OutputArtifacts != nil {
		t.Errorf("want no list override for an empty selection equal to the empty defaults, got %v / %v",
			disp.InputArtifacts, disp.OutputArtifacts)
	}
}

func TestManualDialogue_ConsecutiveInvalidResults_AreBoundedAndEndTheRouting(t *testing.T) {
	d := newDialogue(t).standard("99", "")

	instr, err := consultStaged(t, d, 3)

	requireEnding(t, instr, err, domain.ConsultFailManualBoundExceeded)
	if got, want := len(d.oneQs[domain.QuestionManualRow]), deviation.ManualInvalidResultLimit+1; got != want {
		t.Errorf("want the row asked %d times (the last invalid result ends routing), got %d", want, got)
	}
	if ce := assertConsultationError(t, err, domain.ConsultFailManualBoundExceeded); ce.Detail == "" {
		t.Error("want the bound case named in Detail")
	}
}

func TestManualDialogue_InvalidResultsUpToTheLimit_StillAllowACompleteResult(t *testing.T) {
	d := newDialogue(t).standard("4", "")
	rows := make([]oneFn, 0, deviation.ManualInvalidResultLimit+1)
	for i := 0; i < deviation.ManualInvalidResultLimit; i++ {
		rows = append(rows, pick("99"))
	}
	d.one[domain.QuestionManualRow] = append(rows, pick("4"))

	instr, err := consultStaged(t, d, 3)

	disp := requireDispatch(t, instr, err)
	if disp.RowIndex != 3 {
		t.Errorf("want row 4 dispatched after %d rejected answers, got RowIndex=%d", deviation.ManualInvalidResultLimit, disp.RowIndex)
	}
}

func TestManualDialogue_AcceptedStepsBetweenInvalidResults_DoNotResetTheCount(t *testing.T) {
	d := newDialogue(t).standard("3", "2")
	d.one[domain.QuestionManualRow] = []oneFn{pick("99"), pick("3")}
	d.one[domain.QuestionManualStage] = []oneFn{typed("abc"), typed("0"), typed("-1")}

	instr, err := consultStaged(t, d, 3)

	requireEnding(t, instr, err, domain.ConsultFailManualBoundExceeded)
	requireSequence(t, d,
		domain.QuestionManualRow, domain.QuestionManualRow,
		domain.QuestionManualStage, domain.QuestionManualStage, domain.QuestionManualStage)
}

func TestManualDialogue_RowDefaultsThatNeverResolve_EndAsBoundExceededWithoutDispatch(t *testing.T) {
	d := newDialogue(t).standard("4", "")
	req := manualRequest(stagesUpTo(2))
	req.RowDefaults = func(domain.RoutingRow, domain.StageNumber) (domain.DispatchDefaults, error) {
		return domain.DispatchDefaults{}, errors.New("defaults unavailable")
	}

	instr, err := consultManual(t, d, stagedRoutingTable(), req)

	requireEnding(t, instr, err, domain.ConsultFailManualBoundExceeded)
	if len(d.calls) > deviation.ManualPromptLimit {
		t.Errorf("want at most %d questions, got %d", deviation.ManualPromptLimit, len(d.calls))
	}
}
