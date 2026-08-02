package tui

import (
	"context"
	"sync"

	"mosaic-common/interaction"
)

// pendingKind classifies which Interaction method is waiting for an answer.
type pendingKind int

const (
	pendingSelectOne  pendingKind = iota
	pendingSelectMany pendingKind = iota
	pendingAskText    pendingKind = iota
	pendingConfirm    pendingKind = iota
)

// answerMsg carries the user's answer back to a waiting Interaction method.
type answerMsg struct {
	choiceAns      interaction.ChoiceAnswer
	multiChoiceAns interaction.MultiChoiceAnswer
	textAns        interaction.TextAnswer
	confirmAns     interaction.ConfirmAnswer
}

// Pending is one question awaiting an answer, with the reply channel the
// top-level model writes to when the user responds.
type Pending struct {
	id       interaction.QuestionID
	subject  string
	kind     pendingKind
	choiceQ  interaction.ChoiceQuestion
	textQ    interaction.TextQuestion
	confirmQ interaction.Question
	reply    chan answerMsg
}

// ID returns the question identifier.
func (p Pending) ID() interaction.QuestionID { return p.id }

// Subject returns the question subject (e.g. model:field for pricing rates).
func (p Pending) Subject() string { return p.subject }

// Interaction is the interactive TUI implementation of domain.Interaction.
// Questions surface as overlays on the current screen; answers flow back via
// the pending question's reply channel.
//
// Binding rule: it must not block INDEFINITELY. Every pending question resolves
// on user action, on context cancellation, or on program teardown — teardown
// resolves outstanding questions as interaction.Cancelled.
type Interaction struct {
	questions chan Pending
	done      chan struct{}
	closeOnce sync.Once
}

// NewInteraction returns the implementation plus the read-only channel of
// pending questions the top-level model consumes.
func NewInteraction() (*Interaction, <-chan Pending) {
	questions := make(chan Pending, 1)
	i := &Interaction{
		questions: questions,
		done:      make(chan struct{}),
	}
	return i, questions
}

// Close signals teardown, resolving any outstanding questions as Cancelled.
// Safe to call multiple times.
func (i *Interaction) Close() {
	i.closeOnce.Do(func() {
		close(i.done)
	})
}

// send dispatches a Pending to the questions channel and waits for a reply.
// Returns false when the context or teardown channel fires first.
func (i *Interaction) send(ctx context.Context, p Pending) (answerMsg, bool) {
	select {
	case i.questions <- p:
	case <-ctx.Done():
		return answerMsg{}, false
	case <-i.done:
		return answerMsg{}, false
	}
	select {
	case ans := <-p.reply:
		return ans, true
	case <-ctx.Done():
		return answerMsg{}, false
	case <-i.done:
		return answerMsg{}, false
	}
}

// SelectOne presents a choice question and blocks until the user selects an
// option, the context expires, or the program tears down.
func (i *Interaction) SelectOne(ctx context.Context, q interaction.ChoiceQuestion) (interaction.ChoiceAnswer, error) {
	reply := make(chan answerMsg, 1)
	ans, ok := i.send(ctx, Pending{
		id:      q.ID,
		subject: q.Subject,
		kind:    pendingSelectOne,
		choiceQ: q,
		reply:   reply,
	})
	if !ok {
		return interaction.ChoiceAnswer{Status: interaction.Cancelled}, nil
	}
	return ans.choiceAns, nil
}

// SelectMany presents a multiple-choice question. In the TUI this is treated
// like SelectOne for now; the common interaction patterns do not use it.
func (i *Interaction) SelectMany(ctx context.Context, q interaction.ChoiceQuestion) (interaction.MultiChoiceAnswer, error) {
	reply := make(chan answerMsg, 1)
	ans, ok := i.send(ctx, Pending{
		id:      q.ID,
		subject: q.Subject,
		kind:    pendingSelectMany,
		choiceQ: q,
		reply:   reply,
	})
	if !ok {
		return interaction.MultiChoiceAnswer{Status: interaction.Cancelled}, nil
	}
	return ans.multiChoiceAns, nil
}

// AskText presents a text question and blocks until the user answers, the
// context expires, or the program tears down.
func (i *Interaction) AskText(ctx context.Context, q interaction.TextQuestion) (interaction.TextAnswer, error) {
	reply := make(chan answerMsg, 1)
	ans, ok := i.send(ctx, Pending{
		id:      q.ID,
		subject: q.Subject,
		kind:    pendingAskText,
		textQ:   q,
		reply:   reply,
	})
	if !ok {
		return interaction.TextAnswer{Status: interaction.Cancelled}, nil
	}
	return ans.textAns, nil
}

// Confirm presents a yes/no question and blocks until the user answers, the
// context expires, or the program tears down.
func (i *Interaction) Confirm(ctx context.Context, q interaction.Question) (interaction.ConfirmAnswer, error) {
	reply := make(chan answerMsg, 1)
	ans, ok := i.send(ctx, Pending{
		id:      q.ID,
		subject: q.Subject,
		kind:    pendingConfirm,
		confirmQ: q,
		reply:   reply,
	})
	if !ok {
		return interaction.ConfirmAnswer{Status: interaction.Cancelled}, nil
	}
	return ans.confirmAns, nil
}

// Notify is a one-way notification that never blocks. In the TUI, it is a
// no-op at the Interaction level; important status information (e.g. the
// written config path) surfaces through the app layer's response to the
// pricing flow.
func (i *Interaction) Notify(_ context.Context, _ interaction.Notice) {}

// Progress is a one-way progress event that never blocks.
func (i *Interaction) Progress(_ context.Context, _ interaction.ProgressEvent) {}
