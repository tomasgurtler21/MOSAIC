package cli

import (
	"context"
	"fmt"
	"io"
	"strings"

	"mosaic-deploy/internal/domain"
	"mosaic-deploy/internal/todo"
)

// PreAnswers holds the resolved flag and file values that the non-interactive Interaction
// implementation answers questions from. It is mechanism-agnostic: both repeated flags and
// a selections file produce the same PreAnswers map, so NewInteraction is independent of
// the surface that populated it.
//
// Values maps QuestionID -> subject -> answer string. The subject is the Question.Subject
// field (tier name, agent key, file path); the empty string "" is the run-level subject.
// For SelectMany questions, multiple option IDs are comma-joined in the answer string.
// For Confirm questions, "true" and "false" are the valid answer strings.
type PreAnswers struct {
	Values  map[domain.QuestionID]map[string]string
	SkipAll map[domain.QuestionID]bool
}

// NewInteraction returns a non-interactive implementation of domain.Interaction. It resolves
// each question from answers: a pre-answered question returns Answered; an unresolvable
// question returns SkippedOne and adds a GapSkippedFile entry via todos. No implementation
// path blocks on terminal input (AC19.2, AC19.3).
//
// Option validation: for SelectOne and SelectMany, the answer string must match an OptionID
// in the question's Options list. An answer that names a non-existent option is treated as
// unresolvable and results in a skip-and-record.
func NewInteraction(answers PreAnswers, todos todo.Collector, out io.Writer) domain.Interaction {
	return &nonInteractive{
		answers: answers,
		todos:   todos,
		out:     out,
	}
}

// nonInteractive implements domain.Interaction without ever blocking on terminal input.
type nonInteractive struct {
	answers PreAnswers
	todos   todo.Collector
	out     io.Writer
}

// lookup finds the pre-answer for the given question ID and subject.
func (n *nonInteractive) lookup(id domain.QuestionID, subject string) (string, bool) {
	if n.answers.Values == nil {
		return "", false
	}
	bySubject, ok := n.answers.Values[id]
	if !ok {
		return "", false
	}
	v, ok := bySubject[subject]
	return v, ok
}

// recordSkip adds a GapSkippedFile entry to the todo collector for an unresolvable question.
func (n *nonInteractive) recordSkip(id domain.QuestionID, subject string) {
	detail := fmt.Sprintf("question %q (subject %q) had no pre-answer in non-interactive mode", id, subject)
	n.todos.AddGap(domain.Gap{
		Kind:    domain.GapSkippedFile,
		Subject: string(id),
		Detail:  detail,
	})
}

// SelectOne resolves a select-one question from PreAnswers. The answer must name a valid
// OptionID in the question's Options list; an invalid ID is treated as unresolvable.
func (n *nonInteractive) SelectOne(_ context.Context, q domain.ChoiceQuestion) (domain.ChoiceAnswer, error) {
	if n.answers.SkipAll != nil && n.answers.SkipAll[q.ID] {
		return domain.ChoiceAnswer{Status: domain.SkippedAll}, nil
	}

	v, ok := n.lookup(q.ID, q.Subject)
	if !ok {
		n.recordSkip(q.ID, q.Subject)
		return domain.ChoiceAnswer{Status: domain.SkippedOne}, nil
	}

	// Validate that the answer names a real option.
	for _, opt := range q.Options {
		if opt.ID == v {
			return domain.ChoiceAnswer{Status: domain.Answered, OptionID: v}, nil
		}
	}

	// Answer names an option that does not exist: treat as unresolvable.
	n.recordSkip(q.ID, q.Subject)
	return domain.ChoiceAnswer{Status: domain.SkippedOne}, nil
}

// SelectMany resolves a select-many question from PreAnswers. The answer is a comma-separated
// list of OptionIDs; every ID must be valid or the whole answer is treated as unresolvable.
func (n *nonInteractive) SelectMany(_ context.Context, q domain.ChoiceQuestion) (domain.MultiChoiceAnswer, error) {
	if n.answers.SkipAll != nil && n.answers.SkipAll[q.ID] {
		return domain.MultiChoiceAnswer{Status: domain.SkippedAll}, nil
	}

	v, ok := n.lookup(q.ID, q.Subject)
	if !ok {
		n.recordSkip(q.ID, q.Subject)
		return domain.MultiChoiceAnswer{Status: domain.SkippedOne}, nil
	}

	// Parse the comma-separated list.
	parts := strings.Split(v, ",")
	ids := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		// Validate that the ID exists in the question's option list.
		valid := false
		for _, opt := range q.Options {
			if opt.ID == p {
				valid = true
				break
			}
		}
		if !valid {
			n.recordSkip(q.ID, q.Subject)
			return domain.MultiChoiceAnswer{Status: domain.SkippedOne}, nil
		}
		ids = append(ids, p)
	}

	return domain.MultiChoiceAnswer{Status: domain.Answered, OptionIDs: ids}, nil
}

// AskText resolves a free-text question from PreAnswers.
func (n *nonInteractive) AskText(_ context.Context, q domain.TextQuestion) (domain.TextAnswer, error) {
	v, ok := n.lookup(q.ID, q.Subject)
	if !ok {
		n.recordSkip(q.ID, q.Subject)
		return domain.TextAnswer{Status: domain.SkippedOne}, nil
	}
	return domain.TextAnswer{Status: domain.Answered, Text: v}, nil
}

// Confirm resolves a yes/no question from PreAnswers. The answer must be "true" or "false";
// any other value (including absent) results in a skip-and-record.
func (n *nonInteractive) Confirm(_ context.Context, q domain.Question) (domain.ConfirmAnswer, error) {
	v, ok := n.lookup(q.ID, q.Subject)
	if !ok {
		n.recordSkip(q.ID, q.Subject)
		return domain.ConfirmAnswer{Status: domain.SkippedOne}, nil
	}
	return domain.ConfirmAnswer{Status: domain.Answered, Confirm: v == "true"}, nil
}

// Notify writes a notice to the output writer. It never blocks: the write is a direct
// synchronous call to the injected io.Writer (a bytes.Buffer in tests; stdout in production).
func (n *nonInteractive) Notify(_ context.Context, notice domain.Notice) {
	fmt.Fprintf(n.out, "[%s] %s: %s\n", notice.Level, notice.Title, notice.Message)
}

// Progress writes a progress event to the output writer. It never blocks.
func (n *nonInteractive) Progress(_ context.Context, e domain.ProgressEvent) {
	fmt.Fprintf(n.out, "progress: %s %d/%d %s\n", e.Phase, e.Current, e.Total, e.Subject)
}

// Review resolves the plan confirmation step from PreAnswers. When QPlanConfirm is not
// pre-answered, Review returns SkippedOne rather than blocking — the run is aborted cleanly.
func (n *nonInteractive) Review(_ context.Context, _ domain.Plan) (domain.ConfirmAnswer, error) {
	v, ok := n.lookup(domain.QPlanConfirm, "")
	if !ok {
		return domain.ConfirmAnswer{Status: domain.SkippedOne}, nil
	}
	return domain.ConfirmAnswer{Status: domain.Answered, Confirm: v == "true"}, nil
}
