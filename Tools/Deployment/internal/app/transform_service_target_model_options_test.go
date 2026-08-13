package app_test

// transform_service_target_model_options_test.go tests the target-model ChoiceQuestion
// contract and the surrounding behaviours that must remain unchanged after the fix.
//
// Core assertion (RED-verified):
//
//	The ChoiceQuestion emitted for QTransformTargetModel must carry one option per model ID
//	declared in the resolved target module's descriptor. Before the fix, Options is nil
//	regardless of what the target module declares, causing the selection overlay to render
//	with nothing to pick. Tests capturing the question directly verify this new data flow.
//
// Supplementary assertions (behaviour preserved):
//
//	A target module that declares no models still yields a question with AllowSkip=true, so
//	the user can always skip past model selection. A pre-answered TargetModel and a SkipAll
//	entry for QTransformTargetModel each suppress SelectOne entirely.

import (
	"context"
	"testing"

	"mosaic-deploy/internal/app"
	"mosaic-deploy/internal/app/interactiontest"
	"mosaic-deploy/internal/domain"
)

// ---------------------------------------------------------------------------
// capturingTargetModelSpy — records ChoiceQuestions sent to QTransformTargetModel
// ---------------------------------------------------------------------------

// capturingTargetModelSpy wraps a base domain.Interaction, intercepts SelectOne calls
// for QTransformTargetModel, captures the full ChoiceQuestion (including Options and the
// AllowSkip flag), and returns a scripted answer. All other Interaction methods delegate
// to the base.
//
// Use it when a test needs to assert on the shape of the question — specifically the
// Options field — rather than just whether the question was asked.
type capturingTargetModelSpy struct {
	base                  domain.Interaction
	capturedTargetModelQ  *domain.ChoiceQuestion
	targetModelAnswer     domain.ChoiceAnswer
}

func (s *capturingTargetModelSpy) SelectOne(ctx context.Context, q domain.ChoiceQuestion) (domain.ChoiceAnswer, error) {
	if q.ID == domain.QTransformTargetModel {
		cp := q
		s.capturedTargetModelQ = &cp
		return s.targetModelAnswer, nil
	}
	return s.base.SelectOne(ctx, q)
}

func (s *capturingTargetModelSpy) SelectMany(ctx context.Context, q domain.ChoiceQuestion) (domain.MultiChoiceAnswer, error) {
	return s.base.SelectMany(ctx, q)
}

func (s *capturingTargetModelSpy) AskText(ctx context.Context, q domain.TextQuestion) (domain.TextAnswer, error) {
	return s.base.AskText(ctx, q)
}

func (s *capturingTargetModelSpy) Confirm(ctx context.Context, q domain.Question) (domain.ConfirmAnswer, error) {
	return s.base.Confirm(ctx, q)
}

func (s *capturingTargetModelSpy) Notify(ctx context.Context, n domain.Notice) {
	s.base.Notify(ctx, n)
}

func (s *capturingTargetModelSpy) Progress(ctx context.Context, e domain.ProgressEvent) {
	s.base.Progress(ctx, e)
}

func (s *capturingTargetModelSpy) Review(ctx context.Context, p domain.Plan) (domain.ConfirmAnswer, error) {
	return s.base.Review(ctx, p)
}

// newCapturingTargetModelSpy returns a capturingTargetModelSpy that answers
// QTransformTargetModel with the given modelID (may be empty to simulate a skip) and
// delegates QTransformTargetHarness to the base stub.
func newCapturingTargetModelSpy(modelID string) *capturingTargetModelSpy {
	var ans domain.ChoiceAnswer
	if modelID == "" {
		ans = domain.ChoiceAnswer{Status: domain.SkippedOne}
	} else {
		ans = domain.ChoiceAnswer{Status: domain.Answered, OptionID: modelID}
	}
	return &capturingTargetModelSpy{
		base:              interactiontest.NewBuilder().Build(),
		targetModelAnswer: ans,
	}
}

// newTransformTargetModuleWithModelIDs returns a stubHarnessModule for the target harness
// with the given model IDs in its descriptor's ModelCatalog. All other descriptor fields
// match newTransformTargetModule so that file matching and path resolution behave identically.
func newTransformTargetModuleWithModelIDs(modelIDs []string) *stubHarnessModule {
	return &stubHarnessModule{
		ref: transformTargetHarness,
		descriptor: domain.HarnessDescriptor{
			ID:          "target-harness",
			DisplayName: "Target Harness",
			Frontmatter: domain.FrontmatterSpec{
				ModelKey: "tgt_model",
			},
			Models: domain.ModelCatalog{IDs: modelIDs},
			Paths: domain.PathSpec{
				Agents: domain.ScopedPaths{
					Supported: true,
					Project:   "agents",
				},
			},
			Extensions: map[domain.ArtifactKind]string{
				domain.ArtifactAgent: ".tgt.md",
			},
		},
	}
}

// newTransformDepsWithTargetModelIDs returns app.Deps wired for target-model-options tests.
// The target harness module carries the given model IDs in its descriptor so the service can
// populate QTransformTargetModel.Options from them.
func newTransformDepsWithTargetModelIDs(t *testing.T, stub *interactiontest.Stub, modelIDs []string) (app.Deps, string) {
	t.Helper()
	deps, workspace := newTransformDeps(t, stub)
	tgtModule := newTransformTargetModuleWithModelIDs(modelIDs)
	deps.Registry = &stubRegistry{
		list: []domain.HarnessRef{transformSourceHarness, transformTargetHarness},
		modules: map[string]domain.HarnessModule{
			"source-harness": newTransformSourceModule(),
			"target-harness": tgtModule,
		},
	}
	return deps, workspace
}

// ---------------------------------------------------------------------------
// Core assertion: Options populated from target module's declared model IDs
// ---------------------------------------------------------------------------

// TestTransformHarness_TargetModelQuestion_OptionsFromTargetModuleModelIDs verifies that
// when TargetModel is empty, the ChoiceQuestion emitted for QTransformTargetModel carries
// one option per model ID declared by the resolved target module, in declaration order.
// This is the exact symptom of the bug being fixed: the selection overlay renders with
// nothing to pick when Options is nil, even when the target harness declares models.
func TestTransformHarness_TargetModelQuestion_OptionsFromTargetModuleModelIDs(t *testing.T) {
	// Arrange: target module declares two model IDs.
	modelIDs := []string{"model-x", "model-y"}
	spy := newCapturingTargetModelSpy("model-x")
	dummyStub := interactiontest.NewBuilder().Build()
	deps, _ := newTransformDepsWithTargetModelIDs(t, dummyStub, modelIDs)
	deps.Interaction = spy
	svc := app.New(deps)

	dir := t.TempDir()
	agentPath := writeTransformFile(t, dir, "my-agent.src.md", matchingAgentBytes())

	req := newPreAnsweredTransformRequest(agentPath)
	req.TargetModel = "" // unset to trigger QTransformTargetModel

	// Act
	_, _ = svc.TransformHarness(context.Background(), req)

	// Assert: the question was captured.
	if spy.capturedTargetModelQ == nil {
		t.Fatal("QTransformTargetModel was not asked; want SelectOne to be called when TargetModel is empty")
	}

	opts := spy.capturedTargetModelQ.Options
	// Assert: option count matches declared model ID count.
	if len(opts) != len(modelIDs) {
		t.Errorf("len(Options) = %d, want %d (one per model ID declared by target module); "+
			"this is the exact symptom of the bug: Options is nil when the target module declares "+
			"models, causing the selection overlay to render with nothing to pick",
			len(opts), len(modelIDs))
	}

	// Assert: option IDs match declared model IDs in declaration order.
	for i, wantID := range modelIDs {
		if i >= len(opts) {
			break // already reported length mismatch above
		}
		if opts[i].ID != wantID {
			t.Errorf("Options[%d].ID = %q, want %q (model ID from target module descriptor in declaration order)",
				i, opts[i].ID, wantID)
		}
	}
}

// TestTransformHarness_TargetModelQuestion_OptionLabelsMatchModelIDs verifies that each
// option's Label matches the corresponding model ID from the target module's descriptor.
// buildModelOptions uses the model ID as both ID and Label; this test pins that contract.
func TestTransformHarness_TargetModelQuestion_OptionLabelsMatchModelIDs(t *testing.T) {
	// Arrange
	modelIDs := []string{"claude-3-sonnet", "claude-3-haiku"}
	spy := newCapturingTargetModelSpy("claude-3-sonnet")
	dummyStub := interactiontest.NewBuilder().Build()
	deps, _ := newTransformDepsWithTargetModelIDs(t, dummyStub, modelIDs)
	deps.Interaction = spy
	svc := app.New(deps)

	dir := t.TempDir()
	agentPath := writeTransformFile(t, dir, "my-agent.src.md", matchingAgentBytes())

	req := newPreAnsweredTransformRequest(agentPath)
	req.TargetModel = "" // trigger question

	// Act
	_, _ = svc.TransformHarness(context.Background(), req)

	if spy.capturedTargetModelQ == nil {
		t.Fatal("QTransformTargetModel was not asked")
	}

	opts := spy.capturedTargetModelQ.Options
	if len(opts) < len(modelIDs) {
		t.Fatalf("len(Options) = %d, want %d; cannot verify labels — options list is too short",
			len(opts), len(modelIDs))
	}

	// Assert: each option Label mirrors the model ID (the buildModelOptions convention).
	for i, wantID := range modelIDs {
		if opts[i].Label != wantID {
			t.Errorf("Options[%d].Label = %q, want %q (model ID used as label by buildModelOptions)",
				i, opts[i].Label, wantID)
		}
	}
}

// ---------------------------------------------------------------------------
// Supplementary: target module with no models — question is still skippable
// ---------------------------------------------------------------------------

// TestTransformHarness_TargetModelQuestion_NoModels_QuestionStillSkippable verifies that
// when the target module declares no model IDs, the QTransformTargetModel question is still
// presented with AllowSkip=true. The overlay must remain navigable even when Options is
// empty: the user can always skip past model selection.
//
// NOT RED-VERIFIED: The current implementation already sets AllowSkip=true on the question
// regardless of the Options field. This test passes in the RED phase because the question is
// already asked with AllowSkip=true (the Options fix does not touch AllowSkip). Verify after
// implementation that AllowSkip remains true once the Options-populating code is added.
func TestTransformHarness_TargetModelQuestion_NoModels_QuestionStillSkippable(t *testing.T) {
	// Arrange: target module declares no model IDs.
	spy := newCapturingTargetModelSpy("") // skip answer
	dummyStub := interactiontest.NewBuilder().Build()
	deps, _ := newTransformDepsWithTargetModelIDs(t, dummyStub, nil /* no model IDs */)
	deps.Interaction = spy
	svc := app.New(deps)

	dir := t.TempDir()
	agentPath := writeTransformFile(t, dir, "my-agent.src.md", matchingAgentBytes())

	req := newPreAnsweredTransformRequest(agentPath)
	req.TargetModel = "" // trigger question

	// Act
	_, _ = svc.TransformHarness(context.Background(), req)

	// Assert: the question was still asked.
	if spy.capturedTargetModelQ == nil {
		t.Fatal("QTransformTargetModel was not asked when target module declares no models; " +
			"want the question to be presented so the user can enter a free-text model ID or skip")
	}

	// Assert: question is skippable even when Options is empty.
	if !spy.capturedTargetModelQ.AllowSkip {
		t.Error("QTransformTargetModel.AllowSkip = false when target module declares no models; " +
			"want AllowSkip=true so model selection is always optional regardless of the options list")
	}
}

// ---------------------------------------------------------------------------
// Supplementary: SkipAll[QTransformTargetModel] suppresses the prompt
// ---------------------------------------------------------------------------

// TestTransformHarness_SkipAll_TargetModel_QuestionNotAsked verifies that when
// SkipAll[QTransformTargetModel] is set, the service does not call SelectOne for
// QTransformTargetModel. The SkipAll mechanism is documented in the request contract:
// it suppresses QTransformTargetModel entirely without leaving TargetModel empty via an
// interactive answer — the caller has already decided to omit model selection.
//
// NOT RED-VERIFIED: the SkipAll guard already exists in the current implementation
// (the condition `!req.SkipAll[domain.QTransformTargetModel]` was present before this
// stage). This test passes in the RED phase because the question is already suppressed
// when SkipAll is set. Verify after implementation that it remains green once the
// Options-populating code is added to the interactive path.
func TestTransformHarness_SkipAll_TargetModel_QuestionNotAsked(t *testing.T) {
	// Arrange: spy that records any SelectOne call for QTransformTargetModel.
	spy := &capturingTargetModelSpy{
		base:              interactiontest.NewBuilder().Build(),
		targetModelAnswer: domain.ChoiceAnswer{Status: domain.SkippedOne},
	}
	dummyStub := interactiontest.NewBuilder().Build()
	deps, _ := newTransformDepsWithTargetModelIDs(t, dummyStub, []string{"model-a", "model-b"})
	deps.Interaction = spy
	svc := app.New(deps)

	dir := t.TempDir()
	agentPath := writeTransformFile(t, dir, "my-agent.src.md", matchingAgentBytes())

	req := newPreAnsweredTransformRequest(agentPath)
	req.TargetModel = "" // unset so the code would normally ask
	req.SkipAll = map[domain.QuestionID]bool{
		domain.QTransformTargetModel: true, // suppress the question
	}

	// Act
	_, _ = svc.TransformHarness(context.Background(), req)

	// Assert: spy must not have captured a question.
	if spy.capturedTargetModelQ != nil {
		t.Error("QTransformTargetModel was asked despite SkipAll[QTransformTargetModel]=true; " +
			"the SkipAll entry must suppress SelectOne entirely — no options are built and no " +
			"question is presented when SkipAll suppresses the question")
	}
}
