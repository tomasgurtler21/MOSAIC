package cli_test

// Tests for the non-blocking CLI Interaction implementation.
//
// Coverage:
//   - Every question type returns promptly (no blocking)
//   - When a flag-derived answer is provided for QuestionLogSourcePath, AskText
//     returns Answered with that value
//   - When no answer is provided, AskText returns SkippedOne immediately
//   - SelectOne returns SkippedOne when no flag answer is available
//   - SelectOne returns Answered with the flag value when one is provided
//   - SelectMany always returns promptly
//   - Confirm always returns promptly
//   - Notify and Progress complete without blocking
//   - The implementation satisfies the domain.Interaction interface contract

import (
	"bytes"
	"context"
	"testing"
	"time"

	"mosaic-common/interaction"
	"mosaic-log-analyzer/internal/app"
	"mosaic-log-analyzer/internal/cli"
)

// ---------------------------------------------------------------------------
// Interface satisfaction
// ---------------------------------------------------------------------------

func TestInteraction_ImplementsDomainInteraction(t *testing.T) {
	// Verify at compile time that *cli.Interaction satisfies interaction.Interaction.
	var _ interaction.Interaction = cli.NewInteraction(&bytes.Buffer{}, cli.Answers{})
}

// ---------------------------------------------------------------------------
// AskText: flag-resolved vs skipped
// ---------------------------------------------------------------------------

func TestInteraction_AskText_ReturnsAnsweredWhenFlagProvided(t *testing.T) {
	// When answers contains a value for the question ID, AskText must return
	// Answered with that value — not SkippedOne.
	const flagPath = "/my/logs"
	answers := cli.Answers{
		string(app.QuestionLogSourcePath): flagPath,
	}

	i := cli.NewInteraction(&bytes.Buffer{}, answers)

	q := interaction.TextQuestion{
		Question: interaction.Question{
			ID: app.QuestionLogSourcePath,
		},
	}
	got, err := i.AskText(context.Background(), q)
	if err != nil {
		t.Fatalf("AskText returned error: %v", err)
	}
	if got.Status != interaction.Answered {
		t.Errorf("AskText status = %q, want %q when a flag answer is provided", got.Status, interaction.Answered)
	}
	if got.Text != flagPath {
		t.Errorf("AskText text = %q, want %q", got.Text, flagPath)
	}
}

func TestInteraction_AskText_ReturnsSkippedWhenNoFlagProvided(t *testing.T) {
	// When no answer is registered for the question ID, AskText must return
	// SkippedOne without blocking.
	i := cli.NewInteraction(&bytes.Buffer{}, cli.Answers{})

	q := interaction.TextQuestion{
		Question: interaction.Question{
			ID: app.QuestionLogSourcePath,
		},
	}
	got, err := i.AskText(context.Background(), q)
	if err != nil {
		t.Fatalf("AskText returned error: %v", err)
	}
	if got.Status != interaction.SkippedOne {
		t.Errorf("AskText status = %q, want %q when no flag answer is provided", got.Status, interaction.SkippedOne)
	}
}

func TestInteraction_AskText_CompletesWithinTimeout(t *testing.T) {
	// AskText must return before a short deadline, proving it does not block.
	i := cli.NewInteraction(&bytes.Buffer{}, cli.Answers{})

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	done := make(chan struct{})
	go func() {
		q := interaction.TextQuestion{
			Question: interaction.Question{ID: app.QuestionLogSourcePath},
		}
		i.AskText(ctx, q) //nolint:errcheck
		close(done)
	}()

	select {
	case <-done:
		// returned promptly — correct
	case <-ctx.Done():
		t.Error("AskText did not return within 100ms; it appears to be blocking")
	}
}

// ---------------------------------------------------------------------------
// SelectOne: flag-resolved vs skipped
// ---------------------------------------------------------------------------

func TestInteraction_SelectOne_ReturnsAnsweredWhenFlagProvided(t *testing.T) {
	answers := cli.Answers{
		string(app.QuestionPricingAction): app.PricingActionSkip,
	}

	i := cli.NewInteraction(&bytes.Buffer{}, answers)

	q := interaction.ChoiceQuestion{
		Question: interaction.Question{
			ID: app.QuestionPricingAction,
		},
		Options: []interaction.Option{
			{ID: app.PricingActionEnter, Label: "Enter prices"},
			{ID: app.PricingActionSkip, Label: "Skip"},
		},
	}
	got, err := i.SelectOne(context.Background(), q)
	if err != nil {
		t.Fatalf("SelectOne returned error: %v", err)
	}
	if got.Status != interaction.Answered {
		t.Errorf("SelectOne status = %q, want %q when a flag answer is provided", got.Status, interaction.Answered)
	}
	if got.OptionID != app.PricingActionSkip {
		t.Errorf("SelectOne OptionID = %q, want %q", got.OptionID, app.PricingActionSkip)
	}
}

func TestInteraction_SelectOne_ReturnsSkippedWhenNoFlagProvided(t *testing.T) {
	i := cli.NewInteraction(&bytes.Buffer{}, cli.Answers{})

	q := interaction.ChoiceQuestion{
		Question: interaction.Question{ID: app.QuestionPricingAction},
	}
	got, err := i.SelectOne(context.Background(), q)
	if err != nil {
		t.Fatalf("SelectOne returned error: %v", err)
	}
	if got.Status != interaction.SkippedOne {
		t.Errorf("SelectOne status = %q, want %q when no flag answer is provided", got.Status, interaction.SkippedOne)
	}
}

func TestInteraction_SelectOne_CompletesWithinTimeout(t *testing.T) {
	i := cli.NewInteraction(&bytes.Buffer{}, cli.Answers{})

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	done := make(chan struct{})
	go func() {
		q := interaction.ChoiceQuestion{
			Question: interaction.Question{ID: app.QuestionPricingAction},
		}
		i.SelectOne(ctx, q) //nolint:errcheck
		close(done)
	}()

	select {
	case <-done:
		// returned promptly — correct
	case <-ctx.Done():
		t.Error("SelectOne did not return within 100ms; it appears to be blocking")
	}
}

// ---------------------------------------------------------------------------
// SelectMany: always prompt-free
// ---------------------------------------------------------------------------

func TestInteraction_SelectMany_CompletesWithinTimeout(t *testing.T) {
	i := cli.NewInteraction(&bytes.Buffer{}, cli.Answers{})

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	done := make(chan struct{})
	go func() {
		q := interaction.ChoiceQuestion{
			Question: interaction.Question{ID: app.QuestionRunSelect},
		}
		i.SelectMany(ctx, q) //nolint:errcheck
		close(done)
	}()

	select {
	case <-done:
		// returned promptly — correct
	case <-ctx.Done():
		t.Error("SelectMany did not return within 100ms; it appears to be blocking")
	}
}

func TestInteraction_SelectMany_ReturnsSkipped(t *testing.T) {
	i := cli.NewInteraction(&bytes.Buffer{}, cli.Answers{})

	q := interaction.ChoiceQuestion{
		Question: interaction.Question{ID: app.QuestionRunSelect},
	}
	got, err := i.SelectMany(context.Background(), q)
	if err != nil {
		t.Fatalf("SelectMany returned error: %v", err)
	}
	if got.Status != interaction.SkippedOne {
		t.Errorf("SelectMany status = %q, want SkippedOne", got.Status)
	}
}

// ---------------------------------------------------------------------------
// Confirm: always prompt-free
// ---------------------------------------------------------------------------

func TestInteraction_Confirm_CompletesWithinTimeout(t *testing.T) {
	i := cli.NewInteraction(&bytes.Buffer{}, cli.Answers{})

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	done := make(chan struct{})
	go func() {
		q := interaction.Question{ID: "any-confirm-question"}
		i.Confirm(ctx, q) //nolint:errcheck
		close(done)
	}()

	select {
	case <-done:
		// returned promptly — correct
	case <-ctx.Done():
		t.Error("Confirm did not return within 100ms; it appears to be blocking")
	}
}

func TestInteraction_Confirm_ReturnsSkipped(t *testing.T) {
	i := cli.NewInteraction(&bytes.Buffer{}, cli.Answers{})

	got, err := i.Confirm(context.Background(), interaction.Question{ID: "q"})
	if err != nil {
		t.Fatalf("Confirm returned error: %v", err)
	}
	if got.Status != interaction.SkippedOne {
		t.Errorf("Confirm status = %q, want SkippedOne", got.Status)
	}
}

// ---------------------------------------------------------------------------
// Notify and Progress: fire-and-forget
// ---------------------------------------------------------------------------

func TestInteraction_Notify_CompletesWithinTimeout(t *testing.T) {
	i := cli.NewInteraction(&bytes.Buffer{}, cli.Answers{})

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	done := make(chan struct{})
	go func() {
		n := interaction.Notice{Level: interaction.NoticeInfo, Title: "test", Message: "msg"}
		i.Notify(ctx, n)
		close(done)
	}()

	select {
	case <-done:
		// returned promptly — correct
	case <-ctx.Done():
		t.Error("Notify did not return within 100ms; it appears to be blocking")
	}
}

func TestInteraction_Progress_CompletesWithinTimeout(t *testing.T) {
	i := cli.NewInteraction(&bytes.Buffer{}, cli.Answers{})

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	done := make(chan struct{})
	go func() {
		e := interaction.ProgressEvent{Phase: "testing", Current: 1, Total: 10, Subject: "x"}
		i.Progress(ctx, e)
		close(done)
	}()

	select {
	case <-done:
		// returned promptly — correct
	case <-ctx.Done():
		t.Error("Progress did not return within 100ms; it appears to be blocking")
	}
}

// ---------------------------------------------------------------------------
// Subject-scoped answers (QuestionPricingRate uses "{ID}:{Subject}" keying)
// ---------------------------------------------------------------------------

func TestInteraction_AskText_SubjectScopedAnswerResolved(t *testing.T) {
	// QuestionPricingRate is subject-scoped: the key is "{ID}:{Subject}"
	// where subject is "{model}:{field}".
	const subject = "claude-3-5-sonnet:input"
	const rateValue = "3.00"
	key := string(app.QuestionPricingRate) + ":" + subject
	answers := cli.Answers{key: rateValue}

	i := cli.NewInteraction(&bytes.Buffer{}, answers)

	q := interaction.TextQuestion{
		Question: interaction.Question{
			ID:      app.QuestionPricingRate,
			Subject: subject,
		},
	}
	got, err := i.AskText(context.Background(), q)
	if err != nil {
		t.Fatalf("AskText returned error: %v", err)
	}
	if got.Status != interaction.Answered {
		t.Errorf("AskText status = %q, want %q for subject-scoped answer", got.Status, interaction.Answered)
	}
	if got.Text != rateValue {
		t.Errorf("AskText text = %q, want %q", got.Text, rateValue)
	}
}
