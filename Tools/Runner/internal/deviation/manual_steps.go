package deviation

import (
	"context"
	"fmt"
	"strings"

	"mosaic-common/interaction"
	"mosaic-run/internal/domain"
)

// ManualInvalidResultLimit bounds invalid results in one manual routing
// attempt (one ConsultRouting call). The user is shown the reason and taken
// back; the (ManualInvalidResultLimit+1)-th invalid result ends routing with
// domain.ConsultFailManualBoundExceeded instead of re-prompting. Accepted
// answers to individual steps do not reset the count.
const ManualInvalidResultLimit = 3

// ManualPromptLimit is the last-resort guard against an Interaction that keeps
// answering without converging: at most ManualPromptLimit question calls
// (SelectOne, SelectMany, AskText) per routing attempt. When a further question
// would be needed, routing ends with domain.ConsultFailManualBoundExceeded
// without asking it.
const ManualPromptLimit = 64

// manualTitle is the title of every manual routing question and notice.
const manualTitle = "Manual Routing"

// manualStep is one step of the manual routing dialogue, in dialogue order.
type manualStep int

const (
	stepRow manualStep = iota
	stepStage
	stepTask
	stepInputs
	stepOutputs
	stepConstraints
	stepHITL
)

// listState is the user's selection on one artifact list step. set is false
// until the step has been answered for the current row and stage.
type listState struct {
	set   bool
	paths []string
}

// manualDialogue is the state machine behind ManualResolver.ConsultRouting.
// Each step is a method that asks one question and moves step forward, back
// (Esc) or, for a rejected answer, to the step that can correct it.
type manualDialogue struct {
	in    domain.Interaction
	table domain.RoutingTable
	req   domain.ConsultationRequest

	step   manualStep
	result *domain.RoutingInstruction

	prompts int    // question calls made so far
	invalid int    // invalid results so far
	reason  string // why the user was taken back; shown on the next question

	rowNum      int    // chosen 1-based row; 0 until chosen
	rowChoice   string // the row answer, to pre-select it when asked again
	stage       domain.StageNumber
	stageChoice string
	task        string
	inputs      listState
	outputs     listState
	constraints string
	hitl        string
}

func newManualDialogue(m *ManualResolver, req domain.ConsultationRequest) *manualDialogue {
	return &manualDialogue{in: m.Interact, table: m.Table, req: req, step: stepRow, hitl: domain.ManualHITLDefault}
}

// run drives the steps until a result exists or routing ends.
func (d *manualDialogue) run(ctx context.Context) (domain.RoutingInstruction, error) {
	for d.result == nil {
		if err := ctx.Err(); err != nil {
			return domain.RoutingInstruction{}, d.unavailable("the context ended: "+err.Error(), err)
		}
		if err := d.runStep(ctx); err != nil {
			return domain.RoutingInstruction{}, err
		}
	}
	return *d.result, nil
}

func (d *manualDialogue) runStep(ctx context.Context) error {
	switch d.step {
	case stepRow:
		return d.stepRow(ctx)
	case stepStage:
		return d.stepStage(ctx)
	case stepTask:
		return d.stepTask(ctx)
	case stepInputs:
		return d.stepInputs(ctx)
	case stepOutputs:
		return d.stepOutputs(ctx)
	case stepConstraints:
		return d.stepConstraints(ctx)
	default:
		return d.stepHITL(ctx)
	}
}

// ---- ending outcomes ----

func (d *manualDialogue) abandoned(detail string) error {
	return &domain.ConsultationError{Failure: domain.ConsultFailUserAbandoned, Detail: detail}
}

func (d *manualDialogue) unavailable(detail string, cause error) error {
	return &domain.ConsultationError{Failure: domain.ConsultFailInteractionUnavailable, Detail: "manual routing is unavailable: " + detail, Err: cause}
}

func (d *manualDialogue) boundExceeded(detail string) error {
	return &domain.ConsultationError{Failure: domain.ConsultFailManualBoundExceeded, Detail: "manual routing gave up: " + detail}
}

// invalidResult reports an invalid result to the user and sends the dialogue
// back to step, or ends routing once ManualInvalidResultLimit is exceeded.
func (d *manualDialogue) invalidResult(ctx context.Context, reason string, step manualStep) error {
	d.invalid++
	if d.invalid > ManualInvalidResultLimit {
		return d.boundExceeded(fmt.Sprintf("more than %d invalid results; the last one: %s", ManualInvalidResultLimit, reason))
	}
	d.in.Notify(ctx, interaction.Notice{Level: interaction.NoticeWarning, Title: manualTitle, Message: reason})
	d.reason = reason
	switch step {
	case stepRow:
		d.rowChoice = ""
	case stepStage:
		d.stageChoice = ""
	}
	d.step = step
	return nil
}

// ---- asking ----

// beginPrompt counts one question call and ends routing at the prompt bound.
func (d *manualDialogue) beginPrompt() error {
	if d.prompts >= ManualPromptLimit {
		return d.boundExceeded(fmt.Sprintf("more than %d questions in one routing attempt", ManualPromptLimit))
	}
	d.prompts++
	return nil
}

// takeReason returns the reason to show on the next question, once.
func (d *manualDialogue) takeReason() string {
	r := d.reason
	d.reason = ""
	return r
}

// classify maps an answer status to "cancelled" or an ending error: only
// Answered and Cancelled are usable answers.
func (d *manualDialogue) classify(id interaction.QuestionID, status interaction.AnswerStatus) (cancelled bool, err error) {
	switch status {
	case interaction.Answered:
		return false, nil
	case interaction.Cancelled:
		return true, nil
	default:
		return false, d.unavailable(fmt.Sprintf("the %s question was answered with status %q", id, status), nil)
	}
}

func (d *manualDialogue) askOne(ctx context.Context, q interaction.ChoiceQuestion) (ans interaction.ChoiceAnswer, cancelled bool, err error) {
	if err = d.beginPrompt(); err != nil {
		return ans, false, err
	}
	q.Title, q.Detail = manualTitle, d.takeReason()
	ans, callErr := d.in.SelectOne(ctx, q)
	if callErr != nil {
		return ans, false, d.unavailable(fmt.Sprintf("the %s question failed: %v", q.ID, callErr), callErr)
	}
	cancelled, err = d.classify(q.ID, ans.Status)
	return ans, cancelled, err
}

func (d *manualDialogue) askMany(ctx context.Context, q interaction.ChoiceQuestion) (ans interaction.MultiChoiceAnswer, cancelled bool, err error) {
	if err = d.beginPrompt(); err != nil {
		return ans, false, err
	}
	q.Title, q.Detail = manualTitle, d.takeReason()
	ans, callErr := d.in.SelectMany(ctx, q)
	if callErr != nil {
		return ans, false, d.unavailable(fmt.Sprintf("the %s question failed: %v", q.ID, callErr), callErr)
	}
	cancelled, err = d.classify(q.ID, ans.Status)
	return ans, cancelled, err
}

func (d *manualDialogue) askText(ctx context.Context, q interaction.TextQuestion) (ans interaction.TextAnswer, cancelled bool, err error) {
	if err = d.beginPrompt(); err != nil {
		return ans, false, err
	}
	q.Title, q.Detail = manualTitle, d.takeReason()
	ans, callErr := d.in.AskText(ctx, q)
	if callErr != nil {
		return ans, false, d.unavailable(fmt.Sprintf("the %s question failed: %v", q.ID, callErr), callErr)
	}
	cancelled, err = d.classify(q.ID, ans.Status)
	return ans, cancelled, err
}

// chosenValue is the trimmed value of a select-one answer: the picked option,
// else the typed free-form entry; "" when the answer carries neither.
func chosenValue(a interaction.ChoiceAnswer) string {
	if v := strings.TrimSpace(a.OptionID); v != "" {
		return v
	}
	return strings.TrimSpace(a.Custom)
}
