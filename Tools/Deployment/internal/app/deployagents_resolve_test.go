package app_test

// deployagents_resolve_test.go verifies the merged agent-option builder for QDeployAgents.
//
// These are TDD RED-phase tests. They assert the intended behaviour of the
// askDeployAgents function that does not yet exist (I3.3). Every test that
// exercises the question builder will receive a "not yet implemented" error until
// I3.3 is delivered.
//
// Verified behaviours:
//
// Group naming and catalog scanning (T3.2):
//   - Options presented to QDeployAgents span all three catalog roots: Agents(),
//     UtilityAgents(), and StandaloneAgents().
//   - Group names distinguish the three roots: subagent options carry
//     "Subagents/<Category>" or "Subagents" (uncategorised); utility agents carry
//     "UtilityAgents"; standalone agents carry "StandaloneAgents/<Category>" or
//     "StandaloneAgents".
//   - Group names are derived entirely from catalog content. A fixture category
//     that appears nowhere in the production source code still appears in the
//     option groups, proving the list is scan-based rather than hardcoded.
//   - The orchestrator is never among the options; the orchestrator accessors are
//     never unioned in.
//   - Utility agents outside the UtilityAgentAllowList are still offered; the
//     allow-list gate applies only to the existing askUtilityAgents function used
//     by the Deploy workspace flow.

import (
	"context"
	"sync"
	"testing"

	"mosaic-deploy/internal/app"
	"mosaic-deploy/internal/app/interactiontest"
	"mosaic-deploy/internal/domain"
)

// ---------------------------------------------------------------------------
// optionCapturingInteraction — captures SelectMany question options for assertion
// ---------------------------------------------------------------------------

// deployAgentsOptionCapture wraps an interactiontest.Stub and additionally records
// the full ChoiceQuestion for every SelectMany call. This lets tests assert on
// option content — specifically the Group fields — which the Stub does not expose.
type deployAgentsOptionCapture struct {
	*interactiontest.Stub
	mu              sync.Mutex
	capturedOptions map[domain.QuestionID][]domain.Option
}

// SelectMany overrides the embedded Stub's SelectMany to capture the question's options
// before delegating to the stub for scripted answer delivery.
func (s *deployAgentsOptionCapture) SelectMany(ctx context.Context, q domain.ChoiceQuestion) (domain.MultiChoiceAnswer, error) {
	s.mu.Lock()
	if s.capturedOptions == nil {
		s.capturedOptions = make(map[domain.QuestionID][]domain.Option)
	}
	opts := make([]domain.Option, len(q.Options))
	copy(opts, q.Options)
	s.capturedOptions[q.ID] = opts
	s.mu.Unlock()
	return s.Stub.SelectMany(ctx, q)
}

// OptionsFor returns the options that were passed to SelectMany for the given QuestionID.
// Returns nil if SelectMany was never called for that ID.
func (s *deployAgentsOptionCapture) OptionsFor(id domain.QuestionID) []domain.Option {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.capturedOptions[id]
}

// newCapturingInteraction wraps stub in a deployAgentsOptionCapture.
func newCapturingInteraction(stub *interactiontest.Stub) *deployAgentsOptionCapture {
	return &deployAgentsOptionCapture{Stub: stub}
}

// ---------------------------------------------------------------------------
// Fixtures for option builder tests
// ---------------------------------------------------------------------------

// deployAgentOptionCatalog returns a catalog designed to exercise the group-naming
// requirements. It has:
//   - Two subagents with different categories ("Research", "ZFixtureCategory") — the latter
//     name appears nowhere in the production source, proving scan-based behaviour.
//   - One utility agent (deliberately NOT in the tool config's allow-list, to verify
//     that the allow-list is intentionally bypassed for QDeployAgents).
//   - One standalone agent with a category ("Examples").
//   - The orchestrator (must NOT appear in options).
func deployAgentOptionCatalog() *stubCatalog {
	return &stubCatalog{
		root: "testroot",
		orchestrator: domain.Agent{
			Key:  "orchestrator",
			Role: domain.RoleOrchestrator,
			Name: "Orchestrator",
		},
		agents: []domain.Agent{
			{
				Key:      "research-helper",
				Role:     domain.RoleSubagent,
				Category: "Research",
				Name:     "Research Helper",
				Version:  "1.0",
			},
			{
				Key:      "zzz-fixture-agent",
				Role:     domain.RoleSubagent,
				Category: "ZFixtureCategory",
				Name:     "Fixture Agent",
				Version:  "1.0",
			},
		},
		utilityAgents: []domain.Agent{
			{
				Key:     "non-allowlisted-utility",
				Role:    domain.RoleUtility,
				Name:    "Non-AllowListed Utility",
				Version: "1.0",
			},
		},
		standaloneAgents: []domain.Agent{
			{
				Key:      "session-logger",
				Role:     domain.RoleStandalone,
				Category: "Examples",
				Name:     "Session Logger",
				Version:  "1.0",
			},
		},
	}
}

// deployAgentsPlanNoOrchestrator returns a minimal plan for the deploy-agents mode.
func deployAgentsPlanNoOrchestrator(workspace string) domain.Plan {
	return domain.Plan{
		Mode:          domain.ModeDeployAgents,
		WorkspacePath: workspace,
		Scope:         domain.ScopeProject,
		Items:         []domain.PlanItem{},
	}
}

// ---------------------------------------------------------------------------
// T3.2 — QDeployAgents options span all three catalog roots
// ---------------------------------------------------------------------------

// TestDeployAgents_OptionBuilder_OptionsSpanAllThreeRoots verifies that the ChoiceQuestion
// emitted for QDeployAgents includes options from Agents(), UtilityAgents(), and
// StandaloneAgents(). Options from each source must be present in the merged list so the
// user has one browsable view of the entire deployable catalog.
func TestDeployAgents_OptionBuilder_OptionsSpanAllThreeRoots(t *testing.T) {
	// Arrange — stub returns empty selection; all we need is to capture the question.
	stub := interactiontest.NewBuilder().
		AnswerSelectMany(domain.QDeployAgents, "", []string{}).
		AnswerReview(true).
		Build()
	capInteraction := newCapturingInteraction(stub)

	deps, workspace := newBaseDeps(t, stub)
	deps.Catalog = deployAgentOptionCatalog()
	deps.Planner = &stubPlanner{plan: deployAgentsPlanNoOrchestrator(workspace)}
	deps.Interaction = capInteraction
	svc := app.New(deps)

	// Act — all four ID slices nil so QDeployAgents is asked.
	_, _ = svc.DeployAgents(context.Background(), app.DeployAgentsRequest{
		HarnessID:     "stub-harness",
		WorkspacePath: workspace,
		// All four IDs nil → QDeployAgents must be presented.
		AutoConfirmPlan: true,
	})

	// Assert — options for QDeployAgents must have been captured.
	opts := capInteraction.OptionsFor(domain.QDeployAgents)
	if len(opts) == 0 {
		t.Fatal("QDeployAgents was never presented with options; " +
			"DeployAgents must call askDeployAgents which emits a ChoiceQuestion with QDeployAgents ID; " +
			"the stub returns \"not yet implemented\" until I3.3 is delivered")
	}

	// Verify each expected agent key appears in the options.
	optIDSet := make(map[string]bool, len(opts))
	for _, o := range opts {
		optIDSet[o.ID] = true
	}
	for _, key := range []string{"research-helper", "zzz-fixture-agent", "non-allowlisted-utility", "session-logger"} {
		if !optIDSet[key] {
			t.Errorf("QDeployAgents options missing %q; "+
				"askDeployAgents must include all three catalog roots in the merged option list",
				key)
		}
	}
}

// ---------------------------------------------------------------------------
// T3.2 — Group names distinguish the three catalog roots
// ---------------------------------------------------------------------------

// TestDeployAgents_OptionBuilder_SubagentGroupPrefixedWithSubagents verifies that options
// sourced from Catalog.Agents() carry a Group of "Subagents/<Category>" (or "Subagents"
// for uncategorised agents). This lets the frontend reconstruct a tree mirroring the
// Catalog/Subagents/ folder hierarchy.
func TestDeployAgents_OptionBuilder_SubagentGroupPrefixedWithSubagents(t *testing.T) {
	stub := interactiontest.NewBuilder().
		AnswerSelectMany(domain.QDeployAgents, "", []string{}).
		AnswerReview(true).
		Build()
	capInteraction := newCapturingInteraction(stub)

	deps, workspace := newBaseDeps(t, stub)
	deps.Catalog = deployAgentOptionCatalog()
	deps.Planner = &stubPlanner{plan: deployAgentsPlanNoOrchestrator(workspace)}
	deps.Interaction = capInteraction
	svc := app.New(deps)

	_, _ = svc.DeployAgents(context.Background(), app.DeployAgentsRequest{
		HarnessID:     "stub-harness",
		WorkspacePath: workspace,
		AutoConfirmPlan: true,
	})

	opts := capInteraction.OptionsFor(domain.QDeployAgents)
	if len(opts) == 0 {
		t.Skip("QDeployAgents was not presented with options; skipping group-name assertion until I3.3")
	}

	// Find the "research-helper" option (Agents(), category="Research") and check its group.
	for _, o := range opts {
		if o.ID == "research-helper" {
			if o.Group != "Subagents/Research" {
				t.Errorf("option \"research-helper\" has Group=%q, want %q; "+
					"options from Catalog.Agents() with a non-empty Category must have "+
					"Group = \"Subagents/\" + Category so the frontend can reconstruct "+
					"the Catalog/Subagents/<Category> tree",
					o.Group, "Subagents/Research")
			}
			return
		}
	}
	t.Error("option \"research-helper\" not found in QDeployAgents options; " +
		"agents from Catalog.Agents() must be included in the merged option list")
}

// TestDeployAgents_OptionBuilder_UtilityAgentGroupIsUtilityAgents verifies that options
// sourced from Catalog.UtilityAgents() carry Group = "UtilityAgents", regardless of any
// Category the utility agent might carry. UtilityAgents live in a flat collection under
// Catalog/UtilityAgents/ with no subcategories.
func TestDeployAgents_OptionBuilder_UtilityAgentGroupIsUtilityAgents(t *testing.T) {
	stub := interactiontest.NewBuilder().
		AnswerSelectMany(domain.QDeployAgents, "", []string{}).
		AnswerReview(true).
		Build()
	capInteraction := newCapturingInteraction(stub)

	deps, workspace := newBaseDeps(t, stub)
	deps.Catalog = deployAgentOptionCatalog()
	deps.Planner = &stubPlanner{plan: deployAgentsPlanNoOrchestrator(workspace)}
	deps.Interaction = capInteraction
	svc := app.New(deps)

	_, _ = svc.DeployAgents(context.Background(), app.DeployAgentsRequest{
		HarnessID:     "stub-harness",
		WorkspacePath: workspace,
		AutoConfirmPlan: true,
	})

	opts := capInteraction.OptionsFor(domain.QDeployAgents)
	if len(opts) == 0 {
		t.Skip("QDeployAgents was not presented with options; skipping group-name assertion until I3.3")
	}

	for _, o := range opts {
		if o.ID == "non-allowlisted-utility" {
			if o.Group != "UtilityAgents" {
				t.Errorf("utility agent option \"non-allowlisted-utility\" has Group=%q, want %q; "+
					"options from Catalog.UtilityAgents() must carry Group = \"UtilityAgents\"",
					o.Group, "UtilityAgents")
			}
			return
		}
	}
	t.Error("option \"non-allowlisted-utility\" not found in QDeployAgents options; " +
		"Catalog.UtilityAgents() must be included without allow-list filtering for QDeployAgents")
}

// TestDeployAgents_OptionBuilder_StandaloneGroupPrefixedWithStandaloneAgents verifies that
// options from Catalog.StandaloneAgents() carry Group = "StandaloneAgents/<Category>" (or
// "StandaloneAgents" for uncategorised agents).
func TestDeployAgents_OptionBuilder_StandaloneGroupPrefixedWithStandaloneAgents(t *testing.T) {
	stub := interactiontest.NewBuilder().
		AnswerSelectMany(domain.QDeployAgents, "", []string{}).
		AnswerReview(true).
		Build()
	capInteraction := newCapturingInteraction(stub)

	deps, workspace := newBaseDeps(t, stub)
	deps.Catalog = deployAgentOptionCatalog()
	deps.Planner = &stubPlanner{plan: deployAgentsPlanNoOrchestrator(workspace)}
	deps.Interaction = capInteraction
	svc := app.New(deps)

	_, _ = svc.DeployAgents(context.Background(), app.DeployAgentsRequest{
		HarnessID:     "stub-harness",
		WorkspacePath: workspace,
		AutoConfirmPlan: true,
	})

	opts := capInteraction.OptionsFor(domain.QDeployAgents)
	if len(opts) == 0 {
		t.Skip("QDeployAgents was not presented with options; skipping group-name assertion until I3.3")
	}

	for _, o := range opts {
		if o.ID == "session-logger" {
			if o.Group != "StandaloneAgents/Examples" {
				t.Errorf("standalone agent option \"session-logger\" has Group=%q, want %q; "+
					"options from Catalog.StandaloneAgents() with a non-empty Category must have "+
					"Group = \"StandaloneAgents/\" + Category",
					o.Group, "StandaloneAgents/Examples")
			}
			return
		}
	}
	t.Error("option \"session-logger\" not found in QDeployAgents options; " +
		"Catalog.StandaloneAgents() must be included in the merged option list")
}

// ---------------------------------------------------------------------------
// T3.2 — Scan-based grouping: a fixture category the code never names still appears
// ---------------------------------------------------------------------------

// TestDeployAgents_OptionBuilder_FixtureCategoryAppearsWithoutHardcoding verifies that an
// agent whose Category is "ZFixtureCategory" — a string that appears nowhere in the
// production source — still produces a group "Subagents/ZFixtureCategory" in the option
// list. This is the key proof that grouping is scan-based: a hardcoded enumeration would
// never include a category invented at test time.
func TestDeployAgents_OptionBuilder_FixtureCategoryAppearsWithoutHardcoding(t *testing.T) {
	stub := interactiontest.NewBuilder().
		AnswerSelectMany(domain.QDeployAgents, "", []string{}).
		AnswerReview(true).
		Build()
	capInteraction := newCapturingInteraction(stub)

	deps, workspace := newBaseDeps(t, stub)
	deps.Catalog = deployAgentOptionCatalog()
	deps.Planner = &stubPlanner{plan: deployAgentsPlanNoOrchestrator(workspace)}
	deps.Interaction = capInteraction
	svc := app.New(deps)

	_, _ = svc.DeployAgents(context.Background(), app.DeployAgentsRequest{
		HarnessID:     "stub-harness",
		WorkspacePath: workspace,
		AutoConfirmPlan: true,
	})

	opts := capInteraction.OptionsFor(domain.QDeployAgents)
	if len(opts) == 0 {
		t.Skip("QDeployAgents was not presented with options; skipping fixture-category assertion until I3.3")
	}

	for _, o := range opts {
		if o.ID == "zzz-fixture-agent" {
			if o.Group != "Subagents/ZFixtureCategory" {
				t.Errorf("option \"zzz-fixture-agent\" has Group=%q, want %q; "+
					"the group is derived by scanning the agent's Category field, not by "+
					"enumerating known category names — a hardcoded enumeration would "+
					"miss \"ZFixtureCategory\" entirely",
					o.Group, "Subagents/ZFixtureCategory")
			}
			return
		}
	}
	t.Error("option \"zzz-fixture-agent\" (category \"ZFixtureCategory\") not found in QDeployAgents options; " +
		"a scan-based implementation includes every agent the catalog returns regardless of " +
		"whether the category name is known to the production code")
}

// ---------------------------------------------------------------------------
// T3.2 — Orchestrator is never among QDeployAgents options
// ---------------------------------------------------------------------------

// TestDeployAgents_OptionBuilder_OrchestratorNeverInOptions verifies that the orchestrator
// never appears in the option list for QDeployAgents. The builder reads from
// Catalog.Agents(), Catalog.UtilityAgents(), and Catalog.StandaloneAgents() — never from
// Catalog.Orchestrator() or Catalog.OrchestratorScript(). This is what mechanically
// satisfies the requirement that the orchestrator is never offered for selection.
func TestDeployAgents_OptionBuilder_OrchestratorNeverInOptions(t *testing.T) {
	stub := interactiontest.NewBuilder().
		AnswerSelectMany(domain.QDeployAgents, "", []string{}).
		AnswerReview(true).
		Build()
	capInteraction := newCapturingInteraction(stub)

	deps, workspace := newBaseDeps(t, stub)
	deps.Catalog = deployAgentOptionCatalog()
	deps.Planner = &stubPlanner{plan: deployAgentsPlanNoOrchestrator(workspace)}
	deps.Interaction = capInteraction
	svc := app.New(deps)

	_, _ = svc.DeployAgents(context.Background(), app.DeployAgentsRequest{
		HarnessID:     "stub-harness",
		WorkspacePath: workspace,
		AutoConfirmPlan: true,
	})

	opts := capInteraction.OptionsFor(domain.QDeployAgents)
	for _, o := range opts {
		if o.ID == "orchestrator" {
			t.Errorf("orchestrator appears in QDeployAgents options (Option.ID=%q); "+
				"askDeployAgents must never union in the orchestrator accessor — "+
				"Catalog.Agents() excludes it, Catalog.UtilityAgents() excludes it, "+
				"Catalog.StandaloneAgents() excludes it, and this is what satisfies "+
				"the requirement that the orchestrator is never offered for selection (AC3.4)",
				o.ID)
		}
	}
}

// ---------------------------------------------------------------------------
// T3.2 — Utility agents outside allow-list are offered
// ---------------------------------------------------------------------------

// TestDeployAgents_OptionBuilder_UtilityAgentOutsideAllowListIsOffered verifies that a
// utility agent whose key does not appear in ToolConfig.UtilityAgentAllowList is still
// included in the QDeployAgents options. The allow-list gate applies to askUtilityAgents
// (the Deploy workspace flow question) only; askDeployAgents surfaces the entire
// Catalog.UtilityAgents() set to satisfy FR-2.
func TestDeployAgents_OptionBuilder_UtilityAgentOutsideAllowListIsOffered(t *testing.T) {
	stub := interactiontest.NewBuilder().
		AnswerSelectMany(domain.QDeployAgents, "", []string{}).
		AnswerReview(true).
		Build()
	capInteraction := newCapturingInteraction(stub)

	deps, workspace := newBaseDeps(t, stub)
	// Use deployAgentOptionCatalog which has "non-allowlisted-utility" in utilityAgents;
	// the default tool config's allow-list is empty (DefaultToolConfig), so this utility
	// agent would be invisible to askUtilityAgents — but must appear for askDeployAgents.
	deps.Catalog = deployAgentOptionCatalog()
	deps.Planner = &stubPlanner{plan: deployAgentsPlanNoOrchestrator(workspace)}
	deps.Interaction = capInteraction
	svc := app.New(deps)

	_, _ = svc.DeployAgents(context.Background(), app.DeployAgentsRequest{
		HarnessID:     "stub-harness",
		WorkspacePath: workspace,
		AutoConfirmPlan: true,
	})

	opts := capInteraction.OptionsFor(domain.QDeployAgents)
	if len(opts) == 0 {
		t.Skip("QDeployAgents was not presented with options; skipping allow-list bypass assertion until I3.3")
	}

	found := false
	for _, o := range opts {
		if o.ID == "non-allowlisted-utility" {
			found = true
			break
		}
	}
	if !found {
		t.Error("utility agent \"non-allowlisted-utility\" is absent from QDeployAgents options even though " +
			"it is in Catalog.UtilityAgents(); " +
			"askDeployAgents must NOT apply the UtilityAgentAllowList — it surfaces everything on disk " +
			"to satisfy FR-2; only askUtilityAgents (Deploy workspace flow) keeps its allow-list gate")
	}
}
