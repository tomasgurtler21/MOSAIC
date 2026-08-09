package catalog_test

// Tests for tier discovery (T7.8).
//
// Tiers are open strings carried in each agent's recommended_tier frontmatter field.
// The catalog collects all distinct tier strings present in source and exposes them via
// Catalog.Tiers(). Key invariants:
//
//   - No normalisation: tier strings are returned exactly as written in the source (e.g.
//     "HIGH" not "high", "MEDIUM-HIGH" not "medium-high").
//   - No deduplication by case: "HIGH" and "High" would be two distinct tiers (though in
//     practice the repository uses only uppercase).
//   - No validation against a fixed list: any tier string present in source is returned.
//   - Each TierInfo carries the rationale text from the first agent that declared this tier.
//   - Each TierInfo carries the sorted list of agent keys that declared this tier.
//
// Classification record: TestTiers_MixedCaseTier_PresentVerbatim and
// TestTiers_MixedCaseTier_NotCaseFolded (previously TestTiers_KnownTiers_PresentVerbatim
// and TestTiers_HighTier_NotLowercased, which named the five real tier strings) exist to
// prove the loader does not lowercase, trim, or otherwise normalise tier strings — that is
// behaviour-under-test (Plan.md's classification table), not format vocabulary:
// Agents/Generic/SourceFilesFormat.md documents recommended_tier as "an open string...
// never validated against a fixed enum", and introduces
// "LOW"/"MEDIUM"/"HIGH"/"LOW-MEDIUM"/"MEDIUM-HIGH" only as "examples in the current
// source", not a closed set. Both tests were moved onto a synthetic fixture built via
// writeAgentWithTier, using a deliberately mixed-case tier value ("MiXeD-Case") so the
// no-normalisation property is proven without depending on which tier strings the live
// repository happens to use today. TestTiers_AllAgentTiersDeclared_InTiersList and
// TestTiers_Count_MatchesDistinctTierValuesInSource below independently cover the same
// terrain against the real repository without naming tier strings, for defence in depth.
// TestTiers_HighTier_HasRationale and TestTiers_MediumTier_HasRationale similarly name
// "HIGH" and "MEDIUM" against loadRealCatalog only to pick a representative entry to check
// for non-empty Rationale; TestTiers_AllHaveRationale above already asserts the same
// property over every tier, so these two are disclosed as redundant, low-risk content
// coupling rather than rewritten (see review follow-up).

import (
	"path/filepath"
	"testing"

	"mosaic-deploy/internal/catalog"
)

// writeAgentWithTier writes a minimal subagent file declaring the given recommended_tier
// and tier_rationale at <root>/Agents/Generic/Agents/<category>/<name>.md. Used to prove
// tier-string handling (verbatim, no normalisation) without depending on which tier
// strings the live repository happens to use.
func writeAgentWithTier(t *testing.T, root, category, name, tier, rationale string) {
	t.Helper()
	mustMkdir(t, root, "Agents", "Generic", "Agents", category)
	fm := "---\n"
	fm += "id: \"1\"\n"
	fm += "version: \"1.0.0\"\n"
	fm += "name: " + name + "\n"
	fm += "description: Synthetic agent for tier-parsing tests.\n"
	fm += "role: subagent\n"
	fm += "recommended_tier: " + tier + "\n"
	fm += "tier_rationale: " + rationale + "\n"
	fm += "---\nContent.\n"
	relPath := filepath.Join("Agents", "Generic", "Agents", category, name+".md")
	mustWriteFile(t, root, relPath, []byte(fm))
}

// ---------------------------------------------------------------------------
// Basic invariants
// ---------------------------------------------------------------------------

// TestTiers_NonEmpty verifies that Tiers() returns at least one tier. The repository
// has agents that declare tier values so this must never be empty.
func TestTiers_NonEmpty(t *testing.T) {
	cat := loadRealCatalog(t)
	tiers := cat.Tiers()
	if len(tiers) == 0 {
		t.Fatal("Tiers() returned an empty list; expected at least one tier from agent frontmatter")
	}
}

// TestTiers_AllHaveNonEmptyTierString verifies that every TierInfo has a non-empty Tier
// string. A zero-value Tier in the result is a data integrity error.
func TestTiers_AllHaveNonEmptyTierString(t *testing.T) {
	cat := loadRealCatalog(t)
	for i, ti := range cat.Tiers() {
		if ti.Tier == "" {
			t.Errorf("Tiers()[%d].Tier is empty; all entries must have non-empty tier strings", i)
		}
	}
}

// TestTiers_AllHaveRationale verifies that every TierInfo has non-empty Rationale text.
// The rationale is displayed to the user during model selection to explain the tier choice.
func TestTiers_AllHaveRationale(t *testing.T) {
	cat := loadRealCatalog(t)
	for _, ti := range cat.Tiers() {
		if ti.Rationale == "" {
			t.Errorf("Tiers() entry for tier %q has empty Rationale; expected explanatory text", ti.Tier)
		}
	}
}

// TestTiers_AllHaveAgentKeys verifies that every TierInfo has at least one agent key.
// A tier that no agent references should not appear in the result.
func TestTiers_AllHaveAgentKeys(t *testing.T) {
	cat := loadRealCatalog(t)
	for _, ti := range cat.Tiers() {
		if len(ti.AgentKeys) == 0 {
			t.Errorf("Tiers() entry for tier %q has no AgentKeys; every tier must have at least one declaring agent", ti.Tier)
		}
	}
}

// TestTiers_NoDuplicateTierStrings verifies that no two TierInfo entries share the same
// Tier string. Each distinct tier value must appear exactly once.
func TestTiers_NoDuplicateTierStrings(t *testing.T) {
	cat := loadRealCatalog(t)
	seen := make(map[string]bool)
	for _, ti := range cat.Tiers() {
		if seen[string(ti.Tier)] {
			t.Errorf("Tiers() contains duplicate tier string %q; each tier must appear exactly once", ti.Tier)
		}
		seen[string(ti.Tier)] = true
	}
}

// ---------------------------------------------------------------------------
// No normalisation
// ---------------------------------------------------------------------------

// TestTiers_MixedCaseTier_PresentVerbatim verifies that a tier string is returned exactly
// as written in agent frontmatter — not lowercased, trimmed, or otherwise normalised —
// using a synthetic mixed-case value that no real tier vocabulary would produce, so the
// property holds independent of which tier strings the live repository happens to use.
func TestTiers_MixedCaseTier_PresentVerbatim(t *testing.T) {
	root := makeTempMosaicRoot(t)
	writeAgentWithTier(t, root, "Infrastructure", "sample-tier-agent", "MiXeD-Case", "Synthetic rationale.")

	cat, err := catalog.Load(root)
	if err != nil {
		t.Fatalf("catalog.Load: %v", err)
	}

	found := false
	for _, ti := range cat.Tiers() {
		if string(ti.Tier) == "MiXeD-Case" {
			found = true
		}
	}
	if !found {
		t.Error("Tiers() does not contain \"MiXeD-Case\" verbatim (check that tier strings are returned without normalisation)")
	}
}

// TestTiers_MixedCaseTier_NotCaseFolded verifies specifically that a mixed-case tier
// string appears in its original casing and does NOT also appear fully lowercased or
// fully uppercased, confirming no case-folding occurs.
func TestTiers_MixedCaseTier_NotCaseFolded(t *testing.T) {
	root := makeTempMosaicRoot(t)
	writeAgentWithTier(t, root, "Infrastructure", "sample-tier-agent", "MiXeD-Case", "Synthetic rationale.")

	cat, err := catalog.Load(root)
	if err != nil {
		t.Fatalf("catalog.Load: %v", err)
	}

	foundOriginal := false
	foundLowercase := false
	foundUppercase := false
	for _, ti := range cat.Tiers() {
		switch string(ti.Tier) {
		case "MiXeD-Case":
			foundOriginal = true
		case "mixed-case":
			foundLowercase = true
		case "MIXED-CASE":
			foundUppercase = true
		}
	}
	if !foundOriginal {
		t.Error("Tiers() does not contain \"MiXeD-Case\"; expected it verbatim from agent frontmatter")
	}
	if foundLowercase {
		t.Error("Tiers() contains lowercased \"mixed-case\"; tier strings must not be normalised")
	}
	if foundUppercase {
		t.Error("Tiers() contains uppercased \"MIXED-CASE\"; tier strings must not be normalised")
	}
}

// ---------------------------------------------------------------------------
// Rationale text
// ---------------------------------------------------------------------------

// TestTiers_HighTier_HasRationale verifies that the HIGH tier has rationale text
// carried from at least one of the agents declaring it.
func TestTiers_HighTier_HasRationale(t *testing.T) {
	cat := loadRealCatalog(t)
	for _, ti := range cat.Tiers() {
		if string(ti.Tier) == "HIGH" {
			if ti.Rationale == "" {
				t.Error("HIGH tier has empty Rationale; expected text from agent tier_rationale frontmatter")
			}
			return
		}
	}
	t.Skip("HIGH tier not found in Tiers() (unexpected; skipping rationale check)")
}

// TestTiers_MediumTier_HasRationale verifies that the MEDIUM tier has rationale text.
func TestTiers_MediumTier_HasRationale(t *testing.T) {
	cat := loadRealCatalog(t)
	for _, ti := range cat.Tiers() {
		if string(ti.Tier) == "MEDIUM" {
			if ti.Rationale == "" {
				t.Error("MEDIUM tier has empty Rationale; expected text from agent tier_rationale frontmatter")
			}
			return
		}
	}
	t.Skip("MEDIUM tier not found in Tiers() (unexpected; skipping rationale check)")
}

// ---------------------------------------------------------------------------
// Agent keys
// ---------------------------------------------------------------------------

// TestTiers_AgentKeys_AreSorted verifies that the AgentKeys list in each TierInfo is
// sorted in ascending order. Sorted keys produce stable output.
func TestTiers_AgentKeys_AreSorted(t *testing.T) {
	cat := loadRealCatalog(t)
	for _, ti := range cat.Tiers() {
		for i := 1; i < len(ti.AgentKeys); i++ {
			if ti.AgentKeys[i] < ti.AgentKeys[i-1] {
				t.Errorf("TierInfo for %q: AgentKeys not sorted at index %d: %q < %q",
					ti.Tier, i, ti.AgentKeys[i], ti.AgentKeys[i-1])
			}
		}
	}
}

// TestTiers_AgentKeys_AreKnownAgents verifies that every key in TierInfo.AgentKeys refers
// to an agent present in the catalog. An unknown key is a data integrity error.
func TestTiers_AgentKeys_AreKnownAgents(t *testing.T) {
	cat := loadRealCatalog(t)
	for _, ti := range cat.Tiers() {
		for _, key := range ti.AgentKeys {
			if _, ok := cat.Agent(key); !ok {
				t.Errorf("TierInfo for %q has agent key %q that is not present in the catalog", ti.Tier, key)
			}
		}
	}
}

// TestTiers_OrchestratorDeclaredTier_Included verifies that the orchestrator's tier is
// present in Tiers(). The orchestrator declares a recommended_tier and must be counted.
func TestTiers_OrchestratorDeclaredTier_Included(t *testing.T) {
	cat := loadRealCatalog(t)
	orc := cat.Orchestrator()
	if orc.RecommendedTier == "" {
		t.Skip("Orchestrator has no RecommendedTier; skipping tier inclusion check")
	}

	for _, ti := range cat.Tiers() {
		if ti.Tier == orc.RecommendedTier {
			return // found
		}
	}
	t.Errorf("orchestrator's tier %q is not present in Tiers(); it must be included", orc.RecommendedTier)
}

// TestTiers_AllAgentTiersDeclared_InTiersList verifies that for each worker agent that
// declares a recommended_tier, the tier string appears in Tiers(). This ensures the
// catalog collects tiers from the complete agent set.
func TestTiers_AllAgentTiersDeclared_InTiersList(t *testing.T) {
	cat := loadRealCatalog(t)
	tierMap := make(map[string]bool)
	for _, ti := range cat.Tiers() {
		tierMap[string(ti.Tier)] = true
	}

	for _, a := range cat.Agents() {
		if a.RecommendedTier == "" {
			continue // some agents might legitimately have no tier (unusual)
		}
		if !tierMap[string(a.RecommendedTier)] {
			t.Errorf("agent %q declares tier %q, but that tier is not present in Tiers()",
				a.Key, a.RecommendedTier)
		}
	}
}

// TestTiers_Count_MatchesDistinctTierValuesInSource verifies that the number of TierInfo
// entries equals the count of distinct tier strings in source. The catalog must not
// omit any tier and must not create entries for tiers no agent references.
func TestTiers_Count_MatchesDistinctTierValuesInSource(t *testing.T) {
	cat := loadRealCatalog(t)

	// Collect distinct tiers from agents directly.
	distinctTiers := make(map[string]bool)
	for _, a := range cat.Agents() {
		if a.RecommendedTier != "" {
			distinctTiers[string(a.RecommendedTier)] = true
		}
	}
	// Include the orchestrator.
	if orc := cat.Orchestrator(); orc.RecommendedTier != "" {
		distinctTiers[string(orc.RecommendedTier)] = true
	}
	// Include utility agents.
	for _, a := range cat.UtilityAgents() {
		if a.RecommendedTier != "" {
			distinctTiers[string(a.RecommendedTier)] = true
		}
	}

	wantCount := len(distinctTiers)
	gotCount := len(cat.Tiers())

	if gotCount != wantCount {
		var got []string
		for _, ti := range cat.Tiers() {
			got = append(got, string(ti.Tier))
		}
		var want []string
		for tier := range distinctTiers {
			want = append(want, tier)
		}
		t.Errorf("Tiers() returned %d entries, want %d distinct tiers from source;\n  got:  %v\n  want: %v",
			gotCount, wantCount, got, want)
	}
}
