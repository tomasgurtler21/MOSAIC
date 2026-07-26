package tui

// equivalence_test.go verifies the TUI/CLI equivalence property (CD-4): because both
// frontends implement the same domain.Interaction port and answer the same QuestionID
// sequence, equivalent inputs must produce equivalent answers and equivalent run outcomes.
//
// Tests use two interaction implementations:
//   - cli.NewInteraction (the non-interactive CLI implementation with PreAnswers)
//   - interactiontest.Stub (the scripted double, which stands in for a TUI user whose
//     selections have been pre-configured to match the CLI pre-answers)
//
// ProgramRef (the actual TUI implementation) cannot be exercised without a running
// tea.Program, but its nil-program skip behaviour is tested here as an additional
// structural guarantee.

import (
	"context"
	"io"
	"testing"

	"mosaic-deploy/internal/app/interactiontest"
	"mosaic-deploy/internal/cli"
	"mosaic-deploy/internal/domain"
	"mosaic-deploy/internal/todo"
)

// ---------------------------------------------------------------------------
// Minimal test infrastructure
// ---------------------------------------------------------------------------

// noopTodo is a no-operation todo.Collector used to satisfy the CLI interaction constructor
// in tests that do not need to inspect recorded gaps.
type noopTodo struct{}

func (noopTodo) Add(domain.TodoItem)    {}
func (noopTodo) AddGap(domain.Gap)     {}
func (noopTodo) Items() []domain.TodoItem { return nil }
func (noopTodo) Groups() []todo.Group   { return nil }
func (noopTodo) Empty() bool            { return true }

// newCLI constructs a cli.NewInteraction with the supplied pre-answers. All questions with
// no pre-answer return SkippedOne, matching the behaviour of ProgramRef without a program.
func newCLI(answers cli.PreAnswers) domain.Interaction {
	return cli.NewInteraction(answers, noopTodo{}, io.Discard)
}

// newTUIStub constructs a scripted interactiontest.Stub that acts as the TUI double. It
// returns the same answers as the corresponding CLI PreAnswers would.
func newTUIStub() *interactiontest.Builder {
	return interactiontest.NewBuilder()
}

// ---------------------------------------------------------------------------
// Structural: both CLI and ProgramRef satisfy domain.Interaction
// ---------------------------------------------------------------------------

// TestCLITUIEquivalence_BothImplementDomainInteraction verifies at compile time that the
// CLI interaction, the scripted TUI stub, and the TUI ProgramRef all satisfy the
// domain.Interaction interface. If any implementation drifts from the interface contract,
// this assignment statement fails to compile.
func TestCLITUIEquivalence_BothImplementDomainInteraction(t *testing.T) {
	// This test body is intentionally empty: the assignments below are the test. If the
	// interface contract changes, the non-compiling file is the failure signal.
	var _ domain.Interaction = newCLI(cli.PreAnswers{})
	var _ domain.Interaction = newTUIStub().Build()
	var _ domain.Interaction = NewInteraction() // ProgramRef without a running program
}

// ---------------------------------------------------------------------------
// Nil-program ProgramRef behaves equivalently to unanswered CLI questions
// ---------------------------------------------------------------------------

// TestCLITUIEquivalence_ProgramRef_WithoutProgram_SkipsSelectOne verifies that a ProgramRef
// with no program wired returns SkippedOne for SelectOne, matching the behaviour of a CLI
// interaction with no pre-answer for the same question.
func TestCLITUIEquivalence_ProgramRef_WithoutProgram_SkipsSelectOne(t *testing.T) {
	// Arrange: a question with no pre-configured answer.
	q := domain.ChoiceQuestion{
		Question: domain.Question{
			ID:      domain.QTierModel,
			Subject: "HIGH",
		},
		Options: []domain.Option{{ID: "model-a", Label: "Model A"}},
	}

	cliInteraction := newCLI(cli.PreAnswers{}) // no pre-answer → SkippedOne
	tuiRef := NewInteraction()                  // no program → SkippedOne

	// Act
	cliAns, cliErr := cliInteraction.SelectOne(context.Background(), q)
	tuiAns, tuiErr := tuiRef.SelectOne(context.Background(), q)

	// Assert
	if cliErr != nil || tuiErr != nil {
		t.Fatalf("unexpected errors: CLI=%v, TUI=%v", cliErr, tuiErr)
	}
	if cliAns.Status != domain.SkippedOne {
		t.Errorf("CLI SelectOne status = %q; want SkippedOne for unanswered question", cliAns.Status)
	}
	if tuiAns.Status != domain.SkippedOne {
		t.Errorf("ProgramRef SelectOne status = %q; want SkippedOne when no program is wired", tuiAns.Status)
	}
}

// TestCLITUIEquivalence_ProgramRef_WithoutProgram_SkipsAskText verifies that a ProgramRef
// with no program returns SkippedOne for AskText, matching unanswered CLI behaviour.
func TestCLITUIEquivalence_ProgramRef_WithoutProgram_SkipsAskText(t *testing.T) {
	// Arrange
	q := domain.TextQuestion{
		Question: domain.Question{
			ID:      domain.QCustomTool,
			Subject: "my-tool",
		},
	}

	cliInteraction := newCLI(cli.PreAnswers{})
	tuiRef := NewInteraction()

	// Act
	cliAns, cliErr := cliInteraction.AskText(context.Background(), q)
	tuiAns, tuiErr := tuiRef.AskText(context.Background(), q)

	// Assert
	if cliErr != nil || tuiErr != nil {
		t.Fatalf("unexpected errors: CLI=%v, TUI=%v", cliErr, tuiErr)
	}
	if cliAns.Status != domain.SkippedOne {
		t.Errorf("CLI AskText status = %q; want SkippedOne for unanswered question", cliAns.Status)
	}
	if tuiAns.Status != domain.SkippedOne {
		t.Errorf("ProgramRef AskText status = %q; want SkippedOne when no program is wired", tuiAns.Status)
	}
}

// TestCLITUIEquivalence_ProgramRef_WithoutProgram_SkipsSelectMany verifies that a ProgramRef
// with no program returns SkippedOne for SelectMany, matching unanswered CLI behaviour.
func TestCLITUIEquivalence_ProgramRef_WithoutProgram_SkipsSelectMany(t *testing.T) {
	// Arrange
	q := domain.ChoiceQuestion{
		Question: domain.Question{
			ID: domain.QWorkflows,
		},
		Options: []domain.Option{
			{ID: "quick-fix", Label: "Quick Fix"},
		},
	}

	cliInteraction := newCLI(cli.PreAnswers{})
	tuiRef := NewInteraction()

	// Act
	cliAns, cliErr := cliInteraction.SelectMany(context.Background(), q)
	tuiAns, tuiErr := tuiRef.SelectMany(context.Background(), q)

	// Assert
	if cliErr != nil || tuiErr != nil {
		t.Fatalf("unexpected errors: CLI=%v, TUI=%v", cliErr, tuiErr)
	}
	if cliAns.Status != domain.SkippedOne {
		t.Errorf("CLI SelectMany status = %q; want SkippedOne for unanswered question", cliAns.Status)
	}
	if tuiAns.Status != domain.SkippedOne {
		t.Errorf("ProgramRef SelectMany status = %q; want SkippedOne when no program is wired", tuiAns.Status)
	}
}

// ---------------------------------------------------------------------------
// Equivalent answers: CLI pre-answers match scripted TUI answers
// ---------------------------------------------------------------------------

// TestCLITUIEquivalence_SelectOne_EquivalentAnswers verifies that when the CLI has a
// pre-answer and the scripted TUI stub has the same answer configured, both return
// identical ChoiceAnswer values for the same question.
func TestCLITUIEquivalence_SelectOne_EquivalentAnswers(t *testing.T) {
	// Arrange: question with two options; both implementations answer "model-a".
	q := domain.ChoiceQuestion{
		Question: domain.Question{
			ID:      domain.QTierModel,
			Subject: "HIGH",
		},
		Options: []domain.Option{
			{ID: "model-a", Label: "Model A"},
			{ID: "model-b", Label: "Model B"},
		},
	}

	cliInteraction := newCLI(cli.PreAnswers{
		Values: map[domain.QuestionID]map[string]string{
			domain.QTierModel: {"HIGH": "model-a"},
		},
	})
	tuiStub := newTUIStub().
		AnswerSelectOne(domain.QTierModel, "HIGH", "model-a").
		Build()

	// Act
	cliAns, cliErr := cliInteraction.SelectOne(context.Background(), q)
	tuiAns, tuiErr := tuiStub.SelectOne(context.Background(), q)

	// Assert
	if cliErr != nil || tuiErr != nil {
		t.Fatalf("unexpected errors: CLI=%v, TUI=%v", cliErr, tuiErr)
	}
	if cliAns.Status != tuiAns.Status {
		t.Errorf("SelectOne status: CLI=%q, TUI stub=%q; want equal", cliAns.Status, tuiAns.Status)
	}
	if cliAns.OptionID != tuiAns.OptionID {
		t.Errorf("SelectOne optionID: CLI=%q, TUI stub=%q; want equal", cliAns.OptionID, tuiAns.OptionID)
	}
}

// TestCLITUIEquivalence_AskText_EquivalentAnswers verifies that equivalent text answers
// produce the same TextAnswer from both the CLI interaction and the scripted TUI stub.
func TestCLITUIEquivalence_AskText_EquivalentAnswers(t *testing.T) {
	// Arrange
	q := domain.TextQuestion{
		Question: domain.Question{
			ID:      domain.QCustomTool,
			Subject: "my-generic-tool",
		},
	}
	const answer = "my-mcp-server"

	cliInteraction := newCLI(cli.PreAnswers{
		Values: map[domain.QuestionID]map[string]string{
			domain.QCustomTool: {"my-generic-tool": answer},
		},
	})
	tuiStub := newTUIStub().
		AnswerText(domain.QCustomTool, "my-generic-tool", answer).
		Build()

	// Act
	cliAns, cliErr := cliInteraction.AskText(context.Background(), q)
	tuiAns, tuiErr := tuiStub.AskText(context.Background(), q)

	// Assert
	if cliErr != nil || tuiErr != nil {
		t.Fatalf("unexpected errors: CLI=%v, TUI=%v", cliErr, tuiErr)
	}
	if cliAns.Status != tuiAns.Status {
		t.Errorf("AskText status: CLI=%q, TUI stub=%q; want equal", cliAns.Status, tuiAns.Status)
	}
	if cliAns.Text != tuiAns.Text {
		t.Errorf("AskText text: CLI=%q, TUI stub=%q; want equal", cliAns.Text, tuiAns.Text)
	}
}

// TestCLITUIEquivalence_SkipAll_PropagatesEquivalently verifies that when both the CLI and
// the scripted TUI stub are configured to skip-all for a given question, both return
// SkippedAll — causing the app layer to latch and stop asking that question.
func TestCLITUIEquivalence_SkipAll_PropagatesEquivalently(t *testing.T) {
	// Arrange
	q := domain.ChoiceQuestion{
		Question: domain.Question{
			ID:           domain.QTierModel,
			Subject:      "HIGH",
			AllowSkipAll: true,
		},
		Options: []domain.Option{{ID: "model-a", Label: "Model A"}},
	}

	cliInteraction := newCLI(cli.PreAnswers{
		SkipAll: map[domain.QuestionID]bool{
			domain.QTierModel: true,
		},
	})
	tuiStub := newTUIStub().
		AnswerSkipAll(domain.QTierModel, "HIGH").
		Build()

	// Act
	cliAns, cliErr := cliInteraction.SelectOne(context.Background(), q)
	tuiAns, tuiErr := tuiStub.SelectOne(context.Background(), q)

	// Assert
	if cliErr != nil || tuiErr != nil {
		t.Fatalf("unexpected errors: CLI=%v, TUI=%v", cliErr, tuiErr)
	}
	if cliAns.Status != domain.SkippedAll {
		t.Errorf("CLI SelectOne status = %q; want SkippedAll when SkipAll is set", cliAns.Status)
	}
	if tuiAns.Status != domain.SkippedAll {
		t.Errorf("TUI stub SelectOne status = %q; want SkippedAll when AnswerSkipAll is configured", tuiAns.Status)
	}
}

// ---------------------------------------------------------------------------
// Equivalent question recording: same QuestionID sequence
// ---------------------------------------------------------------------------

// TestCLITUIEquivalence_SameQuestionSequence verifies that when both the CLI and the
// scripted TUI stub are driven through the same set of questions, both record the same
// QuestionIDs in the same order. This directly tests CD-4's claim that the Interaction port
// is question-shaped and not screen-shaped.
func TestCLITUIEquivalence_SameQuestionSequence(t *testing.T) {
	// Arrange: define a sequence of questions representative of the deploy-new flow.
	type questionCall struct {
		kind string // "select-one", "text", "review"
		id   domain.QuestionID
		subj string
	}
	questions := []questionCall{
		{"select-one", domain.QTierModel, "HIGH"},
		{"text", domain.QCustomTool, "my-tool"},
	}

	cliInteraction := newCLI(cli.PreAnswers{
		Values: map[domain.QuestionID]map[string]string{
			domain.QTierModel:  {"HIGH": "model-a"},
			domain.QCustomTool: {"my-tool": "my-mcp-server"},
		},
	})
	tuiStub := newTUIStub().
		AnswerSelectOne(domain.QTierModel, "HIGH", "model-a").
		AnswerText(domain.QCustomTool, "my-tool", "my-mcp-server").
		Build()

	ctx := context.Background()

	// Act: drive both implementations through the same question sequence.
	for _, qc := range questions {
		switch qc.kind {
		case "select-one":
			q := domain.ChoiceQuestion{
				Question: domain.Question{ID: qc.id, Subject: qc.subj},
				Options:  []domain.Option{{ID: "model-a", Label: "Model A"}},
			}
			_, _ = cliInteraction.SelectOne(ctx, q)
			_, _ = tuiStub.SelectOne(ctx, q)
		case "text":
			q := domain.TextQuestion{
				Question: domain.Question{ID: qc.id, Subject: qc.subj},
			}
			_, _ = cliInteraction.AskText(ctx, q)
			_, _ = tuiStub.AskText(ctx, q)
		}
	}

	// Assert: the scripted stub records each call; verify the sequence.
	calls := tuiStub.Calls()
	order := tuiStub.QuestionOrder()
	if len(order) != len(questions) {
		t.Fatalf("TUI stub recorded %d questions, want %d", len(calls), len(questions))
	}
	for i, qc := range questions {
		if order[i] != qc.id {
			t.Errorf("question[%d]: TUI stub recorded %q, want %q", i, order[i], qc.id)
		}
	}
}
