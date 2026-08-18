package tui

// frontend_equivalence_new_modes_test.go verifies the TUI/CLI equivalence property (CD-4)
// for the two new run modes: ModeDeployAgents and ModeDeployHooks.
//
// Both frontends implement the same domain.Interaction port and answer the same QuestionID
// sequence. Equivalent inputs must produce equivalent answers and equivalent run outcomes.
//
// Tests use two interaction implementations:
//   - cli.NewInteraction (the non-interactive CLI implementation with PreAnswers)
//   - interactiontest.Stub (the scripted double, which stands in for a TUI user whose
//     selections have been pre-configured to match the CLI pre-answers)
//
// All tests are RED-phase TDD tests. They fail until:
//   - cli.PreAnswers is extended with AgentIDs field OR the existing Values map is populated
//     by the "agents" subcommand's flag-to-preanswers conversion for QDeployAgents.
//   - cli.PreAnswers is extended with HookIDs field OR the existing Values map is populated
//     by the "hooks" subcommand's flag-to-preanswers conversion for QHooks.
//   - The CLI interaction correctly resolves QDeployAgents as a comma-split SelectMany answer.
//   - The CLI interaction correctly resolves QHooks as a comma-split SelectMany answer
//     (it already handles QHooks for the deploy-workspace mode; the deploy-hooks mode reuses
//     the same question ID).
//
// The tests that verify nil (SkippedOne) equivalence pass in RED phase because both
// implementations already return SkippedOne when no pre-answer is configured.

import (
	"context"
	"testing"

	"mosaic-deploy/internal/cli"
	"mosaic-deploy/internal/domain"
)

// ---------------------------------------------------------------------------
// QDeployAgents: nil → SkippedOne (both frontends agree no answer means ask)
// ---------------------------------------------------------------------------

// TestCLITUIEquivalence_DeployAgents_NoPreAnswer_BothSkip verifies that when no
// pre-answer is configured for QDeployAgents, both the CLI interaction and the scripted TUI
// stub return SkippedOne, which instructs the app layer to ask the question interactively.
// This is the nil-means-ask half of CD-6.
func TestCLITUIEquivalence_DeployAgents_NoPreAnswer_BothSkip(t *testing.T) {
	q := domain.ChoiceQuestion{
		Question: domain.Question{
			ID:      domain.QDeployAgents,
			Subject: "",
		},
		Options: []domain.Option{
			{ID: "agent-alpha", Label: "Agent Alpha", Group: "SynthGroup"},
			{ID: "agent-beta", Label: "Agent Beta", Group: "SynthGroup"},
		},
	}

	cliInteraction := newCLI(cli.PreAnswers{}) // no pre-answer → SkippedOne
	tuiStub := newTUIStub().Build()             // no configured answer → SkippedOne

	cliAns, cliErr := cliInteraction.SelectMany(context.Background(), q)
	tuiAns, tuiErr := tuiStub.SelectMany(context.Background(), q)

	if cliErr != nil || tuiErr != nil {
		t.Fatalf("unexpected errors: CLI=%v, TUI=%v", cliErr, tuiErr)
	}
	if cliAns.Status != domain.SkippedOne {
		t.Errorf("CLI SelectMany for QDeployAgents status = %q; want SkippedOne when no pre-answer is configured",
			cliAns.Status)
	}
	if tuiAns.Status != domain.SkippedOne {
		t.Errorf("TUI stub SelectMany for QDeployAgents status = %q; want SkippedOne when no answer is configured",
			tuiAns.Status)
	}
}

// ---------------------------------------------------------------------------
// QDeployAgents: pre-answered IDs → both return equivalent Answered slice
// ---------------------------------------------------------------------------

// TestCLITUIEquivalence_DeployAgents_PreAnsweredIDs_BothReturnSameIDs verifies that when
// both implementations have the same agent IDs configured, both return the same OptionIDs
// slice in the Answered ChoiceAnswer. This exercises the fully non-interactive path: the
// CLI has flag values converted to comma-joined PreAnswers.Values[QDeployAgents][""], and
// the TUI stub has AnswerSelectMany configured with the same IDs.
func TestCLITUIEquivalence_DeployAgents_PreAnsweredIDs_BothReturnSameIDs(t *testing.T) {
	q := domain.ChoiceQuestion{
		Question: domain.Question{
			ID:      domain.QDeployAgents,
			Subject: "",
		},
		Options: []domain.Option{
			{ID: "agent-alpha", Label: "Agent Alpha", Group: "SynthGroup-A"},
			{ID: "agent-beta", Label: "Agent Beta", Group: "SynthGroup-A"},
			{ID: "agent-gamma", Label: "Agent Gamma", Group: "SynthGroup-B"},
		},
	}

	wantIDs := []string{"agent-alpha", "agent-gamma"}

	// CLI pre-answers: comma-joined option IDs at subject "" (run-level).
	cliInteraction := newCLI(cli.PreAnswers{
		Values: map[domain.QuestionID]map[string]string{
			domain.QDeployAgents: {"": "agent-alpha,agent-gamma"},
		},
	})
	// TUI stub: the same IDs pre-configured via AnswerSelectMany.
	tuiStub := newTUIStub().
		AnswerSelectMany(domain.QDeployAgents, "", wantIDs).
		Build()

	cliAns, cliErr := cliInteraction.SelectMany(context.Background(), q)
	tuiAns, tuiErr := tuiStub.SelectMany(context.Background(), q)

	if cliErr != nil || tuiErr != nil {
		t.Fatalf("unexpected errors: CLI=%v, TUI=%v", cliErr, tuiErr)
	}
	if cliAns.Status != domain.Answered {
		t.Errorf("CLI SelectMany status = %q; want Answered when QDeployAgents is pre-answered",
			cliAns.Status)
	}
	if tuiAns.Status != domain.Answered {
		t.Errorf("TUI stub SelectMany status = %q; want Answered when QDeployAgents is configured",
			tuiAns.Status)
	}
	// Check that CLI and TUI return equivalent IDs (same set, order may differ).
	if !sameIDSet(cliAns.OptionIDs, tuiAns.OptionIDs) {
		t.Errorf("CLI OptionIDs = %v, TUI stub OptionIDs = %v; want the same set",
			cliAns.OptionIDs, tuiAns.OptionIDs)
	}
	// Verify the content against the expected IDs.
	if !sameIDSet(cliAns.OptionIDs, wantIDs) {
		t.Errorf("CLI OptionIDs = %v; want %v", cliAns.OptionIDs, wantIDs)
	}
}

// TestCLITUIEquivalence_DeployAgents_EmptyPreAnswer_BothReturnEmpty verifies that when
// both implementations have an explicitly-empty pre-answer for QDeployAgents (non-nil empty
// slice), both return Answered with an empty OptionIDs slice. This is the "explicitly none"
// path: no agents are selected; the app layer treats this as a deliberate choice and does
// not ask the question interactively.
func TestCLITUIEquivalence_DeployAgents_EmptyPreAnswer_BothReturnEmpty(t *testing.T) {
	q := domain.ChoiceQuestion{
		Question: domain.Question{
			ID:      domain.QDeployAgents,
			Subject: "",
		},
		Options: []domain.Option{
			{ID: "agent-alpha", Label: "Agent Alpha", Group: "SynthGroup-A"},
		},
	}

	// CLI: empty string at subject "" = explicitly none, parsed as non-nil empty slice.
	cliInteraction := newCLI(cli.PreAnswers{
		Values: map[domain.QuestionID]map[string]string{
			domain.QDeployAgents: {"": ""},
		},
	})
	// TUI stub: empty slice = explicitly none.
	tuiStub := newTUIStub().
		AnswerSelectMany(domain.QDeployAgents, "", []string{}).
		Build()

	cliAns, cliErr := cliInteraction.SelectMany(context.Background(), q)
	tuiAns, tuiErr := tuiStub.SelectMany(context.Background(), q)

	if cliErr != nil || tuiErr != nil {
		t.Fatalf("unexpected errors: CLI=%v, TUI=%v", cliErr, tuiErr)
	}
	if cliAns.Status != domain.Answered {
		t.Errorf("CLI SelectMany status = %q; want Answered for empty explicit pre-answer",
			cliAns.Status)
	}
	if tuiAns.Status != domain.Answered {
		t.Errorf("TUI stub SelectMany status = %q; want Answered for empty explicit answer",
			tuiAns.Status)
	}
	if len(cliAns.OptionIDs) != 0 {
		t.Errorf("CLI OptionIDs = %v; want empty slice for explicit-none answer", cliAns.OptionIDs)
	}
	if len(tuiAns.OptionIDs) != 0 {
		t.Errorf("TUI stub OptionIDs = %v; want empty slice for explicit-none answer", tuiAns.OptionIDs)
	}
}

// ---------------------------------------------------------------------------
// QHooks (deploy-hooks mode): nil → SkippedOne (both frontends agree no answer means ask)
// ---------------------------------------------------------------------------

// TestCLITUIEquivalence_DeployHooks_NoPreAnswer_BothSkip verifies that when no
// pre-answer is configured for QHooks (deploy-hooks mode), both the CLI interaction and the scripted TUI
// stub return SkippedOne. This is the nil-means-ask half of CD-6 for hooks.
func TestCLITUIEquivalence_DeployHooks_NoPreAnswer_BothSkip(t *testing.T) {
	q := domain.ChoiceQuestion{
		Question: domain.Question{
			ID:      domain.QHooks,
			Subject: "",
		},
		Options: []domain.Option{
			{ID: "on-deploy", Label: "On Deploy"},
			{ID: "on-teardown", Label: "On Teardown"},
		},
	}

	cliInteraction := newCLI(cli.PreAnswers{})
	tuiStub := newTUIStub().Build()

	cliAns, cliErr := cliInteraction.SelectMany(context.Background(), q)
	tuiAns, tuiErr := tuiStub.SelectMany(context.Background(), q)

	if cliErr != nil || tuiErr != nil {
		t.Fatalf("unexpected errors: CLI=%v, TUI=%v", cliErr, tuiErr)
	}
	if cliAns.Status != domain.SkippedOne {
		t.Errorf("CLI SelectMany for QHooks (deploy-hooks mode) status = %q; want SkippedOne when no pre-answer",
			cliAns.Status)
	}
	if tuiAns.Status != domain.SkippedOne {
		t.Errorf("TUI stub SelectMany for QHooks (deploy-hooks mode) status = %q; want SkippedOne when no answer",
			tuiAns.Status)
	}
}

// ---------------------------------------------------------------------------
// QHooks (deploy-hooks mode): pre-answered IDs → both return equivalent Answered slice
// ---------------------------------------------------------------------------

// TestCLITUIEquivalence_DeployHooks_PreAnsweredIDs_BothReturnSameIDs verifies that when
// both implementations have the same hook IDs configured, both return the same OptionIDs
// slice in the Answered ChoiceAnswer. This exercises the fully non-interactive path for
// the deploy-hooks mode.
func TestCLITUIEquivalence_DeployHooks_PreAnsweredIDs_BothReturnSameIDs(t *testing.T) {
	q := domain.ChoiceQuestion{
		Question: domain.Question{
			ID:      domain.QHooks,
			Subject: "",
		},
		Options: []domain.Option{
			{ID: "on-deploy", Label: "On Deploy"},
			{ID: "on-teardown", Label: "On Teardown"},
			{ID: "on-update", Label: "On Update"},
		},
	}

	wantIDs := []string{"on-deploy", "on-teardown"}

	cliInteraction := newCLI(cli.PreAnswers{
		Values: map[domain.QuestionID]map[string]string{
			domain.QHooks: {"": "on-deploy,on-teardown"},
		},
	})
	tuiStub := newTUIStub().
		AnswerSelectMany(domain.QHooks, "", wantIDs).
		Build()

	cliAns, cliErr := cliInteraction.SelectMany(context.Background(), q)
	tuiAns, tuiErr := tuiStub.SelectMany(context.Background(), q)

	if cliErr != nil || tuiErr != nil {
		t.Fatalf("unexpected errors: CLI=%v, TUI=%v", cliErr, tuiErr)
	}
	if cliAns.Status != domain.Answered {
		t.Errorf("CLI SelectMany status = %q; want Answered when QHooks (deploy-hooks mode) is pre-answered",
			cliAns.Status)
	}
	if tuiAns.Status != domain.Answered {
		t.Errorf("TUI stub SelectMany status = %q; want Answered when QHooks (deploy-hooks mode) is configured",
			tuiAns.Status)
	}
	if !sameIDSet(cliAns.OptionIDs, tuiAns.OptionIDs) {
		t.Errorf("CLI OptionIDs = %v, TUI stub OptionIDs = %v; want the same set",
			cliAns.OptionIDs, tuiAns.OptionIDs)
	}
	if !sameIDSet(cliAns.OptionIDs, wantIDs) {
		t.Errorf("CLI OptionIDs = %v; want %v", cliAns.OptionIDs, wantIDs)
	}
}

// TestCLITUIEquivalence_DeployHooks_EmptyPreAnswer_BothReturnEmpty verifies the
// explicitly-empty path for QHooks (deploy-hooks mode): non-nil empty means "deploy no hooks" without
// interactive prompting (CD-6 empty semantics).
func TestCLITUIEquivalence_DeployHooks_EmptyPreAnswer_BothReturnEmpty(t *testing.T) {
	q := domain.ChoiceQuestion{
		Question: domain.Question{
			ID:      domain.QHooks,
			Subject: "",
		},
		Options: []domain.Option{
			{ID: "on-deploy", Label: "On Deploy"},
		},
	}

	cliInteraction := newCLI(cli.PreAnswers{
		Values: map[domain.QuestionID]map[string]string{
			domain.QHooks: {"": ""},
		},
	})
	tuiStub := newTUIStub().
		AnswerSelectMany(domain.QHooks, "", []string{}).
		Build()

	cliAns, cliErr := cliInteraction.SelectMany(context.Background(), q)
	tuiAns, tuiErr := tuiStub.SelectMany(context.Background(), q)

	if cliErr != nil || tuiErr != nil {
		t.Fatalf("unexpected errors: CLI=%v, TUI=%v", cliErr, tuiErr)
	}
	if cliAns.Status != domain.Answered {
		t.Errorf("CLI SelectMany status = %q; want Answered for empty explicit pre-answer",
			cliAns.Status)
	}
	if tuiAns.Status != domain.Answered {
		t.Errorf("TUI stub SelectMany status = %q; want Answered for empty explicit answer",
			tuiAns.Status)
	}
	if len(cliAns.OptionIDs) != 0 {
		t.Errorf("CLI OptionIDs = %v; want empty slice for explicit-none answer", cliAns.OptionIDs)
	}
	if len(tuiAns.OptionIDs) != 0 {
		t.Errorf("TUI stub OptionIDs = %v; want empty slice for explicit-none answer", tuiAns.OptionIDs)
	}
}

// ---------------------------------------------------------------------------
// Structural: QDeployAgents and QHooks (deploy-hooks mode) satisfy domain.Interaction.SelectMany
// ---------------------------------------------------------------------------

// TestCLITUIEquivalence_NewModes_BothImplementDomainInteraction is a compile-time check
// that the CLI and TUI stub both satisfy domain.Interaction for the new mode question IDs.
// It does not execute logic; the compiler is the test.
func TestCLITUIEquivalence_NewModes_BothImplementDomainInteraction(t *testing.T) {
	var _ domain.Interaction = newCLI(cli.PreAnswers{})
	var _ domain.Interaction = newTUIStub().Build()
	// Both interact with QDeployAgents and QHooks (deploy-hooks mode) via the generic SelectMany method,
	// which is already part of the interface. The presence of these question ID constants
	// in domain is the structural guarantee.
	_ = domain.QDeployAgents
	_ = domain.QHooks
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// sameIDSet reports whether a and b contain exactly the same string IDs (order-insensitive).
func sameIDSet(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	set := make(map[string]int, len(a))
	for _, id := range a {
		set[id]++
	}
	for _, id := range b {
		set[id]--
		if set[id] < 0 {
			return false
		}
	}
	return true
}

