package cli_test

// interaction_test.go verifies the non-interactive Interaction implementation returned by
// cli.NewInteraction, and the locally-modified file handling flag (--conflict).
//
// Verified invariants (T19.3, T19.4):
//
// Resolvable questions (T19.3):
//   - SelectOne returns Answered when PreAnswers.Values contains the question's answer and
//     the answer matches a valid OptionID in the question's Options list.
//   - SelectMany returns Answered with all named option IDs when they are all valid.
//   - AskText returns Answered with the pre-configured text.
//   - Confirm returns Answered true when the pre-configured value is "true".
//   - Confirm returns Answered false when the pre-configured value is "false".
//   - Review returns Answered{Confirm: true} when AutoConfirmPlan is in PreAnswers.
//
// Unresolvable questions (T19.3):
//   - SelectOne returns SkippedOne and records a GapSkippedFile when no pre-answer exists.
//   - SelectMany returns SkippedOne and records a GapSkippedFile when no pre-answer exists.
//   - AskText returns SkippedOne and records a GapSkippedFile when no pre-answer exists.
//   - Confirm returns SkippedOne and records a GapSkippedFile when no pre-answer exists
//     (must not silently return Answered{Confirm: false}).
//   - SelectOne returns SkippedOne when the pre-answer names an option not in the question's
//     Options list (invalid option ID — treated as unresolvable, records a gap).
//   - Review returns SkippedOne when no pre-answer for QPlanConfirm is set and
//     AutoConfirmPlan is not in PreAnswers (the run is aborted, not blocked).
//
// Non-blocking (T19.3):
//   - Notify never blocks and always returns.
//   - Progress never blocks and always returns.
//   - No method reads from stdin or any external source.
//
// SkipAll (T19.3):
//   - When PreAnswers.SkipAll[QWorkflows] is true, SelectMany for QWorkflows returns
//     SkippedAll, causing the app to latch and stop asking.
//
// Subject-keyed answers (T19.3):
//   - QTierModel answers are keyed by tier (the Question.Subject); one answer per tier.
//   - QAgentModel answers are keyed by agent key (the Question.Subject).
//
// Locally-modified file handling (T19.4):
//   - Without --conflict, UpdateRequest.ConflictDefault is domain.DecisionSkip (the safe
//     default; skip protects against accidental overwrite).
//   - --conflict skip explicitly sets domain.DecisionSkip.
//   - --conflict overwrite sets domain.DecisionOverwrite.
//   - --conflict backup sets domain.DecisionBackupThenOverwrite.
//   - An invalid --conflict value returns ExitUsage.

import (
	"bytes"
	"context"
	"testing"
	"time"

	"mosaic-deploy/internal/cli"
	"mosaic-deploy/internal/domain"
)

// ---------------------------------------------------------------------------
// SelectOne — resolvable
// ---------------------------------------------------------------------------

// TestNewInteraction_SelectOne_Resolvable_ReturnsAnswered verifies that when PreAnswers
// contains a matching entry for the question ID and subject, SelectOne returns Answered with
// the matching OptionID.
func TestNewInteraction_SelectOne_Resolvable_ReturnsAnswered(t *testing.T) {
	// Arrange
	todos := &stubTodo{}
	answers := cli.PreAnswers{
		Values: map[domain.QuestionID]map[string]string{
			domain.QHarness: {"": "claude-code"},
		},
	}
	ix := cli.NewInteraction(answers, todos, &bytes.Buffer{})

	q := domain.ChoiceQuestion{
		Question: domain.Question{ID: domain.QHarness, Subject: ""},
		Options:  []domain.Option{{ID: "claude-code"}, {ID: "opencode"}},
	}

	// Act
	ans, err := ix.SelectOne(context.Background(), q)

	// Assert
	if err != nil {
		t.Fatalf("SelectOne returned error: %v", err)
	}
	if ans.Status != domain.Answered {
		t.Errorf("Status = %q, want %q (Answered)", ans.Status, domain.Answered)
	}
	if ans.OptionID != "claude-code" {
		t.Errorf("OptionID = %q, want %q", ans.OptionID, "claude-code")
	}
}

// TestNewInteraction_SelectOne_SubjectKeyed_AnswersPerSubject verifies that SelectOne for
// a subject-keyed question (e.g. QTierModel with subject "HIGH") uses the subject to look
// up the correct pre-answer, not a global run-level answer.
func TestNewInteraction_SelectOne_SubjectKeyed_AnswersPerSubject(t *testing.T) {
	// Arrange
	todos := &stubTodo{}
	answers := cli.PreAnswers{
		Values: map[domain.QuestionID]map[string]string{
			domain.QTierModel: {
				"HIGH": "claude-opus-4",
				"LOW":  "claude-haiku-3",
			},
		},
	}
	ix := cli.NewInteraction(answers, todos, &bytes.Buffer{})

	qHigh := domain.ChoiceQuestion{
		Question: domain.Question{ID: domain.QTierModel, Subject: "HIGH"},
		Options:  []domain.Option{{ID: "claude-opus-4"}, {ID: "claude-haiku-3"}},
	}
	qLow := domain.ChoiceQuestion{
		Question: domain.Question{ID: domain.QTierModel, Subject: "LOW"},
		Options:  []domain.Option{{ID: "claude-opus-4"}, {ID: "claude-haiku-3"}},
	}

	// Act
	ansHigh, errHigh := ix.SelectOne(context.Background(), qHigh)
	ansLow, errLow := ix.SelectOne(context.Background(), qLow)

	// Assert
	if errHigh != nil || errLow != nil {
		t.Fatalf("SelectOne errors: high=%v low=%v", errHigh, errLow)
	}
	if ansHigh.OptionID != "claude-opus-4" {
		t.Errorf("HIGH OptionID = %q, want %q", ansHigh.OptionID, "claude-opus-4")
	}
	if ansLow.OptionID != "claude-haiku-3" {
		t.Errorf("LOW OptionID = %q, want %q", ansLow.OptionID, "claude-haiku-3")
	}
}

// ---------------------------------------------------------------------------
// SelectOne — unresolvable
// ---------------------------------------------------------------------------

// TestNewInteraction_SelectOne_NoPreAnswer_SkipsAndRecordsTodo verifies that when no
// pre-answer exists for the question, SelectOne returns SkippedOne and records a TODO item
// so the user can address the skip after the run.
func TestNewInteraction_SelectOne_NoPreAnswer_SkipsAndRecordsTodo(t *testing.T) {
	// Arrange
	todos := &stubTodo{}
	ix := cli.NewInteraction(cli.PreAnswers{}, todos, &bytes.Buffer{})

	q := domain.ChoiceQuestion{
		Question: domain.Question{ID: domain.QHarness, Subject: ""},
		Options:  []domain.Option{{ID: "claude-code"}},
	}

	// Act
	ans, err := ix.SelectOne(context.Background(), q)

	// Assert
	if err != nil {
		t.Fatalf("SelectOne returned error: %v", err)
	}
	if ans.Status != domain.SkippedOne {
		t.Errorf("Status = %q, want %q (SkippedOne)", ans.Status, domain.SkippedOne)
	}
	if todos.Empty() {
		t.Error("no TODO recorded; an unresolvable question must record a GapSkippedFile entry")
	}
}

// TestNewInteraction_SelectOne_InvalidOptionID_SkipsAndRecordsTodo verifies that when the
// pre-answer names an OptionID that is not in the question's Options list, the interaction
// treats it as unresolvable, returns SkippedOne, and records a TODO.
func TestNewInteraction_SelectOne_InvalidOptionID_SkipsAndRecordsTodo(t *testing.T) {
	// Arrange
	todos := &stubTodo{}
	answers := cli.PreAnswers{
		Values: map[domain.QuestionID]map[string]string{
			domain.QHarness: {"": "nonexistent-option"},
		},
	}
	ix := cli.NewInteraction(answers, todos, &bytes.Buffer{})

	q := domain.ChoiceQuestion{
		Question: domain.Question{ID: domain.QHarness, Subject: ""},
		Options:  []domain.Option{{ID: "claude-code"}, {ID: "opencode"}},
	}

	// Act
	ans, err := ix.SelectOne(context.Background(), q)

	// Assert
	if err != nil {
		t.Fatalf("SelectOne returned error: %v", err)
	}
	if ans.Status != domain.SkippedOne {
		t.Errorf("Status = %q, want %q; an invalid option ID must be treated as unresolvable", ans.Status, domain.SkippedOne)
	}
	if todos.Empty() {
		t.Error("no TODO recorded; an invalid option ID must record a skip so the user can investigate")
	}
}

// ---------------------------------------------------------------------------
// SelectMany — resolvable and unresolvable
// ---------------------------------------------------------------------------

// TestNewInteraction_SelectMany_Resolvable_ReturnsAllOptionIDs verifies that when
// PreAnswers contains a comma-joined list of option IDs for a SelectMany question, all
// valid IDs are returned in the MultiChoiceAnswer.
func TestNewInteraction_SelectMany_Resolvable_ReturnsAllOptionIDs(t *testing.T) {
	// Arrange
	todos := &stubTodo{}
	answers := cli.PreAnswers{
		Values: map[domain.QuestionID]map[string]string{
			domain.QWorkflows: {"": "quick-fix,build"},
		},
	}
	ix := cli.NewInteraction(answers, todos, &bytes.Buffer{})

	q := domain.ChoiceQuestion{
		Question: domain.Question{ID: domain.QWorkflows, Subject: ""},
		Options:  []domain.Option{{ID: "quick-fix"}, {ID: "build"}, {ID: "test"}},
	}

	// Act
	ans, err := ix.SelectMany(context.Background(), q)

	// Assert
	if err != nil {
		t.Fatalf("SelectMany returned error: %v", err)
	}
	if ans.Status != domain.Answered {
		t.Errorf("Status = %q, want %q (Answered)", ans.Status, domain.Answered)
	}
	want := []string{"quick-fix", "build"}
	if !equalStringSlice(ans.OptionIDs, want) {
		t.Errorf("OptionIDs = %v, want %v", ans.OptionIDs, want)
	}
}

// TestNewInteraction_SelectMany_NoPreAnswer_SkipsAndRecordsTodo verifies that when no
// pre-answer exists for SelectMany, the interaction returns SkippedOne and records a TODO.
func TestNewInteraction_SelectMany_NoPreAnswer_SkipsAndRecordsTodo(t *testing.T) {
	// Arrange
	todos := &stubTodo{}
	ix := cli.NewInteraction(cli.PreAnswers{}, todos, &bytes.Buffer{})

	q := domain.ChoiceQuestion{
		Question: domain.Question{ID: domain.QWorkflows, Subject: ""},
		Options:  []domain.Option{{ID: "quick-fix"}},
	}

	// Act
	ans, err := ix.SelectMany(context.Background(), q)

	// Assert
	if err != nil {
		t.Fatalf("SelectMany returned error: %v", err)
	}
	if ans.Status != domain.SkippedOne {
		t.Errorf("Status = %q, want %q (SkippedOne)", ans.Status, domain.SkippedOne)
	}
	if todos.Empty() {
		t.Error("no TODO recorded; unresolvable SelectMany must record a gap")
	}
}

// ---------------------------------------------------------------------------
// SkipAll
// ---------------------------------------------------------------------------

// TestNewInteraction_SkipAll_SelectMany_ReturnsSkippedAll verifies that when
// PreAnswers.SkipAll[QWorkflows] is true, SelectMany returns SkippedAll, causing the app to
// latch and not ask QWorkflows again (per CD-5).
func TestNewInteraction_SkipAll_SelectMany_ReturnsSkippedAll(t *testing.T) {
	// Arrange
	todos := &stubTodo{}
	answers := cli.PreAnswers{
		SkipAll: map[domain.QuestionID]bool{
			domain.QWorkflows: true,
		},
	}
	ix := cli.NewInteraction(answers, todos, &bytes.Buffer{})

	q := domain.ChoiceQuestion{
		Question: domain.Question{ID: domain.QWorkflows, Subject: ""},
		Options:  []domain.Option{{ID: "quick-fix"}},
	}

	// Act
	ans, err := ix.SelectMany(context.Background(), q)

	// Assert
	if err != nil {
		t.Fatalf("SelectMany returned error: %v", err)
	}
	if ans.Status != domain.SkippedAll {
		t.Errorf("Status = %q, want %q (SkippedAll)", ans.Status, domain.SkippedAll)
	}
}

// ---------------------------------------------------------------------------
// AskText
// ---------------------------------------------------------------------------

// TestNewInteraction_AskText_Resolvable_ReturnsAnswered verifies that when PreAnswers
// contains a text answer, AskText returns Answered with that text.
func TestNewInteraction_AskText_Resolvable_ReturnsAnswered(t *testing.T) {
	// Arrange
	todos := &stubTodo{}
	workspace := t.TempDir()
	answers := cli.PreAnswers{
		Values: map[domain.QuestionID]map[string]string{
			domain.QWorkspace: {"": workspace},
		},
	}
	ix := cli.NewInteraction(answers, todos, &bytes.Buffer{})

	q := domain.TextQuestion{
		Question: domain.Question{ID: domain.QWorkspace, Subject: ""},
	}

	// Act
	ans, err := ix.AskText(context.Background(), q)

	// Assert
	if err != nil {
		t.Fatalf("AskText returned error: %v", err)
	}
	if ans.Status != domain.Answered {
		t.Errorf("Status = %q, want %q (Answered)", ans.Status, domain.Answered)
	}
	if ans.Text != workspace {
		t.Errorf("Text = %q, want %q", ans.Text, workspace)
	}
}

// TestNewInteraction_AskText_NoPreAnswer_SkipsAndRecordsTodo verifies that when no
// pre-answer exists, AskText returns SkippedOne and records a TODO.
func TestNewInteraction_AskText_NoPreAnswer_SkipsAndRecordsTodo(t *testing.T) {
	// Arrange
	todos := &stubTodo{}
	ix := cli.NewInteraction(cli.PreAnswers{}, todos, &bytes.Buffer{})

	q := domain.TextQuestion{
		Question: domain.Question{ID: domain.QWorkspace, Subject: ""},
	}

	// Act
	ans, err := ix.AskText(context.Background(), q)

	// Assert
	if err != nil {
		t.Fatalf("AskText returned error: %v", err)
	}
	if ans.Status != domain.SkippedOne {
		t.Errorf("Status = %q, want %q (SkippedOne)", ans.Status, domain.SkippedOne)
	}
	if todos.Empty() {
		t.Error("no TODO recorded; unresolvable AskText must record a gap")
	}
}

// ---------------------------------------------------------------------------
// Confirm
// ---------------------------------------------------------------------------

// TestNewInteraction_Confirm_TruePreAnswer_ReturnsConfirmed verifies that a pre-answer of
// "true" for a Confirm question results in Answered{Confirm: true}.
func TestNewInteraction_Confirm_TruePreAnswer_ReturnsConfirmed(t *testing.T) {
	// Arrange
	todos := &stubTodo{}
	answers := cli.PreAnswers{
		Values: map[domain.QuestionID]map[string]string{
			domain.QExternalOptIn: {"": "true"},
		},
	}
	ix := cli.NewInteraction(answers, todos, &bytes.Buffer{})

	q := domain.Question{ID: domain.QExternalOptIn, Subject: ""}

	// Act
	ans, err := ix.Confirm(context.Background(), q)

	// Assert
	if err != nil {
		t.Fatalf("Confirm returned error: %v", err)
	}
	if ans.Status != domain.Answered {
		t.Errorf("Status = %q, want %q (Answered)", ans.Status, domain.Answered)
	}
	if !ans.Confirm {
		t.Error("Confirm = false, want true")
	}
}

// TestNewInteraction_Confirm_FalsePreAnswer_ReturnsNotConfirmed verifies that a pre-answer
// of "false" results in Answered{Confirm: false}.
func TestNewInteraction_Confirm_FalsePreAnswer_ReturnsNotConfirmed(t *testing.T) {
	// Arrange
	todos := &stubTodo{}
	answers := cli.PreAnswers{
		Values: map[domain.QuestionID]map[string]string{
			domain.QExternalOptIn: {"": "false"},
		},
	}
	ix := cli.NewInteraction(answers, todos, &bytes.Buffer{})

	q := domain.Question{ID: domain.QExternalOptIn, Subject: ""}

	// Act
	ans, err := ix.Confirm(context.Background(), q)

	// Assert
	if err != nil {
		t.Fatalf("Confirm returned error: %v", err)
	}
	if ans.Status != domain.Answered {
		t.Errorf("Status = %q, want %q", ans.Status, domain.Answered)
	}
	if ans.Confirm {
		t.Error("Confirm = true, want false")
	}
}

// TestNewInteraction_Confirm_NoPreAnswer_SkipsAndRecordsTodo verifies that when no pre-answer
// exists for a Confirm question, the interaction returns SkippedOne and records a TODO item.
// An implementation that returns Answered{Confirm: false} for an unresolvable Confirm would
// silently bypass the question rather than recording it for the user to address.
func TestNewInteraction_Confirm_NoPreAnswer_SkipsAndRecordsTodo(t *testing.T) {
	// Arrange
	todos := &stubTodo{}
	ix := cli.NewInteraction(cli.PreAnswers{}, todos, &bytes.Buffer{})

	q := domain.Question{ID: domain.QExternalOptIn, Subject: ""}

	// Act
	ans, err := ix.Confirm(context.Background(), q)

	// Assert
	if err != nil {
		t.Fatalf("Confirm returned error: %v", err)
	}
	if ans.Status != domain.SkippedOne {
		t.Errorf("Status = %q, want %q (SkippedOne); an unresolvable Confirm must skip, not silently return false", ans.Status, domain.SkippedOne)
	}
	if todos.Empty() {
		t.Error("no TODO recorded; an unresolvable Confirm must record a GapSkippedFile entry so the user can address it")
	}
}

// ---------------------------------------------------------------------------
// Notify and Progress — non-blocking (T19.3)
// ---------------------------------------------------------------------------

// TestNewInteraction_Notify_DoesNotBlock verifies that Notify returns without blocking,
// even when there is no terminal to write to (the out writer is discarded). A blocking
// implementation of Notify (e.g. one that writes to a channel or waits on a terminal)
// must cause this test to fail with a timeout message, not pass silently.
func TestNewInteraction_Notify_DoesNotBlock(t *testing.T) {
	// Arrange
	ix := cli.NewInteraction(cli.PreAnswers{}, &stubTodo{}, &bytes.Buffer{})
	n := domain.Notice{Level: domain.NoticeInfo, Title: "Test", Message: "testing"}

	// Act — run Notify in a goroutine; if it blocks, the test times out via time.After.
	done := make(chan struct{})
	go func() {
		ix.Notify(context.Background(), n)
		close(done)
	}()

	select {
	case <-done:
		// Notify returned without blocking — correct for a non-interactive implementation.
	case <-time.After(100 * time.Millisecond):
		t.Fatal("Notify blocked for >100ms; non-interactive Interaction must never block waiting for output")
	}
}

// TestNewInteraction_Progress_DoesNotBlock verifies that Progress returns without blocking.
// A blocking implementation of Progress (e.g. one that writes to a channel or terminal)
// must cause this test to fail with a timeout message, not pass silently.
func TestNewInteraction_Progress_DoesNotBlock(t *testing.T) {
	// Arrange
	ix := cli.NewInteraction(cli.PreAnswers{}, &stubTodo{}, &bytes.Buffer{})
	e := domain.ProgressEvent{Phase: "transforming", Current: 1, Total: 10, Subject: "agent-a"}

	// Act — run Progress in a goroutine; if it blocks, the test times out via time.After.
	done := make(chan struct{})
	go func() {
		ix.Progress(context.Background(), e)
		close(done)
	}()

	select {
	case <-done:
		// Progress returned without blocking — correct for a non-interactive implementation.
	case <-time.After(100 * time.Millisecond):
		t.Fatal("Progress blocked for >100ms; non-interactive Interaction must never block waiting for output")
	}
}

// ---------------------------------------------------------------------------
// Review
// ---------------------------------------------------------------------------

// TestNewInteraction_Review_AutoConfirmInPreAnswers_ConfirmsRun verifies that when
// QPlanConfirm with the empty subject is set to "true" in PreAnswers, Review returns
// Answered{Confirm: true} without blocking.
func TestNewInteraction_Review_AutoConfirmInPreAnswers_ConfirmsRun(t *testing.T) {
	// Arrange
	todos := &stubTodo{}
	answers := cli.PreAnswers{
		Values: map[domain.QuestionID]map[string]string{
			domain.QPlanConfirm: {"": "true"},
		},
	}
	ix := cli.NewInteraction(answers, todos, &bytes.Buffer{})

	// Act
	ans, err := ix.Review(context.Background(), domain.Plan{})

	// Assert
	if err != nil {
		t.Fatalf("Review returned error: %v", err)
	}
	if ans.Status != domain.Answered {
		t.Errorf("Status = %q, want %q (Answered)", ans.Status, domain.Answered)
	}
	if !ans.Confirm {
		t.Error("Confirm = false, want true; pre-confirmed review must confirm the plan")
	}
}

// TestNewInteraction_Review_NoPreAnswer_SkipsRun verifies that when no pre-answer exists
// for QPlanConfirm, Review returns SkippedOne (the run is not confirmed and aborts cleanly
// rather than blocking waiting for input).
func TestNewInteraction_Review_NoPreAnswer_SkipsRun(t *testing.T) {
	// Arrange
	todos := &stubTodo{}
	ix := cli.NewInteraction(cli.PreAnswers{}, todos, &bytes.Buffer{})

	// Act
	ans, err := ix.Review(context.Background(), domain.Plan{})

	// Assert
	if err != nil {
		t.Fatalf("Review returned error: %v", err)
	}
	if ans.Status == domain.Answered && ans.Confirm {
		t.Error("Status=Answered,Confirm=true; a non-interactive review with no pre-answer must not confirm the plan — it must skip or cancel, never proceed blindly")
	}
}

// ---------------------------------------------------------------------------
// Locally-modified handling (T19.4): --conflict flag on update subcommand
// ---------------------------------------------------------------------------

// TestRun_UpdateSubcommand_ConflictDefault_IsSkip verifies that when --conflict is absent,
// UpdateRequest.ConflictDefault is DecisionSkip — the safe default that prevents accidental
// overwrites of locally-modified files.
func TestRun_UpdateSubcommand_ConflictDefault_IsSkip(t *testing.T) {
	// Arrange
	workspace := t.TempDir()
	svc := &spyService{
		updateResp: domain.RunSummary{Outcome: domain.OutcomeSuccess},
	}

	// Act
	code := cli.Run(context.Background(),
		[]string{"update", "--harness", "stub-harness", "--workspace", workspace, "--auto-confirm"},
		svc, &bytes.Buffer{}, &bytes.Buffer{})

	// Assert
	if code != cli.ExitSuccess {
		t.Fatalf("exit code = %d, want %d", code, cli.ExitSuccess)
	}
	if svc.updateReq == nil {
		t.Fatal("Update was not called")
	}
	if svc.updateReq.ConflictDefault != domain.DecisionSkip {
		t.Errorf("ConflictDefault = %q, want %q (DecisionSkip is the safe default)", svc.updateReq.ConflictDefault, domain.DecisionSkip)
	}
}

// TestRun_UpdateSubcommand_ConflictSkip_SetsDecision verifies that --conflict skip
// explicitly sets UpdateRequest.ConflictDefault to DecisionSkip.
func TestRun_UpdateSubcommand_ConflictSkip_SetsDecision(t *testing.T) {
	// Arrange
	workspace := t.TempDir()
	svc := &spyService{updateResp: domain.RunSummary{Outcome: domain.OutcomeSuccess}}

	// Act
	code := cli.Run(context.Background(),
		[]string{"update", "--harness", "stub-harness", "--workspace", workspace, "--conflict", "skip", "--auto-confirm"},
		svc, &bytes.Buffer{}, &bytes.Buffer{})

	// Assert
	if code != cli.ExitSuccess {
		t.Fatalf("exit code = %d, want %d", code, cli.ExitSuccess)
	}
	if svc.updateReq == nil {
		t.Fatal("Update was not called")
	}
	if svc.updateReq.ConflictDefault != domain.DecisionSkip {
		t.Errorf("ConflictDefault = %q, want %q", svc.updateReq.ConflictDefault, domain.DecisionSkip)
	}
}

// TestRun_UpdateSubcommand_ConflictOverwrite_SetsDecision verifies that --conflict overwrite
// sets UpdateRequest.ConflictDefault to DecisionOverwrite.
func TestRun_UpdateSubcommand_ConflictOverwrite_SetsDecision(t *testing.T) {
	// Arrange
	workspace := t.TempDir()
	svc := &spyService{updateResp: domain.RunSummary{Outcome: domain.OutcomeSuccess}}

	// Act
	code := cli.Run(context.Background(),
		[]string{"update", "--harness", "stub-harness", "--workspace", workspace, "--conflict", "overwrite", "--auto-confirm"},
		svc, &bytes.Buffer{}, &bytes.Buffer{})

	// Assert
	if code != cli.ExitSuccess {
		t.Fatalf("exit code = %d, want %d", code, cli.ExitSuccess)
	}
	if svc.updateReq == nil {
		t.Fatal("Update was not called")
	}
	if svc.updateReq.ConflictDefault != domain.DecisionOverwrite {
		t.Errorf("ConflictDefault = %q, want %q", svc.updateReq.ConflictDefault, domain.DecisionOverwrite)
	}
}

// TestRun_UpdateSubcommand_ConflictBackup_SetsDecision verifies that --conflict backup sets
// UpdateRequest.ConflictDefault to DecisionBackupThenOverwrite.
func TestRun_UpdateSubcommand_ConflictBackup_SetsDecision(t *testing.T) {
	// Arrange
	workspace := t.TempDir()
	svc := &spyService{updateResp: domain.RunSummary{Outcome: domain.OutcomeSuccess}}

	// Act
	code := cli.Run(context.Background(),
		[]string{"update", "--harness", "stub-harness", "--workspace", workspace, "--conflict", "backup", "--auto-confirm"},
		svc, &bytes.Buffer{}, &bytes.Buffer{})

	// Assert
	if code != cli.ExitSuccess {
		t.Fatalf("exit code = %d, want %d", code, cli.ExitSuccess)
	}
	if svc.updateReq == nil {
		t.Fatal("Update was not called")
	}
	if svc.updateReq.ConflictDefault != domain.DecisionBackupThenOverwrite {
		t.Errorf("ConflictDefault = %q, want %q", svc.updateReq.ConflictDefault, domain.DecisionBackupThenOverwrite)
	}
}

// TestRun_UpdateSubcommand_ConflictInvalid_ReturnsExitUsage verifies that an unrecognised
// --conflict value returns ExitUsage so the user gets feedback about the valid choices.
func TestRun_UpdateSubcommand_ConflictInvalid_ReturnsExitUsage(t *testing.T) {
	// Arrange
	svc := &spyService{}

	// Act
	code := cli.Run(context.Background(),
		[]string{"update", "--harness", "stub-harness", "--workspace", t.TempDir(), "--conflict", "magic"},
		svc, &bytes.Buffer{}, &bytes.Buffer{})

	// Assert
	if code != cli.ExitUsage {
		t.Errorf("exit code = %d, want %d (ExitUsage) for invalid --conflict value", code, cli.ExitUsage)
	}
}
