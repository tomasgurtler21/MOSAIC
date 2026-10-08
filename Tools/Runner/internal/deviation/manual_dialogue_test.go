package deviation_test

// Tests for the full manual routing dialogue: row, stage (staged rows only),
// task, inputs, outputs, constraints and HITL, and the dispatch instruction
// that carries exactly what the user chose.

import (
	"slices"
	"strings"
	"testing"

	"mosaic-common/interaction"
	"mosaic-run/internal/domain"
)

func TestManualDialogue_StagedRow_AsksEveryStepAndDispatchCarriesTheChosenValues(t *testing.T) {
	d := newDialogue(t).standard("3", "3")
	d.text[domain.QuestionManualTask] = []textFn{say("  implement stage three  ")}
	d.many[domain.QuestionManualInputs] = []manyFn{acceptDefaultsPlus("docs/extra.md")}
	d.text[domain.QuestionManualConstraints] = []textFn{say("  keep it small ")}
	d.one[domain.QuestionManualHITL] = []oneFn{pick(domain.ManualHITLOn)}

	instr, err := consultStaged(t, d, 4)

	disp := requireDispatch(t, instr, err)
	requireSequence(t, d,
		domain.QuestionManualRow, domain.QuestionManualStage, domain.QuestionManualTask,
		domain.QuestionManualInputs, domain.QuestionManualOutputs,
		domain.QuestionManualConstraints, domain.QuestionManualHITL)
	if disp.Agent != "dev" || disp.RowIndex != 2 || disp.Stage != 3 {
		t.Errorf("want dev at RowIndex=2 Stage=3, got %q RowIndex=%d Stage=%d", disp.Agent, disp.RowIndex, disp.Stage)
	}
	if disp.TaskDescription != "implement stage three" {
		t.Errorf("want the trimmed task text, got %q", disp.TaskDescription)
	}
	if disp.Constraints == nil || *disp.Constraints != "keep it small" {
		t.Errorf("want constraints %q, got %v", "keep it small", disp.Constraints)
	}
	wantInputs := []string{"Plan.md", "Stage-3/Spec.md", "docs/extra.md"}
	if disp.InputArtifacts == nil || !slices.Equal(*disp.InputArtifacts, wantInputs) {
		t.Errorf("want explicit inputs %v, got %v", wantInputs, disp.InputArtifacts)
	}
	if disp.OutputArtifacts != nil {
		t.Errorf("want no output override for untouched defaults, got %v", *disp.OutputArtifacts)
	}
	if disp.HITLOverride == nil || !*disp.HITLOverride {
		t.Errorf("want HITL override on, got %v", disp.HITLOverride)
	}
}

func TestManualDialogue_NonStagedRow_SkipsTheStageStep(t *testing.T) {
	d := newDialogue(t).standard("4", "")

	instr, err := consultStaged(t, d, 3)

	disp := requireDispatch(t, instr, err)
	requireSequence(t, d,
		domain.QuestionManualRow, domain.QuestionManualTask, domain.QuestionManualInputs,
		domain.QuestionManualOutputs, domain.QuestionManualConstraints, domain.QuestionManualHITL)
	if disp.Agent != "reviewer" || disp.RowIndex != 3 || disp.Stage != 0 {
		t.Errorf("want reviewer at RowIndex=3 Stage=0, got %q RowIndex=%d Stage=%d", disp.Agent, disp.RowIndex, disp.Stage)
	}
}

func TestManualDialogue_RowAtTwelve_IsShownAsTwelveAndRoutedWithStageAndExtraInput(t *testing.T) {
	d := newDialogue(t).standard("12", "3")
	d.many[domain.QuestionManualInputs] = []manyFn{acceptDefaultsPlus("extra/context.md")}
	table := twelveRowTable()

	instr, err := consultManual(t, d, table, manualRequest(stagesUpTo(3)))

	disp := requireDispatch(t, instr, err)
	rowQ := d.oneQ(domain.QuestionManualRow, 0)
	requireOptions(t, rowQ, 13)
	rowOpts := rowQ.Options
	if len(rowOpts) != 13 {
		t.Fatalf("want 12 row options plus stop, got %d", len(rowOpts))
	}
	if rowOpts[11].ID != "12" || !strings.Contains(rowOpts[11].Label, "[12]") || !strings.Contains(rowOpts[11].Label, "build-review") {
		t.Errorf("want the last row offered as ID 12 labelled [12] build-review, got %+v", rowOpts[11])
	}
	if rowOpts[12].ID != domain.ManualStopOptionID {
		t.Errorf("want the stop option last, got %+v", rowOpts[12])
	}
	if disp.Agent != "build-review" || disp.RowIndex != 11 || disp.Stage != 3 {
		t.Errorf("want build-review at RowIndex=11 Stage=3, got %q RowIndex=%d Stage=%d", disp.Agent, disp.RowIndex, disp.Stage)
	}
	wantInputs := []string{"Plan.md", "Stage-3/Spec.md", "extra/context.md"}
	if disp.InputArtifacts == nil || !slices.Equal(*disp.InputArtifacts, wantInputs) {
		t.Errorf("want inputs %v, got %v", wantInputs, disp.InputArtifacts)
	}
	_, verr := domain.ValidateDispatchTarget(table, stagesUpTo(3), domain.DispatchTarget{
		Agent: disp.Agent, Row: domain.WorkflowRowFromIndex(disp.RowIndex), Stage: disp.Stage,
	})
	if verr != nil {
		t.Errorf("want the dispatch to satisfy the shared validator, got %v", verr)
	}
}

func TestManualDialogue_RowAndStageAnswersMayBeTypedFreeForm(t *testing.T) {
	d := newDialogue(t).standard("3", "2")
	d.one[domain.QuestionManualRow] = []oneFn{typed(" 3 ")}
	d.one[domain.QuestionManualStage] = []oneFn{typed(" 2 ")}

	instr, err := consultStaged(t, d, 3)

	disp := requireDispatch(t, instr, err)
	if disp.RowIndex != 2 || disp.Stage != 2 {
		t.Errorf("want RowIndex=2 Stage=2 from typed answers, got RowIndex=%d Stage=%d", disp.RowIndex, disp.Stage)
	}
}

func TestManualDialogue_StopOption_ReturnsStopInstructionAndAsksNothingFurther(t *testing.T) {
	d := newDialogue(t).standard("3", "1")
	d.one[domain.QuestionManualRow] = []oneFn{pick(domain.ManualStopOptionID)}

	instr, err := consultStaged(t, d, 3)

	if err != nil {
		t.Fatalf("want the stop option to be a routing instruction, not a failure; got %v", err)
	}
	if instr.Stop == nil || instr.Dispatch != nil {
		t.Fatalf("want exactly a stop instruction, got %+v", instr)
	}
	if instr.Stop.Reason != "user requested stop" {
		t.Errorf("want stop reason %q, got %q", "user requested stop", instr.Stop.Reason)
	}
	requireSequence(t, d, domain.QuestionManualRow)
}

func TestManualDialogue_TypedStopAtTheRowStep_IsTheStopOption(t *testing.T) {
	d := newDialogue(t).standard("3", "1")
	d.one[domain.QuestionManualRow] = []oneFn{typed("stop")}

	instr, err := consultStaged(t, d, 3)

	if err != nil || instr.Stop == nil {
		t.Fatalf("want a stop instruction for a typed stop, got %+v, %v", instr, err)
	}
	requireSequence(t, d, domain.QuestionManualRow)
}

func TestManualDialogue_ArtifactLists_ArePreCheckedWithTheDefaultsOfTheChosenRowAndStage(t *testing.T) {
	cases := []struct {
		name        string
		row, stage  string
		wantInputs  []string
		wantOutputs []string
	}{
		{"staged row stage 2", "3", "2", []string{"Plan.md", "Stage-2/Spec.md"}, []string{"Stage-2/Out.md"}},
		{"staged row stage 3", "3", "3", []string{"Plan.md", "Stage-3/Spec.md"}, []string{"Stage-3/Out.md"}},
		{"non-staged row", "4", "", []string{"Plan.md"}, []string{"Row4/Out.md"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d := newDialogue(t).standard(tc.row, tc.stage)

			instr, err := consultStaged(t, d, 3)

			requireDispatch(t, instr, err)
			in := d.manyQ(domain.QuestionManualInputs, 0)
			out := d.manyQ(domain.QuestionManualOutputs, 0)
			if !slices.Equal(in.DefaultOptionIDs, tc.wantInputs) {
				t.Errorf("inputs pre-checked: want %v, got %v", tc.wantInputs, in.DefaultOptionIDs)
			}
			if !slices.Equal(out.DefaultOptionIDs, tc.wantOutputs) {
				t.Errorf("outputs pre-checked: want %v, got %v", tc.wantOutputs, out.DefaultOptionIDs)
			}
			if !in.AllowCustom || !out.AllowCustom {
				t.Errorf("want free-form paths allowed on both lists, got inputs=%v outputs=%v", in.AllowCustom, out.AllowCustom)
			}
		})
	}
}

func TestManualDialogue_Candidates_AreRowDefaultsThenOtherListThenRegistryEachPathOnce(t *testing.T) {
	d := newDialogue(t).standard("3", "3")
	req := manualRequest(stagesUpTo(3))
	req.ArtifactRegistry = []domain.ArtifactRegistryEntry{
		{Artifact: "Plan.md", CreatedIn: "PLANNING", CreatedBy: "planner#1"},
		{Artifact: "Stage-1/Design.md", CreatedIn: "DESIGN.Stage-1", CreatedBy: "architect#4"},
		{Artifact: "Stage-3/Out.md", CreatedIn: "EXECUTION.Implementation.3", CreatedBy: "dev#9"},
	}

	instr, err := consultManual(t, d, stagedRoutingTable(), req)

	requireDispatch(t, instr, err)
	in := d.manyQ(domain.QuestionManualInputs, 0)
	wantIn := []string{"Plan.md", "Stage-3/Spec.md", "Stage-3/Out.md", "Stage-1/Design.md"}
	if !slices.Equal(optionIDs(in), wantIn) {
		t.Errorf("input candidates: want %v, got %v", wantIn, optionIDs(in))
	}
	for _, o := range in.Options {
		if o.Label != o.ID {
			t.Errorf("want label equal to the path for %q, got %q", o.ID, o.Label)
		}
	}
	requireOptions(t, in, 4)
	design := in.Options[3]
	if !strings.Contains(design.Description, "architect#4") || !strings.Contains(design.Description, "DESIGN.Stage-1") {
		t.Errorf("want a registry candidate described by creator and phase, got %q", design.Description)
	}
	if !strings.Contains(in.Options[0].Description, "row default") {
		t.Errorf("want a row default described as such, got %q", in.Options[0].Description)
	}
	out := d.manyQ(domain.QuestionManualOutputs, 0)
	wantOut := []string{"Stage-3/Out.md", "Plan.md", "Stage-3/Spec.md", "Stage-1/Design.md"}
	if !slices.Equal(optionIDs(out), wantOut) {
		t.Errorf("output candidates: want %v, got %v", wantOut, optionIDs(out))
	}
}

func TestManualDialogue_FreeFormPaths_AreTrimmedDeduplicatedAndPassedVerbatim(t *testing.T) {
	d := newDialogue(t).standard("4", "")
	d.many[domain.QuestionManualInputs] = []manyFn{func(interaction.ChoiceQuestion) interaction.MultiChoiceAnswer {
		return interaction.MultiChoiceAnswer{
			Status: interaction.Answered, OptionIDs: []string{"Plan.md"},
			Custom: []string{" new/in.md ", "", "Stage-*/Plan.md", "new/in.md", "Plan.md"},
		}
	}}
	d.many[domain.QuestionManualOutputs] = []manyFn{func(interaction.ChoiceQuestion) interaction.MultiChoiceAnswer {
		return interaction.MultiChoiceAnswer{Status: interaction.Answered}
	}}

	instr, err := consultStaged(t, d, 2)

	disp := requireDispatch(t, instr, err)
	wantIn := []string{"Plan.md", "new/in.md", "Stage-*/Plan.md"}
	if disp.InputArtifacts == nil || !slices.Equal(*disp.InputArtifacts, wantIn) {
		t.Errorf("want inputs %v (trimmed, duplicates and empties dropped, patterns kept), got %v", wantIn, disp.InputArtifacts)
	}
	if disp.OutputArtifacts == nil || len(*disp.OutputArtifacts) != 0 {
		t.Errorf("want an explicit empty output list for an empty selection, got %v", disp.OutputArtifacts)
	}
}

func TestManualDialogue_OnlyAnUntouchedSelectionIsNoOverride(t *testing.T) {
	cases := []struct {
		name         string
		answer       manyFn
		wantOverride []string // nil: no override expected
	}{
		{"defaults in order", acceptDefaults(), nil},
		{"defaults reversed", chooseMany("Stage-1/Spec.md", "Plan.md"), []string{"Stage-1/Spec.md", "Plan.md"}},
		{"subset of the defaults", chooseMany("Plan.md"), []string{"Plan.md"}},
		{"empty selection", chooseMany(), []string{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d := newDialogue(t).standard("3", "1")
			d.many[domain.QuestionManualInputs] = []manyFn{tc.answer}

			instr, err := consultStaged(t, d, 2)

			disp := requireDispatch(t, instr, err)
			switch {
			case tc.wantOverride == nil && disp.InputArtifacts != nil:
				t.Errorf("want no override, got %v", *disp.InputArtifacts)
			case tc.wantOverride != nil && (disp.InputArtifacts == nil || !slices.Equal(*disp.InputArtifacts, tc.wantOverride)):
				t.Errorf("want explicit %v, got %v", tc.wantOverride, disp.InputArtifacts)
			}
		})
	}
}

func TestManualDialogue_ConstraintsAndHITLChoices_MapToTheirOverrides(t *testing.T) {
	cases := []struct {
		name            string
		constraints     string
		hitl            string
		wantConstraints *string
		wantHITL        *bool
	}{
		{"empty constraints and default HITL", "", domain.ManualHITLDefault, nil, nil},
		{"constraints text", "no network", domain.ManualHITLDefault, strptr("no network"), nil},
		{"HITL forced on", "", domain.ManualHITLOn, nil, boolptr(true)},
		{"HITL forced off", "", domain.ManualHITLOff, nil, boolptr(false)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d := newDialogue(t).standard("4", "")
			d.text[domain.QuestionManualConstraints] = []textFn{say(tc.constraints)}
			d.one[domain.QuestionManualHITL] = []oneFn{pick(tc.hitl)}

			instr, err := consultStaged(t, d, 2)

			disp := requireDispatch(t, instr, err)
			if (disp.Constraints == nil) != (tc.wantConstraints == nil) ||
				(tc.wantConstraints != nil && *disp.Constraints != *tc.wantConstraints) {
				t.Errorf("constraints: want %v, got %v", tc.wantConstraints, disp.Constraints)
			}
			if (disp.HITLOverride == nil) != (tc.wantHITL == nil) ||
				(tc.wantHITL != nil && *disp.HITLOverride != *tc.wantHITL) {
				t.Errorf("HITL override: want %v, got %v", tc.wantHITL, disp.HITLOverride)
			}
		})
	}
}

func boolptr(b bool) *bool { return &b }

func TestManualDialogue_HITLQuestion_OffersNoOverrideOnOffAndPreselectsNoOverride(t *testing.T) {
	d := newDialogue(t).standard("3", "1")

	instr, err := consultStaged(t, d, 2)

	requireDispatch(t, instr, err)
	q := d.oneQ(domain.QuestionManualHITL, 0)
	want := []string{domain.ManualHITLDefault, domain.ManualHITLOn, domain.ManualHITLOff}
	if !slices.Equal(optionIDs(q), want) {
		t.Errorf("want HITL options %v, got %v", want, optionIDs(q))
	}
	if q.DefaultOptionID != domain.ManualHITLDefault {
		t.Errorf("want %q preselected, got %q", domain.ManualHITLDefault, q.DefaultOptionID)
	}
}

func TestManualDialogue_TaskQuestion_NamesTheAgentAndRejectsBlankText(t *testing.T) {
	d := newDialogue(t).standard("3", "1")

	instr, err := consultStaged(t, d, 2)

	requireDispatch(t, instr, err)
	q := d.textQ(domain.QuestionManualTask, 0)
	if !strings.Contains(q.Prompt, "dev") {
		t.Errorf("want the task prompt to name the agent, got %q", q.Prompt)
	}
	if q.Validate == nil {
		t.Fatal("want a validator on the task text field")
	}
	if q.Validate("   ") == nil {
		t.Error("want whitespace-only task text rejected")
	}
	if err := q.Validate("real text"); err != nil {
		t.Errorf("want non-blank task text accepted, got %v", err)
	}
}
