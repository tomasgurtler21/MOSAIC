package cli

import (
	"context"
	"fmt"
	"io"

	"mosaic-common/interaction"
)

// Answers carries flag-derived answers keyed by QuestionID (and, where a
// question is subject-scoped, by "{QuestionID}:{Subject}").
type Answers map[string]string

// Interaction is the non-blocking CLI implementation of domain.Interaction.
//
// Binding rule: it MUST NEVER block. Each method resolves an answer from
// the Answers map when one is available and otherwise returns
// interaction.SkippedOne immediately. Notify and Progress write to the
// configured writer and never fail.
type Interaction struct {
	out     io.Writer
	answers Answers
}

// NewInteraction constructs the non-blocking CLI Interaction. answers carries
// flag-derived values keyed by question ID (and optionally subject).
func NewInteraction(out io.Writer, answers Answers) *Interaction {
	return &Interaction{out: out, answers: answers}
}

// SelectOne resolves from answers when a matching key is present; otherwise
// returns SkippedOne immediately without blocking.
func (i *Interaction) SelectOne(_ context.Context, q interaction.ChoiceQuestion) (interaction.ChoiceAnswer, error) {
	key := string(q.ID)
	if val, ok := i.answers[key]; ok {
		return interaction.ChoiceAnswer{Status: interaction.Answered, OptionID: val}, nil
	}
	return interaction.ChoiceAnswer{Status: interaction.SkippedOne}, nil
}

// SelectMany returns SkippedOne immediately without blocking.
func (i *Interaction) SelectMany(_ context.Context, _ interaction.ChoiceQuestion) (interaction.MultiChoiceAnswer, error) {
	return interaction.MultiChoiceAnswer{Status: interaction.SkippedOne}, nil
}

// AskText resolves from answers when a matching key is present; otherwise
// returns SkippedOne immediately without blocking.
//
// Key lookup: "{ID}" for non-subject questions; "{ID}:{Subject}" for
// subject-scoped questions (e.g. QuestionPricingRate with model:field subject).
func (i *Interaction) AskText(_ context.Context, q interaction.TextQuestion) (interaction.TextAnswer, error) {
	key := string(q.ID)
	if q.Subject != "" {
		key = key + ":" + q.Subject
	}
	if val, ok := i.answers[key]; ok {
		return interaction.TextAnswer{Status: interaction.Answered, Text: val}, nil
	}
	return interaction.TextAnswer{Status: interaction.SkippedOne}, nil
}

// Confirm returns SkippedOne immediately without blocking.
func (i *Interaction) Confirm(_ context.Context, _ interaction.Question) (interaction.ConfirmAnswer, error) {
	return interaction.ConfirmAnswer{Status: interaction.SkippedOne}, nil
}

// Notify writes a notice to the configured writer. It never blocks.
func (i *Interaction) Notify(_ context.Context, n interaction.Notice) {
	fmt.Fprintf(i.out, "[%s] %s: %s\n", n.Level, n.Title, n.Message)
}

// Progress writes a progress event to the configured writer. It never blocks.
func (i *Interaction) Progress(_ context.Context, e interaction.ProgressEvent) {
	fmt.Fprintf(i.out, "progress phase=%s current=%d total=%d subject=%s\n", e.Phase, e.Current, e.Total, e.Subject)
}
