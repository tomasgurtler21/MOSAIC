package app_test

// transform_model_application_test.go covers per-agent model application after per-source-model
// questioning (T7.3):
//
// Per-agent model lookup (T7.3):
//   - Each transformed agent's output carries the target model resolved from that agent's
//     own source model, not a single batch-wide constant.
//   - result.AppliedModelMap records the source-model→target-model mapping that was applied
//     during the run, exposing per-agent correctness without requiring file reads.
//   - result.TargetModel equals the single non-empty target model when every agent was mapped
//     to the same target; it is empty when multiple distinct target models were applied.
//
// Pre-answered mapping via ModelMap (T7.3):
//   - A fully-populated ModelMap suppresses all interactive questions and drives the
//     per-agent model lookup directly, producing the same AppliedModelMap as the
//     interactive path would.
//
// All service-level tests call svc.TransformHarness. In the RED phase:
//   - result.AppliedModelMap is nil (not populated by the current single-model implementation).
//   - result.TargetModel is the single batch-wide answer, which means multi-model assertions
//     about its value fail correctly.
//   - Per-file model fields in written output carry the single batch-wide model for all files,
//     so per-source-model assertions about file content fail correctly.

import (
	"context"
	"os"
	"testing"

	"mosaic-common/docformat"
	"mosaic-deploy/internal/app"
	"mosaic-deploy/internal/app/interactiontest"
	"mosaic-deploy/internal/domain"
)

// ---------------------------------------------------------------------------
// Helpers for reading the target model from a written output file
// ---------------------------------------------------------------------------

// readOutputModel reads the target harness's model field ("tgt_model") from a written
// output file, returning its value and whether it was present. Used to verify per-file
// model application independently of AppliedModelMap.
func readOutputModel(t *testing.T, path string) (value string, present bool) {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("readOutputModel: os.ReadFile(%q): %v", path, err)
	}
	doc, parseErr := docformat.Parse(content)
	if parseErr != nil {
		t.Fatalf("readOutputModel: docformat.Parse(%q): %v", path, parseErr)
	}
	v, ok := doc.Frontmatter().Get("tgt_model")
	if !ok {
		return "", false
	}
	if v.Kind != domain.KindScalar {
		return "", true // field present but not a scalar
	}
	return v.Scalar, true
}

// ---------------------------------------------------------------------------
// newMultiModelTransformDepsWithSpy — deps for application tests
// ---------------------------------------------------------------------------

// newMultiModelTransformDepsWithSpy returns app.Deps suitable for model-application tests
// that write real output files. The target module has model IDs and a TargetPath that places
// output next to the source (flat layout) so DestinationPath is predictable.
func newMultiModelTransformDepsWithSpy(t *testing.T, spy domain.Interaction) (app.Deps, string) {
	t.Helper()
	return newMultiModelTransformDeps(t, spy)
}

// ---------------------------------------------------------------------------
// T7.3: AppliedModelMap is populated with the resolved mapping
// ---------------------------------------------------------------------------

// TestTransformHarness_AppliedModelMap_RecordsResolvedMapping verifies that
// result.AppliedModelMap is non-nil and contains an entry for each distinct source model
// that was processed during the run.
func TestTransformHarness_AppliedModelMap_RecordsResolvedMapping(t *testing.T) {
	// Arrange: two distinct source models.
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
	deps, _ := newMultiModelTransformDepsWithSpy(t, spy)
	svc := app.New(deps)

	req := newPreAnsweredTransformRequest(batchDir)
	req.TargetModel = ""
	req.DryRun = true // avoid writing files; we assert on result only

	// Act
	result, err := svc.TransformHarness(context.Background(), req)
	if err != nil {
		t.Fatalf("TransformHarness returned error: %v", err)
	}

	// Assert: AppliedModelMap is non-nil.
	// RED: current implementation does not populate AppliedModelMap (nil).
	if result.AppliedModelMap == nil {
		t.Errorf("result.AppliedModelMap is nil; want a non-nil mapping of source→target models "+
			"(RED: deliver I7.4 to populate AppliedModelMap)")
		return
	}

	// Assert: both source models appear in the map.
	if _, ok := result.AppliedModelMap["claude-3-sonnet"]; !ok {
		t.Errorf("AppliedModelMap missing key %q; want an entry for every processed source model",
			"claude-3-sonnet")
	}
	if _, ok := result.AppliedModelMap["claude-opus"]; !ok {
		t.Errorf("AppliedModelMap missing key %q; want an entry for every processed source model",
			"claude-opus")
	}
}

// ---------------------------------------------------------------------------
// T7.3: Each agent gets the model mapped from its own source model
// ---------------------------------------------------------------------------

// TestTransformHarness_TwoSourceModels_AppliedModelMapShowsPerSourceMapping verifies that
// result.AppliedModelMap records the correct target model for each distinct source model —
// not the same batch-wide value for all groups.
func TestTransformHarness_TwoSourceModels_AppliedModelMapShowsPerSourceMapping(t *testing.T) {
	// Arrange: two source models mapped to different targets.
	batchDir := t.TempDir()
	writeTransformFile(t, batchDir, "a-sonnet.src.md", matchingAgentBytesWithModel("claude-3-sonnet"))
	writeTransformFile(t, batchDir, "b-opus.src.md", matchingAgentBytesWithModel("claude-opus"))

	spy := newCapturingModelSpy(
		interactiontest.NewBuilder().Build(),
		map[string]domain.ChoiceAnswer{
			"claude-3-sonnet": {Status: domain.Answered, OptionID: "target-a"},
			"claude-opus":     {Status: domain.Answered, OptionID: "target-b"},
			// RED-phase fallback: the service asks with Subject="" and returns SkippedOne
			// (not in map) → batch-wide targetModel = "".
		},
	)
	deps, _ := newMultiModelTransformDepsWithSpy(t, spy)
	svc := app.New(deps)

	req := newPreAnsweredTransformRequest(batchDir)
	req.TargetModel = ""
	req.DryRun = true

	// Act
	result, err := svc.TransformHarness(context.Background(), req)
	if err != nil {
		t.Fatalf("TransformHarness returned error: %v", err)
	}

	// Assert: AppliedModelMap carries different targets for each source model.
	// RED: AppliedModelMap is nil.
	if result.AppliedModelMap == nil {
		t.Errorf("result.AppliedModelMap is nil (RED: deliver I7.4)")
		return
	}

	if result.AppliedModelMap["claude-3-sonnet"] != "target-a" {
		t.Errorf("AppliedModelMap[\"claude-3-sonnet\"] = %q, want %q; "+
			"each source model must map to its own answered target model",
			result.AppliedModelMap["claude-3-sonnet"], "target-a")
	}

	if result.AppliedModelMap["claude-opus"] != "target-b" {
		t.Errorf("AppliedModelMap[\"claude-opus\"] = %q, want %q; "+
			"each source model must map to its own answered target model",
			result.AppliedModelMap["claude-opus"], "target-b")
	}
}

// ---------------------------------------------------------------------------
// T7.3: result.TargetModel semantics
// ---------------------------------------------------------------------------

// TestTransformHarness_SingleTargetModel_ResultTargetModelEchoesIt verifies that when all
// agents map to a single non-empty target model, result.TargetModel is set to that value
// for backward-compatible consumption by the TransformSummaryScreen.
//
// NOT RED-VERIFIED: the current service sets result.TargetModel to the batch-wide
// targetModel variable, which in a single-model batch equals the one answered model.
// When the stub answers the batch-wide question with "target-a", result.TargetModel =
// "target-a", so this assertion trivially passes. Once I7.3 replaces the single-model
// path, verify that the single-non-empty-applied-model semantics are preserved.
func TestTransformHarness_SingleTargetModel_ResultTargetModelEchoesIt(t *testing.T) {
	// Arrange: all agents share the same source model → one question → one answered target.
	batchDir := t.TempDir()
	writeTransformFile(t, batchDir, "a-agent.src.md", matchingAgentBytesWithModel("claude-3-sonnet"))
	writeTransformFile(t, batchDir, "b-agent.src.md", matchingAgentBytesWithModel("claude-3-sonnet"))

	// Answer the batch-wide question (Subject="") and the per-model question (Subject="claude-3-sonnet").
	spy := newCapturingModelSpy(
		interactiontest.NewBuilder().Build(),
		map[string]domain.ChoiceAnswer{
			"claude-3-sonnet": {Status: domain.Answered, OptionID: "target-a"},
			"":                {Status: domain.Answered, OptionID: "target-a"},
		},
	)
	deps, _ := newMultiModelTransformDepsWithSpy(t, spy)
	svc := app.New(deps)

	req := newPreAnsweredTransformRequest(batchDir)
	req.TargetModel = ""
	req.DryRun = true

	// Act
	result, err := svc.TransformHarness(context.Background(), req)
	if err != nil {
		t.Fatalf("TransformHarness returned error: %v", err)
	}

	// Assert: result.TargetModel = "target-a" (the single applied model).
	if result.TargetModel != "target-a" {
		t.Errorf("result.TargetModel = %q, want %q; "+
			"when all agents map to the same target model, result.TargetModel must echo it",
			result.TargetModel, "target-a")
	}
}

// TestTransformHarness_MultipleDistinctTargetModels_ResultTargetModelEmpty verifies that
// when the run applies different target models to different source-model groups,
// result.TargetModel is empty — it carries a batch-wide value only when it is unambiguous.
func TestTransformHarness_MultipleDistinctTargetModels_ResultTargetModelEmpty(t *testing.T) {
	// Arrange: two source models mapped to different targets.
	batchDir := t.TempDir()
	writeTransformFile(t, batchDir, "a-sonnet.src.md", matchingAgentBytesWithModel("claude-3-sonnet"))
	writeTransformFile(t, batchDir, "b-opus.src.md", matchingAgentBytesWithModel("claude-opus"))

	// The batch-wide question (RED phase) gets answered with "target-a", which in the
	// current stub makes result.TargetModel = "target-a". The test asserts "" → fails → RED.
	spy := newCapturingModelSpy(
		interactiontest.NewBuilder().Build(),
		map[string]domain.ChoiceAnswer{
			"claude-3-sonnet": {Status: domain.Answered, OptionID: "target-a"},
			"claude-opus":     {Status: domain.Answered, OptionID: "target-b"},
			"":                {Status: domain.Answered, OptionID: "target-a"}, // RED fallback
		},
	)
	deps, _ := newMultiModelTransformDepsWithSpy(t, spy)
	svc := app.New(deps)

	req := newPreAnsweredTransformRequest(batchDir)
	req.TargetModel = ""
	req.DryRun = true

	// Act
	result, err := svc.TransformHarness(context.Background(), req)
	if err != nil {
		t.Fatalf("TransformHarness returned error: %v", err)
	}

	// Assert: result.TargetModel is empty when multiple distinct target models were applied.
	// RED: the service applies the single batch-wide "target-a" to all files, so
	// result.TargetModel = "target-a" != "".
	if result.TargetModel != "" {
		t.Errorf("result.TargetModel = %q, want empty string; "+
			"when different target models were applied to different source groups, "+
			"result.TargetModel must be empty (not the first applied model) "+
			"(RED: service applies single batch-wide model; deliver I7.4)",
			result.TargetModel)
	}
}

// ---------------------------------------------------------------------------
// T7.3: Pre-answered ModelMap drives per-agent application without interaction
// ---------------------------------------------------------------------------

// TestTransformHarness_PreAnsweredModelMap_NoQuestionsAsked verifies that a fully-populated
// ModelMap pre-answers every source model's target, so no interactive QTransformTargetModel
// question is asked. This is the CLI path's expected behavior.
func TestTransformHarness_PreAnsweredModelMap_NoQuestionsAsked(t *testing.T) {
	// Arrange: two source models, both pre-answered via ModelMap.
	batchDir := t.TempDir()
	writeTransformFile(t, batchDir, "a-sonnet.src.md", matchingAgentBytesWithModel("claude-3-sonnet"))
	writeTransformFile(t, batchDir, "b-opus.src.md", matchingAgentBytesWithModel("claude-opus"))

	spy := newCapturingModelSpy(interactiontest.NewBuilder().Build(), nil)
	deps, _ := newMultiModelTransformDepsWithSpy(t, spy)
	svc := app.New(deps)

	req := newPreAnsweredTransformRequest(batchDir)
	req.TargetModel = ""
	req.ModelMap = app.TransformModelMapping{
		"claude-3-sonnet": "target-a",
		"claude-opus":     "target-b",
	}
	req.DryRun = true

	// Act
	_, _ = svc.TransformHarness(context.Background(), req)

	// Assert: no model questions asked.
	// RED: service ignores ModelMap and asks one batch-wide question (Subject="").
	if len(spy.captured) != 0 {
		t.Errorf("QTransformTargetModel asked %d time(s), want 0 when all source models are in ModelMap; "+
			"(RED: service ignores ModelMap; deliver I7.3)",
			len(spy.captured))
	}
}

// TestTransformHarness_PreAnsweredModelMap_AppliedMapMatchesRequest verifies that when
// ModelMap pre-answers every source model, result.AppliedModelMap records the same mapping
// that was supplied on the request.
func TestTransformHarness_PreAnsweredModelMap_AppliedMapMatchesRequest(t *testing.T) {
	// Arrange: two source models, fully pre-answered via ModelMap.
	batchDir := t.TempDir()
	writeTransformFile(t, batchDir, "a-sonnet.src.md", matchingAgentBytesWithModel("claude-3-sonnet"))
	writeTransformFile(t, batchDir, "b-opus.src.md", matchingAgentBytesWithModel("claude-opus"))

	spy := newCapturingModelSpy(interactiontest.NewBuilder().Build(), nil)
	deps, _ := newMultiModelTransformDepsWithSpy(t, spy)
	svc := app.New(deps)

	req := newPreAnsweredTransformRequest(batchDir)
	req.TargetModel = ""
	req.ModelMap = app.TransformModelMapping{
		"claude-3-sonnet": "target-a",
		"claude-opus":     "target-b",
	}
	req.DryRun = true

	// Act
	result, err := svc.TransformHarness(context.Background(), req)
	if err != nil {
		t.Fatalf("TransformHarness returned error: %v", err)
	}

	// Assert: AppliedModelMap reflects the pre-answered mapping.
	// RED: AppliedModelMap is nil.
	if result.AppliedModelMap == nil {
		t.Errorf("result.AppliedModelMap is nil; want the applied mapping to be recorded "+
			"(RED: deliver I7.4)")
		return
	}

	if result.AppliedModelMap["claude-3-sonnet"] != "target-a" {
		t.Errorf("AppliedModelMap[\"claude-3-sonnet\"] = %q, want %q",
			result.AppliedModelMap["claude-3-sonnet"], "target-a")
	}
	if result.AppliedModelMap["claude-opus"] != "target-b" {
		t.Errorf("AppliedModelMap[\"claude-opus\"] = %q, want %q",
			result.AppliedModelMap["claude-opus"], "target-b")
	}
}

// ---------------------------------------------------------------------------
// T7.3: Per-file output carries the correct source-model's target
// ---------------------------------------------------------------------------

// TestTransformHarness_TwoSourceModels_EachOutputFileCarriesCorrectTargetModel verifies that
// the actual written output files carry the target model resolved from each file's own source
// model — not a single batch-wide constant. This is the end-to-end behavioral assertion that
// ties the per-source-model question answers to the per-file output.
func TestTransformHarness_TwoSourceModels_EachOutputFileCarriesCorrectTargetModel(t *testing.T) {
	// Arrange: two agents with different source models. We write to real files and
	// read the output to verify tgt_model was set correctly per source model.
	batchDir := t.TempDir()
	writeTransformFile(t, batchDir, "a-sonnet.src.md", matchingAgentBytesWithModel("claude-3-sonnet"))
	writeTransformFile(t, batchDir, "b-opus.src.md", matchingAgentBytesWithModel("claude-opus"))

	spy := newCapturingModelSpy(
		interactiontest.NewBuilder().Build(),
		map[string]domain.ChoiceAnswer{
			"claude-3-sonnet": {Status: domain.Answered, OptionID: "target-a"},
			"claude-opus":     {Status: domain.Answered, OptionID: "target-b"},
			// RED fallback: batch-wide (Subject="") returns SkippedOne → all get empty model.
		},
	)
	deps, _ := newMultiModelTransformDepsWithSpy(t, spy)
	svc := app.New(deps)

	req := newPreAnsweredTransformRequest(batchDir)
	req.TargetModel = ""
	req.DryRun = false // write real output files

	// Act
	result, err := svc.TransformHarness(context.Background(), req)
	if err != nil {
		t.Fatalf("TransformHarness returned error: %v", err)
	}

	// Locate the outcomes for each source file.
	var sonnetOut, opusOut *app.TransformFileOutcome
	for i := range result.Files {
		switch {
		case len(result.Files[i].SourcePath) > 0 && result.Files[i].SourcePath[len(result.Files[i].SourcePath)-len("a-sonnet.src.md"):] == "a-sonnet.src.md":
			sonnetOut = &result.Files[i]
		case len(result.Files[i].SourcePath) > 0 && result.Files[i].SourcePath[len(result.Files[i].SourcePath)-len("b-opus.src.md"):] == "b-opus.src.md":
			opusOut = &result.Files[i]
		}
	}

	if sonnetOut == nil || opusOut == nil {
		t.Fatalf("could not locate expected file outcomes; Files: %v", result.Files)
	}

	if sonnetOut.Status != app.StatusTransformed || sonnetOut.DestinationPath == "" {
		t.Skipf("skipping per-file model assertion: sonnet agent did not produce StatusTransformed "+
			"(status=%s, dest=%q); implement I7.4 to wire per-source-model retargeting",
			sonnetOut.Status, sonnetOut.DestinationPath)
	}

	if opusOut.Status != app.StatusTransformed || opusOut.DestinationPath == "" {
		t.Skipf("skipping per-file model assertion: opus agent did not produce StatusTransformed "+
			"(status=%s, dest=%q)", opusOut.Status, opusOut.DestinationPath)
	}

	// Assert: each output file carries the target model mapped from its own source model.
	// RED: in the RED phase, the service uses a single batch-wide model for all files.
	// The batch-wide question (Subject="") returns SkippedOne, so both get empty model.
	// Test expects sonnet→"target-a" and opus→"target-b", both fail.
	sonnetModel, _ := readOutputModel(t, sonnetOut.DestinationPath)
	if sonnetModel != "target-a" {
		t.Errorf("output for claude-3-sonnet agent has tgt_model=%q, want %q; "+
			"each agent must carry the target model mapped from its own source model "+
			"(RED: service applies single batch-wide model to all files; deliver I7.4)",
			sonnetModel, "target-a")
	}

	opusModel, _ := readOutputModel(t, opusOut.DestinationPath)
	if opusModel != "target-b" {
		t.Errorf("output for claude-opus agent has tgt_model=%q, want %q; "+
			"(RED: service applies single batch-wide model to all files; deliver I7.4)",
			opusModel, "target-b")
	}
}

// ---------------------------------------------------------------------------
// T7.3: Unset group agents receive the correct target model
// ---------------------------------------------------------------------------

// TestTransformHarness_UnsetGroupAgents_ReceiveTargetModelFromUnsetKey verifies that agents
// with no source model field (SourceModel = UnsetSourceModel = "") are retargeted using the
// model resolved for the "" key — from ModelMap[""] or the interactive question with Subject="".
func TestTransformHarness_UnsetGroupAgents_ReceiveTargetModelFromUnsetKey(t *testing.T) {
	// Arrange: two agents — one with a source model, one with none.
	batchDir := t.TempDir()
	writeTransformFile(t, batchDir, "a-named.src.md", matchingAgentBytesWithModel("claude-3-sonnet"))
	writeTransformFile(t, batchDir, "b-unset.src.md", matchingAgentBytesWithModel(""))

	// Pre-answer via ModelMap so the test is independent of interactive question routing.
	spy := newCapturingModelSpy(interactiontest.NewBuilder().Build(), nil)
	deps, _ := newMultiModelTransformDepsWithSpy(t, spy)
	svc := app.New(deps)

	req := newPreAnsweredTransformRequest(batchDir)
	req.TargetModel = ""
	req.ModelMap = app.TransformModelMapping{
		"claude-3-sonnet": "target-named",
		"":                "target-unset", // maps the unset group
	}
	req.DryRun = true

	// Act
	result, err := svc.TransformHarness(context.Background(), req)
	if err != nil {
		t.Fatalf("TransformHarness returned error: %v", err)
	}

	// Assert: AppliedModelMap records the correct target for both groups.
	// RED: AppliedModelMap is nil.
	if result.AppliedModelMap == nil {
		t.Errorf("result.AppliedModelMap is nil; want the resolved mapping to be recorded "+
			"(RED: deliver I7.4)")
		return
	}

	if result.AppliedModelMap["claude-3-sonnet"] != "target-named" {
		t.Errorf("AppliedModelMap[\"claude-3-sonnet\"] = %q, want %q",
			result.AppliedModelMap["claude-3-sonnet"], "target-named")
	}

	if result.AppliedModelMap[app.UnsetSourceModel] != "target-unset" {
		t.Errorf("AppliedModelMap[UnsetSourceModel] = %q, want %q; "+
			"agents with no model field must use the UnsetSourceModel key for lookup",
			result.AppliedModelMap[app.UnsetSourceModel], "target-unset")
	}
}

// ---------------------------------------------------------------------------
// T7.3: ModelMap empty-value semantics — present key forces empty target
// ---------------------------------------------------------------------------

// TestTransformHarness_ModelMap_EmptyValue_LeavesModelEmpty verifies the three-way
// ModelMap semantics: a present key with an empty target value suppresses the interactive
// question AND forces the applied model to "" — it must NOT fall through to req.TargetModel.
// This pins the distinction between:
//   - Key absent: ask question or use TargetModel fallback.
//   - Key present with non-empty value: pre-answered, suppress question, use value.
//   - Key present with empty value: explicitly pre-answered as "leave model empty";
//     suppress question; do NOT fall through to TargetModel or any other default.
//
// If an implementer treats an empty-value key as absent, the fallthrough to TargetModel
// would silently produce the wrong (non-empty) model. No existing test catches this.
func TestTransformHarness_ModelMap_EmptyValue_LeavesModelEmpty(t *testing.T) {
	// Arrange: one source model pre-answered with empty string; TargetModel is non-empty so
	// a fallthrough is detectable in AppliedModelMap.
	batchDir := t.TempDir()
	writeTransformFile(t, batchDir, "a-model-a.src.md", matchingAgentBytesWithModel("model-a"))

	spy := newCapturingModelSpy(interactiontest.NewBuilder().Build(), nil)
	deps, _ := newMultiModelTransformDepsWithSpy(t, spy)
	svc := app.New(deps)

	req := newPreAnsweredTransformRequest(batchDir)
	req.TargetModel = "target-fallback" // non-empty; a fallthrough would produce this value
	req.ModelMap = app.TransformModelMapping{
		"model-a": "", // present key, empty value — must force empty target, not fall through
	}
	req.DryRun = true

	// Act
	result, err := svc.TransformHarness(context.Background(), req)
	if err != nil {
		t.Fatalf("TransformHarness returned error: %v", err)
	}

	// Assert: no interactive question asked for "model-a" (pre-answered, question suppressed).
	// RED: service ignores ModelMap and asks one batch-wide question (Subject="").
	for _, q := range spy.captured {
		if q.Subject == "model-a" {
			t.Errorf("QTransformTargetModel asked with Subject=%q despite ModelMap[\"model-a\"]=\"\" "+
				"pre-answering it; a present key with empty value must suppress the question "+
				"(RED: service ignores ModelMap; deliver I7.3)", "model-a")
		}
	}

	// Assert: AppliedModelMap["model-a"] == "" (not TargetModel or any other fallback value).
	// RED: AppliedModelMap is nil.
	if result.AppliedModelMap == nil {
		t.Errorf("result.AppliedModelMap is nil; want the applied mapping recorded "+
			"(RED: deliver I7.4)")
		return
	}

	if got := result.AppliedModelMap["model-a"]; got != "" {
		t.Errorf("AppliedModelMap[\"model-a\"] = %q, want empty string; "+
			"a ModelMap entry with empty value must force the target to \"\" and must not fall "+
			"through to TargetModel=%q "+
			"(RED: deliver I7.4)",
			got, req.TargetModel)
	}
}

// ---------------------------------------------------------------------------
// T7.3: Mixed ModelMap + TargetModel — Rule 1 takes precedence over Rule 2
// ---------------------------------------------------------------------------

// TestTransformHarness_MixedModelMapAndTargetModel_RuleOneOverRuleTwo verifies that when
// ModelMap covers some source models (Rule 1) and TargetModel covers the rest (Rule 2), no
// interactive question is asked for either group and each group receives its correct target.
// This pins the rule-precedence boundary end-to-end:
//   - Rule 1 (ModelMap entry): used when the source model appears as a key in ModelMap.
//   - Rule 2 (TargetModel fallback): used for source models not covered by ModelMap.
//
// Neither existing test covers this interaction: TestTransformHarness_LegacyTargetModel tests
// TargetModel alone (single-model batch), and TestTransformHarness_ModelMap_SuppressesQuestion
// tests partial ModelMap but with no TargetModel fallback set (the unmapped model triggers a
// question). This test covers Rule 1 + Rule 2 simultaneously with no prompts for either group.
func TestTransformHarness_MixedModelMapAndTargetModel_RuleOneOverRuleTwo(t *testing.T) {
	// Arrange: two source models. "model-a" is covered by ModelMap (Rule 1); "model-b" is
	// not in ModelMap so it falls back to TargetModel (Rule 2). Neither should trigger a prompt.
	batchDir := t.TempDir()
	writeTransformFile(t, batchDir, "a-agent.src.md", matchingAgentBytesWithModel("model-a"))
	writeTransformFile(t, batchDir, "b-agent.src.md", matchingAgentBytesWithModel("model-b"))

	spy := newCapturingModelSpy(interactiontest.NewBuilder().Build(), nil)
	deps, _ := newMultiModelTransformDepsWithSpy(t, spy)
	svc := app.New(deps)

	req := newPreAnsweredTransformRequest(batchDir)
	req.TargetModel = "target-fallback" // Rule 2 fallback for source models not in ModelMap
	req.ModelMap = app.TransformModelMapping{
		"model-a": "target-a", // Rule 1: explicit mapping, takes precedence
	}
	req.DryRun = true

	// Act
	result, err := svc.TransformHarness(context.Background(), req)
	if err != nil {
		t.Fatalf("TransformHarness returned error: %v", err)
	}

	// Assert: no interactive questions asked (both groups resolved without prompting).
	// RED: service ignores ModelMap and asks one batch-wide question (Subject="").
	if len(spy.captured) != 0 {
		t.Errorf("QTransformTargetModel asked %d time(s), want 0; "+
			"model-a is resolved via ModelMap (Rule 1) and model-b via TargetModel (Rule 2) — "+
			"neither group should trigger an interactive prompt; "+
			"captured subjects: %v "+
			"(RED: service ignores ModelMap; deliver I7.3)",
			len(spy.captured), spy.capturedSubjects())
	}

	// Assert: AppliedModelMap carries the correct target for each source model.
	// RED: AppliedModelMap is nil.
	if result.AppliedModelMap == nil {
		t.Errorf("result.AppliedModelMap is nil; want the applied mapping recorded "+
			"(RED: deliver I7.4)")
		return
	}

	if got := result.AppliedModelMap["model-a"]; got != "target-a" {
		t.Errorf("AppliedModelMap[\"model-a\"] = %q, want %q; "+
			"ModelMap (Rule 1) must take precedence for mapped source models "+
			"(RED: deliver I7.4)", got, "target-a")
	}

	if got := result.AppliedModelMap["model-b"]; got != "target-fallback" {
		t.Errorf("AppliedModelMap[\"model-b\"] = %q, want %q; "+
			"TargetModel (Rule 2) must provide the fallback for source models not in ModelMap "+
			"(RED: deliver I7.4)", got, "target-fallback")
	}
}

// ---------------------------------------------------------------------------
// T7.3: Existing TargetModel field still works as the batch-wide fallback
// ---------------------------------------------------------------------------

// TestTransformHarness_LegacyTargetModel_SingleModelBatch_BehaviorUnchanged verifies that
// existing callers that set only TargetModel (not ModelMap) still receive the same behavior
// as before: all agents get the single TargetModel value, and result.TargetModel echoes it.
// This guards backward compatibility: the CLI path that passes only --target-model must
// continue to work without modification.
//
// NOT RED-VERIFIED: the existing TargetModel handling is already implemented. The test pins
// that it remains working after the per-source-model machinery is added.
func TestTransformHarness_LegacyTargetModel_SingleModelBatch_BehaviorUnchanged(t *testing.T) {
	// Arrange: pre-answered TargetModel on the request (the existing single-model path).
	stub := interactiontest.NewBuilder().Build()
	deps, _ := newTransformDeps(t, stub)
	svc := app.New(deps)

	dir := t.TempDir()
	agentPath := writeTransformFile(t, dir, "my-agent.src.md", matchingAgentBytes())

	req := newPreAnsweredTransformRequest(agentPath)
	// TargetModel is already set by newPreAnsweredTransformRequest ("target-model-x").
	req.DryRun = true

	// Act
	result, err := svc.TransformHarness(context.Background(), req)
	if err != nil {
		t.Fatalf("TransformHarness returned error: %v", err)
	}

	// Assert: the single pre-answered TargetModel is echoed in result.TargetModel.
	if result.TargetModel != "target-model-x" {
		t.Errorf("result.TargetModel = %q, want %q; legacy TargetModel pre-answer must still work",
			result.TargetModel, "target-model-x")
	}

	// Assert: no model questions asked (TargetModel is pre-answered).
	for _, c := range stub.Calls() {
		if c.ID == domain.QTransformTargetModel {
			t.Errorf("QTransformTargetModel was asked despite TargetModel being pre-answered; "+
				"the TargetModel field must suppress model questions when set")
		}
	}
}
