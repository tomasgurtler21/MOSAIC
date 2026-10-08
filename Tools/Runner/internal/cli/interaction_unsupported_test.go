package cli_test

import (
	"bytes"
	"context"
	"testing"
	"time"

	"mosaic-common/interaction"
	"mosaic-run/internal/cli"
)

// answerWithin runs ask and fails the test when it does not return promptly:
// the non-interactive frontend must never block.
func answerWithin[T any](t *testing.T, ask func() (T, error)) T {
	t.Helper()
	type result struct {
		ans T
		err error
	}
	done := make(chan result, 1)
	go func() {
		ans, err := ask()
		done <- result{ans, err}
	}()
	select {
	case r := <-done:
		if r.err != nil {
			t.Fatalf("unexpected error: %v", r.err)
		}
		return r.ans
	case <-time.After(2 * time.Second):
		t.Fatal("the call blocked")
		panic("unreachable")
	}
}

func assertUnsupported(t *testing.T, got interaction.AnswerStatus) {
	t.Helper()
	if got != interaction.Unsupported {
		t.Errorf("Status = %q, want %q (neither %q nor %q)",
			got, interaction.Unsupported, interaction.Answered, interaction.Cancelled)
	}
}

func TestCLIInteractionSelectOneIsUnsupported(t *testing.T) {
	in := cli.NewInteraction(&bytes.Buffer{})
	q := interaction.ChoiceQuestion{
		Question: interaction.Question{DefaultOptionID: "a"},
		Options:  []interaction.Option{{ID: "a", Label: "A"}},
	}

	ans := answerWithin(t, func() (interaction.ChoiceAnswer, error) { return in.SelectOne(context.Background(), q) })

	assertUnsupported(t, ans.Status)
	if ans.OptionID != "" || ans.Custom != "" {
		t.Errorf("answer = %+v, want zero values besides Status", ans)
	}
}

func TestCLIInteractionSelectManyIsUnsupportedEvenWithDefaults(t *testing.T) {
	in := cli.NewInteraction(&bytes.Buffer{})
	q := interaction.ChoiceQuestion{
		Question:         interaction.Question{AllowCustom: true},
		Options:          []interaction.Option{{ID: "a", Label: "A"}},
		DefaultOptionIDs: []string{"a"},
	}

	ans := answerWithin(t, func() (interaction.MultiChoiceAnswer, error) { return in.SelectMany(context.Background(), q) })

	assertUnsupported(t, ans.Status)
	if len(ans.OptionIDs) != 0 || len(ans.Custom) != 0 {
		t.Errorf("answer = %+v, want zero values besides Status", ans)
	}
}

func TestCLIInteractionAskTextIsUnsupportedEvenWithDefault(t *testing.T) {
	in := cli.NewInteraction(&bytes.Buffer{})
	q := interaction.TextQuestion{Default: "suggested"}

	ans := answerWithin(t, func() (interaction.TextAnswer, error) { return in.AskText(context.Background(), q) })

	assertUnsupported(t, ans.Status)
	if ans.Text != "" {
		t.Errorf("Text = %q, want empty", ans.Text)
	}
}

func TestCLIInteractionConfirmStaysAnsweredFalse(t *testing.T) {
	in := cli.NewInteraction(&bytes.Buffer{})

	ans := answerWithin(t, func() (interaction.ConfirmAnswer, error) {
		return in.Confirm(context.Background(), interaction.Question{})
	})

	if ans.Status != interaction.Answered || ans.Confirm {
		t.Errorf("answer = %+v, want Answered with Confirm=false", ans)
	}
}
