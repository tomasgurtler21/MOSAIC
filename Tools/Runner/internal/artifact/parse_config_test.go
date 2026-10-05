package artifact_test

// Tests for artifact.Parse: run configuration frontmatter fields (runner_mode,
// runner_pre_consultation, runner_manual_resolution and their legacy aliases,
// commits, commit_branch and the derived commit branch variant,
// review_loop_limit, infrastructure_selections) and preservation of unknown
// top-level keys. Covers defaults when keys are absent, explicit valid values,
// and refusals on bad values.

import (
	"os"
	"reflect"
	"strings"
	"testing"

	"mosaic-run/internal/artifact"
	"mosaic-run/internal/domain"
)

// parseConfig parses a minimal artifact carrying the given config lines.
func parseConfig(t *testing.T, configLines string) domain.ArtifactState {
	t.Helper()
	state, err := artifact.Parse(minimalArtifactWithConfigBytes(configLines))
	if err != nil {
		t.Fatalf("Parse: unexpected error: %v", err)
	}
	return state
}

// ---- Parse: defaults when configuration keys are absent ----

func TestParse_AbsentRunnerKeys_DefaultToUnsetAndDisabled(t *testing.T) {
	state := parseConfig(t, standardConfigLines)

	if state.Mode != domain.ExecutionModeUnset {
		t.Errorf("Mode: want unset, got %q", state.Mode)
	}
	if state.PreConsultation {
		t.Error("PreConsultation: want false when key absent")
	}
	if state.ManualResolution {
		t.Error("ManualResolution: want false when key absent")
	}
}

func TestParse_AbsentCommitsKey_DefaultsToFalse(t *testing.T) {
	state := parseConfig(t, "checkpoints: disabled\n")

	if state.Commits {
		t.Error("Commits: want false (default when key absent), got true")
	}
}

func TestParse_AbsentCommitBranchKey_DefaultsToEmpty(t *testing.T) {
	state := parseConfig(t, standardConfigLines)

	if state.CommitBranch != "" {
		t.Errorf("CommitBranch: want empty, got %q", state.CommitBranch)
	}
}

func TestParse_AbsentReviewLoopLimit_MeansNoLimit(t *testing.T) {
	state := parseConfig(t, standardConfigLines)

	if state.ReviewLoopLimit != 0 {
		t.Errorf("ReviewLoopLimit: want 0 (no limit) when key absent, got %d", state.ReviewLoopLimit)
	}
}

func TestParse_AbsentInfrastructureSelections_MeansNoSelections(t *testing.T) {
	state := parseConfig(t, standardConfigLines)

	if len(state.InfraClassSelections) != 0 {
		t.Errorf("InfraClassSelections: want none when key absent, got %v", state.InfraClassSelections)
	}
}

// ---- Parse: runner_* keys ----

func TestParse_RunnerMode_ParsedCorrectly(t *testing.T) {
	for _, mode := range []domain.ExecutionMode{
		domain.ExecutionModeOrchestrated,
		domain.ExecutionModeAuto,
		domain.ExecutionModeAutoReview,
	} {
		t.Run(string(mode), func(t *testing.T) {
			state := parseConfig(t, standardConfigLines+
				"runner_mode: "+string(mode)+"\n"+
				"runner_pre_consultation: disabled\n"+
				"runner_manual_resolution: disabled\n")

			if state.Mode != mode {
				t.Errorf("Mode: want %q, got %q", mode, state.Mode)
			}
		})
	}
}

func TestParse_RunnerPreConsultationAndManualResolution_Enabled(t *testing.T) {
	state := parseConfig(t, standardConfigLines+
		"runner_mode: auto\n"+
		"runner_pre_consultation: enabled\n"+
		"runner_manual_resolution: enabled\n")

	if !state.PreConsultation {
		t.Error("PreConsultation: want true (runner_pre_consultation: enabled)")
	}
	if !state.ManualResolution {
		t.Error("ManualResolution: want true (runner_manual_resolution: enabled)")
	}
}

func TestParse_CommitsEnabled_ParsedCorrectly(t *testing.T) {
	state := parseConfig(t, "checkpoints: disabled\ncommits: enabled\n")

	if !state.Commits {
		t.Error("Commits: want true (commits: enabled), got false")
	}
}

func TestParse_UnknownRunnerModeValue_RefusesNamingValue(t *testing.T) {
	_, err := artifact.Parse(minimalArtifactWithConfigBytes("runner_mode: quick\n" + standardConfigLines))

	if err == nil {
		t.Fatal("Parse with unknown runner_mode value: want error, got nil")
	}
	asRefusalError(t, err)
	if !strings.Contains(err.Error(), "quick") {
		t.Errorf("error must name the offending value %q, got: %q", "quick", err.Error())
	}
}

// ---- Parse: legacy aliases (read-only migration) ----

func TestParse_LegacyAliases_ReadIntoRunnerSettings(t *testing.T) {
	state := parseConfig(t, standardConfigLines+
		"mode: auto-review\n"+
		"pre_consultation: enabled\n"+
		"manual_resolution: enabled\n")

	if state.Mode != domain.ExecutionModeAutoReview {
		t.Errorf("Mode from legacy mode: want auto-review, got %q", state.Mode)
	}
	if !state.PreConsultation {
		t.Error("PreConsultation from legacy pre_consultation: want true")
	}
	if !state.ManualResolution {
		t.Error("ManualResolution from legacy manual_resolution: want true")
	}
}

func TestParse_LegacyAliases_AreNotPreservedAsUnknownKeys(t *testing.T) {
	state := parseConfig(t, standardConfigLines+
		"mode: auto\n"+
		"pre_consultation: disabled\n"+
		"manual_resolution: disabled\n"+
		"commit_branch_variant: user-own\n")

	for _, e := range state.UnknownFrontmatter {
		t.Errorf("legacy key %q must be consumed, not preserved as unknown", e.Key)
	}
}

func TestParse_PrefixedKeyWinsOverLegacyAlias(t *testing.T) {
	state := parseConfig(t, standardConfigLines+
		"runner_mode: auto\n"+
		"mode: orchestrated\n"+
		"runner_pre_consultation: enabled\n"+
		"pre_consultation: disabled\n"+
		"runner_manual_resolution: enabled\n"+
		"manual_resolution: disabled\n")

	if state.Mode != domain.ExecutionModeAuto {
		t.Errorf("Mode: want prefixed value auto, got %q", state.Mode)
	}
	if !state.PreConsultation || !state.ManualResolution {
		t.Errorf("PreConsultation/ManualResolution: want prefixed values (true, true), got (%v, %v)",
			state.PreConsultation, state.ManualResolution)
	}
	if len(state.UnknownFrontmatter) != 0 {
		t.Errorf("dropped alias keys must not appear as unknown keys, got %v", state.UnknownFrontmatter)
	}
}

func TestParse_LegacyAliasFixture_ReadsRunnerSettings(t *testing.T) {
	data, err := os.ReadFile(fixturePath("legacy-aliases.md"))
	if err != nil {
		t.Fatalf("reading fixture: %v", err)
	}

	state, err := artifact.Parse(data)

	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if state.Mode != domain.ExecutionModeAutoReview || !state.PreConsultation || !state.ManualResolution {
		t.Errorf("legacy fixture settings: got mode=%q pre=%v manual=%v", state.Mode, state.PreConsultation, state.ManualResolution)
	}
}

func TestParse_UnknownLegacyModeValue_RefusesNamingValue(t *testing.T) {
	_, err := artifact.Parse(minimalArtifactWithConfigBytes("mode: quick\n" + standardConfigLines))

	if err == nil {
		t.Fatal("Parse with unknown legacy mode value: want error, got nil")
	}
	asRefusalError(t, err)
	if !strings.Contains(err.Error(), "quick") {
		t.Errorf("error must name the offending value %q, got: %q", "quick", err.Error())
	}
}

// ---- Parse: commit_branch and the derived variant ----

func TestParse_CommitBranchEqualToRunBranch_DerivesMOSAICOwned(t *testing.T) {
	state := parseConfig(t, "checkpoints: disabled\ncommits: enabled\ncommit_branch: mosaic/run/"+testRunID+"\n")

	if state.CommitBranchVariant != domain.CommitBranchMOSAICOwned {
		t.Errorf("CommitBranchVariant: want %q, got %q", domain.CommitBranchMOSAICOwned, state.CommitBranchVariant)
	}
	if state.CommitBranch != "mosaic/run/"+testRunID {
		t.Errorf("CommitBranch: got %q", state.CommitBranch)
	}
}

func TestParse_CommitBranchOtherThanRunBranch_DerivesUserOwn(t *testing.T) {
	for _, branch := range []string{"main", "feature/login", "mosaic/run/20990101T000000Z-ffff"} {
		t.Run(branch, func(t *testing.T) {
			state := parseConfig(t, "checkpoints: disabled\ncommits: enabled\ncommit_branch: "+branch+"\n")

			if state.CommitBranchVariant != domain.CommitBranchUserOwn {
				t.Errorf("CommitBranchVariant for %q: want %q, got %q", branch, domain.CommitBranchUserOwn, state.CommitBranchVariant)
			}
		})
	}
}

func TestParse_AbsentCommitBranch_LeavesVariantEmpty(t *testing.T) {
	for name, lines := range map[string]string{
		"commits disabled": standardConfigLines,
		"commits enabled":  "checkpoints: disabled\ncommits: enabled\n",
	} {
		t.Run(name, func(t *testing.T) {
			state := parseConfig(t, lines)

			if state.CommitBranchVariant != "" {
				t.Errorf("CommitBranchVariant: want empty when commit_branch absent, got %q", state.CommitBranchVariant)
			}
		})
	}
}

func TestParse_LegacyCommitBranchVariantKey_IgnoredInFavourOfDerivation(t *testing.T) {
	// A legacy artifact that still carries commit_branch_variant: the key is
	// consumed and the variant comes from commit_branch, even when they disagree.
	state := parseConfig(t, "checkpoints: disabled\ncommits: enabled\n"+
		"commit_branch_variant: user-own\n"+
		"commit_branch: mosaic/run/"+testRunID+"\n")

	if state.CommitBranchVariant != domain.CommitBranchMOSAICOwned {
		t.Errorf("CommitBranchVariant: want derived %q, got %q", domain.CommitBranchMOSAICOwned, state.CommitBranchVariant)
	}
	if len(state.UnknownFrontmatter) != 0 {
		t.Errorf("commit_branch_variant must be consumed, got unknown keys %v", state.UnknownFrontmatter)
	}
}

func TestDeriveCommitBranchVariant(t *testing.T) {
	cases := []struct {
		name   string
		branch string
		want   domain.CommitBranchVariant
	}{
		{"run branch", "mosaic/run/" + testRunID, domain.CommitBranchMOSAICOwned},
		{"other run branch", "mosaic/run/20990101T000000Z-ffff", domain.CommitBranchUserOwn},
		{"user branch", "feature/x", domain.CommitBranchUserOwn},
		{"empty", "", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := domain.DeriveCommitBranchVariant(c.branch, testRunID); got != c.want {
				t.Errorf("DeriveCommitBranchVariant(%q): want %q, got %q", c.branch, c.want, got)
			}
		})
	}
}

// ---- Parse: review_loop_limit ----

func TestParse_ReviewLoopLimit_PositiveInteger_Parsed(t *testing.T) {
	state := parseConfig(t, standardConfigLines+"review_loop_limit: 3\n")

	if state.ReviewLoopLimit != 3 {
		t.Errorf("ReviewLoopLimit: want 3, got %d", state.ReviewLoopLimit)
	}
}

func TestParse_ReviewLoopLimit_InvalidValue_Refused(t *testing.T) {
	for _, bad := range []string{"0", "-1", "abc", "2.5", "\"\"", "null"} {
		t.Run(bad, func(t *testing.T) {
			_, err := artifact.Parse(minimalArtifactWithConfigBytes(standardConfigLines + "review_loop_limit: " + bad + "\n"))

			if err == nil {
				t.Fatalf("Parse with review_loop_limit %s: want refusal, got nil", bad)
			}
			asRefusalError(t, err)
		})
	}
}

// ---- Parse: infrastructure_selections ----

func TestParse_InfrastructureSelections_BlockMapping(t *testing.T) {
	state := parseConfig(t, standardConfigLines+
		"infrastructure_selections:\n"+
		"  checkpoint: checkpoint-manager-git\n"+
		"  commit: commit-manager-git\n")

	want := map[string]string{"checkpoint": "checkpoint-manager-git", "commit": "commit-manager-git"}
	if !reflect.DeepEqual(state.InfraClassSelections, want) {
		t.Errorf("InfraClassSelections: want %v, got %v", want, state.InfraClassSelections)
	}
}

func TestParse_InfrastructureSelections_FlowMapping(t *testing.T) {
	state := parseConfig(t, standardConfigLines+
		"infrastructure_selections: {checkpoint: checkpoint-manager-git, restore: restore-manager-git}\n")

	want := map[string]string{"checkpoint": "checkpoint-manager-git", "restore": "restore-manager-git"}
	if !reflect.DeepEqual(state.InfraClassSelections, want) {
		t.Errorf("InfraClassSelections: want %v, got %v", want, state.InfraClassSelections)
	}
}

func TestParse_InfrastructureSelections_NotAGatedClass_Refused(t *testing.T) {
	_, err := artifact.Parse(minimalArtifactWithConfigBytes(standardConfigLines +
		"infrastructure_selections:\n  review: orchestration-review\n"))

	if err == nil {
		t.Fatal("Parse with non-gated class key: want refusal, got nil")
	}
	asRefusalError(t, err)
	if !strings.Contains(err.Error(), "review") {
		t.Errorf("error must name the offending class %q, got: %q", "review", err.Error())
	}
}

func TestParse_InfrastructureSelections_EmptyValue_Refused(t *testing.T) {
	_, err := artifact.Parse(minimalArtifactWithConfigBytes(standardConfigLines +
		"infrastructure_selections:\n  checkpoint:\n"))

	if err == nil {
		t.Fatal("Parse with empty selection value: want refusal, got nil")
	}
	asRefusalError(t, err)
}

func TestParse_InfrastructureSelections_NotPreservedAsUnknownKey(t *testing.T) {
	state := parseConfig(t, standardConfigLines+
		"review_loop_limit: 2\n"+
		"infrastructure_selections:\n  commit: commit-manager-git\n")

	if len(state.UnknownFrontmatter) != 0 {
		t.Errorf("modelled keys must not appear as unknown, got %v", state.UnknownFrontmatter)
	}
}

// ---- Parse: unknown top-level keys ----

func TestParse_UnknownKeys_CollectedInFileOrderVerbatim(t *testing.T) {
	state := parseConfig(t, standardConfigLines+
		"zeta_note: hello world\n"+
		"native_block:\n"+
		"  a: 1\n"+
		"  b:\n"+
		"    - x\n"+
		"    - y\n"+
		"alpha: 2\n")

	want := []domain.FrontmatterEntry{
		{Key: "zeta_note", Lines: []string{"zeta_note: hello world"}},
		{Key: "native_block", Lines: []string{"native_block:", "  a: 1", "  b:", "    - x", "    - y"}},
		{Key: "alpha", Lines: []string{"alpha: 2"}},
	}
	if !reflect.DeepEqual(state.UnknownFrontmatter, want) {
		t.Errorf("UnknownFrontmatter:\n want %#v\n  got %#v", want, state.UnknownFrontmatter)
	}
}

func TestParse_NoUnknownKeys_UnknownFrontmatterEmpty(t *testing.T) {
	state := parseConfig(t, standardConfigLines)

	if len(state.UnknownFrontmatter) != 0 {
		t.Errorf("UnknownFrontmatter: want empty, got %v", state.UnknownFrontmatter)
	}
}

// ---- Parse: refusals on bad configuration values ----

func TestParse_MalformedCheckpointsValue_ReturnsRefusalError(t *testing.T) {
	// A checkpoints value that is neither "enabled" nor "disabled" is refused;
	// typos like "enbaled" must not silently disable checkpoints.
	data, err := os.ReadFile(fixturePath("malformed-checkpoints.md"))
	if err != nil {
		t.Fatalf("reading fixture: %v", err)
	}

	_, err = artifact.Parse(data)

	if err == nil {
		t.Fatal("Parse with malformed checkpoints value: want error, got nil")
	}
	asRefusalError(t, err)
}

// ---- Parse: runner settings are all present or all absent ----

func TestParse_PartialRunnerSettings_Refused(t *testing.T) {
	cases := map[string]string{
		"only runner_mode":                    "runner_mode: auto\n",
		"only runner_pre_consultation":        "runner_pre_consultation: enabled\n",
		"only runner_manual_resolution":       "runner_manual_resolution: enabled\n",
		"mode and pre_consultation":           "runner_mode: auto\nrunner_pre_consultation: enabled\n",
		"pre_consultation and manual":         "runner_pre_consultation: enabled\nrunner_manual_resolution: disabled\n",
		"only legacy mode alias":              "mode: auto\n",
		"legacy alias plus one prefixed peer": "mode: auto\nrunner_pre_consultation: enabled\n",
	}
	for name, runnerLines := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := artifact.Parse(minimalArtifactWithConfigBytes(standardConfigLines + runnerLines))

			if err == nil {
				t.Fatalf("Parse with partial runner settings %q: want refusal, got nil", runnerLines)
			}
			asRefusalError(t, err)
		})
	}
}

func TestParse_AllRunnerSettingsAbsent_NotRefused(t *testing.T) {
	if _, err := artifact.Parse(minimalArtifactWithConfigBytes(standardConfigLines)); err != nil {
		t.Fatalf("Parse with no runner settings: want no error, got %v", err)
	}
}

func TestParse_AllRunnerSettingsViaMixedAliasAndPrefixed_NotRefused(t *testing.T) {
	// After alias resolution all three settings are present, so the set is complete.
	_, err := artifact.Parse(minimalArtifactWithConfigBytes(standardConfigLines +
		"mode: auto\n" +
		"runner_pre_consultation: enabled\n" +
		"manual_resolution: disabled\n"))

	if err != nil {
		t.Fatalf("Parse with a complete set resolved from mixed aliases: want no error, got %v", err)
	}
}
