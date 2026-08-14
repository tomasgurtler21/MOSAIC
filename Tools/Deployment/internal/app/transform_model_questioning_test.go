package app_test

// transform_model_questioning_test.go covers the per-source-model questioning behavior
// of the transform service (T7.2):
//
// One question per distinct source model (T7.2):
//   - A batch spanning N distinct source models triggers exactly N QTransformTargetModel
//     questions, each with Subject = the source model identifier being mapped.
//   - Multiple agents sharing one source model produce exactly one question for that model.
//   - Questions are asked in Distinct order: lexicographically ascending, unset group last.
//   - Each question's Title identifies which source model is being mapped.
//
// Skip and SkipAll behavior:
//   - Skipping an individual question leaves that group's target model empty, while other
//     groups that are answered carry the answered model.
//   - SkipAll[QTransformTargetModel] suppresses every model question regardless of how many
//     distinct source models exist.
//
// Pre-answered mapping via ModelMap:
//   - A ModelMap entry for a source model suppresses that model's interactive question.
//   - Source models not covered by ModelMap still trigger their question.
//
// Unset model group:
//   - Agents whose source file carries no model field form one group keyed by
//     UnsetSourceModel (""), and their question's Subject is "".
//   - The question Title for the unset group identifies it as "agents with no model set".
//
// All service-level tests call svc.TransformHarness, which uses the old single-model
// questioning logic until I7.3 replaces it. Tests asserting per-source-model questions
// (specific counts, subjects, or titles) fail in the RED phase for the correct reason:
// the service currently asks at most one batch-wide QTransformTargetModel (Subject="").

import (
	"context"
	"strings"
	"testing"

	"mosaic-deploy/internal/app"
	"mosaic-deploy/internal/app/interactiontest"
	"mosaic-deploy/internal/domain"
)

// ---------------------------------------------------------------------------
// Agent byte fixtures for multi-source-model batches
// ---------------------------------------------------------------------------

// matchingAgentBytesWithModel returns source-harness agent bytes with the given
// src_model value. When model is empty the field is omitted (unset group).
// All other fields match matchingAgentBytes so DetectHarnessMatch returns HarnessMatchYes
// or HarnessMatchIndeterminate (never HarnessMatchNo) for the source harness.
func matchingAgentBytesWithModel(model string) []byte {
	if model != "" {
		return []byte("---\n" +
			"transform_version: \"2.1.0\"\n" +
			"injections_version: \"3.0.0\"\n" +
			"src_model: " + model + "\n" +
			"---\n" +
			"<Identity type=\"core\">\n" +
			"You are an agent with src_model " + model + ".\n" +
			"</Identity>\n")
	}
	return []byte("---\n" +
		"transform_version: \"2.1.0\"\n" +
		"injections_version: \"3.0.0\"\n" +
		"---\n" +
		"<Identity type=\"core\">\n" +
		"You are an agent with no model set.\n" +
		"</Identity>\n")
}

// ---------------------------------------------------------------------------
// Spy that captures every QTransformTargetModel question in call order
// ---------------------------------------------------------------------------

// capturingModelSpy records every SelectOne call for QTransformTargetModel, capturing the
// full ChoiceQuestion (including Subject and Title). All other Interaction calls delegate
// to the base stub. Answers for each (ID, Subject) pair are resolved from the provided map;
// unscripted model questions return SkippedOne.
type capturingModelSpy struct {
	base     domain.Interaction
	captured []domain.ChoiceQuestion
	answers  map[string]domain.ChoiceAnswer // keyed by Subject; "" for unset/batch-wide
}

func newCapturingModelSpy(base domain.Interaction, answers map[string]domain.ChoiceAnswer) *capturingModelSpy {
	if answers == nil {
		answers = make(map[string]domain.ChoiceAnswer)
	}
	return &capturingModelSpy{base: base, answers: answers}
}

func (s *capturingModelSpy) SelectOne(ctx context.Context, q domain.ChoiceQuestion) (domain.ChoiceAnswer, error) {
	if q.ID == domain.QTransformTargetModel {
		cp := q
		s.captured = append(s.captured, cp)
		if a, ok := s.answers[q.Subject]; ok {
			return a, nil
		}
		return domain.ChoiceAnswer{Status: domain.SkippedOne}, nil
	}
	return s.base.SelectOne(ctx, q)
}

func (s *capturingModelSpy) SelectMany(ctx context.Context, q domain.ChoiceQuestion) (domain.MultiChoiceAnswer, error) {
	return s.base.SelectMany(ctx, q)
}

func (s *capturingModelSpy) AskText(ctx context.Context, q domain.TextQuestion) (domain.TextAnswer, error) {
	return s.base.AskText(ctx, q)
}

func (s *capturingModelSpy) Confirm(ctx context.Context, q domain.Question) (domain.ConfirmAnswer, error) {
	return s.base.Confirm(ctx, q)
}

func (s *capturingModelSpy) Notify(ctx context.Context, n domain.Notice) {
	s.base.Notify(ctx, n)
}

func (s *capturingModelSpy) Progress(ctx context.Context, e domain.ProgressEvent) {
	s.base.Progress(ctx, e)
}

func (s *capturingModelSpy) Review(ctx context.Context, p domain.Plan) (domain.ConfirmAnswer, error) {
	return s.base.Review(ctx, p)
}

// capturedSubjects returns the Subject of every captured QTransformTargetModel question,
// in call order. Used to verify both count and ordering.
func (s *capturingModelSpy) capturedSubjects() []string {
	out := make([]string, len(s.captured))
	for i, q := range s.captured {
		out[i] = q.Subject
	}
	return out
}

// ---------------------------------------------------------------------------
// newMultiModelTransformDeps — deps with a target module that has model IDs
// ---------------------------------------------------------------------------

// newMultiModelTransformDeps returns app.Deps suitable for multi-source-model questioning
// tests. The target harness module declares model IDs so questions carry non-empty options.
func newMultiModelTransformDeps(t *testing.T, spy domain.Interaction) (app.Deps, string) {
	t.Helper()
	baseStub := interactiontest.NewBuilder().Build()
	deps, workspace := newTransformDeps(t, baseStub)

	// Build a target module with model IDs so QTransformTargetModel has options.
	tgtModule := newTransformTargetModuleWithModelIDs([]string{"target-a", "target-b", "target-c"})
	deps.Registry = &stubRegistry{
		list: []domain.HarnessRef{transformSourceHarness, transformTargetHarness},
		modules: map[string]domain.HarnessModule{
			"source-harness": newTransformSourceModule(),
			"target-harness": tgtModule,
		},
	}
	deps.Interaction = spy
	return deps, workspace
}

// ---------------------------------------------------------------------------
// T7.2: Two distinct source models → two questions asked
// ---------------------------------------------------------------------------

// TestTransformHarness_TwoDistinctSourceModels_AsksTwoModelQuestions verifies that a batch
// containing agents from two distinct source models triggers exactly two QTransformTargetModel
// questions — one per distinct source model, not one for the whole batch and not one per agent.
func TestTransformHarness_TwoDistinctSourceModels_AsksTwoModelQuestions(t *testing.T) {
	// Arrange: two agents with different source models.
	batchDir := t.TempDir()
	writeTransformFile(t, batchDir, "a-sonnet.src.md", matchingAgentBytesWithModel("claude-3-sonnet"))
	writeTransformFile(t, batchDir, "b-opus.src.md", matchingAgentBytesWithModel("claude-opus"))

	spy := newCapturingModelSpy(
		interactiontest.NewBuilder().Build(),
		map[string]domain.ChoiceAnswer{
			"claude-3-sonnet": {Status: domain.Answered, OptionID: "target-a"},
			"claude-opus":     {Status: domain.Answered, OptionID: "target-b"},
			// Batch-wide fallback for RED phase (service asks with Subject="").
			"": {Status: domain.Answered, OptionID: "target-a"},
		},
	)
	deps, _ := newMultiModelTransformDeps(t, spy)
	svc := app.New(deps)

	req := newPreAnsweredTransformRequest(batchDir)
	req.TargetModel = "" // unset to trigger model questions

	// Act
	_, _ = svc.TransformHarness(context.Background(), req)

	// Assert: exactly two QTransformTargetModel questions must have been asked.
	// RED: the service asks at most one batch-wide question (Subject=""), so
	// len(spy.captured) == 1 != 2.
	if len(spy.captured) != 2 {
		t.Errorf("QTransformTargetModel asked %d time(s), want 2 (once per distinct source model); "+
			"captured subjects: %v; "+
			"(RED: the service asks a single batch-wide question; deliver I7.3 to ask one per distinct source model)",
			len(spy.captured), spy.capturedSubjects())
	}
}

// ---------------------------------------------------------------------------
// T7.2: Multiple agents sharing one source model → one question
// ---------------------------------------------------------------------------

// TestTransformHarness_MultipleAgentsSameSourceModel_AsksOneQuestionWithCorrectSubject
// verifies that three agents all carrying the same source model ("claude-3-sonnet") produce
// exactly one QTransformTargetModel question whose Subject is "claude-3-sonnet". Deduplication
// happens in the pre-pass; the questioning loop must see only one distinct source model.
func TestTransformHarness_MultipleAgentsSameSourceModel_AsksOneQuestionWithCorrectSubject(t *testing.T) {
	// Arrange: three agents all with src_model=claude-3-sonnet.
	batchDir := t.TempDir()
	writeTransformFile(t, batchDir, "a-agent.src.md", matchingAgentBytesWithModel("claude-3-sonnet"))
	writeTransformFile(t, batchDir, "b-agent.src.md", matchingAgentBytesWithModel("claude-3-sonnet"))
	writeTransformFile(t, batchDir, "c-agent.src.md", matchingAgentBytesWithModel("claude-3-sonnet"))

	spy := newCapturingModelSpy(
		interactiontest.NewBuilder().Build(),
		map[string]domain.ChoiceAnswer{
			"claude-3-sonnet": {Status: domain.Answered, OptionID: "target-a"},
			"":                {Status: domain.Answered, OptionID: "target-a"},
		},
	)
	deps, _ := newMultiModelTransformDeps(t, spy)
	svc := app.New(deps)

	req := newPreAnsweredTransformRequest(batchDir)
	req.TargetModel = ""

	// Act
	_, _ = svc.TransformHarness(context.Background(), req)

	// Assert: exactly one question asked.
	// Count passes trivially in RED (service also asks one). Subject assertion fails:
	// the service asks with Subject="" not "claude-3-sonnet".
	if len(spy.captured) != 1 {
		t.Errorf("QTransformTargetModel asked %d time(s), want 1 (three agents same model = one question); "+
			"(RED: deliver I7.3)", len(spy.captured))
	}

	// Assert: question Subject identifies the source model being mapped.
	// RED: Subject="" (batch-wide) != "claude-3-sonnet".
	if len(spy.captured) > 0 && spy.captured[0].Subject != "claude-3-sonnet" {
		t.Errorf("QTransformTargetModel Subject = %q, want %q; "+
			"the question must identify which source model it is mapping via its Subject field "+
			"(RED: service uses Subject=\"\" for the batch-wide question)",
			spy.captured[0].Subject, "claude-3-sonnet")
	}
}

// ---------------------------------------------------------------------------
// T7.2: Each question Subject matches its source model
// ---------------------------------------------------------------------------

// TestTransformHarness_EachQuestionSubjectMatchesSourceModel verifies that when two distinct
// source models are present in the batch, each QTransformTargetModel question carries the
// matching source model as its Subject. Subject is the raw source model identifier — a test
// can assert which group a question belongs to without parsing the Title prose.
func TestTransformHarness_EachQuestionSubjectMatchesSourceModel(t *testing.T) {
	// Arrange: one agent per source model.
	batchDir := t.TempDir()
	writeTransformFile(t, batchDir, "a-sonnet.src.md", matchingAgentBytesWithModel("claude-3-sonnet"))
	writeTransformFile(t, batchDir, "b-opus.src.md", matchingAgentBytesWithModel("claude-opus"))

	spy := newCapturingModelSpy(
		interactiontest.NewBuilder().Build(),
		map[string]domain.ChoiceAnswer{
			"claude-3-sonnet": {Status: domain.Answered, OptionID: "target-a"},
			"claude-opus":     {Status: domain.Answered, OptionID: "target-b"},
			"":                {Status: domain.Answered, OptionID: "target-a"},
		},
	)
	deps, _ := newMultiModelTransformDeps(t, spy)
	svc := app.New(deps)

	req := newPreAnsweredTransformRequest(batchDir)
	req.TargetModel = ""

	// Act
	_, _ = svc.TransformHarness(context.Background(), req)

	// Assert: questions asked for each source model by Subject.
	// RED: service asks with Subject=""; WasAsked returns false for specific subjects.
	sonnetAsked := false
	opusAsked := false
	for _, q := range spy.captured {
		switch q.Subject {
		case "claude-3-sonnet":
			sonnetAsked = true
		case "claude-opus":
			opusAsked = true
		}
	}

	if !sonnetAsked {
		t.Errorf("QTransformTargetModel was not asked with Subject=%q; "+
			"each distinct source model must trigger its own question with matching Subject "+
			"(RED: service uses Subject=\"\" for all model questions)", "claude-3-sonnet")
	}
	if !opusAsked {
		t.Errorf("QTransformTargetModel was not asked with Subject=%q; "+
			"(RED: service uses Subject=\"\" for all model questions)", "claude-opus")
	}
}

// ---------------------------------------------------------------------------
// T7.2: Questions asked in Distinct order (lexicographic, unset last)
// ---------------------------------------------------------------------------

// TestTransformHarness_ModelQuestionsAskedInDistinctOrder verifies that when multiple source
// models are present the QTransformTargetModel questions are asked in the SourceModelIndex.Distinct
// order: lexicographically ascending for named models, with the unset group last.
func TestTransformHarness_ModelQuestionsAskedInDistinctOrder(t *testing.T) {
	// Arrange: files that would produce Distinct = ["model-a", "model-z", ""] (unset last).
	batchDir := t.TempDir()
	writeTransformFile(t, batchDir, "a-zed.src.md", matchingAgentBytesWithModel("model-z"))
	writeTransformFile(t, batchDir, "b-alpha.src.md", matchingAgentBytesWithModel("model-a"))
	writeTransformFile(t, batchDir, "c-unset.src.md", matchingAgentBytesWithModel(""))

	spy := newCapturingModelSpy(
		interactiontest.NewBuilder().Build(),
		map[string]domain.ChoiceAnswer{
			"model-a": {Status: domain.Answered, OptionID: "target-a"},
			"model-z": {Status: domain.Answered, OptionID: "target-z"},
			"":        {Status: domain.Answered, OptionID: "target-unset"},
		},
	)
	deps, _ := newMultiModelTransformDeps(t, spy)
	svc := app.New(deps)

	req := newPreAnsweredTransformRequest(batchDir)
	req.TargetModel = ""

	// Act
	_, _ = svc.TransformHarness(context.Background(), req)

	// Assert: exactly 3 questions, in order ["model-a", "model-z", ""].
	// RED: service asks one question with Subject="" → completely wrong.
	wantOrder := []string{"model-a", "model-z", ""}
	got := spy.capturedSubjects()

	if len(got) != len(wantOrder) {
		t.Errorf("QTransformTargetModel asked %d time(s), want %d; subjects: %v; "+
			"(RED: deliver I7.3)", len(got), len(wantOrder), got)
		return
	}

	for i, want := range wantOrder {
		if got[i] != want {
			t.Errorf("question[%d].Subject = %q, want %q; "+
				"questions must be asked in Distinct order (lexicographic, unset last)", i, got[i], want)
		}
	}
}

// ---------------------------------------------------------------------------
// T7.2: Unset group — question title and subject
// ---------------------------------------------------------------------------

// TestTransformHarness_UnsetModelGroup_QuestionHasEmptySubjectAndDescriptiveTitle verifies
// that agents with no source model field produce one QTransformTargetModel question whose
// Subject is "" (UnsetSourceModel) and whose Title contains "no model set" to identify the
// group for the user.
func TestTransformHarness_UnsetModelGroup_QuestionHasEmptySubjectAndDescriptiveTitle(t *testing.T) {
	// Arrange: one agent with no src_model field.
	batchDir := t.TempDir()
	writeTransformFile(t, batchDir, "a-unset.src.md", matchingAgentBytesWithModel(""))

	spy := newCapturingModelSpy(
		interactiontest.NewBuilder().Build(),
		map[string]domain.ChoiceAnswer{
			"": {Status: domain.Answered, OptionID: "target-a"},
		},
	)
	deps, _ := newMultiModelTransformDeps(t, spy)
	svc := app.New(deps)

	req := newPreAnsweredTransformRequest(batchDir)
	req.TargetModel = ""

	// Act
	_, _ = svc.TransformHarness(context.Background(), req)

	if len(spy.captured) == 0 {
		t.Fatalf("no QTransformTargetModel question was asked; want exactly one for the unset group")
	}

	q := spy.captured[0]

	// Subject must be UnsetSourceModel (empty string) for the unset group.
	// NOT RED-VERIFIED for Subject: the current service asks with Subject="" (batch-wide),
	// which matches UnsetSourceModel. This assertion trivially passes in RED phase.
	if q.Subject != app.UnsetSourceModel {
		t.Errorf("question Subject = %q, want UnsetSourceModel (%q)", q.Subject, app.UnsetSourceModel)
	}

	// Title must identify the group as agents with no model set.
	// RED: the current Title is "Select target model (optional, enter to skip)", which does
	// NOT contain "no model set".
	if q.Title == "" {
		t.Errorf("question Title is empty; want a descriptive title identifying the unset group")
	}
	// We check for the key phrase rather than an exact string to allow minor wording changes.
	wantPhrase := "no model set"
	if !strings.Contains(q.Title, wantPhrase) {
		t.Errorf("question Title = %q; want it to contain %q to identify the unset group "+
			"(RED: current Title does not mention the unset group)", q.Title, wantPhrase)
	}
}

// ---------------------------------------------------------------------------
// T7.2: Named source model — question title
// ---------------------------------------------------------------------------

// TestTransformHarness_NamedSourceModel_QuestionTitleIncludesModelName verifies that the
// QTransformTargetModel question for a named source model includes the model identifier in
// its Title, so the user knows which source model they are mapping.
func TestTransformHarness_NamedSourceModel_QuestionTitleIncludesModelName(t *testing.T) {
	// Arrange: one agent with a named source model.
	batchDir := t.TempDir()
	writeTransformFile(t, batchDir, "a-sonnet.src.md", matchingAgentBytesWithModel("claude-3-sonnet"))

	spy := newCapturingModelSpy(
		interactiontest.NewBuilder().Build(),
		map[string]domain.ChoiceAnswer{
			"claude-3-sonnet": {Status: domain.Answered, OptionID: "target-a"},
			"":                {Status: domain.Answered, OptionID: "target-a"},
		},
	)
	deps, _ := newMultiModelTransformDeps(t, spy)
	svc := app.New(deps)

	req := newPreAnsweredTransformRequest(batchDir)
	req.TargetModel = ""

	// Act
	_, _ = svc.TransformHarness(context.Background(), req)

	if len(spy.captured) == 0 {
		t.Fatalf("no QTransformTargetModel question captured")
	}

	q := spy.captured[0]

	// Title must include the source model identifier.
	// RED: the current Title does not include "claude-3-sonnet".
	wantPhrase := "claude-3-sonnet"
	if !strings.Contains(q.Title, wantPhrase) {
		t.Errorf("question Title = %q; want it to contain the source model name %q "+
			"(RED: current Title is the generic batch-wide title, not per-source-model; deliver I7.3)",
			q.Title, wantPhrase)
	}
}

// ---------------------------------------------------------------------------
// T7.2: Skipping a question leaves that group's target model empty
// ---------------------------------------------------------------------------

// TestTransformHarness_SkippedModelQuestion_LeavesGroupTargetEmpty verifies that when the
// user skips the QTransformTargetModel question for one source model, that group's agents
// get an empty target model while other groups whose question was answered carry their
// answered target model.
//
// Per-group emptying is asserted via result.AppliedModelMap.
func TestTransformHarness_SkippedModelQuestion_LeavesGroupTargetEmpty(t *testing.T) {
	// Arrange: two distinct source models. "model-a" question is skipped, "model-b" answered.
	batchDir := t.TempDir()
	writeTransformFile(t, batchDir, "a-agent.src.md", matchingAgentBytesWithModel("model-a"))
	writeTransformFile(t, batchDir, "b-agent.src.md", matchingAgentBytesWithModel("model-b"))

	spy := newCapturingModelSpy(
		interactiontest.NewBuilder().Build(),
		map[string]domain.ChoiceAnswer{
			"model-a": {Status: domain.SkippedOne}, // user skips model-a
			"model-b": {Status: domain.Answered, OptionID: "target-b"},
			// Batch-wide fallback for RED phase: the service asks Subject="" and the spy
			// returns SkippedOne (not in answers map → default SkippedOne). This means
			// in RED phase ALL agents get an empty model.
		},
	)
	deps, _ := newMultiModelTransformDeps(t, spy)
	svc := app.New(deps)

	req := newPreAnsweredTransformRequest(batchDir)
	req.TargetModel = ""

	// Act
	result, err := svc.TransformHarness(context.Background(), req)
	if err != nil {
		t.Fatalf("TransformHarness returned error: %v", err)
	}

	// Assert: model-b group received "target-b" in AppliedModelMap.
	// RED: result.AppliedModelMap is nil (not populated by stub implementation).
	if result.AppliedModelMap == nil {
		t.Errorf("result.AppliedModelMap is nil; want a non-nil mapping recording what was applied "+
			"(RED: deliver I7.3 to populate AppliedModelMap)")
		return
	}

	if result.AppliedModelMap["model-b"] != "target-b" {
		t.Errorf("AppliedModelMap[\"model-b\"] = %q, want %q; "+
			"the answered group must carry its target model", result.AppliedModelMap["model-b"], "target-b")
	}

	if result.AppliedModelMap["model-a"] != "" {
		t.Errorf("AppliedModelMap[\"model-a\"] = %q, want empty; "+
			"the skipped group must have an empty target model", result.AppliedModelMap["model-a"])
	}
}

// ---------------------------------------------------------------------------
// T7.2: SkipAll suppresses all model questions (NOT RED-VERIFIED)
// ---------------------------------------------------------------------------

// TestTransformHarness_SkipAll_SuppressesAllModelQuestions verifies that when
// SkipAll[QTransformTargetModel] is true, no QTransformTargetModel question is asked
// regardless of how many distinct source models are in the batch.
//
// NOT RED-VERIFIED: the existing SkipAll guard (`!req.SkipAll[domain.QTransformTargetModel]`)
// already suppresses the batch-wide question in the current implementation. This test passes
// trivially in the RED phase. Once I7.3 introduces per-source-model questioning, verify that
// SkipAll still suppresses all questions and no per-source-model questions are leaked.
func TestTransformHarness_SkipAll_SuppressesAllModelQuestions(t *testing.T) {
	// Arrange: two distinct source models but SkipAll suppresses all model questions.
	batchDir := t.TempDir()
	writeTransformFile(t, batchDir, "a-sonnet.src.md", matchingAgentBytesWithModel("claude-3-sonnet"))
	writeTransformFile(t, batchDir, "b-opus.src.md", matchingAgentBytesWithModel("claude-opus"))

	spy := newCapturingModelSpy(interactiontest.NewBuilder().Build(), nil)
	deps, _ := newMultiModelTransformDeps(t, spy)
	svc := app.New(deps)

	req := newPreAnsweredTransformRequest(batchDir)
	req.TargetModel = ""
	req.SkipAll = map[domain.QuestionID]bool{
		domain.QTransformTargetModel: true,
	}

	// Act
	_, _ = svc.TransformHarness(context.Background(), req)

	// Assert: no model questions asked.
	if len(spy.captured) != 0 {
		t.Errorf("QTransformTargetModel asked %d time(s), want 0 when SkipAll is set; "+
			"captured subjects: %v", len(spy.captured), spy.capturedSubjects())
	}
}

// ---------------------------------------------------------------------------
// T7.2: ModelMap pre-answer suppresses corresponding source model's question
// ---------------------------------------------------------------------------

// TestTransformHarness_ModelMap_SuppressesQuestionForMappedSourceModel verifies that a
// ModelMap entry for a source model suppresses that model's interactive question while
// source models not in the map still trigger their question.
func TestTransformHarness_ModelMap_SuppressesQuestionForMappedSourceModel(t *testing.T) {
	// Arrange: two source models. claude-3-sonnet is pre-answered via ModelMap;
	// claude-opus must still trigger its question.
	batchDir := t.TempDir()
	writeTransformFile(t, batchDir, "a-sonnet.src.md", matchingAgentBytesWithModel("claude-3-sonnet"))
	writeTransformFile(t, batchDir, "b-opus.src.md", matchingAgentBytesWithModel("claude-opus"))

	spy := newCapturingModelSpy(
		interactiontest.NewBuilder().Build(),
		map[string]domain.ChoiceAnswer{
			"claude-opus": {Status: domain.Answered, OptionID: "target-b"},
			// No entry for "" (batch-wide) so in RED phase the service gets SkippedOne.
		},
	)
	deps, _ := newMultiModelTransformDeps(t, spy)
	svc := app.New(deps)

	req := newPreAnsweredTransformRequest(batchDir)
	req.TargetModel = ""
	req.ModelMap = app.TransformModelMapping{
		"claude-3-sonnet": "target-a", // pre-answered — must suppress the question
	}

	// Act
	_, _ = svc.TransformHarness(context.Background(), req)

	// Assert: question asked for claude-opus (not pre-answered).
	// RED: the service ignores ModelMap and asks a single batch-wide question with Subject="",
	// so WasAsked("claude-opus") is false.
	opusAsked := false
	for _, q := range spy.captured {
		if q.Subject == "claude-opus" {
			opusAsked = true
		}
	}
	if !opusAsked {
		t.Errorf("QTransformTargetModel was not asked with Subject=%q; "+
			"a source model not covered by ModelMap must still trigger its question "+
			"(RED: service ignores ModelMap; deliver I7.3)", "claude-opus")
	}

	// Assert: question NOT asked for claude-3-sonnet (pre-answered via ModelMap).
	// NOT RED-VERIFIED for the negative: in RED phase the question is asked with Subject=""
	// not "claude-3-sonnet", so this check trivially holds.
	sonnetAsked := false
	for _, q := range spy.captured {
		if q.Subject == "claude-3-sonnet" {
			sonnetAsked = true
		}
	}
	if sonnetAsked {
		t.Errorf("QTransformTargetModel was asked with Subject=%q despite being pre-answered via ModelMap; "+
			"a ModelMap entry must suppress the interactive question for that source model",
			"claude-3-sonnet")
	}
}

// TestTransformHarness_ModelMap_EmptyValue_SuppressesQuestion verifies that a ModelMap entry
// with an empty target value suppresses the interactive question for that source model.
// The design specifies a three-way distinction:
//   - Key absent → ask the interactive question.
//   - Key present with non-empty value → pre-answered, suppress question, use value.
//   - Key present with empty value → explicitly pre-answered as "leave model empty"; suppress
//     question; do NOT fall through to TargetModel or prompt.
//
// This test covers the third case, verifying that the question is suppressed even when the
// pre-answered target is "". Other source models without a ModelMap entry still get their
// question asked normally.
func TestTransformHarness_ModelMap_EmptyValue_SuppressesQuestion(t *testing.T) {
	// Arrange: two source models. "model-a" is pre-answered with empty string via ModelMap;
	// "model-b" has no ModelMap entry so its question must still be asked.
	batchDir := t.TempDir()
	writeTransformFile(t, batchDir, "a-agent.src.md", matchingAgentBytesWithModel("model-a"))
	writeTransformFile(t, batchDir, "b-agent.src.md", matchingAgentBytesWithModel("model-b"))

	spy := newCapturingModelSpy(
		interactiontest.NewBuilder().Build(),
		map[string]domain.ChoiceAnswer{
			"model-b": {Status: domain.Answered, OptionID: "target-b"},
		},
	)
	deps, _ := newMultiModelTransformDeps(t, spy)
	svc := app.New(deps)

	req := newPreAnsweredTransformRequest(batchDir)
	req.TargetModel = "target-fallback" // non-empty so a fallthrough would be detectable
	req.ModelMap = app.TransformModelMapping{
		"model-a": "", // present key, empty value — must suppress question, not fall through
	}

	// Act
	_, _ = svc.TransformHarness(context.Background(), req)

	// Assert: question NOT asked for "model-a" (pre-answered with empty, question suppressed).
	// NOT RED-VERIFIED for the negative: in RED phase the service asks with Subject="" (batch-wide),
	// not "model-a", so no captured question has Subject="model-a" and this check trivially holds.
	// The substantive RED signal for the empty-value semantic is in
	// TestTransformHarness_ModelMap_EmptyValue_LeavesModelEmpty (T7.3), which asserts
	// AppliedModelMap["model-a"] == "" and correctly fails in RED.
	for _, q := range spy.captured {
		if q.Subject == "model-a" {
			t.Errorf("QTransformTargetModel was asked with Subject=%q despite ModelMap[\"model-a\"]=\"\" "+
				"pre-answering it; a present ModelMap key with empty value must suppress the interactive "+
				"question regardless of the value being empty "+
				"(RED: service ignores ModelMap; deliver I7.3)", "model-a")
		}
	}
}

// TestTransformHarness_FullModelMap_SuppressesAllModelQuestions verifies that when ModelMap
// provides pre-answers for every source model in the batch, no interactive QTransformTargetModel
// question is asked at all.
func TestTransformHarness_FullModelMap_SuppressesAllModelQuestions(t *testing.T) {
	// Arrange: two source models, both pre-answered via ModelMap.
	batchDir := t.TempDir()
	writeTransformFile(t, batchDir, "a-sonnet.src.md", matchingAgentBytesWithModel("claude-3-sonnet"))
	writeTransformFile(t, batchDir, "b-opus.src.md", matchingAgentBytesWithModel("claude-opus"))

	spy := newCapturingModelSpy(interactiontest.NewBuilder().Build(), nil)
	deps, _ := newMultiModelTransformDeps(t, spy)
	svc := app.New(deps)

	req := newPreAnsweredTransformRequest(batchDir)
	req.TargetModel = ""
	req.ModelMap = app.TransformModelMapping{
		"claude-3-sonnet": "target-a",
		"claude-opus":     "target-b",
	}

	// Act
	_, _ = svc.TransformHarness(context.Background(), req)

	// Assert: no model questions asked.
	// RED: service ignores ModelMap and asks one batch-wide question (Subject=""), so
	// len(spy.captured) == 1 != 0.
	if len(spy.captured) != 0 {
		t.Errorf("QTransformTargetModel asked %d time(s), want 0 when all source models are pre-answered via ModelMap; "+
			"captured subjects: %v "+
			"(RED: service ignores ModelMap; deliver I7.3)",
			len(spy.captured), spy.capturedSubjects())
	}
}
