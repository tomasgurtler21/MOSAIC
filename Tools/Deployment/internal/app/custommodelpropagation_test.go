package app_test

// custommodelpropagation_test.go verifies that custom model IDs entered by the user during
// one model-resolution question are offered as selectable options in subsequent
// model-resolution questions within the same run.
//
// Verified invariants:
//
// In-run agent-model propagation:
//   - A custom model ID entered for one agent's model question appears in the options list
//     for the next agent's model question in the same DeployNew run.
//   - Multiple custom model IDs accumulate: the third agent sees all custom IDs entered for
//     the first and second agents.
//
// In-run tier-model propagation:
//   - A custom model ID entered for one tier's model question appears in the options list
//     for the next tier's model question in the same run.
//
// These tests will fail until resolveModels is updated to append custom entries to
// harnessOptions in-place as each custom answer is received.

import (
	"context"
	"testing"

	"mosaic-deploy/internal/app"
	"mosaic-deploy/internal/domain"
)

// ---------------------------------------------------------------------------
// Option-capturing interaction
// ---------------------------------------------------------------------------

// capturedChoiceCall records one SelectOne call: the question ID, subject, and the full
// options list that was presented. Tests inspect this slice to verify option propagation.
type capturedChoiceCall struct {
	ID      domain.QuestionID
	Subject string
	Options []domain.Option
}

type customModelPropagationKey struct {
	ID      domain.QuestionID
	Subject string
}

// optionCapturingInteraction records every SelectOne call with its presented options.
// Answers are taken from selectOneAnswers; unscripted calls receive SkippedOne.
// It implements the full domain.Interaction interface.
type optionCapturingInteraction struct {
	recorded         []capturedChoiceCall
	selectOneAnswers map[customModelPropagationKey]domain.ChoiceAnswer
}

// optionsFor returns the options presented when SelectOne was called with the given
// question ID and subject. Returns nil if that call was not recorded.
func (c *optionCapturingInteraction) optionsFor(id domain.QuestionID, subject string) []domain.Option {
	for _, rec := range c.recorded {
		if rec.ID == id && rec.Subject == subject {
			return rec.Options
		}
	}
	return nil
}

func (c *optionCapturingInteraction) SelectOne(_ context.Context, q domain.ChoiceQuestion) (domain.ChoiceAnswer, error) {
	c.recorded = append(c.recorded, capturedChoiceCall{ID: q.ID, Subject: q.Subject, Options: q.Options})
	if ans, ok := c.selectOneAnswers[customModelPropagationKey{q.ID, q.Subject}]; ok {
		return ans, nil
	}
	return domain.ChoiceAnswer{Status: domain.SkippedOne}, nil
}

func (c *optionCapturingInteraction) SelectMany(_ context.Context, _ domain.ChoiceQuestion) (domain.MultiChoiceAnswer, error) {
	return domain.MultiChoiceAnswer{Status: domain.SkippedOne}, nil
}

func (c *optionCapturingInteraction) AskText(_ context.Context, _ domain.TextQuestion) (domain.TextAnswer, error) {
	return domain.TextAnswer{Status: domain.SkippedOne}, nil
}

func (c *optionCapturingInteraction) Confirm(_ context.Context, _ domain.Question) (domain.ConfirmAnswer, error) {
	return domain.ConfirmAnswer{Status: domain.Answered, Confirm: true}, nil
}

func (c *optionCapturingInteraction) Notify(_ context.Context, _ domain.Notice)          {}
func (c *optionCapturingInteraction) Progress(_ context.Context, _ domain.ProgressEvent) {}
func (c *optionCapturingInteraction) Review(_ context.Context, _ domain.Plan) (domain.ConfirmAnswer, error) {
	return domain.ConfirmAnswer{Status: domain.Answered, Confirm: true}, nil
}

// optionIDsPresent reports whether options contains an entry with the given ID.
func optionIDsPresent(options []domain.Option, id string) bool {
	for _, opt := range options {
		if opt.ID == id {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// Catalog helpers for multi-agent and multi-tier scenarios
// ---------------------------------------------------------------------------

// twoAgentSameTierCatalog returns a catalog with two worker agents (test-runner and
// code-writer) both in the HIGH tier, both referenced by the "quick-fix" workflow.
// The catalog is otherwise identical to the minimal one.
func twoAgentSameTierCatalog() *stubCatalog {
	cat := newMinimalCatalog()
	codeWriter := domain.Agent{
		Key:             "code-writer",
		NumericID:       "2",
		Version:         "1.0",
		Name:            "Code Writer",
		Role:            domain.RoleWorker,
		Category:        "Creation",
		RecommendedTier: "HIGH",
	}
	cat.agents = append(cat.agents, codeWriter)
	cat.workflows = []domain.Workflow{
		{
			ID:               "quick-fix",
			Name:             "Quick Fix",
			Version:          "1.0",
			Category:         "Build",
			ReferencedAgents: []string{"test-runner", "code-writer"},
		},
	}
	return cat
}

// threeAgentSameTierCatalog returns a catalog with three worker agents all in the HIGH tier,
// all referenced by the "quick-fix" workflow.
func threeAgentSameTierCatalog() *stubCatalog {
	cat := twoAgentSameTierCatalog()
	thirdAgent := domain.Agent{
		Key:             "reviewer",
		NumericID:       "3",
		Version:         "1.0",
		Name:            "Reviewer",
		Role:            domain.RoleWorker,
		Category:        "Review",
		RecommendedTier: "HIGH",
	}
	cat.agents = append(cat.agents, thirdAgent)
	cat.workflows = []domain.Workflow{
		{
			ID:               "quick-fix",
			Name:             "Quick Fix",
			Version:          "1.0",
			Category:         "Build",
			ReferencedAgents: []string{"test-runner", "code-writer", "reviewer"},
		},
	}
	return cat
}

// twoTierTwoAgentCatalog returns a catalog with two tiers (HIGH and MEDIUM) and two worker
// agents, one in each tier, both referenced by the "quick-fix" workflow.
func twoTierTwoAgentCatalog() *stubCatalog {
	cat := newMinimalCatalog()
	mediumAgent := domain.Agent{
		Key:             "code-writer",
		NumericID:       "2",
		Version:         "1.0",
		Name:            "Code Writer",
		Role:            domain.RoleWorker,
		Category:        "Creation",
		RecommendedTier: "MEDIUM",
	}
	cat.agents = append(cat.agents, mediumAgent)
	cat.tiers = append(cat.tiers, domain.TierInfo{
		Tier:      "MEDIUM",
		Rationale: "Medium capability required",
		AgentKeys: []string{"code-writer"},
	})
	cat.workflows = []domain.Workflow{
		{
			ID:               "quick-fix",
			Name:             "Quick Fix",
			Version:          "1.0",
			Category:         "Build",
			ReferencedAgents: []string{"test-runner", "code-writer"},
		},
	}
	return cat
}

// ---------------------------------------------------------------------------
// In-run deduplication of custom model IDs
// ---------------------------------------------------------------------------

// TestDeployNew_SameCustomModelIDEnteredByTwoAgents_AppearsOnceInThirdAgentOptions verifies
// that when the same custom model ID is entered for two different agents in a single run,
// subsequent agents see that ID exactly once in their options list — not twice. The in-run
// accumulation must deduplicate by exact string match.
func TestDeployNew_SameCustomModelIDEnteredByTwoAgents_AppearsOnceInThirdAgentOptions(t *testing.T) {
	// Arrange — three agents in HIGH tier; test-runner and code-writer both enter the same
	// custom model ID "dedup-custom-model". reviewer must see it exactly once in its options.
	capture := &optionCapturingInteraction{
		selectOneAnswers: map[customModelPropagationKey]domain.ChoiceAnswer{
			{domain.QAgentModel, "test-runner"}: {
				Status: domain.Answered,
				Custom: "dedup-custom-model",
			},
			{domain.QAgentModel, "code-writer"}: {
				Status: domain.Answered,
				Custom: "dedup-custom-model",
			},
			// orchestrator, reviewer: not scripted → SkippedOne (default)
		},
	}

	deps, workspace := newBaseDeps(t, nil)
	deps.Catalog = threeAgentSameTierCatalog()
	deps.UserConfig = &spyUserConfig{}
	deps.Interaction = capture
	svc := app.New(deps)

	// Act
	_, _ = svc.DeployNew(context.Background(), app.DeployRequest{
		HarnessID:       "stub-harness",
		WorkspacePath:   workspace,
		WorkflowIDs:     []string{"quick-fix"},
		SkipAll:         map[domain.QuestionID]bool{domain.QTierModel: true},
		AutoConfirmPlan: true,
	})

	// Assert — "dedup-custom-model" must appear exactly once, not twice.
	reviewerOptions := capture.optionsFor(domain.QAgentModel, "reviewer")
	if reviewerOptions == nil {
		t.Fatal("QAgentModel was not asked for \"reviewer\"; cannot verify in-run deduplication")
	}
	count := 0
	for _, opt := range reviewerOptions {
		if opt.ID == "dedup-custom-model" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("QAgentModel options for \"reviewer\" contains \"dedup-custom-model\" %d time(s), want exactly 1; "+
			"the same custom model ID entered by multiple agents in one run must be deduplicated "+
			"in the options list presented to subsequent agents", count)
	}
}

// ---------------------------------------------------------------------------
// In-run agent-model propagation
// ---------------------------------------------------------------------------

// TestDeployNew_CustomAgentModel_IsOfferedToSubsequentAgent verifies that when a custom
// model ID is entered for one agent's model question, that custom ID appears in the options
// list presented to the next agent's model question in the same run.
func TestDeployNew_CustomAgentModel_IsOfferedToSubsequentAgent(t *testing.T) {
	// Arrange — two worker agents (test-runner, code-writer) both in HIGH tier.
	// Skip the tier question so both agents receive a QAgentModel question.
	// Enter a custom model for test-runner; capture what is presented to code-writer.
	capture := &optionCapturingInteraction{
		selectOneAnswers: map[customModelPropagationKey]domain.ChoiceAnswer{
			{domain.QAgentModel, "test-runner"}: {
				Status: domain.Answered,
				Custom: "custom-agent-model-propagation-1",
			},
			// orchestrator, code-writer: not scripted → SkippedOne (default)
		},
	}

	deps, workspace := newBaseDeps(t, nil)
	deps.Catalog = twoAgentSameTierCatalog()
	deps.UserConfig = &spyUserConfig{}
	deps.Interaction = capture
	svc := app.New(deps)

	// Act
	_, _ = svc.DeployNew(context.Background(), app.DeployRequest{
		HarnessID:       "stub-harness",
		WorkspacePath:   workspace,
		WorkflowIDs:     []string{"quick-fix"},
		SkipAll:         map[domain.QuestionID]bool{domain.QTierModel: true},
		AutoConfirmPlan: true,
	})

	// Assert — the custom ID entered for test-runner must appear in code-writer's options.
	codeWriterOptions := capture.optionsFor(domain.QAgentModel, "code-writer")
	if codeWriterOptions == nil {
		t.Fatal("QAgentModel was not asked for \"code-writer\"; cannot verify option propagation")
	}
	if !optionIDsPresent(codeWriterOptions, "custom-agent-model-propagation-1") {
		t.Errorf("QAgentModel options for \"code-writer\" = %v; "+
			"want to contain \"custom-agent-model-propagation-1\" entered for \"test-runner\"; "+
			"custom model IDs entered during a run must appear as options for subsequent agents", codeWriterOptions)
	}
}

// TestDeployNew_MultipleCustomAgentModels_AccumulateAcrossAgents verifies that when more
// than one custom model ID is entered across different agents, all previously-entered custom
// IDs appear in the options list for later agents in the same run.
func TestDeployNew_MultipleCustomAgentModels_AccumulateAcrossAgents(t *testing.T) {
	// Arrange — three agents all in HIGH tier.
	// test-runner enters "custom-alpha", code-writer enters "custom-beta",
	// reviewer must see both "custom-alpha" and "custom-beta" in its options.
	capture := &optionCapturingInteraction{
		selectOneAnswers: map[customModelPropagationKey]domain.ChoiceAnswer{
			{domain.QAgentModel, "test-runner"}: {
				Status: domain.Answered,
				Custom: "custom-alpha",
			},
			{domain.QAgentModel, "code-writer"}: {
				Status: domain.Answered,
				Custom: "custom-beta",
			},
			// orchestrator, reviewer: not scripted → SkippedOne
		},
	}

	deps, workspace := newBaseDeps(t, nil)
	deps.Catalog = threeAgentSameTierCatalog()
	deps.UserConfig = &spyUserConfig{}
	deps.Interaction = capture
	svc := app.New(deps)

	// Act
	_, _ = svc.DeployNew(context.Background(), app.DeployRequest{
		HarnessID:       "stub-harness",
		WorkspacePath:   workspace,
		WorkflowIDs:     []string{"quick-fix"},
		SkipAll:         map[domain.QuestionID]bool{domain.QTierModel: true},
		AutoConfirmPlan: true,
	})

	// Assert — "reviewer" must see both custom IDs in its options.
	reviewerOptions := capture.optionsFor(domain.QAgentModel, "reviewer")
	if reviewerOptions == nil {
		t.Fatal("QAgentModel was not asked for \"reviewer\"; cannot verify option accumulation")
	}
	if !optionIDsPresent(reviewerOptions, "custom-alpha") {
		t.Errorf("QAgentModel options for \"reviewer\" = %v; "+
			"want to contain \"custom-alpha\" entered for \"test-runner\"; "+
			"custom model IDs must accumulate across all agents in a run", reviewerOptions)
	}
	if !optionIDsPresent(reviewerOptions, "custom-beta") {
		t.Errorf("QAgentModel options for \"reviewer\" = %v; "+
			"want to contain \"custom-beta\" entered for \"code-writer\"; "+
			"custom model IDs must accumulate across all agents in a run", reviewerOptions)
	}
}

// ---------------------------------------------------------------------------
// In-run tier-model propagation
// ---------------------------------------------------------------------------

// TestDeployNew_CustomTierModel_IsOfferedToSubsequentTierQuestion verifies that when a
// custom model ID is entered for one tier's model question, that custom ID appears in the
// options list for the next tier's model question in the same run.
func TestDeployNew_CustomTierModel_IsOfferedToSubsequentTierQuestion(t *testing.T) {
	// Arrange — two tiers (HIGH and MEDIUM); HIGH is asked first.
	// Enter a custom model for HIGH; capture what is presented for MEDIUM.
	capture := &optionCapturingInteraction{
		selectOneAnswers: map[customModelPropagationKey]domain.ChoiceAnswer{
			{domain.QTierModel, "HIGH"}: {
				Status: domain.Answered,
				Custom: "custom-tier-model-propagation-x",
			},
			// MEDIUM: not scripted → SkippedOne; we only care about the options presented
		},
	}

	deps, workspace := newBaseDeps(t, nil)
	deps.Catalog = twoTierTwoAgentCatalog()
	deps.UserConfig = &spyUserConfig{}
	deps.Interaction = capture
	svc := app.New(deps)

	// Act
	_, _ = svc.DeployNew(context.Background(), app.DeployRequest{
		HarnessID:       "stub-harness",
		WorkspacePath:   workspace,
		WorkflowIDs:     []string{"quick-fix"},
		AutoConfirmPlan: true,
	})

	// Assert — the custom ID entered for HIGH must appear in MEDIUM's options.
	mediumOptions := capture.optionsFor(domain.QTierModel, "MEDIUM")
	if mediumOptions == nil {
		t.Fatal("QTierModel was not asked for \"MEDIUM\"; cannot verify tier option propagation")
	}
	if !optionIDsPresent(mediumOptions, "custom-tier-model-propagation-x") {
		t.Errorf("QTierModel options for \"MEDIUM\" = %v; "+
			"want to contain \"custom-tier-model-propagation-x\" entered for \"HIGH\"; "+
			"custom model IDs entered during tier questions must appear as options for subsequent tier questions", mediumOptions)
	}
}
