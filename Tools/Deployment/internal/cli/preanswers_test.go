package cli_test

// preanswers_test.go verifies two related behaviours:
//
// Builder — PreAnswersFromSelectionsFile (T6.1):
//   A selections YAML file is converted into a PreAnswers value that encodes each
//   section using the correct QuestionID, subject, and answer-string format expected
//   by nonInteractive (NewInteraction).
//
//   Correct encoding rules:
//   - Workflows → QWorkflows, subject "", comma-joined IDs
//   - UtilityAgents → QUtilityAgents, subject "", comma-joined IDs
//   - Hooks → QHooks, subject "", comma-joined IDs
//   - TierModels → QTierModel, subject = tier name (e.g. "HIGH"), answer = model ID
//   - Empty / absent sections produce no entry in PreAnswers.Values
//   - A missing file returns an error (no panic)
//   - Invalid YAML content returns an error (no panic)
//
// Round-trip resolution (T6.2):
//   A PreAnswers value built from a selections file drives nonInteractive to resolve
//   the corresponding questions as Answered rather than recording a gap. The tests
//   cover workflows, utility agents, hooks, and tier models. They also verify that
//   questions absent from the selections file still produce SkippedOne + a recorded
//   gap, confirming that only the explicitly supplied answers are wired through.

import (
	"bytes"
	"context"
	"testing"

	"mosaic-deploy/internal/cli"
	"mosaic-deploy/internal/domain"
)

// ---------------------------------------------------------------------------
// T6.1 — Builder: workflow encoding
// ---------------------------------------------------------------------------

// TestPreAnswersFromSelectionsFile_MultipleWorkflows_CommaJoinedRunLevel verifies that
// two or more workflow IDs in the selections file are encoded as a single comma-joined
// run-level answer (subject "") for QWorkflows.
func TestPreAnswersFromSelectionsFile_MultipleWorkflows_CommaJoinedRunLevel(t *testing.T) {
	// Arrange
	path := writeTempYAML(t, "workflows:\n  - quick-fix\n  - build\n")

	// Act
	pa, err := cli.PreAnswersFromSelectionsFile(path)

	// Assert
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got, ok := pa.Values[domain.QWorkflows][""]
	if !ok {
		t.Fatal("QWorkflows entry missing from PreAnswers.Values; selections workflows must be encoded under QWorkflows subject \"\"")
	}
	const want = "quick-fix,build"
	if got != want {
		t.Errorf("QWorkflows answer = %q, want %q; multiple workflow IDs must be comma-joined", got, want)
	}
}

// TestPreAnswersFromSelectionsFile_SingleWorkflow_NoBoundingComma verifies that a single
// workflow ID is encoded without a surrounding comma (no trailing or leading comma).
func TestPreAnswersFromSelectionsFile_SingleWorkflow_NoBoundingComma(t *testing.T) {
	// Arrange
	path := writeTempYAML(t, "workflows:\n  - quick-fix\n")

	// Act
	pa, err := cli.PreAnswersFromSelectionsFile(path)

	// Assert
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got, ok := pa.Values[domain.QWorkflows][""]
	if !ok {
		t.Fatal("QWorkflows entry missing from PreAnswers.Values")
	}
	const want = "quick-fix"
	if got != want {
		t.Errorf("QWorkflows answer = %q, want %q; a single ID must not be padded with commas", got, want)
	}
}

// ---------------------------------------------------------------------------
// T6.1 — Builder: utility-agent encoding
// ---------------------------------------------------------------------------

// TestPreAnswersFromSelectionsFile_UtilityAgents_CommaJoinedRunLevel verifies that
// utility-agent keys from utility_agents are encoded as a comma-joined run-level answer
// for QUtilityAgents.
func TestPreAnswersFromSelectionsFile_UtilityAgents_CommaJoinedRunLevel(t *testing.T) {
	// Arrange
	path := writeTempYAML(t, "utility_agents:\n  - agent-a\n  - agent-b\n")

	// Act
	pa, err := cli.PreAnswersFromSelectionsFile(path)

	// Assert
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got, ok := pa.Values[domain.QUtilityAgents][""]
	if !ok {
		t.Fatal("QUtilityAgents entry missing from PreAnswers.Values; utility_agents must be encoded under QUtilityAgents subject \"\"")
	}
	const want = "agent-a,agent-b"
	if got != want {
		t.Errorf("QUtilityAgents answer = %q, want %q", got, want)
	}
}

// ---------------------------------------------------------------------------
// T6.1 — Builder: hook encoding
// ---------------------------------------------------------------------------

// TestPreAnswersFromSelectionsFile_Hooks_CommaJoinedRunLevel verifies that hook keys
// from hooks are encoded as a comma-joined run-level answer for QHooks.
func TestPreAnswersFromSelectionsFile_Hooks_CommaJoinedRunLevel(t *testing.T) {
	// Arrange
	path := writeTempYAML(t, "hooks:\n  - hook-a\n  - hook-b\n")

	// Act
	pa, err := cli.PreAnswersFromSelectionsFile(path)

	// Assert
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got, ok := pa.Values[domain.QHooks][""]
	if !ok {
		t.Fatal("QHooks entry missing from PreAnswers.Values; hooks must be encoded under QHooks subject \"\"")
	}
	const want = "hook-a,hook-b"
	if got != want {
		t.Errorf("QHooks answer = %q, want %q", got, want)
	}
}

// ---------------------------------------------------------------------------
// T6.1 — Builder: tier-model encoding
// ---------------------------------------------------------------------------

// TestPreAnswersFromSelectionsFile_TierModels_SubjectKeyedPerTier verifies that each
// tier_models entry produces a separate QTierModel answer keyed by the tier name as the
// subject, not by the empty run-level subject "".
func TestPreAnswersFromSelectionsFile_TierModels_SubjectKeyedPerTier(t *testing.T) {
	// Arrange
	path := writeTempYAML(t, "tier_models:\n  HIGH: claude-opus-4\n  LOW: claude-haiku-3\n")

	// Act
	pa, err := cli.PreAnswersFromSelectionsFile(path)

	// Assert
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	tierMap, ok := pa.Values[domain.QTierModel]
	if !ok {
		t.Fatal("QTierModel entry missing from PreAnswers.Values; tier_models must be encoded under QTierModel")
	}
	if got := tierMap["HIGH"]; got != "claude-opus-4" {
		t.Errorf("QTierModel[HIGH] = %q, want %q; tier name must be the subject, not the empty run-level key", got, "claude-opus-4")
	}
	if got := tierMap["LOW"]; got != "claude-haiku-3" {
		t.Errorf("QTierModel[LOW] = %q, want %q", got, "claude-haiku-3")
	}
}

// TestPreAnswersFromSelectionsFile_TierModels_RunLevelSubjectNotUsed verifies that
// tier-model answers are NOT stored under the empty run-level subject "". Using the empty
// subject would cause all tiers to resolve to the same model.
func TestPreAnswersFromSelectionsFile_TierModels_RunLevelSubjectNotUsed(t *testing.T) {
	// Arrange
	path := writeTempYAML(t, "tier_models:\n  HIGH: claude-opus-4\n")

	// Act
	pa, err := cli.PreAnswersFromSelectionsFile(path)

	// Assert
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tierMap, ok := pa.Values[domain.QTierModel]; ok {
		if _, runLevel := tierMap[""]; runLevel {
			t.Error("QTierModel has run-level subject \"\"; tier-model answers must use the tier name as the subject, never \"\"")
		}
	}
}

// ---------------------------------------------------------------------------
// T6.1 — Builder: all sections combined in one file
// ---------------------------------------------------------------------------

// TestPreAnswersFromSelectionsFile_AllSections_AllEntriesPresent verifies that a file
// supplying all four sections produces a PreAnswers with all four QuestionID entries.
func TestPreAnswersFromSelectionsFile_AllSections_AllEntriesPresent(t *testing.T) {
	// Arrange
	yaml := "" +
		"workflows:\n  - quick-fix\n" +
		"utility_agents:\n  - agent-a\n" +
		"hooks:\n  - hook-b\n" +
		"tier_models:\n  HIGH: claude-opus-4\n"
	path := writeTempYAML(t, yaml)

	// Act
	pa, err := cli.PreAnswersFromSelectionsFile(path)

	// Assert
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, qid := range []domain.QuestionID{domain.QWorkflows, domain.QUtilityAgents, domain.QHooks, domain.QTierModel} {
		if _, ok := pa.Values[qid]; !ok {
			t.Errorf("PreAnswers.Values[%q] missing; all four sections were present in the file", qid)
		}
	}
}

// ---------------------------------------------------------------------------
// T6.1 — Builder: empty / absent sections
// ---------------------------------------------------------------------------

// ---------------------------------------------------------------------------
// T1.2 — Builder: infrastructure_agents empty-list handling
// ---------------------------------------------------------------------------

// TestPreAnswersFromSelectionsFile_EmptyInfrastructureAgents_ProducesEntry verifies that
// an explicit `infrastructure_agents: []` in the selections file produces a pre-answer
// entry for QInfrastructureAgents with an empty value (""). This distinguishes "deploy
// none" from "not specified" (absent key), preventing the deploy tool from asking an
// interactive question when the caller explicitly chose no infrastructure agents.
//
// RED: the current guard is `len(sf.InfrastructureAgents) > 0`, which skips the empty
// list. The fix changes it to `sf.InfrastructureAgents != nil`.
func TestPreAnswersFromSelectionsFile_EmptyInfrastructureAgents_ProducesEntry(t *testing.T) {
	// Arrange
	path := writeTempYAML(t, "infrastructure_agents: []\n")

	// Act
	pa, err := cli.PreAnswersFromSelectionsFile(path)

	// Assert
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	innerMap, ok := pa.Values[domain.QInfrastructureAgents]
	if !ok {
		t.Fatal("QInfrastructureAgents entry missing from PreAnswers.Values; " +
			"an explicit empty list must produce a pre-answer entry so the deploy tool " +
			"knows not to ask an interactive question (\"deploy none\" vs \"not specified\")")
	}
	got, ok := innerMap[""]
	if !ok {
		t.Fatal("QInfrastructureAgents run-level subject \"\" missing from entry map")
	}
	if got != "" {
		t.Errorf("QInfrastructureAgents answer = %q, want empty string; "+
			"strings.Join([]string{}, \",\") must produce the empty string", got)
	}
}

// TestPreAnswersFromSelectionsFile_AbsentInfrastructureAgents_NoEntry verifies that when
// the infrastructure_agents key is absent from the selections file, no entry is added to
// PreAnswers.Values for QInfrastructureAgents. This preserves the "ask interactively"
// behavior for callers that did not supply an explicit selection.
func TestPreAnswersFromSelectionsFile_AbsentInfrastructureAgents_NoEntry(t *testing.T) {
	// Arrange — file with no infrastructure_agents key at all
	path := writeTempYAML(t, "{}\n")

	// Act
	pa, err := cli.PreAnswersFromSelectionsFile(path)

	// Assert
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pa.Values != nil {
		if _, ok := pa.Values[domain.QInfrastructureAgents]; ok {
			t.Error("QInfrastructureAgents entry present for absent key; " +
				"an absent infrastructure_agents key must not produce a pre-answer entry " +
				"so the deploy tool can ask its interactive question")
		}
	}
}

// TestPreAnswersFromSelectionsFile_PopulatedInfrastructureAgents_CommaJoinedEntry verifies
// that a non-empty infrastructure_agents list produces a pre-answer entry with the IDs
// comma-joined, consistent with the encoding used by utility_agents and hooks.
func TestPreAnswersFromSelectionsFile_PopulatedInfrastructureAgents_CommaJoinedEntry(t *testing.T) {
	// Arrange
	path := writeTempYAML(t, "infrastructure_agents:\n  - checkpoint-manager-git\n  - commit-manager-git\n")

	// Act
	pa, err := cli.PreAnswersFromSelectionsFile(path)

	// Assert
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	innerMap, ok := pa.Values[domain.QInfrastructureAgents]
	if !ok {
		t.Fatal("QInfrastructureAgents entry missing from PreAnswers.Values")
	}
	const want = "checkpoint-manager-git,commit-manager-git"
	if got := innerMap[""]; got != want {
		t.Errorf("QInfrastructureAgents answer = %q, want %q; "+
			"multiple IDs must be comma-joined", got, want)
	}
}

// ---------------------------------------------------------------------------
// T1.2 — Builder: utility_agents empty-list handling
// ---------------------------------------------------------------------------

// TestPreAnswersFromSelectionsFile_EmptyUtilityAgents_ProducesEntry verifies that an
// explicit `utility_agents: []` produces a pre-answer entry for QUtilityAgents with an
// empty value, preventing the deploy tool from asking an interactive question.
//
// RED: the current guard is `len(sf.UtilityAgents) > 0`; the fix changes it to
// `sf.UtilityAgents != nil`.
func TestPreAnswersFromSelectionsFile_EmptyUtilityAgents_ProducesEntry(t *testing.T) {
	// Arrange
	path := writeTempYAML(t, "utility_agents: []\n")

	// Act
	pa, err := cli.PreAnswersFromSelectionsFile(path)

	// Assert
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	innerMap, ok := pa.Values[domain.QUtilityAgents]
	if !ok {
		t.Fatal("QUtilityAgents entry missing from PreAnswers.Values; " +
			"an explicit empty list must produce a pre-answer entry (\"deploy none\") " +
			"so the deploy tool does not ask an interactive question")
	}
	if got := innerMap[""]; got != "" {
		t.Errorf("QUtilityAgents answer = %q, want empty string", got)
	}
}

// TestPreAnswersFromSelectionsFile_AbsentUtilityAgents_NoEntry verifies that an absent
// utility_agents key produces no QUtilityAgents entry in PreAnswers.
func TestPreAnswersFromSelectionsFile_AbsentUtilityAgents_NoEntry(t *testing.T) {
	// Arrange
	path := writeTempYAML(t, "{}\n")

	// Act
	pa, err := cli.PreAnswersFromSelectionsFile(path)

	// Assert
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pa.Values != nil {
		if _, ok := pa.Values[domain.QUtilityAgents]; ok {
			t.Error("QUtilityAgents entry present for absent key; " +
				"an absent utility_agents key must not produce a pre-answer entry")
		}
	}
}

// ---------------------------------------------------------------------------
// T1.2 — Builder: hooks empty-list handling
// ---------------------------------------------------------------------------

// TestPreAnswersFromSelectionsFile_EmptyHooks_ProducesEntry verifies that an explicit
// `hooks: []` produces a pre-answer entry for QHooks with an empty value, preventing
// the deploy tool from asking an interactive question.
//
// RED: the current guard is `len(sf.Hooks) > 0`; the fix changes it to
// `sf.Hooks != nil`.
func TestPreAnswersFromSelectionsFile_EmptyHooks_ProducesEntry(t *testing.T) {
	// Arrange
	path := writeTempYAML(t, "hooks: []\n")

	// Act
	pa, err := cli.PreAnswersFromSelectionsFile(path)

	// Assert
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	innerMap, ok := pa.Values[domain.QHooks]
	if !ok {
		t.Fatal("QHooks entry missing from PreAnswers.Values; " +
			"an explicit empty list must produce a pre-answer entry (\"deploy none\") " +
			"so the deploy tool does not ask an interactive question")
	}
	if got := innerMap[""]; got != "" {
		t.Errorf("QHooks answer = %q, want empty string", got)
	}
}

// TestPreAnswersFromSelectionsFile_AbsentHooks_NoEntry verifies that an absent hooks
// key produces no QHooks entry in PreAnswers.
func TestPreAnswersFromSelectionsFile_AbsentHooks_NoEntry(t *testing.T) {
	// Arrange
	path := writeTempYAML(t, "{}\n")

	// Act
	pa, err := cli.PreAnswersFromSelectionsFile(path)

	// Assert
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pa.Values != nil {
		if _, ok := pa.Values[domain.QHooks]; ok {
			t.Error("QHooks entry present for absent key; " +
				"an absent hooks key must not produce a pre-answer entry")
		}
	}
}

// ---------------------------------------------------------------------------
// Existing tests below (workflows empty / absent sections / error cases)
// ---------------------------------------------------------------------------

// TestPreAnswersFromSelectionsFile_EmptyWorkflows_NoQWorkflowsEntry verifies that an
// empty workflows list does not produce a QWorkflows entry. Supplying an empty list as a
// pre-answer would encode as "" which nonInteractive resolves as "no IDs selected",
// recording a gap — the same outcome as having no pre-answer at all; so it is simpler and
// more predictable not to produce the entry.
func TestPreAnswersFromSelectionsFile_EmptyWorkflows_NoQWorkflowsEntry(t *testing.T) {
	// Arrange
	path := writeTempYAML(t, "workflows: []\n")

	// Act
	pa, err := cli.PreAnswersFromSelectionsFile(path)

	// Assert
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pa.Values != nil {
		if _, ok := pa.Values[domain.QWorkflows]; ok {
			t.Error("QWorkflows entry present for empty workflows list; an empty list must not produce a pre-answer entry")
		}
	}
}

// TestPreAnswersFromSelectionsFile_AbsentSections_EmptyPreAnswers verifies that a file
// with no selections fields produces a PreAnswers with an empty (or nil) Values map.
func TestPreAnswersFromSelectionsFile_AbsentSections_EmptyPreAnswers(t *testing.T) {
	// Arrange — file with no recognised fields at all
	path := writeTempYAML(t, "{}\n")

	// Act
	pa, err := cli.PreAnswersFromSelectionsFile(path)

	// Assert
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(pa.Values) != 0 {
		t.Errorf("Values map has %d entries, want 0; an empty file must produce empty PreAnswers", len(pa.Values))
	}
}

// ---------------------------------------------------------------------------
// T6.1 — Builder: error cases
// ---------------------------------------------------------------------------

// TestPreAnswersFromSelectionsFile_MissingFile_ReturnsError verifies that attempting to
// load a path that does not exist returns a non-nil error without panicking. The caller
// is responsible for mapping the error to the appropriate exit code.
func TestPreAnswersFromSelectionsFile_MissingFile_ReturnsError(t *testing.T) {
	// Arrange — a path that is guaranteed not to exist
	path := t.TempDir() + "/nonexistent-selections.yaml"

	// Act
	_, err := cli.PreAnswersFromSelectionsFile(path)

	// Assert
	if err == nil {
		t.Error("expected an error for a non-existent file, got nil; must not silently succeed on a missing selections file")
	}
}

// TestPreAnswersFromSelectionsFile_InvalidYAML_ReturnsError verifies that a file
// containing syntactically invalid YAML returns a non-nil error without panicking.
func TestPreAnswersFromSelectionsFile_InvalidYAML_ReturnsError(t *testing.T) {
	// Arrange — deliberately broken YAML (mapping key collision)
	path := writeTempYAML(t, ":\n  bad: [unclosed\n")

	// Act
	_, err := cli.PreAnswersFromSelectionsFile(path)

	// Assert
	if err == nil {
		t.Error("expected an error for invalid YAML content, got nil; must not silently succeed on unparseable YAML")
	}
}

// ---------------------------------------------------------------------------
// T6.2 — Round-trip: workflows resolve as Answered, not Skipped
// ---------------------------------------------------------------------------

// TestPreAnswers_Roundtrip_Workflows_ResolvesAnswered verifies the end-to-end path from
// selections file → PreAnswers → nonInteractive: a SelectMany call for QWorkflows returns
// Answered with the correct option IDs and records no gap.
func TestPreAnswers_Roundtrip_Workflows_ResolvesAnswered(t *testing.T) {
	// Arrange
	path := writeTempYAML(t, "workflows:\n  - quick-fix\n  - build\n")
	pa, err := cli.PreAnswersFromSelectionsFile(path)
	if err != nil {
		t.Fatalf("PreAnswersFromSelectionsFile error: %v", err)
	}
	todos := &stubTodo{}
	ix := cli.NewInteraction(pa, todos, &bytes.Buffer{})

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
		t.Errorf("Status = %q, want %q (Answered); workflows from the selections file must resolve as answered, not skipped", ans.Status, domain.Answered)
	}
	want := []string{"quick-fix", "build"}
	if !equalStringSlice(ans.OptionIDs, want) {
		t.Errorf("OptionIDs = %v, want %v; must return exactly the IDs from the selections file", ans.OptionIDs, want)
	}
	if !todos.Empty() {
		t.Error("gap recorded for an answered workflows question; an answered question must not produce a gap")
	}
}

// ---------------------------------------------------------------------------
// T6.2 — Round-trip: utility agents resolve as Answered, not Skipped
// ---------------------------------------------------------------------------

// TestPreAnswers_Roundtrip_UtilityAgents_ResolvesAnswered verifies that a SelectMany
// call for QUtilityAgents returns Answered when the selections file supplied the agents.
func TestPreAnswers_Roundtrip_UtilityAgents_ResolvesAnswered(t *testing.T) {
	// Arrange
	path := writeTempYAML(t, "utility_agents:\n  - agent-a\n")
	pa, err := cli.PreAnswersFromSelectionsFile(path)
	if err != nil {
		t.Fatalf("PreAnswersFromSelectionsFile error: %v", err)
	}
	todos := &stubTodo{}
	ix := cli.NewInteraction(pa, todos, &bytes.Buffer{})

	q := domain.ChoiceQuestion{
		Question: domain.Question{ID: domain.QUtilityAgents, Subject: ""},
		Options:  []domain.Option{{ID: "agent-a"}, {ID: "agent-b"}},
	}

	// Act
	ans, err := ix.SelectMany(context.Background(), q)

	// Assert
	if err != nil {
		t.Fatalf("SelectMany returned error: %v", err)
	}
	if ans.Status != domain.Answered {
		t.Errorf("Status = %q, want %q (Answered); utility agents from the selections file must resolve as answered", ans.Status, domain.Answered)
	}
	if !todos.Empty() {
		t.Error("gap recorded for an answered utility-agents question; an answered question must not produce a gap")
	}
}

// ---------------------------------------------------------------------------
// T6.2 — Round-trip: hooks resolve as Answered, not Skipped
// ---------------------------------------------------------------------------

// TestPreAnswers_Roundtrip_Hooks_ResolvesAnswered verifies that a SelectMany call for
// QHooks returns Answered when the selections file supplied the hooks.
func TestPreAnswers_Roundtrip_Hooks_ResolvesAnswered(t *testing.T) {
	// Arrange
	path := writeTempYAML(t, "hooks:\n  - hook-a\n  - hook-b\n")
	pa, err := cli.PreAnswersFromSelectionsFile(path)
	if err != nil {
		t.Fatalf("PreAnswersFromSelectionsFile error: %v", err)
	}
	todos := &stubTodo{}
	ix := cli.NewInteraction(pa, todos, &bytes.Buffer{})

	q := domain.ChoiceQuestion{
		Question: domain.Question{ID: domain.QHooks, Subject: ""},
		Options:  []domain.Option{{ID: "hook-a"}, {ID: "hook-b"}, {ID: "hook-c"}},
	}

	// Act
	ans, err := ix.SelectMany(context.Background(), q)

	// Assert
	if err != nil {
		t.Fatalf("SelectMany returned error: %v", err)
	}
	if ans.Status != domain.Answered {
		t.Errorf("Status = %q, want %q (Answered); hooks from the selections file must resolve as answered", ans.Status, domain.Answered)
	}
	want := []string{"hook-a", "hook-b"}
	if !equalStringSlice(ans.OptionIDs, want) {
		t.Errorf("OptionIDs = %v, want %v", ans.OptionIDs, want)
	}
	if !todos.Empty() {
		t.Error("gap recorded for an answered hooks question; an answered question must not produce a gap")
	}
}

// ---------------------------------------------------------------------------
// T6.2 — Round-trip: tier models resolve as Answered per-tier, not Skipped
// ---------------------------------------------------------------------------

// TestPreAnswers_Roundtrip_TierModel_ResolvesAnsweredPerTier verifies that SelectOne for
// QTierModel returns Answered using the tier name as the subject, matching the selections
// file's tier_models map.
func TestPreAnswers_Roundtrip_TierModel_ResolvesAnswered(t *testing.T) {
	// Arrange
	path := writeTempYAML(t, "tier_models:\n  HIGH: claude-opus-4\n  LOW: claude-haiku-3\n")
	pa, err := cli.PreAnswersFromSelectionsFile(path)
	if err != nil {
		t.Fatalf("PreAnswersFromSelectionsFile error: %v", err)
	}
	todos := &stubTodo{}
	ix := cli.NewInteraction(pa, todos, &bytes.Buffer{})

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
		t.Fatalf("SelectOne errors: HIGH=%v LOW=%v", errHigh, errLow)
	}
	if ansHigh.Status != domain.Answered {
		t.Errorf("HIGH Status = %q, want %q (Answered); tier-model for HIGH must resolve from selections file pre-answer", ansHigh.Status, domain.Answered)
	}
	if ansHigh.OptionID != "claude-opus-4" {
		t.Errorf("HIGH OptionID = %q, want %q", ansHigh.OptionID, "claude-opus-4")
	}
	if ansLow.Status != domain.Answered {
		t.Errorf("LOW Status = %q, want %q (Answered); tier-model for LOW must resolve from selections file pre-answer", ansLow.Status, domain.Answered)
	}
	if ansLow.OptionID != "claude-haiku-3" {
		t.Errorf("LOW OptionID = %q, want %q", ansLow.OptionID, "claude-haiku-3")
	}
	if !todos.Empty() {
		t.Error("gap recorded for answered tier-model questions; answered questions must not produce a gap")
	}
}

// ---------------------------------------------------------------------------
// T6.2 — Round-trip: questions absent from the selections file still skip
// ---------------------------------------------------------------------------

// TestPreAnswers_Roundtrip_AbsentSection_StillSkipsAndRecordsGap verifies that a question
// whose section was not present in the selections file still resolves to SkippedOne and
// records a gap. This confirms the pre-answers path is additive: it only answers
// explicitly-supplied questions and does not silently swallow unresolved ones.
func TestPreAnswers_Roundtrip_AbsentSection_StillSkipsAndRecordsGap(t *testing.T) {
	// Arrange — only workflows in the selections file; utility agents are absent
	path := writeTempYAML(t, "workflows:\n  - quick-fix\n")
	pa, err := cli.PreAnswersFromSelectionsFile(path)
	if err != nil {
		t.Fatalf("PreAnswersFromSelectionsFile error: %v", err)
	}
	todos := &stubTodo{}
	ix := cli.NewInteraction(pa, todos, &bytes.Buffer{})

	q := domain.ChoiceQuestion{
		Question: domain.Question{ID: domain.QUtilityAgents, Subject: ""},
		Options:  []domain.Option{{ID: "agent-a"}},
	}

	// Act
	ans, err := ix.SelectMany(context.Background(), q)

	// Assert
	if err != nil {
		t.Fatalf("SelectMany returned error: %v", err)
	}
	if ans.Status != domain.SkippedOne {
		t.Errorf("Status = %q, want %q (SkippedOne); a question absent from the selections file must still skip", ans.Status, domain.SkippedOne)
	}
	if todos.Empty() {
		t.Error("no gap recorded for an unresolvable question; absent sections must produce a gap entry so the user can address them")
	}
}
