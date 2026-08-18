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
//
// Classification record (every loadRealCatalog-based assertion in this file):
//   - Content-coupled, rewritten to structural invariants: the worker-ID allowlist (was
//     TestAgents_KnownWorkerIDs_AllPresent) and the known-category allowlist (was
//     TestAgents_KnownCategories_AllPresent) are replaced by
//     TestAgents_EveryOnDiskCategoryYieldsAtLeastOneAgent, which walks the real category
//     directories and asserts every one produces at least one Agents() entry with a
//     matching Category, without naming any category or agent.
//   - Content-coupled, rewritten to a structural invariant: the utility-agent exact count
//     (was TestUtilityAgents_Count_Matches6, whose name disagreed with its own literal —
//     a symptom of the coupling, not preserved) is replaced by
//     TestUtilityAgents_ReturnsNonEmptyList.
//   - Content-coupled, removed as redundant: TestAgent_KnownAgent_TestRunner spot-checked
//     field population (Role, Category, RecommendedTier, TierRationale) on one named agent.
//     Every field it checked is already asserted generically, over every agent, by the
//     TestAgents_AllHave* tests below; naming "test-runner" added no additional detection
//     power once those generic tests exist.
//   - Content-coupled, removed as redundant: TestAgentLookup_ExistingWorker_ReturnsAgent
//     spot-checked Agent(key) lookup against the hardcoded key "test-runner". The identical
//     property — Agent(key) finds every key that Agents() returns — is already asserted
//     generically, over every real agent, by TestAgentLookup_AllAgents_FoundByKey directly
//     below; naming "test-runner" added no additional detection power.
//   - Behaviour-under-test, moved onto a synthetic fixture: the
//     TestInfrastructureAgent_* family verifies frontmatter parsing (infrastructure class,
//     trigger list, null trigger_param normalisation, on_failure policy), not the presence
//     of any particular agent. It now builds its own agent files under t.TempDir() via
//     writeInfrastructureAgentFile instead of reading checkpoint-manager-git,
//     commit-manager-git, orchestration-review, and checkpoint-restore-git from the real
//     repository. All original assertions are preserved.
//   - Everything else in this file (existence, sortedness, non-empty-field, lookup-by-key,
//     path, and role-segregation checks) asserts a property that holds over whatever the
//     catalog returns, never a specific agent or count, and needed no change.

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mosaic-deploy/internal/catalog"
	"mosaic-deploy/internal/domain"
)

// loadRealCatalog calls catalog.Load with the real repository root and fails the test if
// loading fails. It is a shared helper used by all agent, skill, hook, workflow, and tier tests.
func loadRealCatalog(t *testing.T) catalog.Catalog {
	t.Helper()
	root, err := catalog.ResolveRoot(repoRoot())
	if err != nil {
		t.Fatalf("ResolveRoot: %v", err)
	}
	cat, err := catalog.Load(root, "")
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

// TestAgents_EveryOnDiskCategoryYieldsAtLeastOneAgent verifies that every category
// directory present under Catalog/Subagents/ in the real repository tree produces at
// least one agent in Agents() with a matching Category, and that Agents() contains no
// category not backed by an on-disk directory. This is a structural invariant: it detects
// a whole category (or every agent within one) silently disappearing from the catalog
// without naming any specific agent, workflow, or category, so it survives content moves
// like relocating a whole category out of the scanned tree.
func TestAgents_EveryOnDiskCategoryYieldsAtLeastOneAgent(t *testing.T) {
	cat := loadRealCatalog(t)
	agentsDir := filepath.Join(cat.CatalogRoot(), "Subagents")

	entries, err := os.ReadDir(agentsDir)
	if err != nil {
		t.Fatalf("os.ReadDir(%q): %v", agentsDir, err)
	}
	onDiskCategories := make(map[string]bool)
	for _, e := range entries {
		if e.IsDir() {
			onDiskCategories[e.Name()] = true
		}
	}
	if len(onDiskCategories) == 0 {
		t.Fatal("no category directories found under Subagents/; cannot verify the invariant")
	}

	agents := cat.Agents()
	categoriesInCatalog := make(map[string]bool)
	for _, a := range agents {
		categoriesInCatalog[a.Category] = true
	}

	for category := range onDiskCategories {
		if !categoriesInCatalog[category] {
			t.Errorf("category directory %q exists on disk but no agent in Agents() has that Category; "+
				"the catalog may have lost an entire category", category)
		}
	}
	for category := range categoriesInCatalog {
		if !onDiskCategories[category] {
			t.Errorf("Agents() contains Category %q with no matching on-disk directory under %q", category, agentsDir)
		}
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

// TestAgents_AllHaveSubagentRole verifies that every agent returned by Agents() carries
// RoleSubagent. Orchestrator and utility agents must not appear in this list.
func TestAgents_AllHaveSubagentRole(t *testing.T) {
	cat := loadRealCatalog(t)
	for _, a := range cat.Agents() {
		if a.Role != domain.RoleSubagent {
			t.Errorf("Agents()[%q].Role = %q, want RoleSubagent", a.Key, a.Role)
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

// TestUtilityAgents_ReturnsNonEmptyList verifies that UtilityAgents() returns at least
// one utility agent. The repository is expected to always ship at least one; an exact
// count is content-coupled and is asserted nowhere in this suite.
func TestUtilityAgents_ReturnsNonEmptyList(t *testing.T) {
	cat := loadRealCatalog(t)
	utility := cat.UtilityAgents()
	if len(utility) == 0 {
		t.Fatal("UtilityAgents() returned an empty list; expected at least one utility agent")
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
	resolved, err := catalog.ResolveRoot(repoRoot())
	if err != nil {
		t.Fatalf("ResolveRoot: %v", err)
	}
	cat, err := catalog.Load(resolved, "")
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


// ---------------------------------------------------------------------------
// Infrastructure agent frontmatter parsing — T3.1
// ---------------------------------------------------------------------------
//
// Tests below verify that parseAgentFile correctly populates the Infrastructure,
// Triggers, and OnFailure fields on domain.Agent. This is behaviour-under-test: the
// assertions exist to prove parsing and null-normalisation logic, not to confirm that any
// particular agent is present. Each test builds its own synthetic agent file under
// t.TempDir() via writeInfrastructureAgentFile rather than reading a named agent from the
// real repository, so the suite is unaffected by any real infrastructure agent being
// renamed, moved, or removed. All assertions from the original repository-backed tests
// are preserved.

// writeInfrastructureAgentFile writes a minimal subagent file carrying infrastructure-agent
// frontmatter (infrastructure, triggers, on_failure) at
// <root>/Catalog/Subagents/<category>/<name>.md. A trigger with an empty TriggerParam
// is written as `trigger_param: null`, matching the real frontmatter convention that the
// parser must normalise to an empty string.
func writeInfrastructureAgentFile(t *testing.T, root, category, name, infrastructure, onFailure string, triggers []domain.InfrastructureTrigger) {
	t.Helper()
	mustMkdir(t, root, "Catalog", "Subagents", category)
	fm := "---\n"
	fm += "id: \"1\"\n"
	fm += "version: \"1.0.0\"\n"
	fm += "name: " + name + "\n"
	fm += "description: Synthetic infrastructure agent for parser tests.\n"
	fm += "role: subagent\n"
	fm += "infrastructure: " + infrastructure + "\n"
	fm += "triggers:\n"
	for _, trig := range triggers {
		fm += "  - trigger: " + trig.Trigger + "\n"
		if trig.TriggerParam == "" {
			fm += "    trigger_param: null\n"
		} else {
			fm += "    trigger_param: \"" + trig.TriggerParam + "\"\n"
		}
	}
	fm += "on_failure: " + onFailure + "\n"
	fm += "---\nContent.\n"
	relPath := filepath.Join("Catalog", "Subagents", category, name+".md")
	mustWriteFile(t, root, relPath, []byte(fm))
}

// TestInfrastructureAgent_TwoTriggers_InfrastructureAndFirstTriggerParsed verifies that a
// synthetic agent declaring two triggers (the first with a null trigger_param) parses its
// infrastructure class correctly and normalises the first trigger's null TriggerParam to
// an empty string.
func TestInfrastructureAgent_TwoTriggers_InfrastructureAndFirstTriggerParsed(t *testing.T) {
	root := makeTempMosaicRoot(t)
	writeInfrastructureAgentFile(t, root, "Infrastructure", "sample-checkpoint-agent", "checkpoint", "halt",
		[]domain.InfrastructureTrigger{
			{Trigger: "STAGE_END", TriggerParam: ""},
			{Trigger: "INVOCATION_INTERVAL", TriggerParam: "10"},
		})

	cat, err := catalog.Load(root, "")
	if err != nil {
		t.Fatalf("catalog.Load: %v", err)
	}
	a, ok := cat.Agent("sample-checkpoint-agent")
	if !ok {
		t.Fatal("Agent(\"sample-checkpoint-agent\") not found in catalog")
	}
	if a.Infrastructure != "checkpoint" {
		t.Errorf("Infrastructure = %q, want %q", a.Infrastructure, "checkpoint")
	}
	if len(a.Triggers) != 2 {
		t.Fatalf("has %d triggers, want 2; triggers: %+v", len(a.Triggers), a.Triggers)
	}
	if a.Triggers[0].Trigger != "STAGE_END" {
		t.Errorf("Triggers[0].Trigger = %q, want %q", a.Triggers[0].Trigger, "STAGE_END")
	}
	if a.Triggers[0].TriggerParam != "" {
		t.Errorf("Triggers[0].TriggerParam = %q, want empty string (null trigger_param)", a.Triggers[0].TriggerParam)
	}
}

// TestInfrastructureAgent_TwoTriggers_SecondTriggerAndOnFailureParsed verifies that the
// second trigger entry of the same two-trigger fixture parses its non-null TriggerParam
// verbatim, and that on_failure is parsed as "halt".
func TestInfrastructureAgent_TwoTriggers_SecondTriggerAndOnFailureParsed(t *testing.T) {
	root := makeTempMosaicRoot(t)
	writeInfrastructureAgentFile(t, root, "Infrastructure", "sample-checkpoint-agent", "checkpoint", "halt",
		[]domain.InfrastructureTrigger{
			{Trigger: "STAGE_END", TriggerParam: ""},
			{Trigger: "INVOCATION_INTERVAL", TriggerParam: "10"},
		})

	cat, err := catalog.Load(root, "")
	if err != nil {
		t.Fatalf("catalog.Load: %v", err)
	}
	a, ok := cat.Agent("sample-checkpoint-agent")
	if !ok {
		t.Fatal("Agent(\"sample-checkpoint-agent\") not found in catalog")
	}
	if len(a.Triggers) < 2 {
		t.Fatal("fewer than 2 triggers parsed; cannot check second trigger")
	}
	if a.Triggers[1].Trigger != "INVOCATION_INTERVAL" {
		t.Errorf("Triggers[1].Trigger = %q, want %q", a.Triggers[1].Trigger, "INVOCATION_INTERVAL")
	}
	if a.Triggers[1].TriggerParam != "10" {
		t.Errorf("Triggers[1].TriggerParam = %q, want %q", a.Triggers[1].TriggerParam, "10")
	}
	if a.OnFailure != "halt" {
		t.Errorf("OnFailure = %q, want %q", a.OnFailure, "halt")
	}
}

// TestInfrastructureAgent_SingleNullTrigger_NormalisesToEmptyString verifies that a
// synthetic agent declaring a single trigger with `trigger_param: null` has its
// TriggerParam normalised to an empty string — not the literal string "null".
func TestInfrastructureAgent_SingleNullTrigger_NormalisesToEmptyString(t *testing.T) {
	root := makeTempMosaicRoot(t)
	writeInfrastructureAgentFile(t, root, "Infrastructure", "sample-commit-agent", "commit", "halt",
		[]domain.InfrastructureTrigger{{Trigger: "STAGE_END", TriggerParam: ""}})

	cat, err := catalog.Load(root, "")
	if err != nil {
		t.Fatalf("catalog.Load: %v", err)
	}
	a, ok := cat.Agent("sample-commit-agent")
	if !ok {
		t.Fatal("Agent(\"sample-commit-agent\") not found in catalog")
	}
	if len(a.Triggers) != 1 {
		t.Fatalf("has %d triggers, want 1; triggers: %+v", len(a.Triggers), a.Triggers)
	}
	if a.Triggers[0].TriggerParam != "" {
		t.Errorf("Triggers[0].TriggerParam = %q, want empty string (null → empty)", a.Triggers[0].TriggerParam)
	}
}

// TestInfrastructureAgent_SingleParamTrigger_AllFieldsParsed verifies infrastructure
// class, a single trigger with a non-null TriggerParam, and on_failure "continue" all
// parse correctly together on one synthetic agent.
func TestInfrastructureAgent_SingleParamTrigger_AllFieldsParsed(t *testing.T) {
	root := makeTempMosaicRoot(t)
	writeInfrastructureAgentFile(t, root, "Infrastructure", "sample-review-agent", "review", "continue",
		[]domain.InfrastructureTrigger{{Trigger: "INVOCATION_INTERVAL", TriggerParam: "30"}})

	cat, err := catalog.Load(root, "")
	if err != nil {
		t.Fatalf("catalog.Load: %v", err)
	}
	a, ok := cat.Agent("sample-review-agent")
	if !ok {
		t.Fatal("Agent(\"sample-review-agent\") not found in catalog")
	}
	if a.Infrastructure != "review" {
		t.Errorf("Infrastructure = %q, want %q", a.Infrastructure, "review")
	}
	if a.OnFailure != "continue" {
		t.Errorf("OnFailure = %q, want %q", a.OnFailure, "continue")
	}
	if len(a.Triggers) != 1 {
		t.Fatalf("has %d triggers, want 1", len(a.Triggers))
	}
	if a.Triggers[0].Trigger != "INVOCATION_INTERVAL" {
		t.Errorf("Triggers[0].Trigger = %q, want %q", a.Triggers[0].Trigger, "INVOCATION_INTERVAL")
	}
	if a.Triggers[0].TriggerParam != "30" {
		t.Errorf("Triggers[0].TriggerParam = %q, want %q", a.Triggers[0].TriggerParam, "30")
	}
}

// TestInfrastructureAgent_ManualTriggerNullParam_RestoreClassParsed verifies that a
// synthetic "restore"-class agent with a single MANUAL trigger (null trigger_param) and
// on_failure "halt" parses all three fields correctly.
func TestInfrastructureAgent_ManualTriggerNullParam_RestoreClassParsed(t *testing.T) {
	root := makeTempMosaicRoot(t)
	writeInfrastructureAgentFile(t, root, "Infrastructure", "sample-restore-agent", "restore", "halt",
		[]domain.InfrastructureTrigger{{Trigger: "MANUAL", TriggerParam: ""}})

	cat, err := catalog.Load(root, "")
	if err != nil {
		t.Fatalf("catalog.Load: %v", err)
	}
	a, ok := cat.Agent("sample-restore-agent")
	if !ok {
		t.Fatal("Agent(\"sample-restore-agent\") not found in catalog")
	}
	if a.Infrastructure != "restore" {
		t.Errorf("Infrastructure = %q, want %q", a.Infrastructure, "restore")
	}
	if len(a.Triggers) != 1 {
		t.Fatalf("has %d triggers, want 1; triggers: %+v", len(a.Triggers), a.Triggers)
	}
	if a.Triggers[0].Trigger != "MANUAL" {
		t.Errorf("Triggers[0].Trigger = %q, want %q", a.Triggers[0].Trigger, "MANUAL")
	}
	if a.Triggers[0].TriggerParam != "" {
		t.Errorf("Triggers[0].TriggerParam = %q, want empty string (null trigger_param)", a.Triggers[0].TriggerParam)
	}
	if a.OnFailure != "halt" {
		t.Errorf("OnFailure = %q, want %q", a.OnFailure, "halt")
	}
}

// ---------------------------------------------------------------------------
// Standalone agents — discovery (T4.2)
// ---------------------------------------------------------------------------
//
// Tests below verify that loadAgents correctly discovers standalone agent files under
// Catalog/StandaloneAgents/ across flat and category-subfolder layouts.
//
// All tests build synthetic fixture trees under t.TempDir(); none reads from the real
// repository tree. This keeps the suite independent of any standalone agents that may or
// may not exist in the repository at any given time.
//
// Scenarios covered:
//   - A flat file directly under StandaloneAgents/ is discovered.
//   - A file one level deep under StandaloneAgents/<Category>/ is discovered.
//   - Both layouts are discovered together in the same scan.
//   - An absent StandaloneAgents/ directory loads cleanly (no error, no agents).
//   - An empty StandaloneAgents/ directory loads cleanly with no agents.
//   - README.md files under StandaloneAgents/ are skipped.
//   - Non-.md files under StandaloneAgents/ are skipped.
//   - Files more than one level deep (StandaloneAgents/Cat/Sub/file.md) are not discovered.
//   - When a flat and categorised file share the same key, the flat entry wins (processed first).

// agentKeys extracts the Key fields from a slice of agents for use in error messages.
func agentKeys(agents []domain.Agent) []string {
	keys := make([]string, len(agents))
	for i, a := range agents {
		keys[i] = a.Key
	}
	return keys
}

// writeStandaloneAgentFile writes a minimal standalone agent markdown file.
// When category is empty the file is placed flat under Catalog/StandaloneAgents/.
// When category is non-empty it is placed under Catalog/StandaloneAgents/<category>/.
func writeStandaloneAgentFile(t *testing.T, root, category, agentKey string) {
	t.Helper()
	var relPath string
	if category == "" {
		mustMkdir(t, root, "Catalog", "StandaloneAgents")
		relPath = filepath.Join("Catalog", "StandaloneAgents", agentKey+".md")
	} else {
		mustMkdir(t, root, "Catalog", "StandaloneAgents", category)
		relPath = filepath.Join("Catalog", "StandaloneAgents", category, agentKey+".md")
	}
	content := []byte("---\nversion: \"1.0\"\nname: " + agentKey + "\ndescription: Synthetic standalone agent for catalog tests.\n---\nContent.\n")
	mustWriteFile(t, root, relPath, content)
}

// TestStandaloneDiscovery_FlatFile_Discovered verifies that a standalone agent file placed
// directly under Catalog/StandaloneAgents/ (flat layout) is discovered and returned by
// StandaloneAgents().
func TestStandaloneDiscovery_FlatFile_Discovered(t *testing.T) {
	root := makeTempMosaicRoot(t)
	writeStandaloneAgentFile(t, root, "", "standalone-flat")

	cat, err := catalog.Load(root, "")
	if err != nil {
		t.Fatalf("catalog.Load: %v", err)
	}

	agents := cat.StandaloneAgents()
	if len(agents) == 0 {
		t.Fatal("StandaloneAgents() returned empty slice; expected standalone-flat to be discovered")
	}
	for _, a := range agents {
		if a.Key == "standalone-flat" {
			return
		}
	}
	t.Errorf("standalone-flat not found in StandaloneAgents(); got keys: %v", agentKeys(agents))
}

// TestStandaloneDiscovery_CategorisedFile_Discovered verifies that a standalone agent file
// placed one level deep under Catalog/StandaloneAgents/<Category>/ is discovered and
// returned by StandaloneAgents().
func TestStandaloneDiscovery_CategorisedFile_Discovered(t *testing.T) {
	root := makeTempMosaicRoot(t)
	writeStandaloneAgentFile(t, root, "Analytics", "standalone-categorised")

	cat, err := catalog.Load(root, "")
	if err != nil {
		t.Fatalf("catalog.Load: %v", err)
	}

	agents := cat.StandaloneAgents()
	if len(agents) == 0 {
		t.Fatal("StandaloneAgents() returned empty slice; expected standalone-categorised to be discovered")
	}
	for _, a := range agents {
		if a.Key == "standalone-categorised" {
			return
		}
	}
	t.Errorf("standalone-categorised not found in StandaloneAgents(); got keys: %v", agentKeys(agents))
}

// TestStandaloneDiscovery_BothLayouts_DiscoveredTogether verifies that a flat standalone
// file and a categorised standalone file are both discovered in the same catalog load.
func TestStandaloneDiscovery_BothLayouts_DiscoveredTogether(t *testing.T) {
	root := makeTempMosaicRoot(t)
	writeStandaloneAgentFile(t, root, "", "flat-agent")
	writeStandaloneAgentFile(t, root, "Analytics", "categorised-agent")

	cat, err := catalog.Load(root, "")
	if err != nil {
		t.Fatalf("catalog.Load: %v", err)
	}

	agents := cat.StandaloneAgents()
	keys := make(map[string]bool, len(agents))
	for _, a := range agents {
		keys[a.Key] = true
	}
	if !keys["flat-agent"] {
		t.Errorf("flat-agent not found in StandaloneAgents(); got keys: %v", agentKeys(agents))
	}
	if !keys["categorised-agent"] {
		t.Errorf("categorised-agent not found in StandaloneAgents(); got keys: %v", agentKeys(agents))
	}
}

// TestStandaloneDiscovery_AbsentDirectory_LoadsCleanly verifies that a catalog load succeeds
// without error and returns an empty (or nil) StandaloneAgents() slice when
// Catalog/StandaloneAgents/ does not exist on disk.
func TestStandaloneDiscovery_AbsentDirectory_LoadsCleanly(t *testing.T) {
	root := makeTempMosaicRoot(t)
	// Deliberately do not create Catalog/StandaloneAgents/.

	cat, err := catalog.Load(root, "")
	if err != nil {
		t.Fatalf("catalog.Load failed when standalone directory is absent: %v", err)
	}

	if n := len(cat.StandaloneAgents()); n != 0 {
		t.Errorf("StandaloneAgents() returned %d agents for absent directory; want 0", n)
	}
}

// TestStandaloneDiscovery_EmptyDirectory_LoadsCleanly verifies that a catalog load succeeds
// without error and returns no agents when Catalog/StandaloneAgents/ exists but contains
// no .md files.
func TestStandaloneDiscovery_EmptyDirectory_LoadsCleanly(t *testing.T) {
	root := makeTempMosaicRoot(t)
	mustMkdir(t, root, "Catalog", "StandaloneAgents")
	// Directory exists but has no .md files.

	cat, err := catalog.Load(root, "")
	if err != nil {
		t.Fatalf("catalog.Load failed for empty standalone directory: %v", err)
	}

	if n := len(cat.StandaloneAgents()); n != 0 {
		t.Errorf("StandaloneAgents() returned %d agents for empty directory; want 0", n)
	}
}

// TestStandaloneDiscovery_ReadmeSkipped verifies that README.md files under
// Catalog/StandaloneAgents/ are not treated as agent files, matching the convention
// of the subagent and utility scans.
func TestStandaloneDiscovery_ReadmeSkipped(t *testing.T) {
	root := makeTempMosaicRoot(t)
	mustMkdir(t, root, "Catalog", "StandaloneAgents")
	mustWriteFile(t, root,
		filepath.Join("Catalog", "StandaloneAgents", "README.md"),
		[]byte("---\nversion: \"1.0\"\n---\nThis is a readme, not an agent.\n"))
	// Write one valid agent to confirm the scan ran.
	writeStandaloneAgentFile(t, root, "", "valid-standalone")

	cat, err := catalog.Load(root, "")
	if err != nil {
		t.Fatalf("catalog.Load: %v", err)
	}

	for _, a := range cat.StandaloneAgents() {
		if a.Key == "README" {
			t.Error("README appeared in StandaloneAgents(); README.md must be skipped by the scanner")
		}
	}
}

// TestStandaloneDiscovery_NonMarkdownFileSkipped verifies that non-.md files under
// Catalog/StandaloneAgents/ (including .gitkeep) are not treated as agent files.
func TestStandaloneDiscovery_NonMarkdownFileSkipped(t *testing.T) {
	root := makeTempMosaicRoot(t)
	mustMkdir(t, root, "Catalog", "StandaloneAgents")
	mustWriteFile(t, root,
		filepath.Join("Catalog", "StandaloneAgents", ".gitkeep"),
		[]byte(""))
	// Write one valid agent to confirm the scan ran.
	writeStandaloneAgentFile(t, root, "", "valid-standalone")

	cat, err := catalog.Load(root, "")
	if err != nil {
		t.Fatalf("catalog.Load: %v", err)
	}

	for _, a := range cat.StandaloneAgents() {
		if a.Key == ".gitkeep" || a.Key == "gitkeep" {
			t.Error("non-.md file appeared in StandaloneAgents(); non-markdown files must be skipped")
		}
	}
}

// TestStandaloneDiscovery_DeeplyNestedFileIgnored verifies that a file placed more than
// one level deep under Catalog/StandaloneAgents/ (e.g. Category/SubCategory/file.md) is
// not discovered. Only one level of category nesting is supported.
func TestStandaloneDiscovery_DeeplyNestedFileIgnored(t *testing.T) {
	root := makeTempMosaicRoot(t)
	mustMkdir(t, root, "Catalog", "StandaloneAgents", "Category", "SubCategory")
	mustWriteFile(t, root,
		filepath.Join("Catalog", "StandaloneAgents", "Category", "SubCategory", "deep-agent.md"),
		[]byte("---\nversion: \"1.0\"\n---\nContent.\n"))
	// Write one valid agent so we can confirm the scan ran without crashing.
	writeStandaloneAgentFile(t, root, "", "valid-standalone")

	cat, err := catalog.Load(root, "")
	if err != nil {
		t.Fatalf("catalog.Load: %v", err)
	}

	for _, a := range cat.StandaloneAgents() {
		if a.Key == "deep-agent" {
			t.Error("deep-agent appeared in StandaloneAgents(); files nested more than one level must be ignored")
		}
	}
}

// TestStandaloneDiscovery_DuplicateKey_FlatEntryWins verifies that when a flat file and a
// categorised file share the same key (same base filename), the flat entry takes priority
// because flat entries are processed before category entries. Exactly one entry with the
// shared key must appear in StandaloneAgents(), and it must carry an empty Category.
func TestStandaloneDiscovery_DuplicateKey_FlatEntryWins(t *testing.T) {
	root := makeTempMosaicRoot(t)
	// Flat entry: no category → Category will be "".
	writeStandaloneAgentFile(t, root, "", "shared-key")
	// Categorised entry: same base filename → same key "shared-key".
	writeStandaloneAgentFile(t, root, "Analytics", "shared-key")

	cat, err := catalog.Load(root, "")
	if err != nil {
		t.Fatalf("catalog.Load: %v", err)
	}

	agents := cat.StandaloneAgents()

	// Exactly one entry must be present.
	var matches []domain.Agent
	for _, a := range agents {
		if a.Key == "shared-key" {
			matches = append(matches, a)
		}
	}
	if len(matches) == 0 {
		t.Fatal("shared-key not found in StandaloneAgents(); expected one entry")
	}
	if len(matches) > 1 {
		t.Errorf("StandaloneAgents() contains %d entries with key shared-key; want exactly 1", len(matches))
	}
	// The flat entry must have won: its Category is empty.
	if matches[0].Category != "" {
		t.Errorf("shared-key Category = %q; want empty string (flat entry must win over categorised duplicate)", matches[0].Category)
	}
}

// ---------------------------------------------------------------------------
// Standalone agents — role, category, lookup, and accessor exclusion (T4.3)
// ---------------------------------------------------------------------------
//
// Tests below verify the behavioral contract of the StandaloneAgents() accessor:
//   - Flat-layout agents carry an empty Category field.
//   - Categorised-layout agents carry the folder name as Category.
//   - Every agent returned by StandaloneAgents() carries RoleStandalone.
//   - Every standalone agent is reachable via Agent(key).
//   - Standalone agents are excluded from Agents() (the worker-agent accessor).
//   - StandaloneAgents() returns agents sorted ascending by Key.
//   - A file declaring `role: standalone` in frontmatter confirms the path-derived role
//     without producing an invalid-role issue.

// TestStandaloneAgents_FlatFile_HasEmptyCategory verifies that a standalone agent discovered
// from the flat layout (directly under StandaloneAgents/) has an empty Category field.
func TestStandaloneAgents_FlatFile_HasEmptyCategory(t *testing.T) {
	root := makeTempMosaicRoot(t)
	writeStandaloneAgentFile(t, root, "", "flat-standalone")

	cat, err := catalog.Load(root, "")
	if err != nil {
		t.Fatalf("catalog.Load: %v", err)
	}

	for _, a := range cat.StandaloneAgents() {
		if a.Key == "flat-standalone" {
			if a.Category != "" {
				t.Errorf("flat-standalone Category = %q; want empty string for flat layout", a.Category)
			}
			return
		}
	}
	t.Fatal("flat-standalone not found in StandaloneAgents(); cannot verify Category")
}

// TestStandaloneAgents_CategorisedFile_HasFolderNameAsCategory verifies that a standalone
// agent discovered from a category subfolder carries the folder name verbatim as its Category.
func TestStandaloneAgents_CategorisedFile_HasFolderNameAsCategory(t *testing.T) {
	root := makeTempMosaicRoot(t)
	writeStandaloneAgentFile(t, root, "Analytics", "categorised-standalone")

	cat, err := catalog.Load(root, "")
	if err != nil {
		t.Fatalf("catalog.Load: %v", err)
	}

	for _, a := range cat.StandaloneAgents() {
		if a.Key == "categorised-standalone" {
			if a.Category != "Analytics" {
				t.Errorf("categorised-standalone Category = %q; want %q (the folder name)", a.Category, "Analytics")
			}
			return
		}
	}
	t.Fatal("categorised-standalone not found in StandaloneAgents(); cannot verify Category")
}

// TestStandaloneAgents_AllHaveStandaloneRole verifies that every agent returned by
// StandaloneAgents() carries RoleStandalone, regardless of layout.
func TestStandaloneAgents_AllHaveStandaloneRole(t *testing.T) {
	root := makeTempMosaicRoot(t)
	writeStandaloneAgentFile(t, root, "", "flat-standalone")
	writeStandaloneAgentFile(t, root, "Analytics", "categorised-standalone")

	cat, err := catalog.Load(root, "")
	if err != nil {
		t.Fatalf("catalog.Load: %v", err)
	}

	standalones := cat.StandaloneAgents()
	if len(standalones) == 0 {
		t.Fatal("StandaloneAgents() returned empty slice; cannot verify role assignment")
	}
	for _, a := range standalones {
		if a.Role != domain.RoleStandalone {
			t.Errorf("StandaloneAgents()[%q].Role = %q; want RoleStandalone", a.Key, a.Role)
		}
	}
}

// TestStandaloneAgents_ReachableByKey verifies that every agent returned by
// StandaloneAgents() can be looked up via Agent(key), confirming the global agent index
// includes standalone agents.
func TestStandaloneAgents_ReachableByKey(t *testing.T) {
	root := makeTempMosaicRoot(t)
	writeStandaloneAgentFile(t, root, "", "flat-standalone")
	writeStandaloneAgentFile(t, root, "Analytics", "categorised-standalone")

	cat, err := catalog.Load(root, "")
	if err != nil {
		t.Fatalf("catalog.Load: %v", err)
	}

	standalones := cat.StandaloneAgents()
	if len(standalones) == 0 {
		t.Fatal("StandaloneAgents() returned empty slice; cannot verify Agent(key) lookup")
	}
	for _, a := range standalones {
		found, ok := cat.Agent(a.Key)
		if !ok {
			t.Errorf("Agent(%q): returned not-found; standalone agents must be reachable via Agent(key)", a.Key)
			continue
		}
		if found.Key != a.Key {
			t.Errorf("Agent(%q).Key = %q; want %q", a.Key, found.Key, a.Key)
		}
	}
}

// TestStandaloneAgents_ExcludedFromAgents verifies that standalone agents do not appear in
// the list returned by Agents() (the worker-agent accessor). Standalone agents are a distinct
// role and must be reached exclusively through StandaloneAgents().
func TestStandaloneAgents_ExcludedFromAgents(t *testing.T) {
	root := makeTempMosaicRoot(t)
	writeStandaloneAgentFile(t, root, "", "flat-standalone")
	writeStandaloneAgentFile(t, root, "Analytics", "categorised-standalone")

	cat, err := catalog.Load(root, "")
	if err != nil {
		t.Fatalf("catalog.Load: %v", err)
	}

	// Discovery must succeed for the exclusion assertion to be meaningful.
	standalones := cat.StandaloneAgents()
	if len(standalones) == 0 {
		t.Fatal("StandaloneAgents() returned empty slice; cannot verify exclusion from Agents()")
	}

	standaloneKeys := make(map[string]bool, len(standalones))
	for _, a := range standalones {
		standaloneKeys[a.Key] = true
	}
	for _, a := range cat.Agents() {
		if standaloneKeys[a.Key] {
			t.Errorf("standalone agent %q appeared in Agents(); standalone agents must be excluded from the worker-agent accessor", a.Key)
		}
	}
}

// TestStandaloneAgents_SortedByKey verifies that StandaloneAgents() returns agents in
// ascending key order. Deterministic ordering is required for stable UI rendering and
// plan output.
func TestStandaloneAgents_SortedByKey(t *testing.T) {
	root := makeTempMosaicRoot(t)
	writeStandaloneAgentFile(t, root, "", "zebra-agent")
	writeStandaloneAgentFile(t, root, "Analytics", "alpha-agent")

	cat, err := catalog.Load(root, "")
	if err != nil {
		t.Fatalf("catalog.Load: %v", err)
	}

	agents := cat.StandaloneAgents()
	if len(agents) < 2 {
		t.Fatal("StandaloneAgents() returned fewer than 2 agents; cannot verify sort order")
	}
	for i := 1; i < len(agents); i++ {
		if agents[i].Key < agents[i-1].Key {
			t.Errorf("StandaloneAgents() is not sorted by Key at index %d: %q < %q",
				i, agents[i].Key, agents[i-1].Key)
		}
	}
}

// TestStandaloneAgents_FrontmatterRoleConfirmsPathDerived verifies that when a standalone
// agent file declares `role: standalone` in its frontmatter, it is loaded with RoleStandalone
// (confirming, not contradicting, the path-derived role) and no invalid-role issue is produced.
func TestStandaloneAgents_FrontmatterRoleConfirmsPathDerived(t *testing.T) {
	root := makeTempMosaicRoot(t)
	mustMkdir(t, root, "Catalog", "StandaloneAgents")
	mustWriteFile(t, root,
		filepath.Join("Catalog", "StandaloneAgents", "explicit-standalone.md"),
		[]byte("---\nrole: standalone\nversion: \"1.0\"\nname: Explicit Standalone\ndescription: Declares its role explicitly.\n---\nContent.\n"))

	cat, err := catalog.Load(root, "")
	if err != nil {
		t.Fatalf("catalog.Load: %v", err)
	}

	a, ok := cat.Agent("explicit-standalone")
	if !ok {
		t.Fatal("Agent(\"explicit-standalone\") not found; expected the agent to be present in catalog")
	}
	if a.Role != domain.RoleStandalone {
		t.Errorf("explicit-standalone Role = %q; want RoleStandalone", a.Role)
	}
	for _, iss := range cat.Issues() {
		if iss.Code == "invalid-role" && iss.Subject == "explicit-standalone" {
			t.Errorf("unexpected invalid-role issue for agent with valid frontmatter role %q: %s", "standalone", iss.Message)
		}
	}
}
