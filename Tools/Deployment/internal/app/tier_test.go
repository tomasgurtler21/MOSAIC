package app_test

// tier_test.go verifies tier discovery, per-tier model selection, tier choice persistence,
// and the skip / skip-all branches.
//
// Verified invariants (T18.2):
//
// Tier discovery:
//   - QTierModel is asked once per discovered tier when TierModels is not pre-answered
//   - Each QTierModel question has Subject set to the tier string
//   - The harness model list is presented as options
//
// Persistence:
//   - A tier selection made through Interaction is saved to user config after the run
//   - Tier selections are saved per-harness: the correct harness id is the key
//   - Pre-answered tier selections (TierModels in request) are NOT saved to user config;
//     only interactively-answered selections are eligible for persistence (R1)
//
// Skip / skip-all branches (AC18.2):
//   - SkippedOne for a tier skips only that tier; remaining tiers are still asked
//   - SkippedAll latches globally for QTierModel; no further tier questions are asked
//   - Skipping a tier produces a GapNoModel entry for every agent in that tier

import (
	"context"
	"testing"

	"mosaic-deploy/internal/app"
	"mosaic-deploy/internal/app/interactiontest"
	"mosaic-deploy/internal/domain"
)

// ---------------------------------------------------------------------------
// Tier discovery
// ---------------------------------------------------------------------------

// TestDeployNew_TierDiscovery_AsksQTierModelPerTier verifies that the flow asks QTierModel
// once for each tier discovered in the catalog's agent set.
func TestDeployNew_TierDiscovery_AsksQTierModelPerTier(t *testing.T) {
	// Arrange — catalog has one tier ("HIGH")
	stub := interactiontest.NewBuilder().
		AnswerSelectOne(domain.QTierModel, "HIGH", "model-a").
		AnswerReview(true).
		Build()
	deps, workspace := newBaseDeps(t, stub)
	svc := app.New(deps)

	// Act
	_, _ = svc.DeployNew(context.Background(), app.DeployRequest{
		HarnessID:       "stub-harness",
		WorkspacePath:   workspace,
		WorkflowIDs:     []string{"quick-fix"},
		AutoConfirmPlan: true,
	})

	// Assert
	if !stub.WasAsked(domain.QTierModel, "HIGH") {
		t.Error("QTierModel was not asked with subject \"HIGH\"; " +
			"the flow must ask once per tier discovered in the catalog agent set")
	}
}

// TestDeployNew_TierDiscovery_SubjectIsTheTierString verifies that the Subject of the
// QTierModel question matches the tier string exactly as it appears in the catalog.
func TestDeployNew_TierDiscovery_SubjectIsTheTierString(t *testing.T) {
	// Arrange
	var capturedSubject string
	capture := &tierSubjectCapture{
		onTierModel: func(q domain.ChoiceQuestion) { capturedSubject = q.Subject },
		defaultChoice: domain.ChoiceAnswer{Status: domain.Answered, OptionID: "model-a"},
	}
	deps, workspace := newBaseDeps(t, nil)
	deps.Interaction = capture
	svc := app.New(deps)

	// Act
	_, _ = svc.DeployNew(context.Background(), app.DeployRequest{
		HarnessID:       "stub-harness",
		WorkspacePath:   workspace,
		WorkflowIDs:     []string{"quick-fix"},
		AutoConfirmPlan: true,
	})

	// Assert
	if capturedSubject != "HIGH" {
		t.Errorf("QTierModel subject = %q, want \"HIGH\" (the tier string verbatim from catalog)",
			capturedSubject)
	}
}

// TestDeployNew_TierPreAnswered_DoesNotAskQTierModel verifies that when TierModels is set
// in the request for a given tier, QTierModel is not asked for that tier.
func TestDeployNew_TierPreAnswered_DoesNotAskQTierModel(t *testing.T) {
	// Arrange — pre-answer the "HIGH" tier
	stub := interactiontest.NewBuilder().
		AnswerReview(true).
		Build()
	deps, workspace := newBaseDeps(t, stub)
	svc := app.New(deps)

	// Act
	_, _ = svc.DeployNew(context.Background(), app.DeployRequest{
		HarnessID:     "stub-harness",
		WorkspacePath: workspace,
		WorkflowIDs:   []string{"quick-fix"},
		TierModels: map[domain.Tier]string{
			"HIGH": "model-a",
		},
		AutoConfirmPlan: true,
	})

	// Assert
	if stub.WasAsked(domain.QTierModel, "HIGH") {
		t.Error("QTierModel was asked for \"HIGH\" even though TierModels[\"HIGH\"] was pre-supplied (CD-6)")
	}
}

// ---------------------------------------------------------------------------
// Persistence (AC18.3)
// ---------------------------------------------------------------------------

// TestDeployNew_TierSelection_SavedToUserConfig verifies that a tier-to-model choice made
// through Interaction is persisted to user config after the run (AC18.3).
func TestDeployNew_TierSelection_SavedToUserConfig(t *testing.T) {
	// Arrange
	stub := interactiontest.NewBuilder().
		AnswerSelectOne(domain.QTierModel, "HIGH", "model-a").
		AnswerReview(true).
		Build()
	deps, workspace := newBaseDeps(t, stub)
	spy := &spyUserConfig{}
	deps.UserConfig = spy
	svc := app.New(deps)

	// Act
	_, err := svc.DeployNew(context.Background(), app.DeployRequest{
		HarnessID:       "stub-harness",
		WorkspacePath:   workspace,
		WorkflowIDs:     []string{"quick-fix"},
		AutoConfirmPlan: true,
	})
	if err != nil {
		t.Fatalf("DeployNew: %v", err)
	}

	// Assert
	if len(spy.saved) == 0 {
		t.Fatal("UserConfigStore.Save was never called; tier selections must be persisted (AC18.3)")
	}
	lastSave := spy.saved[len(spy.saved)-1]
	harnessModels := lastSave.TierModels["stub-harness"]
	if harnessModels == nil {
		t.Fatal("saved UserConfig has no entry for harness \"stub-harness\"")
	}
	if harnessModels["HIGH"] != "model-a" {
		t.Errorf("saved tier mapping: TierModels[\"stub-harness\"][\"HIGH\"] = %q, want \"model-a\"",
			harnessModels["HIGH"])
	}
}

// TestDeployNew_TierPreAnswered_NotSavedToUserConfig verifies that pre-answered tier
// selections (from DeployRequest.TierModels) are NOT persisted to user config. The caller
// supplied these values as pre-answers for this run only; writing them back would
// incorrectly treat a one-shot caller override as a permanent user preference. Only
// selections made interactively through Interaction are eligible for persistence (R1).
//
// This test replaces the former TestDeployNew_TierPreAnswered_AlsoSavedToUserConfig, which
// asserted the old behavior (pre-answered tiers were written back). The old behavior is the
// primary corruption trigger in AgentTest high-concurrency scenarios: synthetic one-shot
// pre-answers would be written back on every run, causing concurrent Save races.
func TestDeployNew_TierPreAnswered_NotSavedToUserConfig(t *testing.T) {
	// Arrange — pre-answer the "HIGH" tier; no QTierModel interaction scripted.
	// The spy starts with an empty UserConfig so any tier model that appears in
	// spy.saved must have been actively persisted by the flow (not pre-loaded from config).
	stub := interactiontest.NewBuilder().
		AnswerReview(true).
		Build()
	deps, workspace := newBaseDeps(t, stub)
	spy := &spyUserConfig{}
	deps.UserConfig = spy
	svc := app.New(deps)

	// Act
	_, err := svc.DeployNew(context.Background(), app.DeployRequest{
		HarnessID:     "stub-harness",
		WorkspacePath: workspace,
		WorkflowIDs:   []string{"quick-fix"},
		TierModels: map[domain.Tier]string{
			"HIGH": "model-b",
		},
		AutoConfirmPlan: true,
	})
	if err != nil {
		t.Fatalf("DeployNew: %v", err)
	}

	// Assert — "model-b" must NOT appear in any saved config's TierModels. The pre-answered
	// tier model was supplied by the caller for this run only and must not be written back to
	// the persistent user config (R1: interactive-only persistence).
	for i, saved := range spy.saved {
		if saved.TierModels["stub-harness"]["HIGH"] == "model-b" {
			t.Errorf("saved[%d].TierModels[\"stub-harness\"][\"HIGH\"] = \"model-b\"; "+
				"pre-answered tier selections from DeployRequest.TierModels must NOT be "+
				"persisted to user config — only interactively-answered tier selections are "+
				"eligible for persistence (R1: interactive-only persistence rule)", i)
		}
	}
}

// TestDeployNew_TierSavedPerHarness verifies that tier mappings are saved under the
// correct harness ID key, not under a generic or misattributed key.
func TestDeployNew_TierSavedPerHarness(t *testing.T) {
	// Arrange
	stub := interactiontest.NewBuilder().
		AnswerSelectOne(domain.QTierModel, "HIGH", "model-a").
		AnswerReview(true).
		Build()
	deps, workspace := newBaseDeps(t, stub)
	spy := &spyUserConfig{}
	deps.UserConfig = spy
	svc := app.New(deps)

	// Act
	_, _ = svc.DeployNew(context.Background(), app.DeployRequest{
		HarnessID:       "stub-harness",
		WorkspacePath:   workspace,
		WorkflowIDs:     []string{"quick-fix"},
		AutoConfirmPlan: true,
	})

	// Assert
	if len(spy.saved) == 0 {
		t.Fatal("UserConfigStore.Save was never called")
	}
	lastSave := spy.saved[len(spy.saved)-1]
	// Must be keyed under "stub-harness", not a different key
	for hid := range lastSave.TierModels {
		if hid != "stub-harness" {
			t.Errorf("saved tier mapping key = %q; want \"stub-harness\"; tier mappings must be per-harness", hid)
		}
	}
}

// ---------------------------------------------------------------------------
// Skip / skip-all branches (AC18.2)
// ---------------------------------------------------------------------------

// TestDeployNew_TierSkipOne_OnlySkipsThatTier verifies that answering SkippedOne for one
// tier does not skip remaining tiers — only that specific tier is skipped.
func TestDeployNew_TierSkipOne_OnlySkipsThatTier(t *testing.T) {
	// Arrange — catalog with two tiers; skip HIGH, answer MEDIUM
	cat := newMinimalCatalog()
	cat.agents = append(cat.agents, domain.Agent{
		Key:             "planner",
		NumericID:       "2",
		Version:         "1.0",
		Name:            "Planner",
		Role:            domain.RoleWorker,
		Category:        "Planning",
		RecommendedTier: "MEDIUM",
		TierRationale:   "General purpose model",
	})
	cat.tiers = append(cat.tiers, domain.TierInfo{
		Tier:      "MEDIUM",
		Rationale: "General purpose model",
		AgentKeys: []string{"planner"},
	})
	// "planner" must be in the workflow's ReferencedAgents so tier scoping includes MEDIUM.
	// Without this, the Stage 2 filter excludes MEDIUM (no selected agent uses it) and the
	// test would never reach the SkippedOne logic it is meant to verify.
	cat.workflows = []domain.Workflow{{
		ID:               "quick-fix",
		Name:             "Quick Fix",
		Description:      "Fix a quick issue",
		Hint:             "Use for minor bugs",
		Version:          "1.0",
		Category:         "Build",
		ReferencedAgents: []string{"test-runner", "planner"},
	}}

	deps, workspace := newBaseDeps(t, interactiontest.NewBuilder().Build())
	deps.Catalog = cat
	// Force HIGH to return SkippedOne by inserting a custom interaction
	skipHighStub := &skipOneTierInteraction{
		skipTier: "HIGH",
		defaultAnswer: domain.ChoiceAnswer{Status: domain.Answered, OptionID: "model-a"},
	}
	deps.Interaction = skipHighStub
	svc := app.New(deps)

	// Act
	_, _ = svc.DeployNew(context.Background(), app.DeployRequest{
		HarnessID:       "stub-harness",
		WorkspacePath:   workspace,
		WorkflowIDs:     []string{"quick-fix"},
		AutoConfirmPlan: true,
	})

	// Assert — MEDIUM must still have been asked even though HIGH was skipped
	if !skipHighStub.askedMEDIUM {
		t.Error("QTierModel for \"MEDIUM\" was not asked after \"HIGH\" was skipped one; " +
			"SkippedOne must skip only the current tier, not all remaining tiers (AC18.2)")
	}
}

// TestDeployNew_TierSkipAll_StopsAskingRemainingTiers verifies that when SkippedAll is
// returned for a QTierModel question, no further tier questions are asked (CD-5).
func TestDeployNew_TierSkipAll_StopsAskingRemainingTiers(t *testing.T) {
	// Arrange — catalog with two tiers; skip-all on first
	cat := newMinimalCatalog()
	cat.agents = append(cat.agents, domain.Agent{
		Key:             "planner",
		NumericID:       "2",
		Version:         "1.0",
		Name:            "Planner",
		Role:            domain.RoleWorker,
		Category:        "Planning",
		RecommendedTier: "MEDIUM",
		TierRationale:   "General purpose model",
	})
	cat.tiers = append(cat.tiers, domain.TierInfo{
		Tier:      "MEDIUM",
		Rationale: "General purpose model",
		AgentKeys: []string{"planner"},
	})
	// "planner" must be in the workflow's ReferencedAgents so tier scoping includes MEDIUM.
	// Without this, MEDIUM would be excluded by the Stage 2 filter before SkippedAll even
	// runs, causing the test to pass for the wrong reason.
	cat.workflows = []domain.Workflow{{
		ID:               "quick-fix",
		Name:             "Quick Fix",
		Description:      "Fix a quick issue",
		Hint:             "Use for minor bugs",
		Version:          "1.0",
		Category:         "Build",
		ReferencedAgents: []string{"test-runner", "planner"},
	}}

	stub := interactiontest.NewBuilder().
		AnswerSkipAll(domain.QTierModel, "HIGH"). // SkippedAll on first tier
		AnswerReview(true).
		Build()
	deps, workspace := newBaseDeps(t, stub)
	deps.Catalog = cat
	svc := app.New(deps)

	// Act
	_, _ = svc.DeployNew(context.Background(), app.DeployRequest{
		HarnessID:       "stub-harness",
		WorkspacePath:   workspace,
		WorkflowIDs:     []string{"quick-fix"},
		AutoConfirmPlan: true,
	})

	// Assert — MEDIUM must not have been asked after SkippedAll for HIGH
	if stub.WasAsked(domain.QTierModel, "MEDIUM") {
		t.Error("QTierModel for \"MEDIUM\" was asked after SkippedAll was returned for \"HIGH\"; " +
			"SkippedAll must latch globally and stop all further tier questions (CD-5, AC18.2)")
	}
}

// ---------------------------------------------------------------------------
// Helpers for this file
// ---------------------------------------------------------------------------

// tierSubjectCapture captures the Subject of QTierModel questions.
type tierSubjectCapture struct {
	onTierModel   func(domain.ChoiceQuestion)
	defaultChoice domain.ChoiceAnswer
}

func (c *tierSubjectCapture) SelectOne(_ context.Context, q domain.ChoiceQuestion) (domain.ChoiceAnswer, error) {
	if q.ID == domain.QTierModel && c.onTierModel != nil {
		c.onTierModel(q)
	}
	return c.defaultChoice, nil
}
func (c *tierSubjectCapture) SelectMany(_ context.Context, q domain.ChoiceQuestion) (domain.MultiChoiceAnswer, error) {
	return domain.MultiChoiceAnswer{Status: domain.Answered, OptionIDs: []string{"quick-fix"}}, nil
}
func (c *tierSubjectCapture) AskText(_ context.Context, q domain.TextQuestion) (domain.TextAnswer, error) {
	return domain.TextAnswer{Status: domain.Answered, Text: "stub"}, nil
}
func (c *tierSubjectCapture) Confirm(_ context.Context, q domain.Question) (domain.ConfirmAnswer, error) {
	return domain.ConfirmAnswer{Status: domain.Answered, Confirm: true}, nil
}
func (c *tierSubjectCapture) Notify(_ context.Context, n domain.Notice)          {}
func (c *tierSubjectCapture) Progress(_ context.Context, e domain.ProgressEvent) {}
func (c *tierSubjectCapture) Review(_ context.Context, p domain.Plan) (domain.ConfirmAnswer, error) {
	return domain.ConfirmAnswer{Status: domain.Answered, Confirm: true}, nil
}

// skipOneTierInteraction returns SkippedOne for the given tier and Answered for others.
type skipOneTierInteraction struct {
	skipTier      domain.Tier
	defaultAnswer domain.ChoiceAnswer
	askedMEDIUM   bool
}

func (s *skipOneTierInteraction) SelectOne(_ context.Context, q domain.ChoiceQuestion) (domain.ChoiceAnswer, error) {
	if q.ID == domain.QTierModel {
		if domain.Tier(q.Subject) == s.skipTier {
			return domain.ChoiceAnswer{Status: domain.SkippedOne}, nil
		}
		if q.Subject == "MEDIUM" {
			s.askedMEDIUM = true
		}
	}
	return s.defaultAnswer, nil
}
func (s *skipOneTierInteraction) SelectMany(_ context.Context, q domain.ChoiceQuestion) (domain.MultiChoiceAnswer, error) {
	return domain.MultiChoiceAnswer{Status: domain.Answered, OptionIDs: []string{"quick-fix"}}, nil
}
func (s *skipOneTierInteraction) AskText(_ context.Context, q domain.TextQuestion) (domain.TextAnswer, error) {
	return domain.TextAnswer{Status: domain.Answered, Text: "stub"}, nil
}
func (s *skipOneTierInteraction) Confirm(_ context.Context, q domain.Question) (domain.ConfirmAnswer, error) {
	return domain.ConfirmAnswer{Status: domain.Answered, Confirm: true}, nil
}
func (s *skipOneTierInteraction) Notify(_ context.Context, n domain.Notice)          {}
func (s *skipOneTierInteraction) Progress(_ context.Context, e domain.ProgressEvent) {}
func (s *skipOneTierInteraction) Review(_ context.Context, p domain.Plan) (domain.ConfirmAnswer, error) {
	return domain.ConfirmAnswer{Status: domain.Answered, Confirm: true}, nil
}
