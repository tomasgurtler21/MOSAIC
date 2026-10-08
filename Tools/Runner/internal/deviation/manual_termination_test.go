package deviation_test

// Termination tests: manual routing must end, within a bounded number of
// Interaction calls and without dispatching, whatever Interaction drives it.
// Every run is guarded by a short deadline so a loop fails the test.

import (
	"context"
	"errors"
	"testing"
	"time"

	"mosaic-common/interaction"
	"mosaic-run/internal/deviation"
	"mosaic-run/internal/domain"
)

type reply int

const (
	replyValid reply = iota
	replyCancelled
	replyUnsupported
	replySkipped
	replySkippedAll
	replyEmptyAnswered
	replyError
)

var errFrontendBroke = errors.New("frontend broke")

// loopInteraction answers every question from decide(callNumber, questionID).
// A valid answer is a complete, sensible one for the staged row 3 / stage 2.
type loopInteraction struct {
	t      *testing.T
	decide func(n int, id interaction.QuestionID) reply
	calls  []interaction.QuestionID
}

func (l *loopInteraction) next(id interaction.QuestionID) reply {
	l.calls = append(l.calls, id)
	if len(l.calls) > 4*deviation.ManualPromptLimit {
		l.t.Fatalf("more than %d Interaction calls; routing does not terminate", 4*deviation.ManualPromptLimit)
	}
	return l.decide(len(l.calls), id)
}

func status(r reply) interaction.AnswerStatus {
	switch r {
	case replyCancelled:
		return interaction.Cancelled
	case replyUnsupported:
		return interaction.Unsupported
	case replySkipped:
		return interaction.SkippedOne
	case replySkippedAll:
		return interaction.SkippedAll
	default:
		return interaction.Answered
	}
}

func (l *loopInteraction) SelectOne(_ context.Context, q interaction.ChoiceQuestion) (interaction.ChoiceAnswer, error) {
	r := l.next(q.ID)
	if r == replyError {
		return interaction.ChoiceAnswer{}, errFrontendBroke
	}
	ans := interaction.ChoiceAnswer{Status: status(r)}
	if r != replyValid {
		return ans, nil
	}
	switch q.ID {
	case domain.QuestionManualRow:
		ans.OptionID = "3"
	case domain.QuestionManualStage:
		ans.OptionID = "2"
	default:
		ans.OptionID = domain.ManualHITLDefault
	}
	return ans, nil
}

func (l *loopInteraction) SelectMany(_ context.Context, q interaction.ChoiceQuestion) (interaction.MultiChoiceAnswer, error) {
	r := l.next(q.ID)
	if r == replyError {
		return interaction.MultiChoiceAnswer{}, errFrontendBroke
	}
	ans := interaction.MultiChoiceAnswer{Status: status(r)}
	if r == replyValid {
		ans.OptionIDs = q.DefaultOptionIDs
	}
	return ans, nil
}

func (l *loopInteraction) AskText(_ context.Context, q interaction.TextQuestion) (interaction.TextAnswer, error) {
	r := l.next(q.ID)
	if r == replyError {
		return interaction.TextAnswer{}, errFrontendBroke
	}
	ans := interaction.TextAnswer{Status: status(r)}
	if r == replyValid && q.ID == domain.QuestionManualTask {
		ans.Text = "do the work"
	}
	return ans, nil
}

func (l *loopInteraction) Confirm(context.Context, interaction.Question) (interaction.ConfirmAnswer, error) {
	return interaction.ConfirmAnswer{Status: interaction.Unsupported}, nil
}
func (l *loopInteraction) Notify(context.Context, interaction.Notice)          {}
func (l *loopInteraction) Progress(context.Context, interaction.ProgressEvent) {}

// runGuarded runs manual routing with a short deadline and fails the test when
// the deadline passes.
func runGuarded(t *testing.T, in interaction.Interaction) (domain.RoutingInstruction, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	m := &deviation.ManualResolver{Interact: in, Table: stagedRoutingTable()}
	instr, err := m.ConsultRouting(ctx, manualRequest(stagesUpTo(3)))
	if ctx.Err() != nil {
		t.Fatalf("manual routing did not end before the deadline: %v", ctx.Err())
	}
	return instr, err
}

func always(r reply) func(int, interaction.QuestionID) reply {
	return func(int, interaction.QuestionID) reply { return r }
}

func TestManualTermination_AnUnavailableAnswer_EndsAtTheFirstQuestionWithoutRepeating(t *testing.T) {
	for name, r := range map[string]reply{
		"unsupported":       replyUnsupported,
		"skipped":           replySkipped,
		"skipped all":       replySkippedAll,
		"empty answered":    replyEmptyAnswered,
		"interaction error": replyError,
	} {
		t.Run(name, func(t *testing.T) {
			in := &loopInteraction{t: t, decide: always(r)}

			instr, err := runGuarded(t, in)

			requireEnding(t, instr, err, domain.ConsultFailInteractionUnavailable)
			if len(in.calls) != 1 {
				t.Errorf("want the dialogue not repeated (1 Interaction call), got %d: %v", len(in.calls), in.calls)
			}
			if ce := assertConsultationError(t, err, domain.ConsultFailInteractionUnavailable); ce.Detail == "" {
				t.Error("want the case named in Detail")
			}
		})
	}
}

func TestManualTermination_InteractionError_IsKeptAsTheCause(t *testing.T) {
	in := &loopInteraction{t: t, decide: always(replyError)}

	_, err := runGuarded(t, in)

	assertConsultationError(t, err, domain.ConsultFailInteractionUnavailable)
	if !errors.Is(err, errFrontendBroke) {
		t.Errorf("want the Interaction error wrapped in the failure, got %v", err)
	}
}

func TestManualTermination_UnavailableAtALaterStep_EndsAtThatStep(t *testing.T) {
	steps := []interaction.QuestionID{
		domain.QuestionManualStage, domain.QuestionManualTask, domain.QuestionManualInputs,
		domain.QuestionManualOutputs, domain.QuestionManualHITL,
	}
	for _, failing := range steps {
		for name, r := range map[string]reply{"unsupported": replyUnsupported, "skipped": replySkipped} {
			t.Run(string(failing)+" "+name, func(t *testing.T) {
				in := &loopInteraction{t: t, decide: func(_ int, id interaction.QuestionID) reply {
					if id == failing {
						return r
					}
					return replyValid
				}}

				instr, err := runGuarded(t, in)

				requireEnding(t, instr, err, domain.ConsultFailInteractionUnavailable)
				if last := in.calls[len(in.calls)-1]; last != failing {
					t.Errorf("want routing to end at the first unanswerable step %s, last asked %s", failing, last)
				}
			})
		}
	}
}

func TestManualTermination_EmptyAnswerOnARequiredStep_IsUnavailable(t *testing.T) {
	for _, failing := range []interaction.QuestionID{
		domain.QuestionManualRow, domain.QuestionManualStage, domain.QuestionManualTask, domain.QuestionManualHITL,
	} {
		t.Run(string(failing), func(t *testing.T) {
			in := &loopInteraction{t: t, decide: func(_ int, id interaction.QuestionID) reply {
				if id == failing {
					return replyEmptyAnswered
				}
				return replyValid
			}}

			instr, err := runGuarded(t, in)

			requireEnding(t, instr, err, domain.ConsultFailInteractionUnavailable)
			if last := in.calls[len(in.calls)-1]; last != failing {
				t.Errorf("want routing to end at %s, last asked %s", failing, last)
			}
		})
	}
}

func TestManualTermination_EmptyAnswerOnAnOptionalStep_IsAValidAnswer(t *testing.T) {
	for _, optional := range []interaction.QuestionID{
		domain.QuestionManualInputs, domain.QuestionManualOutputs, domain.QuestionManualConstraints,
	} {
		t.Run(string(optional), func(t *testing.T) {
			in := &loopInteraction{t: t, decide: func(_ int, id interaction.QuestionID) reply {
				if id == optional {
					return replyEmptyAnswered
				}
				return replyValid
			}}

			instr, err := runGuarded(t, in)

			disp := requireDispatch(t, instr, err)
			if disp.RowIndex != 2 || disp.Stage != 2 {
				t.Errorf("want the dispatch of row 3 stage 2, got RowIndex=%d Stage=%d", disp.RowIndex, disp.Stage)
			}
		})
	}
}

func TestManualTermination_NeverConvergingInteraction_EndsAtThePromptBound(t *testing.T) {
	// A valid answer, then a cancel, forever: the dialogue keeps stepping back.
	in := &loopInteraction{t: t, decide: func(n int, _ interaction.QuestionID) reply {
		if n%2 == 1 {
			return replyValid
		}
		return replyCancelled
	}}

	instr, err := runGuarded(t, in)

	requireEnding(t, instr, err, domain.ConsultFailManualBoundExceeded)
	if len(in.calls) != deviation.ManualPromptLimit {
		t.Errorf("want exactly %d questions asked before routing ends, got %d", deviation.ManualPromptLimit, len(in.calls))
	}
}

func TestManualTermination_AlwaysCancelled_EndsAsUserCancelAtTheFirstQuestion(t *testing.T) {
	in := &loopInteraction{t: t, decide: always(replyCancelled)}

	instr, err := runGuarded(t, in)

	requireEnding(t, instr, err, domain.ConsultFailUserAbandoned)
	if len(in.calls) != 1 {
		t.Errorf("want 1 question asked, got %d", len(in.calls))
	}
}
