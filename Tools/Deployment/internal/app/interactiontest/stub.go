// Package interactiontest provides a scripted test double for domain.Interaction. It is a
// real package (not a _test file) so that both the app tests (Stage 18) and the frontend
// tests (Stages 19, 20) can import it without duplication.
//
// Usage:
//
//	stub := interactiontest.NewBuilder().
//	    AnswerSelectOne(domain.QHarness, "", "claude-code").
//	    AnswerSelectMany(domain.QWorkflows, "", []string{"quick-fix"}).
//	    AnswerReview(true).
//	    Build()
//
//	// ...pass stub as domain.Interaction to app.Deps...
//
//	order := stub.QuestionOrder()
//	if order[0] != domain.QHarness { ... }
package interactiontest

import (
	"context"
	"sync"

	"mosaic-deploy/internal/domain"
)

// Call records one invocation of an Interaction method.
type Call struct {
	// ID is the QuestionID for question methods; empty for Notify and Progress.
	ID domain.QuestionID
	// Subject mirrors Question.Subject; empty for run-level questions and non-question calls.
	Subject string
	// Kind is the method name: "select-one", "select-many", "text", "confirm", "review",
	// "notify", "progress".
	Kind string
}

// script holds pre-configured answers keyed by question identity.
type script struct {
	choices      map[scriptKey]domain.ChoiceAnswer
	multiChoices map[scriptKey]domain.MultiChoiceAnswer
	texts        map[scriptKey]domain.TextAnswer
	confirms     map[scriptKey]domain.ConfirmAnswer
	reviews      []domain.ConfirmAnswer
}

type scriptKey struct {
	ID      domain.QuestionID
	Subject string
}

// Builder accumulates scripted answers. Unscripted questions receive a SkippedOne answer by
// default; Review() receives Answered{Confirm: true} when no review answers remain in the script.
type Builder struct {
	s script
}

// NewBuilder returns an empty Builder.
func NewBuilder() *Builder {
	return &Builder{s: script{
		choices:      make(map[scriptKey]domain.ChoiceAnswer),
		multiChoices: make(map[scriptKey]domain.MultiChoiceAnswer),
		texts:        make(map[scriptKey]domain.TextAnswer),
		confirms:     make(map[scriptKey]domain.ConfirmAnswer),
	}}
}

// AnswerSelectOne scripts the answer returned when SelectOne is called with the given id and
// subject. Subject is empty for run-level questions (e.g. QHarness, QWorkspace).
func (b *Builder) AnswerSelectOne(id domain.QuestionID, subject, optionID string) *Builder {
	b.s.choices[scriptKey{id, subject}] = domain.ChoiceAnswer{
		Status:   domain.Answered,
		OptionID: optionID,
	}
	return b
}

// AnswerSelectOneCustom scripts a free-text (custom) answer for a SelectOne call.
func (b *Builder) AnswerSelectOneCustom(id domain.QuestionID, subject, custom string) *Builder {
	b.s.choices[scriptKey{id, subject}] = domain.ChoiceAnswer{
		Status: domain.Answered,
		Custom: custom,
	}
	return b
}

// AnswerSelectMany scripts the answer returned when SelectMany is called with the given id
// and subject.
func (b *Builder) AnswerSelectMany(id domain.QuestionID, subject string, optionIDs []string) *Builder {
	b.s.multiChoices[scriptKey{id, subject}] = domain.MultiChoiceAnswer{
		Status:    domain.Answered,
		OptionIDs: optionIDs,
	}
	return b
}

// AnswerSkipAll scripts a SkippedAll response for SelectOne, SelectMany, or AskText calls
// matching id and subject. The app will latch SkippedAll and not ask the same QuestionID again.
func (b *Builder) AnswerSkipAll(id domain.QuestionID, subject string) *Builder {
	b.s.choices[scriptKey{id, subject}] = domain.ChoiceAnswer{Status: domain.SkippedAll}
	b.s.multiChoices[scriptKey{id, subject}] = domain.MultiChoiceAnswer{Status: domain.SkippedAll}
	b.s.texts[scriptKey{id, subject}] = domain.TextAnswer{Status: domain.SkippedAll}
	return b
}

// AnswerText scripts the answer returned when AskText is called with the given id and subject.
func (b *Builder) AnswerText(id domain.QuestionID, subject, text string) *Builder {
	b.s.texts[scriptKey{id, subject}] = domain.TextAnswer{
		Status: domain.Answered,
		Text:   text,
	}
	return b
}

// AnswerCancelledText scripts a Cancelled response for an AskText call matching id and
// subject. The service must treat Cancelled the same way it treats SkippedOne: carry the
// subject verbatim into the output without blocking. This allows tests to verify that every
// non-answered outcome (skip, skip-all, cancellation) is handled identically.
func (b *Builder) AnswerCancelledText(id domain.QuestionID, subject string) *Builder {
	b.s.texts[scriptKey{id, subject}] = domain.TextAnswer{Status: domain.Cancelled}
	return b
}

// AnswerCancelledChoice scripts a Cancelled response for a SelectOne or SelectMany call
// matching id and subject — the answer the TUI produces when the user presses Esc. Unlike
// AnswerCancelledText, which the service treats as a skip, a cancelled choice answer at a
// model question aborts the run.
func (b *Builder) AnswerCancelledChoice(id domain.QuestionID, subject string) *Builder {
	b.s.choices[scriptKey{id, subject}] = domain.ChoiceAnswer{Status: domain.Cancelled}
	b.s.multiChoices[scriptKey{id, subject}] = domain.MultiChoiceAnswer{Status: domain.Cancelled}
	return b
}

// AnswerConfirm scripts the answer returned when Confirm is called with the given id and subject.
func (b *Builder) AnswerConfirm(id domain.QuestionID, subject string, confirm bool) *Builder {
	b.s.confirms[scriptKey{id, subject}] = domain.ConfirmAnswer{
		Status:  domain.Answered,
		Confirm: confirm,
	}
	return b
}

// AnswerReview appends a Review answer. Calls to Review consume answers in append order;
// when all scripted reviews are exhausted, Review returns Answered{Confirm: true}.
func (b *Builder) AnswerReview(confirm bool) *Builder {
	b.s.reviews = append(b.s.reviews, domain.ConfirmAnswer{
		Status:  domain.Answered,
		Confirm: confirm,
	})
	return b
}

// Build returns a Stub ready to use as domain.Interaction.
func (b *Builder) Build() *Stub {
	cp := script{
		choices:      make(map[scriptKey]domain.ChoiceAnswer, len(b.s.choices)),
		multiChoices: make(map[scriptKey]domain.MultiChoiceAnswer, len(b.s.multiChoices)),
		texts:        make(map[scriptKey]domain.TextAnswer, len(b.s.texts)),
		confirms:     make(map[scriptKey]domain.ConfirmAnswer, len(b.s.confirms)),
		reviews:      append([]domain.ConfirmAnswer(nil), b.s.reviews...),
	}
	for k, v := range b.s.choices {
		cp.choices[k] = v
	}
	for k, v := range b.s.multiChoices {
		cp.multiChoices[k] = v
	}
	for k, v := range b.s.texts {
		cp.texts[k] = v
	}
	for k, v := range b.s.confirms {
		cp.confirms[k] = v
	}
	return &Stub{script: cp}
}

// Stub is a scripted test double that implements domain.Interaction. It is safe for concurrent
// use by a single test (the app runs sequentially within one goroutine in tests).
type Stub struct {
	mu          sync.Mutex
	script      script
	calls       []Call
	notices     []domain.Notice
	reviewIndex int
}

// Calls returns all recorded interaction calls in invocation order. The returned slice is a
// snapshot; modifications to it do not affect the Stub.
func (s *Stub) Calls() []Call {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Call, len(s.calls))
	copy(out, s.calls)
	return out
}

// QuestionOrder returns the QuestionIDs of every question-shaped call (SelectOne, SelectMany,
// AskText, Confirm, Review) in invocation order. Notify and Progress calls are excluded.
func (s *Stub) QuestionOrder() []domain.QuestionID {
	var ids []domain.QuestionID
	for _, c := range s.Calls() {
		if c.Kind == "notify" || c.Kind == "progress" {
			continue
		}
		ids = append(ids, c.ID)
	}
	return ids
}

// Notices returns every Notice passed to Notify, in call order, so a test can assert that a
// warning was surfaced to the user and inspect its level and message. The returned slice is a
// snapshot; modifications to it do not affect the Stub.
func (s *Stub) Notices() []domain.Notice {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]domain.Notice, len(s.notices))
	copy(out, s.notices)
	return out
}

// WasAsked reports whether a question with the given id and subject was called at least once.
func (s *Stub) WasAsked(id domain.QuestionID, subject string) bool {
	for _, c := range s.Calls() {
		if c.ID == id && c.Subject == subject {
			return true
		}
	}
	return false
}

// CountAsked returns the number of times a question with the given id and subject was called.
func (s *Stub) CountAsked(id domain.QuestionID, subject string) int {
	n := 0
	for _, c := range s.Calls() {
		if c.ID == id && c.Subject == subject {
			n++
		}
	}
	return n
}

func (s *Stub) record(c Call) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls = append(s.calls, c)
}

// SelectOne implements domain.Interaction. Returns the scripted answer if configured;
// otherwise returns SkippedOne.
func (s *Stub) SelectOne(ctx context.Context, q domain.ChoiceQuestion) (domain.ChoiceAnswer, error) {
	s.record(Call{ID: q.ID, Subject: q.Subject, Kind: "select-one"})
	if a, ok := s.script.choices[scriptKey{q.ID, q.Subject}]; ok {
		return a, nil
	}
	return domain.ChoiceAnswer{Status: domain.SkippedOne}, nil
}

// SelectMany implements domain.Interaction. Returns the scripted answer if configured;
// otherwise returns SkippedOne.
func (s *Stub) SelectMany(ctx context.Context, q domain.ChoiceQuestion) (domain.MultiChoiceAnswer, error) {
	s.record(Call{ID: q.ID, Subject: q.Subject, Kind: "select-many"})
	if a, ok := s.script.multiChoices[scriptKey{q.ID, q.Subject}]; ok {
		return a, nil
	}
	return domain.MultiChoiceAnswer{Status: domain.SkippedOne}, nil
}

// AskText implements domain.Interaction. Returns the scripted answer if configured;
// otherwise returns SkippedOne.
func (s *Stub) AskText(ctx context.Context, q domain.TextQuestion) (domain.TextAnswer, error) {
	s.record(Call{ID: q.ID, Subject: q.Subject, Kind: "text"})
	if a, ok := s.script.texts[scriptKey{q.ID, q.Subject}]; ok {
		return a, nil
	}
	return domain.TextAnswer{Status: domain.SkippedOne}, nil
}

// Confirm implements domain.Interaction. Returns the scripted answer if configured;
// otherwise returns Answered{Confirm: true} (proceed by default, to not block flows).
func (s *Stub) Confirm(ctx context.Context, q domain.Question) (domain.ConfirmAnswer, error) {
	s.record(Call{ID: q.ID, Subject: q.Subject, Kind: "confirm"})
	if a, ok := s.script.confirms[scriptKey{q.ID, q.Subject}]; ok {
		return a, nil
	}
	return domain.ConfirmAnswer{Status: domain.Answered, Confirm: true}, nil
}

// Notify implements domain.Interaction. It records the call, and the Notice value itself
// (retrievable via Notices), and returns without blocking.
func (s *Stub) Notify(ctx context.Context, n domain.Notice) {
	s.record(Call{Kind: "notify"})
	s.mu.Lock()
	s.notices = append(s.notices, n)
	s.mu.Unlock()
}

// Progress implements domain.Interaction. It records the call and returns without blocking.
func (s *Stub) Progress(ctx context.Context, e domain.ProgressEvent) {
	s.record(Call{Kind: "progress"})
}

// Review implements domain.Interaction. Consumes pre-scripted review answers in order;
// falls back to Answered{Confirm: true} when all scripted answers are exhausted.
func (s *Stub) Review(ctx context.Context, p domain.Plan) (domain.ConfirmAnswer, error) {
	s.record(Call{ID: domain.QPlanConfirm, Kind: "review"})
	s.mu.Lock()
	defer s.mu.Unlock()
	// A scripted Confirm answer for QPlanConfirm (subject "") takes precedence over the
	// review queue, allowing callers to express a single fixed review outcome via
	// AnswerConfirm without needing the append-order AnswerReview mechanism.
	if a, ok := s.script.confirms[scriptKey{domain.QPlanConfirm, ""}]; ok {
		return a, nil
	}
	if s.reviewIndex < len(s.script.reviews) {
		a := s.script.reviews[s.reviewIndex]
		s.reviewIndex++
		return a, nil
	}
	return domain.ConfirmAnswer{Status: domain.Answered, Confirm: true}, nil
}
