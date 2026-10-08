package deviation

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"mosaic-common/interaction"
	"mosaic-run/internal/domain"
)

// stepTask asks for the task message. It is required: an empty answer means
// the frontend cannot ask for text.
func (d *manualDialogue) stepTask(ctx context.Context) error {
	row := d.currentRow()
	ans, cancelled, err := d.askText(ctx, interaction.TextQuestion{
		Question: interaction.Question{
			ID:     domain.QuestionManualTask,
			Prompt: fmt.Sprintf("Enter the task description for %s (row %d):", row.Agent, d.rowNum),
		},
		Default: d.task,
		Validate: func(s string) error {
			if strings.TrimSpace(s) == "" {
				return errors.New("the task description must not be empty")
			}
			return nil
		},
	})
	if err != nil {
		return err
	}
	if cancelled {
		d.step = d.stageOrRow()
		return nil
	}
	text := strings.TrimSpace(ans.Text)
	if text == "" {
		return d.unavailable("the task description was answered empty", nil)
	}
	d.task = text
	d.step = stepInputs
	return nil
}

// stepConstraints asks for optional constraints; an empty answer means none.
func (d *manualDialogue) stepConstraints(ctx context.Context) error {
	ans, cancelled, err := d.askText(ctx, interaction.TextQuestion{
		Question: interaction.Question{
			ID:     domain.QuestionManualConstraints,
			Prompt: "Enter constraints for this dispatch (optional, leave empty for none):",
		},
		Default: d.constraints,
	})
	if err != nil {
		return err
	}
	if cancelled {
		d.step = stepOutputs
		return nil
	}
	d.constraints = strings.TrimSpace(ans.Text)
	d.step = stepHITL
	return nil
}

// finish validates the complete result and builds the dispatch instruction. A
// rejected result sends the user back to the step that can correct it.
func (d *manualDialogue) finish(ctx context.Context) error {
	row := d.currentRow()
	resolved, vErr := domain.ValidateDispatchTarget(d.table, d.req.Stages, domain.DispatchTarget{
		Agent: row.Agent, Row: domain.WorkflowRow(d.rowNum), Stage: d.stage,
	})
	if vErr != nil {
		return d.invalidResult(ctx, describeTargetError(vErr), targetErrorStep(vErr, d.stageOrRow()))
	}
	defaults, err := d.rowDefaults()
	if err != nil {
		return d.invalidResult(ctx, "the row's defaults cannot be resolved: "+err.Error(), d.stageOrRow())
	}
	instr := &domain.DispatchInstruction{
		Agent:           resolved.Row.Agent,
		RowIndex:        resolved.Row.Index,
		Stage:           resolved.StageNumber,
		TaskDescription: d.task,
		InputArtifacts:  listOverride(d.inputs, defaults.InputArtifacts),
		OutputArtifacts: listOverride(d.outputs, defaults.OutputArtifacts),
	}
	if d.constraints != "" {
		instr.Constraints = &d.constraints
	}
	switch d.hitl {
	case domain.ManualHITLOn:
		on := true
		instr.HITLOverride = &on
	case domain.ManualHITLOff:
		off := false
		instr.HITLOverride = &off
	}
	d.result = &domain.RoutingInstruction{Dispatch: instr}
	return nil
}

// targetErrorStep is the step that corrects a rejected target: the stage step
// (fallback) for a stage problem, else the row step.
func targetErrorStep(err error, stageStep manualStep) manualStep {
	var te *domain.DispatchTargetError
	if errors.As(err, &te) {
		switch te.Reason {
		case domain.ReasonMissingStage, domain.ReasonUnknownStage, domain.ReasonStageGroupMismatch:
			return stageStep
		}
	}
	return stepRow
}
