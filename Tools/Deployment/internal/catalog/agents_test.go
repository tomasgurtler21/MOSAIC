package catalog_test

// Tests for agent enumeration against the real repository tree.
//
// The catalog exposes three distinct agent roles — worker, orchestrator, and utility —
// through three separate methods. Tests here verify:
//   - Correct counts matching the actual repository tree
//   - Role segregation: Agents() contains only workers; Orchestrator() and UtilityAgents()
//     expose the remaining roles
//   - Frontmatter fields are populated: key, version, name, description, recommended tier,
//     tier rationale, tools (or tools placeholder), and required_skills
//   - SourcePath is absolute and points to an existing file
//   - Agent(key) lookup works for any role
//
// All tests load from the real repository root to ensure the catalog correctly handles
// the actual source tree.

import (
	"bytes"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"mosaic-deploy/internal/catalog"
	"mosaic-deploy/internal/domain"
)

// loadRealCatalog calls catalog.Load with the real repository root and fails the test if
// loading fails. It is a shared helper used by all agent, skill, hook, workflow, and tier tests.
func loadRealCatalog(t *testing.T) catalog.Catalog {
	t.Helper()
	root, err := catalog.ResolveRoot(".")
	if err != nil {
		t.Fatalf("ResolveRoot: %v", err)
	}
	cat, err := catalog.Load(root)
	if err != nil {
		t.Fatalf("catalog.Load(%q): %v", root, err)
	}
	return cat
}

// ---------------------------------------------------------------------------
// Worker agents — Agents()
// ---------------------------------------------------------------------------

// TestAgents_ReturnsNonEmptyList verifies that Agents() returns at least one agent.
// This is the minimum sanity check for the repository tree walk.
func TestAgents_ReturnsNonEmptyList(t *testing.T) {
	cat := loadRealCatalog(t)
	agents := cat.Agents()
	if len(agents) == 0 {
		t.Fatal("Agents() returned an empty list; expected at least one worker agent")
	}
}

// TestAgents_Count_Matches35Workers verifies that Agents() returns exactly 35 worker
// agents, matching the known count of generic agent files in the repository (each category
// folder contains one README.md that is not an agent and must be excluded).
func TestAgents_Count_Matches35Workers(t *testing.T) {
	const wantCount = 35
	cat := loadRealCatalog(t)
	agents := cat.Agents()
	if len(agents) != wantCount {
		t.Errorf("Agents() returned %d agents, want %d", len(agents), wantCount)
	}
}

// TestAgents_SortedByKey verifies that Agents() returns agents in ascending key order.
// Deterministic ordering is required so the UI and plan output are stable across runs.
func TestAgents_SortedByKey(t *testing.T) {
	cat := loadRealCatalog(t)
	agents := cat.Agents()
	if len(agents) < 2 {
		t.Skip("fewer than 2 agents returned; cannot check ordering")
	}

	for i := 1; i < len(agents); i++ {
		if agents[i].Key < agents[i-1].Key {
			t.Errorf("Agents() is not sorted by Key at index %d: %q < %q",
				i, agents[i].Key, agents[i-1].Key)
		}
	}
}

// TestAgents_AllHaveWorkerRole verifies that every agent returned by Agents() carries
// RoleWorker. Orchestrator and utility agents must not appear in this list.
func TestAgents_AllHaveWorkerRole(t *testing.T) {
	cat := loadRealCatalog(t)
	for _, a := range cat.Agents() {
		if a.Role != domain.RoleWorker {
			t.Errorf("Agents()[%q].Role = %q, want RoleWorker", a.Key, a.Role)
		}
	}
}

// TestAgents_AllHaveNonEmptyKey verifies that every worker agent has a non-empty key.
func TestAgents_AllHaveNonEmptyKey(t *testing.T) {
	cat := loadRealCatalog(t)
	for _, a := range cat.Agents() {
		if a.Key == "" {
			t.Errorf("agent at SourcePath %q has empty Key", a.SourcePath)
		}
	}
}

// TestAgents_AllHaveVersion verifies that every worker agent has a non-empty version string.
func TestAgents_AllHaveVersion(t *testing.T) {
	cat := loadRealCatalog(t)
	for _, a := range cat.Agents() {
		if a.Version == "" {
			t.Errorf("agent %q has empty Version", a.Key)
		}
	}
}

// TestAgents_AllHaveName verifies that every worker agent has a non-empty name.
func TestAgents_AllHaveName(t *testing.T) {
	cat := loadRealCatalog(t)
	for _, a := range cat.Agents() {
		if a.Name == "" {
			t.Errorf("agent %q has empty Name", a.Key)
		}
	}
}

// TestAgents_AllHaveDescription verifies that every worker agent has a non-empty description.
func TestAgents_AllHaveDescription(t *testing.T) {
	cat := loadRealCatalog(t)
	for _, a := range cat.Agents() {
		if a.Description == "" {
			t.Errorf("agent %q has empty Description", a.Key)
		}
	}
}

// TestAgents_AllHaveRecommendedTier verifies that every worker agent has a non-empty
// recommended tier string. Tier strings are open and not validated against a fixed list.
func TestAgents_AllHaveRecommendedTier(t *testing.T) {
	cat := loadRealCatalog(t)
	for _, a := range cat.Agents() {
		if a.RecommendedTier == "" {
			t.Errorf("agent %q has empty RecommendedTier", a.Key)
		}
	}
}

// TestAgents_AllHaveTierRationale verifies that every worker agent has non-empty tier
// rationale text. The rationale is shown to the user during model selection.
func TestAgents_AllHaveTierRationale(t *testing.T) {
	cat := loadRealCatalog(t)
	for _, a := range cat.Agents() {
		if a.TierRationale == "" {
			t.Errorf("agent %q has empty TierRationale", a.Key)
		}
	}
}

// TestAgents_AllHaveToolsOrPlaceholder verifies that every worker agent has either a
// non-nil Tools slice or a non-empty ToolsPlaceholder, but never both simultaneously.
// Both being set at once is a data integrity error.
func TestAgents_AllHaveToolsOrPlaceholder(t *testing.T) {
	cat := loadRealCatalog(t)
	for _, a := range cat.Agents() {
		hasTools := a.Tools != nil
		hasPlaceholder := a.ToolsPlaceholder != ""

		if !hasTools && !hasPlaceholder {
			t.Errorf("agent %q has neither Tools nor ToolsPlaceholder", a.Key)
		}
		if hasTools && hasPlaceholder {
			t.Errorf("agent %q has both Tools (%v) and ToolsPlaceholder (%q); only one is permitted",
				a.Key, a.Tools, a.ToolsPlaceholder)
		}
	}
}

// TestAgents_AllHaveRequiredSkills verifies that every worker agent has a non-nil
// RequiredSkills slice (may be empty, but must not be nil).
func TestAgents_AllHaveRequiredSkills(t *testing.T) {
	cat := loadRealCatalog(t)
	for _, a := range cat.Agents() {
		if a.RequiredSkills == nil {
			t.Errorf("agent %q has nil RequiredSkills; expected non-nil (possibly empty) slice", a.Key)
		}
	}
}

// TestAgents_AllHaveCategory verifies that every worker agent has a non-empty category
// (the source folder name, e.g. "Execution").
func TestAgents_AllHaveCategory(t *testing.T) {
	cat := loadRealCatalog(t)
	for _, a := range cat.Agents() {
		if a.Category == "" {
			t.Errorf("agent %q has empty Category", a.Key)
		}
	}
}

// TestAgents_AllHaveNumericID verifies that every worker agent has a non-empty NumericID.
// Worker agents carry an integer id in their frontmatter (e.g. "id: 17").
func TestAgents_AllHaveNumericID(t *testing.T) {
	cat := loadRealCatalog(t)
	for _, a := range cat.Agents() {
		if a.NumericID == "" {
			t.Errorf("worker agent %q has empty NumericID", a.Key)
		}
	}
}

// TestAgents_AllHaveAbsoluteSourcePath verifies that SourcePath is absolute and that
// the file actually exists on disk.
func TestAgents_AllHaveAbsoluteSourcePath(t *testing.T) {
	cat := loadRealCatalog(t)
	for _, a := range cat.Agents() {
		if !filepath.IsAbs(a.SourcePath) {
			t.Errorf("agent %q SourcePath is not absolute: %q", a.Key, a.SourcePath)
			continue
		}
		if _, err := os.Stat(a.SourcePath); os.IsNotExist(err) {
			t.Errorf("agent %q SourcePath %q does not exist on disk", a.Key, a.SourcePath)
		}
	}
}

// TestAgents_KnownCategories_AllPresent verifies that the catalog returns at least one
// agent for each of the seven known category folders.
func TestAgents_KnownCategories_AllPresent(t *testing.T) {
	knownCategories := []string{
		"Audit", "Creation", "Execution", "Interface", "Planning", "Research", "Validation",
	}
	cat := loadRealCatalog(t)
	agents := cat.Agents()

	categoriesFound := make(map[string]bool)
	for _, a := range agents {
		categoriesFound[a.Category] = true
	}

	for _, cat := range knownCategories {
		if !categoriesFound[cat] {
			t.Errorf("no agents found for expected category %q (all agent keys: %v)", cat, allAgentKeys(agents))
		}
	}
}

// TestAgent_KnownAgent_TestRunner verifies specific field values for the well-known
// test-runner agent, which is a stable representative worker agent.
func TestAgent_KnownAgent_TestRunner(t *testing.T) {
	cat := loadRealCatalog(t)

	a, ok := cat.Agent("test-runner")
	if !ok {
		t.Fatal("Agent(\"test-runner\") returned not-found; expected this agent to be present")
	}
	if a.Key != "test-runner" {
		t.Errorf("test-runner Key = %q, want %q", a.Key, "test-runner")
	}
	if a.Role != domain.RoleWorker {
		t.Errorf("test-runner Role = %q, want RoleWorker", a.Role)
	}
	if a.Category != "Execution" {
		t.Errorf("test-runner Category = %q, want %q", a.Category, "Execution")
	}
	if a.RecommendedTier == "" {
		t.Error("test-runner RecommendedTier is empty")
	}
	if a.TierRationale == "" {
		t.Error("test-runner TierRationale is empty")
	}
}

// TestAgent_KeysAreUnique verifies that no two agents share the same Key.
// Keys are slugs derived from the file base name and must be unique.
func TestAgent_KeysAreUnique(t *testing.T) {
	cat := loadRealCatalog(t)
	agents := cat.Agents()
	seen := make(map[string]bool, len(agents))
	for _, a := range agents {
		if seen[a.Key] {
			t.Errorf("duplicate agent key: %q", a.Key)
		}
		seen[a.Key] = true
	}
}

// ---------------------------------------------------------------------------
// Orchestrator — Orchestrator()
// ---------------------------------------------------------------------------

// TestOrchestrator_ReturnsOrchestratorRole verifies that Orchestrator() returns an agent
// with RoleOrchestrator.
func TestOrchestrator_ReturnsOrchestratorRole(t *testing.T) {
	cat := loadRealCatalog(t)
	orc := cat.Orchestrator()
	if orc.Role != domain.RoleOrchestrator {
		t.Errorf("Orchestrator().Role = %q, want RoleOrchestrator", orc.Role)
	}
}

// TestOrchestrator_Key_IsOrchestrator verifies that the orchestrator's key is "orchestrator".
func TestOrchestrator_Key_IsOrchestrator(t *testing.T) {
	cat := loadRealCatalog(t)
	orc := cat.Orchestrator()
	if orc.Key != "orchestrator" {
		t.Errorf("Orchestrator().Key = %q, want %q", orc.Key, "orchestrator")
	}
}

// TestOrchestrator_ToolsPlaceholder_IsSet verifies that the orchestrator uses a tools
// placeholder (the orchestrator's tools list is determined at deploy time, not statically).
func TestOrchestrator_ToolsPlaceholder_IsSet(t *testing.T) {
	cat := loadRealCatalog(t)
	orc := cat.Orchestrator()
	if orc.ToolsPlaceholder == "" {
		t.Error("Orchestrator().ToolsPlaceholder is empty; expected a non-empty placeholder")
	}
	if orc.Tools != nil {
		t.Errorf("Orchestrator().Tools is non-nil: %v; orchestrator must use a placeholder", orc.Tools)
	}
}

// TestOrchestrator_Category_IsEmpty verifies that the orchestrator has an empty category.
// The orchestrator is not in any category folder; only worker agents have categories.
func TestOrchestrator_Category_IsEmpty(t *testing.T) {
	cat := loadRealCatalog(t)
	orc := cat.Orchestrator()
	if orc.Category != "" {
		t.Errorf("Orchestrator().Category = %q, want empty string", orc.Category)
	}
}

// TestOrchestrator_NumericID_IsEmpty verifies that the orchestrator does not have a
// numeric id. The frontmatter convention says numeric id is absent from the orchestrator.
func TestOrchestrator_NumericID_IsEmpty(t *testing.T) {
	cat := loadRealCatalog(t)
	orc := cat.Orchestrator()
	if orc.NumericID != "" {
		t.Errorf("Orchestrator().NumericID = %q, want empty string (orchestrator has no numeric id)", orc.NumericID)
	}
}

// TestOrchestrator_LookupByKey_ReturnsOrchestrator verifies that Agent("orchestrator")
// returns the same agent as Orchestrator().
func TestOrchestrator_LookupByKey_ReturnsOrchestrator(t *testing.T) {
	cat := loadRealCatalog(t)
	orc := cat.Orchestrator()

	byKey, ok := cat.Agent("orchestrator")
	if !ok {
		t.Fatal("Agent(\"orchestrator\") returned not-found; orchestrator must be reachable via Agent lookup")
	}
	if byKey.Key != orc.Key || byKey.Role != orc.Role {
		t.Errorf("Agent(\"orchestrator\") returned different agent than Orchestrator():\n  by key: %+v\n  direct: %+v",
			byKey, orc)
	}
}

// TestOrchestrator_NotIncludedInAgents verifies that the orchestrator does not appear
// in the list returned by Agents(), which must contain only worker agents.
func TestOrchestrator_NotIncludedInAgents(t *testing.T) {
	cat := loadRealCatalog(t)
	for _, a := range cat.Agents() {
		if a.Key == "orchestrator" {
			t.Error("orchestrator appeared in Agents() list; it must be excluded (use Orchestrator() instead)")
		}
	}
}

// ---------------------------------------------------------------------------
// Utility agents — UtilityAgents()
// ---------------------------------------------------------------------------

// TestUtilityAgents_Count_Matches6 verifies that UtilityAgents() returns exactly 6
// utility agents, matching the known count in the repository.
func TestUtilityAgents_Count_Matches6(t *testing.T) {
	const wantCount = 6
	cat := loadRealCatalog(t)
	utility := cat.UtilityAgents()
	if len(utility) != wantCount {
		t.Errorf("UtilityAgents() returned %d agents, want %d", len(utility), wantCount)
	}
}

// TestUtilityAgents_AllHaveUtilityRole verifies that every agent returned by
// UtilityAgents() carries RoleUtility.
func TestUtilityAgents_AllHaveUtilityRole(t *testing.T) {
	cat := loadRealCatalog(t)
	for _, a := range cat.UtilityAgents() {
		if a.Role != domain.RoleUtility {
			t.Errorf("UtilityAgents()[%q].Role = %q, want RoleUtility", a.Key, a.Role)
		}
	}
}

// TestUtilityAgents_Category_IsEmpty verifies that utility agents have an empty category.
// Utility agents are not in category folders.
func TestUtilityAgents_Category_IsEmpty(t *testing.T) {
	cat := loadRealCatalog(t)
	for _, a := range cat.UtilityAgents() {
		if a.Category != "" {
			t.Errorf("utility agent %q Category = %q, want empty string", a.Key, a.Category)
		}
	}
}

// TestUtilityAgents_NumericID_IsEmpty verifies that utility agents have no numeric id.
// The frontmatter convention says numeric id is absent from utility agents.
func TestUtilityAgents_NumericID_IsEmpty(t *testing.T) {
	cat := loadRealCatalog(t)
	for _, a := range cat.UtilityAgents() {
		if a.NumericID != "" {
			t.Errorf("utility agent %q NumericID = %q, want empty string", a.Key, a.NumericID)
		}
	}
}

// TestUtilityAgents_NotIncludedInAgents verifies that utility agents do not appear in
// the list returned by Agents().
func TestUtilityAgents_NotIncludedInAgents(t *testing.T) {
	cat := loadRealCatalog(t)
	utilityKeys := make(map[string]bool)
	for _, a := range cat.UtilityAgents() {
		utilityKeys[a.Key] = true
	}
	for _, a := range cat.Agents() {
		if utilityKeys[a.Key] {
			t.Errorf("utility agent %q appeared in Agents() list; it must be excluded", a.Key)
		}
	}
}

// ---------------------------------------------------------------------------
// Lookup — Agent(key)
// ---------------------------------------------------------------------------

// TestAgentLookup_ExistingWorker_ReturnsAgent verifies that Agent(key) returns a
// valid agent and true for a known worker key.
func TestAgentLookup_ExistingWorker_ReturnsAgent(t *testing.T) {
	cat := loadRealCatalog(t)

	a, ok := cat.Agent("test-runner")
	if !ok {
		t.Fatal("Agent(\"test-runner\"): returned not-found")
	}
	if a.Key != "test-runner" {
		t.Errorf("Agent(\"test-runner\").Key = %q", a.Key)
	}
}

// TestAgentLookup_UnknownKey_ReturnsFalse verifies that Agent(key) returns false for
// a key that does not match any agent in any role.
func TestAgentLookup_UnknownKey_ReturnsFalse(t *testing.T) {
	cat := loadRealCatalog(t)

	_, ok := cat.Agent("this-agent-does-not-exist-in-the-repository")
	if ok {
		t.Error("Agent(\"this-agent-does-not-exist-in-the-repository\"): returned true, want false")
	}
}

// TestAgentLookup_AllAgents_FoundByKey verifies that every agent returned by Agents()
// can be looked up by key via Agent(key). This confirms the lookup index is consistent
// with the enumeration.
func TestAgentLookup_AllAgents_FoundByKey(t *testing.T) {
	cat := loadRealCatalog(t)
	for _, a := range cat.Agents() {
		found, ok := cat.Agent(a.Key)
		if !ok {
			t.Errorf("Agent(%q): returned not-found, but key was returned by Agents()", a.Key)
			continue
		}
		if found.Key != a.Key {
			t.Errorf("Agent(%q).Key = %q, want %q", a.Key, found.Key, a.Key)
		}
	}
}

// TestAgents_RequiredSkills_AreKnownSkillKeys verifies that every skill key listed in
// an agent's RequiredSkills refers to a skill present in the catalog.
func TestAgents_RequiredSkills_AreKnownSkillKeys(t *testing.T) {
	cat := loadRealCatalog(t)
	for _, a := range cat.Agents() {
		for _, sk := range a.RequiredSkills {
			if _, ok := cat.Skill(sk); !ok {
				t.Errorf("agent %q has required_skill %q that is not present in the skill catalog", a.Key, sk)
			}
		}
	}
}

// TestAgents_SourcePathHasMdExtension verifies that each agent's SourcePath ends with
// ".md", confirming the scanner correctly identifies markdown files.
func TestAgents_SourcePathHasMdExtension(t *testing.T) {
	cat := loadRealCatalog(t)
	for _, a := range cat.Agents() {
		if ext := strings.ToLower(filepath.Ext(a.SourcePath)); ext != ".md" {
			t.Errorf("agent %q SourcePath %q has unexpected extension %q, want .md",
				a.Key, a.SourcePath, ext)
		}
	}
}

// TestAgents_KeyMatchesFileName verifies that each worker agent's Key matches the file
// base name (without the .md extension) of its SourcePath. The key is the slug; slugs
// derive from file names.
func TestAgents_KeyMatchesFileName(t *testing.T) {
	cat := loadRealCatalog(t)
	for _, a := range cat.Agents() {
		base := strings.TrimSuffix(filepath.Base(a.SourcePath), ".md")
		if base != a.Key {
			t.Errorf("agent Key %q does not match file base name %q from SourcePath %q",
				a.Key, base, a.SourcePath)
		}
	}
}

// ---------------------------------------------------------------------------
// Catalog.Root() — return value correctness
// ---------------------------------------------------------------------------

// TestCatalog_Root_NonEmpty verifies that Root() returns a non-empty string.
// An empty root would be invalid as it cannot be an absolute path.
func TestCatalog_Root_NonEmpty(t *testing.T) {
	cat := loadRealCatalog(t)
	if cat.Root() == "" {
		t.Error("Catalog.Root() returned empty string; expected the absolute MOSAIC repository root")
	}
}

// TestCatalog_Root_IsAbsolute verifies that Root() returns an absolute path.
// Callers rely on this guarantee when constructing paths relative to the root.
func TestCatalog_Root_IsAbsolute(t *testing.T) {
	cat := loadRealCatalog(t)
	root := cat.Root()
	if !filepath.IsAbs(root) {
		t.Errorf("Catalog.Root() returned non-absolute path: %q", root)
	}
}

// TestCatalog_Root_MatchesLoadInput verifies that Root() returns the same path that
// was passed to Load. Callers that resolve the root first and then pass it to Load
// should get back the exact same string.
func TestCatalog_Root_MatchesLoadInput(t *testing.T) {
	resolved, err := catalog.ResolveRoot(".")
	if err != nil {
		t.Fatalf("ResolveRoot: %v", err)
	}
	cat, err := catalog.Load(resolved)
	if err != nil {
		t.Fatalf("catalog.Load(%q): %v", resolved, err)
	}
	if cat.Root() != resolved {
		t.Errorf("Catalog.Root() = %q, want %q (path passed to Load)", cat.Root(), resolved)
	}
}

// ---------------------------------------------------------------------------
// Catalog.ReadSource() — catalogued paths and invented paths
// ---------------------------------------------------------------------------

// TestCatalog_ReadSource_CataloguedPath_ReturnsCorrectBytes verifies that ReadSource
// returns the raw bytes of a file that the catalog emitted (via a SourcePath field).
// The returned bytes must be byte-identical to os.ReadFile of the same path.
func TestCatalog_ReadSource_CataloguedPath_ReturnsCorrectBytes(t *testing.T) {
	cat := loadRealCatalog(t)
	agents := cat.Agents()
	if len(agents) == 0 {
		t.Skip("no agents available for ReadSource test")
	}
	// Use the first agent's SourcePath — it was emitted by the catalog, so it is valid.
	sp := agents[0].SourcePath
	got, err := cat.ReadSource(sp)
	if err != nil {
		t.Fatalf("ReadSource(%q): unexpected error: %v", sp, err)
	}
	want, readErr := os.ReadFile(sp)
	if readErr != nil {
		t.Fatalf("os.ReadFile(%q): %v", sp, readErr)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("ReadSource(%q) returned %d bytes, want %d bytes; byte content differs",
			sp, len(got), len(want))
	}
}

// TestCatalog_ReadSource_InventedPath_ReturnsError verifies that ReadSource returns an
// error for a path that was not emitted by the catalog. The catalog contract states
// "callers must not invent paths"; returning an error for an unrecognised path enforces
// this at runtime and prevents callers from reading arbitrary files via the catalog.
func TestCatalog_ReadSource_InventedPath_ReturnsError(t *testing.T) {
	cat := loadRealCatalog(t)
	// Construct an absolute path that looks plausible but was never emitted by the catalog.
	invented := filepath.Join(cat.Root(), "this-path-was-not-emitted-by-the-catalog.md")
	_, err := cat.ReadSource(invented)
	if err == nil {
		t.Errorf("ReadSource(%q): expected error for invented path, got nil", invented)
	}
}

// allAgentKeys returns all agent keys from the given list, for use in diagnostic messages.
func allAgentKeys(agents []domain.Agent) []string {
	keys := make([]string, len(agents))
	for i, a := range agents {
		keys[i] = a.Key
	}
	sort.Strings(keys)
	return keys
}
