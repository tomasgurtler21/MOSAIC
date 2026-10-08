package deviation_test

// Tests for Esc (a Cancelled answer) at every step of the manual routing
// dialogue: it steps back, and at the first step it ends routing.

import (
	"slices"
	"testing"

	"mosaic-common/interaction"
	"mosaic-run/internal/deviation"
	"mosaic-run/internal/domain"
)

// questionAfterFirst returns the question asked right after the first call
// of id, or "" when id is not asked or is the last call.
func questionAfterFirst(calls []interaction.QuestionID, id interaction.QuestionID) interaction.QuestionID {
	i := slices.Index(calls, id)
	if i < 0 || i+1 >= len(calls) {
		return ""
	}
	return calls[i+1]
}

func TestManualDialogue_EscAtAStep_ReturnsToThePreviousStepAndRoutingStillCompletes(t *testing.T) {
	cases := []struct {
		name     string
		row      string
		stage    string
		cancelAt interaction.QuestionID
		wantNext interaction.QuestionID
	}{
		{"stage goes back to row", "3", "2", domain.QuestionManualStage, domain.QuestionManualRow},
		{"task of a staged row goes back to stage", "3", "2", domain.QuestionManualTask, domain.QuestionManualStage},
		{"task of a non-staged row goes back to row", "4", "", domain.QuestionManualTask, domain.QuestionManualRow},
		{"inputs go back to task", "3", "2", domain.QuestionManualInputs, domain.QuestionManualTask},
		{"outputs go back to inputs", "3", "2", domain.QuestionManualOutputs, domain.QuestionManualInputs},
		{"constraints go back to outputs", "3", "2", domain.QuestionManualConstraints, domain.QuestionManualOutputs},
		{"HITL goes back to constraints", "3", "2", domain.QuestionManualHITL, domain.QuestionManualConstraints},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d := newDialogue(t).standard(tc.row, tc.stage).cancelFirst(tc.cancelAt)

			instr, err := consultStaged(t, d, 3)

			if got := questionAfterFirst(d.calls, tc.cancelAt); got != tc.wantNext {
				t.Errorf("after Esc at %s want %s asked next, got %s (sequence %v)", tc.cancelAt, tc.wantNext, got, d.calls)
			}
			disp := requireDispatch(t, instr, err)
			if tc.stage == "2" && (disp.RowIndex != 2 || disp.Stage != 2) {
				t.Errorf("want the dialogue to complete at RowIndex=2 Stage=2, got RowIndex=%d Stage=%d", disp.RowIndex, disp.Stage)
			}
		})
	}
}

func TestManualDialogue_EscAtTheFirstStep_CancelsRoutingWithoutAnInstruction(t *testing.T) {
	d := newDialogue(t).standard("3", "2").cancelFirst(domain.QuestionManualRow)

	instr, err := consultStaged(t, d, 3)

	requireEnding(t, instr, err, domain.ConsultFailUserAbandoned)
	requireSequence(t, d, domain.QuestionManualRow)
	if ce := assertConsultationError(t, err, domain.ConsultFailUserAbandoned); ce.Detail == "" {
		t.Error("want the cancellation to name its case in Detail")
	}
}

func TestManualDialogue_RepeatedEsc_WalksBackStepByStepAndCancelsAtTheRow(t *testing.T) {
	d := newDialogue(t).standard("3", "2")
	d.one[domain.QuestionManualRow] = []oneFn{pick("3"), cancelOne}
	d.one[domain.QuestionManualStage] = []oneFn{pick("2"), cancelOne}
	d.text[domain.QuestionManualTask] = []textFn{say("do the work"), cancelText}
	d.many[domain.QuestionManualInputs] = []manyFn{cancelMany}

	instr, err := consultStaged(t, d, 3)

	requireEnding(t, instr, err, domain.ConsultFailUserAbandoned)
	requireSequence(t, d,
		domain.QuestionManualRow, domain.QuestionManualStage, domain.QuestionManualTask,
		domain.QuestionManualInputs,
		domain.QuestionManualTask, domain.QuestionManualStage, domain.QuestionManualRow)
}

func TestManualDialogue_EscIsNotAnInvalidResult_RepeatedEscStillCompletes(t *testing.T) {
	d := newDialogue(t).standard("3", "2")
	var constraints []textFn
	for i := 0; i < deviation.ManualInvalidResultLimit+2; i++ {
		constraints = append(constraints, cancelText)
	}
	d.text[domain.QuestionManualConstraints] = append(constraints, say("final constraint"))

	instr, err := consultStaged(t, d, 3)

	disp := requireDispatch(t, instr, err)
	if disp.Constraints == nil || *disp.Constraints != "final constraint" {
		t.Errorf("want the constraint entered after the repeated Esc, got %v", disp.Constraints)
	}
}

func TestManualDialogue_ReturningToAStep_PrefillsThePreviousAnswer(t *testing.T) {
	cases := []struct {
		name  string
		build func(d *dialogue)
		check func(t *testing.T, d *dialogue)
	}{
		{"row preselected after Esc at stage",
			func(d *dialogue) { d.cancelFirst(domain.QuestionManualStage) },
			func(t *testing.T, d *dialogue) {
				if got := d.oneQ(domain.QuestionManualRow, 1).DefaultOptionID; got != "3" {
					t.Errorf("want the earlier row choice preselected, got %q", got)
				}
			}},
		{"stage preselected after Esc at task",
			func(d *dialogue) { d.cancelFirst(domain.QuestionManualTask) },
			func(t *testing.T, d *dialogue) {
				if got := d.oneQ(domain.QuestionManualStage, 1).DefaultOptionID; got != "2" {
					t.Errorf("want the earlier stage choice preselected, got %q", got)
				}
			}},
		{"task text kept after Esc at inputs",
			func(d *dialogue) {
				d.text[domain.QuestionManualTask] = []textFn{say("write it"), keepDefault}
				d.many[domain.QuestionManualInputs] = []manyFn{cancelMany, acceptDefaults()}
			},
			func(t *testing.T, d *dialogue) {
				if got := d.textQ(domain.QuestionManualTask, 1).Default; got != "write it" {
					t.Errorf("want the earlier task text pre-filled, got %q", got)
				}
			}},
		{"input selection kept after Esc at outputs",
			func(d *dialogue) {
				d.many[domain.QuestionManualInputs] = []manyFn{acceptDefaultsPlus("x/y.md"), acceptDefaults()}
				d.many[domain.QuestionManualOutputs] = []manyFn{cancelMany, acceptDefaults()}
			},
			func(t *testing.T, d *dialogue) {
				q := d.manyQ(domain.QuestionManualInputs, 1)
				want := []string{"Plan.md", "Stage-2/Spec.md", "x/y.md"}
				if !slices.Equal(q.DefaultOptionIDs, want) {
					t.Errorf("want the earlier selection pre-checked %v, got %v", want, q.DefaultOptionIDs)
				}
				if !slices.Contains(optionIDs(q), "x/y.md") {
					t.Errorf("want the earlier free-form path offered as an option, got %v", optionIDs(q))
				}
			}},
		{"constraints text kept after Esc at HITL",
			func(d *dialogue) {
				d.text[domain.QuestionManualConstraints] = []textFn{say("no network"), keepDefault}
				d.cancelFirst(domain.QuestionManualHITL)
			},
			func(t *testing.T, d *dialogue) {
				if got := d.textQ(domain.QuestionManualConstraints, 1).Default; got != "no network" {
					t.Errorf("want the earlier constraints pre-filled, got %q", got)
				}
			}},
		{"HITL asked again after Esc at it and the constraints answered again",
			func(d *dialogue) {
				d.one[domain.QuestionManualHITL] = []oneFn{cancelOne, pick(domain.ManualHITLOff)}
				d.text[domain.QuestionManualConstraints] = []textFn{say("c")}
			},
			func(t *testing.T, d *dialogue) {
				if len(d.oneQs[domain.QuestionManualHITL]) != 2 {
					t.Fatalf("want the HITL question asked twice, got %d", len(d.oneQs[domain.QuestionManualHITL]))
				}
			}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d := newDialogue(t).standard("3", "2")
			tc.build(d)

			instr, err := consultStaged(t, d, 3)

			requireDispatch(t, instr, err)
			tc.check(t, d)
		})
	}
}

func TestManualDialogue_ChangingTheStageOnTheWayBack_DiscardsSelectionsButKeepsTheTask(t *testing.T) {
	d := newDialogue(t).standard("3", "3")
	d.one[domain.QuestionManualStage] = []oneFn{pick("3"), pick("2")}
	d.text[domain.QuestionManualTask] = []textFn{say("T"), cancelText, keepDefault}
	d.many[domain.QuestionManualInputs] = []manyFn{acceptDefaultsPlus("extra/x.md"), cancelMany, acceptDefaults()}
	d.many[domain.QuestionManualOutputs] = []manyFn{cancelMany, acceptDefaults()}

	instr, err := consultStaged(t, d, 3)

	disp := requireDispatch(t, instr, err)
	requireSequence(t, d,
		domain.QuestionManualRow, domain.QuestionManualStage, domain.QuestionManualTask,
		domain.QuestionManualInputs, domain.QuestionManualOutputs, // Esc at outputs
		domain.QuestionManualInputs,                               // Esc at inputs
		domain.QuestionManualTask,                                 // Esc at task
		domain.QuestionManualStage, domain.QuestionManualTask,
		domain.QuestionManualInputs, domain.QuestionManualOutputs,
		domain.QuestionManualConstraints, domain.QuestionManualHITL)
	if got := d.oneQ(domain.QuestionManualStage, 1).DefaultOptionID; got != "3" {
		t.Errorf("want the earlier stage preselected when the stage is asked again, got %q", got)
	}
	inputs := []interaction.ChoiceQuestion{d.manyQ(domain.QuestionManualInputs, 0), d.manyQ(domain.QuestionManualInputs, 1), d.manyQ(domain.QuestionManualInputs, 2)}
	if want := []string{"Plan.md", "Stage-2/Spec.md"}; !slices.Equal(inputs[2].DefaultOptionIDs, want) {
		t.Errorf("want the new stage's defaults %v pre-checked, got %v", want, inputs[2].DefaultOptionIDs)
	}
	if slices.Contains(optionIDs(inputs[2]), "extra/x.md") || slices.Contains(inputs[2].DefaultOptionIDs, "extra/x.md") {
		t.Errorf("want the selection made for stage 3 discarded, got %v", inputs[2].DefaultOptionIDs)
	}
	if got := d.textQ(domain.QuestionManualTask, 2).Default; got != "T" {
		t.Errorf("want the task text kept, got %q", got)
	}
	if disp.Stage != 2 || disp.TaskDescription != "T" || disp.InputArtifacts != nil {
		t.Errorf("want Stage=2 task T and untouched input defaults, got Stage=%d task=%q inputs=%v",
			disp.Stage, disp.TaskDescription, disp.InputArtifacts)
	}
}

func TestManualDialogue_ChangingTheRowOnTheWayBack_AsksTheNewRowsDefaults(t *testing.T) {
	d := newDialogue(t).standard("3", "1")
	d.one[domain.QuestionManualRow] = []oneFn{pick("3"), pick("4")}
	d.text[domain.QuestionManualTask] = []textFn{say("T"), cancelText, keepDefault}
	d.many[domain.QuestionManualInputs] = []manyFn{cancelMany, acceptDefaults()}
	d.one[domain.QuestionManualStage] = []oneFn{pick("1"), cancelOne}

	instr, err := consultStaged(t, d, 3)

	disp := requireDispatch(t, instr, err)
	requireSequence(t, d,
		domain.QuestionManualRow, domain.QuestionManualStage, domain.QuestionManualTask,
		domain.QuestionManualInputs, domain.QuestionManualTask, domain.QuestionManualStage,
		domain.QuestionManualRow, domain.QuestionManualTask,
		domain.QuestionManualInputs, domain.QuestionManualOutputs,
		domain.QuestionManualConstraints, domain.QuestionManualHITL)
	inputs := []interaction.ChoiceQuestion{d.manyQ(domain.QuestionManualInputs, 0), d.manyQ(domain.QuestionManualInputs, 1)}
	if want := []string{"Plan.md"}; !slices.Equal(inputs[1].DefaultOptionIDs, want) {
		t.Errorf("want the defaults of the new row %v pre-checked, got %v", want, inputs[1].DefaultOptionIDs)
	}
	if disp.RowIndex != 3 || disp.Stage != 0 || disp.TaskDescription != "T" {
		t.Errorf("want reviewer row 4 with the kept task, got RowIndex=%d Stage=%d task=%q", disp.RowIndex, disp.Stage, disp.TaskDescription)
	}
}
