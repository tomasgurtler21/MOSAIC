package deviation

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"mosaic-common/interaction"
	"mosaic-run/internal/domain"
)

// currentRow is the chosen routing table row. Only valid once rowNum is set.
func (d *manualDialogue) currentRow() domain.RoutingRow {
	return d.table.Rows[d.rowNum-1]
}

// stageOrRow is the step that can correct the chosen target: the stage step
// for a staged row, else the row step.
func (d *manualDialogue) stageOrRow() manualStep {
	if d.rowNum > 0 && d.currentRow().PhaseParsed.IsStaged {
		return stepStage
	}
	return stepRow
}

// setRow records the chosen row. Choosing a different row discards the stage
// and the artifact selections made for the previous one; the task is kept.
func (d *manualDialogue) setRow(n int, choice string) {
	if n != d.rowNum {
		d.stage, d.stageChoice = 0, ""
		d.inputs, d.outputs = listState{}, listState{}
	}
	d.rowNum, d.rowChoice = n, choice
}

// setStage records the chosen stage; a different stage discards the artifact
// selections made for the previous one.
func (d *manualDialogue) setStage(n domain.StageNumber, choice string) {
	if n != d.stage {
		d.inputs, d.outputs = listState{}, listState{}
	}
	d.stage, d.stageChoice = n, choice
}

// rowDefaults resolves the default payload of the chosen row and stage. Without
// a defaults function the payload is empty.
func (d *manualDialogue) rowDefaults() (domain.DispatchDefaults, error) {
	if d.req.RowDefaults == nil {
		return domain.DispatchDefaults{}, nil
	}
	return d.req.RowDefaults(d.currentRow(), d.stage)
}

// ---- row ----

func (d *manualDialogue) stepRow(ctx context.Context) error {
	ans, cancelled, err := d.askOne(ctx, d.rowQuestion())
	if err != nil {
		return err
	}
	if cancelled {
		return d.abandoned("the user cancelled manual routing at the row choice")
	}
	choice := chosenValue(ans)
	if choice == "" {
		return d.unavailable("the row question was answered without a choice", nil)
	}
	if strings.EqualFold(choice, domain.ManualStopOptionID) {
		d.result = &domain.RoutingInstruction{Stop: &domain.StopInstruction{Reason: "user requested stop"}}
		return nil
	}
	n, convErr := strconv.Atoi(choice)
	if convErr != nil || n < 1 || n > len(d.table.Rows) {
		return d.invalidResult(ctx, fmt.Sprintf("row choice %q is not a row of the routing table (1 to %d)", choice, len(d.table.Rows)), stepRow)
	}
	d.setRow(n, choice)
	d.step = stepTask
	if d.currentRow().PhaseParsed.IsStaged {
		d.step = stepStage
	}
	return nil
}

func (d *manualDialogue) rowQuestion() interaction.ChoiceQuestion {
	options := make([]interaction.Option, 0, len(d.table.Rows)+1)
	for i, row := range d.table.Rows {
		options = append(options, interaction.Option{
			ID:    strconv.Itoa(i + 1),
			Label: fmt.Sprintf("[%d] %s (%s)", i+1, row.Agent, row.Phase),
		})
	}
	options = append(options, interaction.Option{ID: domain.ManualStopOptionID, Label: "Stop the run"})
	return interaction.ChoiceQuestion{
		Question: interaction.Question{
			ID:              domain.QuestionManualRow,
			Prompt:          "Choose which row to dispatch next, or stop the run:",
			AllowCustom:     true,
			CustomPrompt:    "Row number",
			DefaultOptionID: d.rowPreselection(),
		},
		Options: options,
	}
}

// rowPreselection is the earlier row answer, else the row that deviated.
func (d *manualDialogue) rowPreselection() string {
	if d.rowChoice != "" {
		return d.rowChoice
	}
	dev := d.req.Deviation
	if dev == nil || dev.CurrentRow < 0 || dev.CurrentRow >= len(d.table.Rows) {
		return ""
	}
	return strconv.Itoa(dev.CurrentRow + 1)
}

// ---- stage ----

func (d *manualDialogue) stepStage(ctx context.Context) error {
	stages := d.req.Stages
	if stages == nil || stages.Count() == 0 {
		return d.invalidResult(ctx, "the chosen row is a staged row but no stage set is available", stepRow)
	}
	ans, cancelled, err := d.askOne(ctx, d.stageQuestion(stages))
	if err != nil {
		return err
	}
	if cancelled {
		d.step = stepRow
		return nil
	}
	choice := chosenValue(ans)
	if choice == "" {
		return d.unavailable("the stage question was answered without a choice", nil)
	}
	n, convErr := strconv.Atoi(choice)
	if convErr != nil || n < 1 {
		return d.invalidResult(ctx, fmt.Sprintf("stage choice %q is not a stage number", choice), stepStage)
	}
	d.setStage(domain.StageNumber(n), choice)
	d.step = stepTask
	return nil
}

func (d *manualDialogue) stageQuestion(stages *domain.StageSet) interaction.ChoiceQuestion {
	options := make([]interaction.Option, 0, stages.Count())
	for _, n := range stages.Numbers() {
		options = append(options, interaction.Option{ID: strconv.Itoa(int(n)), Label: fmt.Sprintf("Stage %d", n)})
	}
	return interaction.ChoiceQuestion{
		Question: interaction.Question{
			ID:              domain.QuestionManualStage,
			Prompt:          "Choose the stage to dispatch:",
			AllowCustom:     true,
			CustomPrompt:    "Stage number",
			DefaultOptionID: d.stagePreselection(stages),
		},
		Options: options,
	}
}

// stagePreselection is the earlier stage answer, else the stage that deviated
// when it is of the chosen row's group and still in the stage set.
func (d *manualDialogue) stagePreselection(stages *domain.StageSet) string {
	if d.stageChoice != "" {
		return d.stageChoice
	}
	dev := d.req.Deviation
	if dev == nil {
		return ""
	}
	group, n, ok := domain.ParseStageValue(dev.CurrentStage)
	if !ok || group != d.currentRow().PhaseParsed.Group {
		return ""
	}
	if _, inSet := stages.Entry(n); !inSet {
		return ""
	}
	return strconv.Itoa(int(n))
}

// ---- HITL ----

func (d *manualDialogue) stepHITL(ctx context.Context) error {
	defaults, err := d.rowDefaults()
	if err != nil {
		return d.invalidResult(ctx, "the row's defaults cannot be resolved: "+err.Error(), d.stageOrRow())
	}
	ans, cancelled, askErr := d.askOne(ctx, d.hitlQuestion(defaults.HITL))
	if askErr != nil {
		return askErr
	}
	if cancelled {
		d.step = stepConstraints
		return nil
	}
	choice := chosenValue(ans)
	switch choice {
	case "":
		return d.unavailable("the HITL question was answered without a choice", nil)
	case domain.ManualHITLDefault, domain.ManualHITLOn, domain.ManualHITLOff:
		d.hitl = choice
		return d.finish(ctx)
	}
	return d.invalidResult(ctx, fmt.Sprintf("HITL choice %q is not one of default, on, off", choice), stepHITL)
}

func (d *manualDialogue) hitlQuestion(rowHITL bool) interaction.ChoiceQuestion {
	setting := "off"
	if rowHITL {
		setting = "on"
	}
	return interaction.ChoiceQuestion{
		Question: interaction.Question{
			ID:              domain.QuestionManualHITL,
			Prompt:          "Human in the loop for this dispatch:",
			DefaultOptionID: d.hitl,
		},
		Options: []interaction.Option{
			{ID: domain.ManualHITLDefault, Label: "No override", Description: "use the row's setting (" + setting + ")"},
			{ID: domain.ManualHITLOn, Label: "On"},
			{ID: domain.ManualHITLOff, Label: "Off"},
		},
	}
}
