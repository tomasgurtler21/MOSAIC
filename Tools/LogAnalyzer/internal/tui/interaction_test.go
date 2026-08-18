package tui

// interaction_test.go verifies the Interaction implementation: each question
// type is delivered and its answer (including skipped and cancelled) is
// returned to the caller without blocking indefinitely.

import (
	"context"
	"testing"

	"mosaic-common/interaction"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// drainAndAnswer reads the next Pending from the channel and writes the given
// answer to it.
func drainAndAnswer(ch <-chan Pending, ans answerMsg) Pending {
	p := <-ch
	p.reply <- ans
	return p
}

// ---------------------------------------------------------------------------
// Tests: SelectOne
// ---------------------------------------------------------------------------

func TestInteraction_SelectOne_Answered(t *testing.T) {
	i, ch := NewInteraction()
	defer i.Close()

	var got interaction.ChoiceAnswer
	done := make(chan struct{})
	go func() {
		defer close(done)
		q := interaction.ChoiceQuestion{
			Question: interaction.Question{ID: "test-select", Title: "Pick one"},
			Options: []interaction.Option{
				{ID: "a", Label: "Option A"},
				{ID: "b", Label: "Option B"},
			},
		}
		ans, err := i.SelectOne(context.Background(), q)
		if err != nil {
			t.Errorf("SelectOne returned error: %v", err)
		}
		got = ans
	}()

	p := drainAndAnswer(ch, answerMsg{
		choiceAns: interaction.ChoiceAnswer{Status: interaction.Answered, OptionID: "a"},
	})
	<-done

	if got.Status != interaction.Answered {
		t.Errorf("status = %q, want %q", got.Status, interaction.Answered)
	}
	if got.OptionID != "a" {
		t.Errorf("OptionID = %q, want %q", got.OptionID, "a")
	}
	if p.ID() != "test-select" {
		t.Errorf("Pending.ID() = %q, want %q", p.ID(), "test-select")
	}
}

func TestInteraction_SelectOne_Skipped(t *testing.T) {
	i, ch := NewInteraction()
	defer i.Close()

	var got interaction.ChoiceAnswer
	done := make(chan struct{})
	go func() {
		defer close(done)
		q := interaction.ChoiceQuestion{
			Question: interaction.Question{ID: "test-skip"},
		}
		got, _ = i.SelectOne(context.Background(), q)
	}()

	drainAndAnswer(ch, answerMsg{
		choiceAns: interaction.ChoiceAnswer{Status: interaction.SkippedOne},
	})
	<-done

	if got.Status != interaction.SkippedOne {
		t.Errorf("status = %q, want %q", got.Status, interaction.SkippedOne)
	}
}

// ---------------------------------------------------------------------------
// Tests: AskText
// ---------------------------------------------------------------------------

func TestInteraction_AskText_Answered(t *testing.T) {
	i, ch := NewInteraction()
	defer i.Close()

	var got interaction.TextAnswer
	done := make(chan struct{})
	go func() {
		defer close(done)
		q := interaction.TextQuestion{
			Question: interaction.Question{
				ID:      "pricing-rate",
				Subject: "mymodel:input",
				Title:   "Input rate",
			},
		}
		got, _ = i.AskText(context.Background(), q)
	}()

	p := drainAndAnswer(ch, answerMsg{
		textAns: interaction.TextAnswer{Status: interaction.Answered, Text: "3.00"},
	})
	<-done

	if got.Status != interaction.Answered {
		t.Errorf("status = %q, want %q", got.Status, interaction.Answered)
	}
	if got.Text != "3.00" {
		t.Errorf("Text = %q, want %q", got.Text, "3.00")
	}
	if p.Subject() != "mymodel:input" {
		t.Errorf("Pending.Subject() = %q, want %q", p.Subject(), "mymodel:input")
	}
}

func TestInteraction_AskText_Cancelled(t *testing.T) {
	i, ch := NewInteraction()
	defer i.Close()

	var got interaction.TextAnswer
	done := make(chan struct{})
	go func() {
		defer close(done)
		got, _ = i.AskText(context.Background(), interaction.TextQuestion{
			Question: interaction.Question{ID: "log-source-path"},
		})
	}()

	drainAndAnswer(ch, answerMsg{
		textAns: interaction.TextAnswer{Status: interaction.Cancelled},
	})
	<-done

	if got.Status != interaction.Cancelled {
		t.Errorf("status = %q, want %q", got.Status, interaction.Cancelled)
	}
}

// ---------------------------------------------------------------------------
// Tests: context cancellation resolves without blocking
// ---------------------------------------------------------------------------

func TestInteraction_SelectOne_ContextCancelled_DoesNotBlock(t *testing.T) {
	i, _ := NewInteraction()
	defer i.Close()

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan interaction.ChoiceAnswer, 1)
	go func() {
		ans, _ := i.SelectOne(ctx, interaction.ChoiceQuestion{
			Question: interaction.Question{ID: "log-source-path"},
		})
		done <- ans
	}()

	// Cancel the context; the SelectOne should return without a reply on ch.
	cancel()

	ans := <-done
	if ans.Status != interaction.Cancelled {
		t.Errorf("status after context cancel = %q, want %q", ans.Status, interaction.Cancelled)
	}
}

func TestInteraction_AskText_TeardownResolves(t *testing.T) {
	i, _ := NewInteraction()

	done := make(chan interaction.TextAnswer, 1)
	go func() {
		ans, _ := i.AskText(context.Background(), interaction.TextQuestion{
			Question: interaction.Question{ID: "pricing-rate", Subject: "m:input"},
		})
		done <- ans
	}()

	// Close resolves the blocked call as Cancelled.
	i.Close()

	ans := <-done
	if ans.Status != interaction.Cancelled {
		t.Errorf("status after Close() = %q, want %q", ans.Status, interaction.Cancelled)
	}
}

// ---------------------------------------------------------------------------
// Tests: Notify and Progress are non-blocking
// ---------------------------------------------------------------------------

func TestInteraction_Notify_NonBlocking(t *testing.T) {
	i, _ := NewInteraction()
	defer i.Close()

	// Should return immediately without blocking even with no consumer.
	i.Notify(context.Background(), interaction.Notice{
		Level:   interaction.NoticeInfo,
		Message: "test notice",
	})
}

func TestInteraction_Progress_NonBlocking(t *testing.T) {
	i, _ := NewInteraction()
	defer i.Close()

	i.Progress(context.Background(), interaction.ProgressEvent{Phase: "test"})
}

// ---------------------------------------------------------------------------
// Tests: Pending accessors
// ---------------------------------------------------------------------------

func TestPending_ID_And_Subject(t *testing.T) {
	i, ch := NewInteraction()
	defer i.Close()

	go func() {
		i.AskText(context.Background(), interaction.TextQuestion{
			Question: interaction.Question{
				ID:      "pricing-rate",
				Subject: "claude-model:cache_write",
			},
		})
	}()

	p := <-ch
	if p.ID() != "pricing-rate" {
		t.Errorf("ID() = %q, want %q", p.ID(), "pricing-rate")
	}
	if p.Subject() != "claude-model:cache_write" {
		t.Errorf("Subject() = %q, want %q", p.Subject(), "claude-model:cache_write")
	}

	// Reply to unblock the goroutine.
	p.reply <- answerMsg{textAns: interaction.TextAnswer{Status: interaction.SkippedOne}}
}
