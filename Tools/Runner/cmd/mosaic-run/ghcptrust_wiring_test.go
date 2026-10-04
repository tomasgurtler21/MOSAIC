package main

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"mosaic-common/interaction"

	"mosaic-run/internal/domain"
	"mosaic-run/internal/ghcptrust"
)

func TestTrustPreflight_CLIUntrusted_WarnsAndProceeds(t *testing.T) {
	runFolder, ws := mlRunFolder(t)
	in := &MockInteraction{}
	inner := &mlSession{start: func(context.Context, domain.RunConfig) (domain.RunOutcome, error) {
		return domain.RunOutcome{Status: domain.RunCompleted, Message: "inner done"}, nil
	}}
	checker := &MockTrustChecker{Result: trustResult(ghcptrust.StatusUntrusted)}

	out, err := newGHCPTrustPreflight(inner, newTrustConfig(runFolder, checker, in, false)).
		Start(context.Background(), domain.RunConfig{})

	if err != nil {
		t.Fatal(err)
	}
	if inner.calls != 1 {
		t.Fatalf("wrapped Start calls = %d, want 1", inner.calls)
	}
	if out.Status != domain.RunCompleted || out.Message != "inner done" {
		t.Errorf("outcome = %+v, want the wrapped outcome unchanged", out)
	}
	if len(in.Notices) != 1 {
		t.Fatalf("notices = %d, want 1 warning", len(in.Notices))
	}
	assertWarning(t, in.Notices[0], ws)
	if len(in.Questions) != 0 {
		t.Errorf("CLI must never route the decision through Confirm, got %d question(s)", len(in.Questions))
	}
}

func TestTrustPreflight_ChecksTheRunFolderParent(t *testing.T) {
	runFolder, ws := mlRunFolder(t)
	checker := &MockTrustChecker{Result: trustResult(ghcptrust.StatusTrusted)}

	_, _ = newGHCPTrustPreflight(&mlSession{}, newTrustConfig(runFolder, checker, &MockInteraction{}, false)).
		Start(context.Background(), domain.RunConfig{})

	if len(checker.Checked) != 1 || filepath.Clean(checker.Checked[0]) != filepath.Clean(ws) {
		t.Fatalf("checked = %v, want [%q]", checker.Checked, ws)
	}
}

func TestTrustPreflight_TUIUntrusted_AnswerRows(t *testing.T) {
	cases := []struct {
		name      string
		reply     interaction.ConfirmAnswer
		replyErr  error
		wantStart bool
	}{
		{"answered yes proceeds", interaction.ConfirmAnswer{Status: interaction.Answered, Confirm: true}, nil, true},
		{"answered no aborts", interaction.ConfirmAnswer{Status: interaction.Answered, Confirm: false}, nil, false},
		{"cancelled aborts", interaction.ConfirmAnswer{Status: interaction.Cancelled}, nil, false},
		{"skipped one proceeds", interaction.ConfirmAnswer{Status: interaction.SkippedOne}, nil, true},
		{"skipped all proceeds", interaction.ConfirmAnswer{Status: interaction.SkippedAll}, nil, true},
		{"confirm error proceeds", interaction.ConfirmAnswer{Status: interaction.Cancelled}, errors.New("prompt failed"), true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			runFolder, ws := mlRunFolder(t)
			in := &MockInteraction{ConfirmReply: tc.reply, ConfirmErr: tc.replyErr}
			inner := &mlSession{}
			checker := &MockTrustChecker{Result: trustResult(ghcptrust.StatusUntrusted)}

			out, err := newGHCPTrustPreflight(inner, newTrustConfig(runFolder, checker, in, true)).
				Start(context.Background(), domain.RunConfig{})

			if err != nil {
				t.Fatalf("err = %v, want nil (an abort is an outcome, not an error)", err)
			}
			if (inner.calls == 1) != tc.wantStart || inner.calls > 1 {
				t.Fatalf("wrapped Start calls = %d, want called=%v", inner.calls, tc.wantStart)
			}
			if tc.wantStart && out.Status != domain.RunCompleted {
				t.Errorf("outcome = %+v, want the wrapped outcome", out)
			}
			if !tc.wantStart {
				if out.Status != domain.RunRefused || strings.TrimSpace(out.Message) == "" {
					t.Errorf("outcome = %+v, want RunRefused with a message", out)
				}
			}
			if len(in.Notices) == 0 {
				t.Fatal("no warning shown before the question")
			}
			assertWarning(t, in.Notices[0], ws)
			if len(in.Questions) != 1 {
				t.Fatalf("questions = %d, want 1", len(in.Questions))
			}
		})
	}
}

func TestTrustPreflight_TUIQuestionContract(t *testing.T) {
	runFolder, ws := mlRunFolder(t)
	in := &MockInteraction{ConfirmReply: interaction.ConfirmAnswer{Status: interaction.Answered, Confirm: true}}
	checker := &MockTrustChecker{Result: trustResult(ghcptrust.StatusUntrusted)}

	_, _ = newGHCPTrustPreflight(&mlSession{}, newTrustConfig(runFolder, checker, in, true)).
		Start(context.Background(), domain.RunConfig{})

	if len(in.Questions) != 1 {
		t.Fatalf("questions = %d, want 1", len(in.Questions))
	}
	q := in.Questions[0]
	if q.Title == "" || q.Prompt == "" {
		t.Errorf("question needs a title and a prompt: %+v", q)
	}
	if filepath.Clean(q.Subject) != filepath.Clean(ws) {
		t.Errorf("Subject = %q, want the checked folder %q", q.Subject, ws)
	}
	if q.AllowSkip || q.AllowSkipAll {
		t.Errorf("skip options must be off: %+v", q)
	}
	if q.ID != "" || q.DefaultOptionID != "" {
		t.Errorf("ID and DefaultOptionID must be empty: %+v", q)
	}
}

func TestTrustPreflight_TUIAbort_ReturnsRefusedWithoutStartingSession(t *testing.T) {
	runFolder, _ := mlRunFolder(t)
	in := &MockInteraction{ConfirmReply: interaction.ConfirmAnswer{Status: interaction.Answered, Confirm: false}}
	inner := &mlSession{}
	checker := &MockTrustChecker{Result: trustResult(ghcptrust.StatusUntrusted)}

	out, err := newGHCPTrustPreflight(inner, newTrustConfig(runFolder, checker, in, true)).
		Start(context.Background(), domain.RunConfig{})

	if err != nil || out.Status != domain.RunRefused {
		t.Fatalf("got (%+v, %v), want RunRefused and a nil error", out, err)
	}
	if inner.calls != 0 {
		t.Errorf("wrapped Start was called %d time(s) after an abort", inner.calls)
	}
}

func TestTrustPreflight_Trusted_NoWarningNoQuestion(t *testing.T) {
	for _, interactive := range []bool{false, true} {
		runFolder, _ := mlRunFolder(t)
		in := &MockInteraction{}
		inner := &mlSession{start: func(context.Context, domain.RunConfig) (domain.RunOutcome, error) {
			return domain.RunOutcome{Status: domain.RunStopped}, errScriptedStart
		}}
		checker := &MockTrustChecker{Result: trustResult(ghcptrust.StatusTrusted)}

		out, err := newGHCPTrustPreflight(inner, newTrustConfig(runFolder, checker, in, interactive)).
			Start(context.Background(), domain.RunConfig{})

		if inner.calls != 1 || !errors.Is(err, errScriptedStart) || out.Status != domain.RunStopped {
			t.Errorf("interactive=%v: got (%+v, %v) after %d call(s), want the wrapped result unchanged",
				interactive, out, err, inner.calls)
		}
		if len(in.Notices) != 0 || len(in.Questions) != 0 {
			t.Errorf("interactive=%v: trusted folder produced notices=%d questions=%d, want none",
				interactive, len(in.Notices), len(in.Questions))
		}
	}
}

func TestTrustPreflight_Indeterminate_WarnsAndProceedsWithoutQuestion(t *testing.T) {
	for _, interactive := range []bool{false, true} {
		runFolder, ws := mlRunFolder(t)
		in := &MockInteraction{ConfirmReply: interaction.ConfirmAnswer{Status: interaction.Cancelled}}
		inner := &mlSession{}
		checker := &MockTrustChecker{Result: trustResult(ghcptrust.StatusIndeterminate)}

		out, err := newGHCPTrustPreflight(inner, newTrustConfig(runFolder, checker, in, interactive)).
			Start(context.Background(), domain.RunConfig{})

		if err != nil || out.Status != domain.RunCompleted || inner.calls != 1 {
			t.Errorf("interactive=%v: got (%+v, %v) after %d call(s), want proceed", interactive, out, err, inner.calls)
		}
		if len(in.Notices) != 1 {
			t.Fatalf("interactive=%v: notices = %d, want 1", interactive, len(in.Notices))
		}
		n := in.Notices[0]
		if n.Level != interaction.NoticeWarning || n.Title == "" ||
			!strings.Contains(strings.ToLower(n.Message), "hook") {
			t.Errorf("interactive=%v: notice %+v must be a warning about hook logging", interactive, n)
		}
		_ = ws
		if len(in.Questions) != 0 {
			t.Errorf("interactive=%v: indeterminate must not ask, got %d question(s)", interactive, len(in.Questions))
		}
	}
}

func TestTrustPreflight_OtherHarnesses_SkipTheCheck(t *testing.T) {
	for _, harnessID := range []string{"claude-code", "opencode", "fake", ""} {
		for _, interactive := range []bool{false, true} {
			runFolder, _ := mlRunFolder(t)
			in := &MockInteraction{}
			inner := &mlSession{}
			checker := &MockTrustChecker{Result: trustResult(ghcptrust.StatusUntrusted)}
			cfg := newTrustConfig(runFolder, checker, in, interactive)
			cfg.HarnessID = harnessID

			out, err := newGHCPTrustPreflight(inner, cfg).Start(context.Background(), domain.RunConfig{})

			if err != nil || out.Status != domain.RunCompleted || inner.calls != 1 {
				t.Errorf("harness %q: got (%+v, %v) after %d call(s), want pass-through", harnessID, out, err, inner.calls)
			}
			if len(checker.Checked) != 0 || len(in.Notices) != 0 || len(in.Questions) != 0 {
				t.Errorf("harness %q: checked=%d notices=%d questions=%d, want none",
					harnessID, len(checker.Checked), len(in.Notices), len(in.Questions))
			}
		}
	}
}

func TestTrustPreflight_ChecksOnEveryStart(t *testing.T) {
	runFolder, _ := mlRunFolder(t)
	in := &MockInteraction{}
	inner := &mlSession{}
	checker := &MockTrustChecker{Result: trustResult(ghcptrust.StatusUntrusted)}
	sess := newGHCPTrustPreflight(inner, newTrustConfig(runFolder, checker, in, false))

	_, _ = sess.Start(context.Background(), domain.RunConfig{})
	_, _ = sess.Start(context.Background(), domain.RunConfig{})

	if len(checker.Checked) != 2 || len(in.Notices) != 2 || inner.calls != 2 {
		t.Fatalf("checks=%d notices=%d starts=%d, want 2 each (resume re-checks)",
			len(checker.Checked), len(in.Notices), inner.calls)
	}
}

func TestTrustPreflight_ProceedReturnsWrappedErrorUnchanged(t *testing.T) {
	runFolder, _ := mlRunFolder(t)
	inner := &mlSession{start: func(context.Context, domain.RunConfig) (domain.RunOutcome, error) {
		return domain.RunOutcome{Status: domain.RunStopped, Message: "m"}, errScriptedStart
	}}
	checker := &MockTrustChecker{Result: trustResult(ghcptrust.StatusUntrusted)}

	out, err := newGHCPTrustPreflight(inner, newTrustConfig(runFolder, checker, &MockInteraction{}, false)).
		Start(context.Background(), domain.RunConfig{})

	if !errors.Is(err, errScriptedStart) || out.Status != domain.RunStopped || out.Message != "m" {
		t.Fatalf("got (%+v, %v), want the wrapped outcome and error unchanged", out, err)
	}
}
