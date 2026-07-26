package tui

import (
	"context"
	"fmt"
	"strings"
	"sync/atomic"

	tea "github.com/charmbracelet/bubbletea"

	"mosaic-deploy/internal/domain"
)

// questionKind classifies which Interaction method sent a question to the TUI.
type questionKind int

const (
	questionSelectOne questionKind = iota
	questionSelectMany
	questionAskText
	questionConfirm
	questionReview
	questionNotice
	questionProgress
)

// questionMsg is the message sent to the tea.Program when the app layer calls any blocking
// Interaction method. The root model receives it and transitions to the appropriate question
// screen. The reply channel carries the answer back.
type questionMsg struct {
	kind       questionKind
	choiceQ    domain.ChoiceQuestion // questionSelectOne, questionSelectMany
	textQ      domain.TextQuestion   // questionAskText
	confirmQ   domain.Question       // questionConfirm
	plan       domain.Plan           // questionReview
	notice     domain.Notice         // questionNotice (non-blocking)
	progress   domain.ProgressEvent  // questionProgress (non-blocking)
	reply      chan answerMsg
}

// answerMsg carries the user's answer back to the Interaction method that sent the question.
type answerMsg struct {
	choiceAns      domain.ChoiceAnswer
	multiChoiceAns domain.MultiChoiceAnswer
	textAns        domain.TextAnswer
	confirmAns     domain.ConfirmAnswer
	err            error
}

// ProgramRef is the interactive TUI implementation of domain.Interaction. It holds the
// tea.Program pointer atomically so that it can be created before the program exists and
// wired in by Run() once tea.NewProgram returns.
//
// Typical usage:
//
//	ref := tui.NewInteraction()
//	svc := app.New(app.Deps{Interaction: ref, ...})
//	err := tui.Run(ctx, svc, tui.Options{Interaction: ref})
type ProgramRef struct {
	prog atomic.Pointer[tea.Program]
}

// NewInteraction returns a ProgramRef that implements domain.Interaction. Pass the returned
// value as Deps.Interaction when constructing the app service, and as Options.Interaction
// when calling Run(). Run() wires the tea.Program into the ref once it is created.
func NewInteraction() *ProgramRef {
	return &ProgramRef{}
}

// set stores the program pointer. Called by Run() once tea.NewProgram returns.
func (r *ProgramRef) set(p *tea.Program) {
	r.prog.Store(p)
}

// program returns the stored tea.Program, or nil if not yet set.
func (r *ProgramRef) program() *tea.Program {
	return r.prog.Load()
}

func (r *ProgramRef) SelectOne(_ context.Context, q domain.ChoiceQuestion) (domain.ChoiceAnswer, error) {
	p := r.program()
	if p == nil {
		return domain.ChoiceAnswer{Status: domain.SkippedOne}, nil
	}
	reply := make(chan answerMsg, 1)
	p.Send(questionMsg{
		kind:    questionSelectOne,
		choiceQ: q,
		reply:   reply,
	})
	ans := <-reply
	return ans.choiceAns, ans.err
}

func (r *ProgramRef) SelectMany(_ context.Context, q domain.ChoiceQuestion) (domain.MultiChoiceAnswer, error) {
	p := r.program()
	if p == nil {
		return domain.MultiChoiceAnswer{Status: domain.SkippedOne}, nil
	}
	reply := make(chan answerMsg, 1)
	p.Send(questionMsg{
		kind:    questionSelectMany,
		choiceQ: q,
		reply:   reply,
	})
	ans := <-reply
	return ans.multiChoiceAns, ans.err
}

func (r *ProgramRef) AskText(_ context.Context, q domain.TextQuestion) (domain.TextAnswer, error) {
	p := r.program()
	if p == nil {
		return domain.TextAnswer{Status: domain.SkippedOne}, nil
	}
	reply := make(chan answerMsg, 1)
	p.Send(questionMsg{
		kind:  questionAskText,
		textQ: q,
		reply: reply,
	})
	ans := <-reply
	return ans.textAns, ans.err
}

func (r *ProgramRef) Confirm(_ context.Context, q domain.Question) (domain.ConfirmAnswer, error) {
	p := r.program()
	if p == nil {
		return domain.ConfirmAnswer{Status: domain.SkippedOne}, nil
	}
	reply := make(chan answerMsg, 1)
	p.Send(questionMsg{
		kind:     questionConfirm,
		confirmQ: q,
		reply:    reply,
	})
	ans := <-reply
	return ans.confirmAns, ans.err
}

func (r *ProgramRef) Review(_ context.Context, p domain.Plan) (domain.ConfirmAnswer, error) {
	prog := r.program()
	if prog == nil {
		return domain.ConfirmAnswer{Status: domain.SkippedOne}, nil
	}
	reply := make(chan answerMsg, 1)
	prog.Send(questionMsg{
		kind:  questionReview,
		plan:  p,
		reply: reply,
	})
	ans := <-reply
	return ans.confirmAns, ans.err
}

// Notify sends a non-blocking notice to the TUI. It never blocks.
func (r *ProgramRef) Notify(_ context.Context, n domain.Notice) {
	if p := r.program(); p != nil {
		p.Send(questionMsg{
			kind:   questionNotice,
			notice: n,
		})
	}
}

// Progress sends a non-blocking progress event to the TUI. It never blocks.
func (r *ProgramRef) Progress(_ context.Context, e domain.ProgressEvent) {
	if p := r.program(); p != nil {
		p.Send(questionMsg{
			kind:     questionProgress,
			progress: e,
		})
	}
}

// inlineSelectOne renders a simple interactive select-one question overlay for use during
// the deployment flow. It is used by the root model's question-handling logic.
type inlineSelectOne struct {
	q      domain.ChoiceQuestion
	cursor int
	done   bool
	back   bool
	styles Theme
	width  int
	height int
}

func newInlineSelectOne(q domain.ChoiceQuestion, styles Theme, width, height int) *inlineSelectOne {
	return &inlineSelectOne{q: q, styles: styles, width: width, height: height}
}

func (s *inlineSelectOne) update(msg tea.Msg) bool {
	keyMsg, ok := msg.(tea.KeyMsg)
	if !ok {
		return false
	}
	switch keyMsg.String() {
	case "up", "k":
		if s.cursor > 0 {
			s.cursor--
		}
	case "down", "j":
		if s.cursor < len(s.q.Options)-1 {
			s.cursor++
		}
	case "enter":
		if s.cursor >= 0 && s.cursor < len(s.q.Options) {
			if !s.q.Options[s.cursor].Disabled {
				s.done = true
			}
		}
	case "esc":
		s.back = true
	}
	return s.done || s.back
}

func (s *inlineSelectOne) answer() domain.ChoiceAnswer {
	if s.back {
		return domain.ChoiceAnswer{Status: domain.Cancelled}
	}
	if s.cursor >= 0 && s.cursor < len(s.q.Options) {
		return domain.ChoiceAnswer{Status: domain.Answered, OptionID: s.q.Options[s.cursor].ID}
	}
	return domain.ChoiceAnswer{Status: domain.SkippedOne}
}

func (s *inlineSelectOne) view() string {
	var sb strings.Builder
	sb.WriteString(s.styles.Style(RoleTitle).Width(s.width).Render(s.q.Title))
	sb.WriteByte('\n')
	if s.q.Prompt != "" {
		sb.WriteString(s.styles.Style(RoleBody).Width(s.width).Render(s.q.Prompt))
		sb.WriteByte('\n')
	}
	if s.q.Detail != "" {
		sb.WriteString(s.styles.Style(RoleMuted).Width(s.width).Render(s.q.Detail))
		sb.WriteByte('\n')
	}
	for i, opt := range s.q.Options {
		prefix := "  "
		if i == s.cursor {
			prefix = "▶ "
		}
		label := opt.Label
		if opt.Disabled {
			if opt.DisabledReason != "" {
				label = fmt.Sprintf("%s (%s)", label, opt.DisabledReason)
			}
			sb.WriteString(prefix + s.styles.Style(RoleMuted).Render(label) + "\n")
		} else if i == s.cursor {
			sb.WriteString(prefix + s.styles.Style(RoleSelected).Render(label) + "\n")
		} else {
			sb.WriteString(prefix + s.styles.Style(RoleBody).Render(label) + "\n")
		}
		if opt.Description != "" && i == s.cursor {
			sb.WriteString("    " + s.styles.Style(RoleMuted).Render(opt.Description) + "\n")
		}
	}
	sb.WriteString(s.styles.Style(RoleHelp).Width(s.width).Render("↑/k up  ↓/j down  enter select  esc cancel"))
	return sb.String()
}

// inlineConfirm renders a simple yes/no question overlay.
type inlineConfirm struct {
	q      domain.Question
	cursor int // 0 = yes, 1 = no
	done   bool
	back   bool
	styles Theme
	width  int
}

func newInlineConfirm(q domain.Question, styles Theme, width int) *inlineConfirm {
	return &inlineConfirm{q: q, cursor: 1, styles: styles, width: width} // default no
}

func (c *inlineConfirm) update(msg tea.Msg) bool {
	keyMsg, ok := msg.(tea.KeyMsg)
	if !ok {
		return false
	}
	switch keyMsg.String() {
	case "left", "h", "y", "Y":
		c.cursor = 0
	case "right", "l", "n", "N":
		c.cursor = 1
	case "enter":
		c.done = true
	case "esc":
		c.back = true
	}
	return c.done || c.back
}

func (c *inlineConfirm) answer() domain.ConfirmAnswer {
	if c.back {
		return domain.ConfirmAnswer{Status: domain.Cancelled}
	}
	return domain.ConfirmAnswer{Status: domain.Answered, Confirm: c.cursor == 0}
}

func (c *inlineConfirm) view() string {
	var sb strings.Builder
	sb.WriteString(c.styles.Style(RoleTitle).Width(c.width).Render(c.q.Title))
	sb.WriteByte('\n')
	if c.q.Prompt != "" {
		sb.WriteString(c.styles.Style(RoleBody).Width(c.width).Render(c.q.Prompt))
		sb.WriteByte('\n')
	}

	yes, no := "[ Yes ]", "[ No ]"
	if c.cursor == 0 {
		yes = c.styles.Style(RoleSelected).Render("[ Yes ]")
		no = c.styles.Style(RoleBody).Render("[ No ]")
	} else {
		yes = c.styles.Style(RoleBody).Render("[ Yes ]")
		no = c.styles.Style(RoleSelected).Render("[ No ]")
	}
	sb.WriteString("\n  " + yes + "  " + no + "\n")
	sb.WriteString(c.styles.Style(RoleHelp).Width(c.width).Render("←/h yes  →/l no  enter confirm  esc cancel"))
	return sb.String()
}
