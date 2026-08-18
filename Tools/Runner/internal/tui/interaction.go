package tui

import (
	"context"
	"sync/atomic"

	tea "github.com/charmbracelet/bubbletea"

	"mosaic-common/interaction"
)

// questionKind classifies which Interaction method sent a message to the TUI.
type questionKind int

const (
	questionSelectOne  questionKind = iota
	questionSelectMany              // currently unused in runner, kept for interface compliance
	questionAskText
	questionConfirm
	questionNotice
	questionProgress
)

// questionMsg is the message sent to the tea.Program when the service layer calls any
// blocking Interaction method. The root model receives it and shows the appropriate overlay.
// The reply channel carries the user's answer back to the blocking caller.
type questionMsg struct {
	kind      questionKind
	choiceQ   interaction.ChoiceQuestion // questionSelectOne
	textQ     interaction.TextQuestion   // questionAskText
	confirmQ  interaction.Question       // questionConfirm
	notice    interaction.Notice         // questionNotice (non-blocking)
	progress  interaction.ProgressEvent  // questionProgress (non-blocking)
	reply     chan answerMsg
}

// answerMsg carries the user's answer back to the Interaction method that sent the question.
type answerMsg struct {
	choiceAns      interaction.ChoiceAnswer
	multiChoiceAns interaction.MultiChoiceAnswer
	textAns        interaction.TextAnswer
	confirmAns     interaction.ConfirmAnswer
	err            error
}

// ProgramRef is the TUI implementation of interaction.Interaction. It holds the
// tea.Program pointer atomically so it can be created before the program exists and wired
// in by Run() once tea.NewProgram returns.
//
// Typical usage:
//
//	ref := tui.NewProgramRef()
//	sess := session.New(session.Deps{Interact: ref, ...})
//	err := tui.Run(ctx, workflows, sess, tui.Options{Interaction: ref})
type ProgramRef struct {
	prog atomic.Pointer[tea.Program]
}

// NewProgramRef returns a ProgramRef that implements interaction.Interaction. Pass it as
// the session Interact dependency and as Options.Interaction; Run() wires the program in.
func NewProgramRef() *ProgramRef {
	return &ProgramRef{}
}

// set stores the program pointer. Called by Run() once tea.NewProgram returns.
func (r *ProgramRef) set(p *tea.Program) {
	r.prog.Store(p)
}

// program returns the stored tea.Program, or nil if not yet wired.
func (r *ProgramRef) program() *tea.Program {
	return r.prog.Load()
}

// SelectOne sends a choice question to the TUI and blocks until the user answers.
func (r *ProgramRef) SelectOne(_ context.Context, q interaction.ChoiceQuestion) (interaction.ChoiceAnswer, error) {
	p := r.program()
	if p == nil {
		return interaction.ChoiceAnswer{Status: interaction.SkippedOne}, nil
	}
	reply := make(chan answerMsg, 1)
	p.Send(questionMsg{kind: questionSelectOne, choiceQ: q, reply: reply})
	ans := <-reply
	return ans.choiceAns, ans.err
}

// SelectMany is not used by the runner's session; it returns immediately with no selection.
func (r *ProgramRef) SelectMany(_ context.Context, _ interaction.ChoiceQuestion) (interaction.MultiChoiceAnswer, error) {
	return interaction.MultiChoiceAnswer{Status: interaction.Answered}, nil
}

// AskText sends a text question to the TUI and blocks until the user answers.
func (r *ProgramRef) AskText(_ context.Context, q interaction.TextQuestion) (interaction.TextAnswer, error) {
	p := r.program()
	if p == nil {
		return interaction.TextAnswer{Status: interaction.SkippedOne}, nil
	}
	reply := make(chan answerMsg, 1)
	p.Send(questionMsg{kind: questionAskText, textQ: q, reply: reply})
	ans := <-reply
	return ans.textAns, ans.err
}

// Confirm sends a yes/no question to the TUI and blocks until the user answers.
func (r *ProgramRef) Confirm(_ context.Context, q interaction.Question) (interaction.ConfirmAnswer, error) {
	p := r.program()
	if p == nil {
		return interaction.ConfirmAnswer{Status: interaction.Answered, Confirm: false}, nil
	}
	reply := make(chan answerMsg, 1)
	p.Send(questionMsg{kind: questionConfirm, confirmQ: q, reply: reply})
	ans := <-reply
	return ans.confirmAns, ans.err
}

// Notify sends a non-blocking notice to the TUI. It never blocks.
func (r *ProgramRef) Notify(_ context.Context, n interaction.Notice) {
	if p := r.program(); p != nil {
		p.Send(questionMsg{kind: questionNotice, notice: n})
	}
}

// Progress sends a non-blocking progress event to the TUI. It never blocks.
func (r *ProgramRef) Progress(_ context.Context, e interaction.ProgressEvent) {
	if p := r.program(); p != nil {
		p.Send(questionMsg{kind: questionProgress, progress: e})
	}
}
