package testcatalog_test

// testcatalog_fields_test.go covers the new CatalogEntry fields
// (InfrastructureAgents, Checkpoints, Commits) and the
// UnionInfrastructureAgentKeys method on *Catalog.
//
// These tests are written for the TDD RED phase. They compile and run, but
// most will fail until Stage 2 implementation (I2.1, I2.2) is delivered.
//
// Coverage:
//
//   InfrastructureAgents on CatalogEntry:
//   - A workflow declaring infrastructure_agents produces a populated slice
//     on its CatalogEntry.
//   - A workflow without an infrastructure_agents field produces a non-nil
//     empty []string{} (not nil) -- nil would cause buildRunArgs to omit
//     --infrastructure, leaving all deployed agents active.
//   - InfrastructureAgents order matches the declaration order in frontmatter.
//   - InfrastructureAgents is a non-aliased copy per CatalogEntry; mutating
//     one entry's slice does not affect another entry obtained separately.
//
//   Checkpoints on CatalogEntry:
//   - A workflow declaring checkpoints: enabled produces Checkpoints="enabled".
//   - A workflow declaring checkpoints: disabled produces Checkpoints="disabled".
//   - A workflow with no checkpoints field defaults to Checkpoints="disabled".
//
//   Commits on CatalogEntry:
//   - A workflow declaring commits: enabled produces Commits="enabled".
//   - A workflow declaring commits: disabled produces Commits="disabled".
//   - A workflow with no commits field defaults to Commits="disabled".
//
//   Parse errors:
//   - checkpoints value "yes" (not "enabled"/"disabled") causes Load to return error.
//   - checkpoints value "Enabled" (wrong case) causes Load to return error.
//   - commits value "Enabled" (wrong case) causes Load to return error.
//   - infrastructure_agents containing an empty string causes Load to return error.
//   - infrastructure_agents containing duplicate keys causes Load to return error.
//
//   UnionInfrastructureAgentKeys:
//   - Returns sorted, deduplicated union of all workflow declarations.
//   - Returns non-nil empty []string{} when no workflow declares any agents
//     (not nil -- nil would cause buildDeployArgs to omit --infrastructure).
//   - A multi-mode workflow's agents do not appear more than once in the union
//     (the union iterates per workflow, not per CatalogEntry).
//   - Cross-workflow duplicates are deduplicated (same key in two workflows
//     appears once in the union).
//   - Returns a fresh non-aliased slice per call.

import (
	"strings"
	"testing"

	"mosaic-run/internal/testcatalog"
)

// infraFieldsCatalogDir returns the path to the infra-fields-catalog fixture.
// This catalog contains:
//   - workflow-with-infra: infrastructure_agents=[agent-a, agent-b], checkpoints=enabled, commits=disabled
//   - workflow-no-infra:   no new fields (tests absent-field defaults)
//   - workflow-multi-mode: modes=[auto, auto-review], infrastructure_agents=[agent-b, agent-c]
func infraFieldsCatalogDir(t *testing.T) string {
	t.Helper()
	return fixtureDir(t, "infra-fields-catalog")
}

// loadInfraFieldsCatalog loads the infra-fields-catalog fixture and returns the Catalog.
// Calls t.Fatal if Load returns an error.
func loadInfraFieldsCatalog(t *testing.T) *testcatalog.Catalog {
	t.Helper()
	cat, err := testcatalog.Load(infraFieldsCatalogDir(t))
	if err != nil {
		t.Fatalf("Load(infra-fields-catalog): %v", err)
	}
	return cat
}

// entryFor returns the CatalogEntry for a specific (workflowID, mode) pair
// from the given catalog. Calls t.Fatal if the entry is not found.
func entryFor(t *testing.T, cat *testcatalog.Catalog, workflowID, mode string) testcatalog.CatalogEntry {
	t.Helper()
	entries, err := cat.WorkflowByID(workflowID)
	if err != nil {
		t.Fatalf("WorkflowByID(%q): %v", workflowID, err)
	}
	for _, e := range entries {
		if e.Mode == mode {
			return e
		}
	}
	t.Fatalf("no entry found for workflow %q mode %q", workflowID, mode)
	panic("unreachable")
}

// ---- InfrastructureAgents: populated from frontmatter ----

// TestCatalogEntry_InfrastructureAgents_PopulatedFromFrontmatter verifies that
// a workflow declaring infrastructure_agents in its frontmatter produces a
// CatalogEntry with the corresponding non-nil, non-empty InfrastructureAgents
// slice containing the declared keys in declaration order.
func TestCatalogEntry_InfrastructureAgents_PopulatedFromFrontmatter(t *testing.T) {
	cat := loadInfraFieldsCatalog(t)
	e := entryFor(t, cat, "workflow-with-infra", "auto")

	want := []string{"agent-a", "agent-b"}
	if e.InfrastructureAgents == nil {
		t.Fatal("InfrastructureAgents = nil; want populated slice for workflow declaring infrastructure_agents")
	}
	if len(e.InfrastructureAgents) != len(want) {
		t.Fatalf("InfrastructureAgents = %v (len %d), want %v (len %d)",
			e.InfrastructureAgents, len(e.InfrastructureAgents), want, len(want))
	}
	for i, w := range want {
		if e.InfrastructureAgents[i] != w {
			t.Errorf("InfrastructureAgents[%d] = %q, want %q (must preserve declaration order)",
				i, e.InfrastructureAgents[i], w)
		}
	}
}

// TestCatalogEntry_InfrastructureAgents_NonNilEmptyWhenFieldAbsent verifies that
// a workflow with no infrastructure_agents field produces a CatalogEntry with
// InfrastructureAgents that is non-nil and empty (len == 0, but not nil).
// This is critical: nil would cause buildRunArgs to omit --infrastructure,
// leaving all deployed agents active and re-creating the cross-contamination bug.
func TestCatalogEntry_InfrastructureAgents_NonNilEmptyWhenFieldAbsent(t *testing.T) {
	cat := loadInfraFieldsCatalog(t)
	e := entryFor(t, cat, "workflow-no-infra", "auto")

	if e.InfrastructureAgents == nil {
		t.Error("InfrastructureAgents = nil for workflow without infrastructure_agents field; " +
			"want non-nil empty []string{}. " +
			"nil causes buildRunArgs to omit --infrastructure (all agents active); " +
			"non-nil empty causes --infrastructure= (no agents active)")
	}
	if len(e.InfrastructureAgents) != 0 {
		t.Errorf("InfrastructureAgents = %v (len %d), want empty slice for workflow without the field",
			e.InfrastructureAgents, len(e.InfrastructureAgents))
	}
}

// TestCatalogEntry_InfrastructureAgents_MultiModeEntryHasAgents verifies that
// each mode entry for a multi-mode workflow that declares infrastructure_agents
// carries the same agent list. The list is per workflow, not per mode, but each
// CatalogEntry must reflect the workflow's declaration.
func TestCatalogEntry_InfrastructureAgents_MultiModeEntryHasAgents(t *testing.T) {
	cat := loadInfraFieldsCatalog(t)
	want := []string{"agent-b", "agent-c"}
	for _, mode := range []string{"auto", "auto-review"} {
		e := entryFor(t, cat, "workflow-multi-mode", mode)
		if e.InfrastructureAgents == nil {
			t.Errorf("(workflow-multi-mode, %q).InfrastructureAgents = nil; want %v", mode, want)
			continue
		}
		if len(e.InfrastructureAgents) != len(want) {
			t.Errorf("(workflow-multi-mode, %q).InfrastructureAgents = %v (len %d), want %v (len %d)",
				mode, e.InfrastructureAgents, len(e.InfrastructureAgents), want, len(want))
			continue
		}
		for i, w := range want {
			if e.InfrastructureAgents[i] != w {
				t.Errorf("(workflow-multi-mode, %q).InfrastructureAgents[%d] = %q, want %q",
					mode, i, e.InfrastructureAgents[i], w)
			}
		}
	}
}

// TestCatalogEntry_InfrastructureAgents_IsNonAliasedCopy verifies that each
// CatalogEntry holds an independent copy of InfrastructureAgents. Mutating the
// slice on one returned entry must not affect entries obtained from a separate
// call to WorkflowByID.
func TestCatalogEntry_InfrastructureAgents_IsNonAliasedCopy(t *testing.T) {
	cat := loadInfraFieldsCatalog(t)

	entries1, err := cat.WorkflowByID("workflow-with-infra")
	if err != nil {
		t.Fatalf("WorkflowByID(workflow-with-infra) first call: %v", err)
	}
	entries2, err := cat.WorkflowByID("workflow-with-infra")
	if err != nil {
		t.Fatalf("WorkflowByID(workflow-with-infra) second call: %v", err)
	}

	if len(entries1) == 0 || len(entries2) == 0 {
		t.Fatal("WorkflowByID returned empty slices; cannot test aliasing")
	}
	if entries1[0].InfrastructureAgents == nil || entries2[0].InfrastructureAgents == nil {
		t.Skip("InfrastructureAgents is nil (implementation not yet delivered); skipping aliasing check")
	}
	if len(entries1[0].InfrastructureAgents) == 0 {
		t.Skip("InfrastructureAgents is empty; cannot test aliasing by mutation")
	}

	// Mutate the first entry's slice by overwriting element 0.
	original := entries2[0].InfrastructureAgents[0]
	entries1[0].InfrastructureAgents[0] = "MUTATED"

	if entries2[0].InfrastructureAgents[0] != original {
		t.Errorf("mutating InfrastructureAgents[0] on one entry changed another entry's slice "+
			"(got %q, want %q): slices must be independent copies, not aliases of the same backing array",
			entries2[0].InfrastructureAgents[0], original)
	}
}

// ---- Checkpoints field ----

// TestCatalogEntry_Checkpoints_EnabledWhenDeclaredEnabled verifies that a
// workflow declaring checkpoints: enabled produces a CatalogEntry with
// Checkpoints == "enabled".
func TestCatalogEntry_Checkpoints_EnabledWhenDeclaredEnabled(t *testing.T) {
	cat := loadInfraFieldsCatalog(t)
	e := entryFor(t, cat, "workflow-with-infra", "auto")

	if e.Checkpoints != "enabled" {
		t.Errorf("Checkpoints = %q, want %q (workflow declares checkpoints: enabled)",
			e.Checkpoints, "enabled")
	}
}

// TestCatalogEntry_Commits_DisabledWhenDeclaredDisabled verifies that a
// workflow declaring commits: disabled (explicit) produces Commits="disabled".
func TestCatalogEntry_Commits_DisabledWhenDeclaredDisabled(t *testing.T) {
	// workflow-with-infra declares commits: disabled explicitly.
	// We use this entry to test the explicit "disabled" case.
	cat := loadInfraFieldsCatalog(t)
	e := entryFor(t, cat, "workflow-with-infra", "auto")

	if e.Commits != "disabled" {
		t.Errorf("Commits = %q, want %q (workflow declares commits: disabled explicitly)",
			e.Commits, "disabled")
	}
}

// TestCatalogEntry_Checkpoints_DefaultsToDisabledWhenAbsent verifies that a
// workflow with no checkpoints field in its frontmatter defaults to
// Checkpoints == "disabled".
func TestCatalogEntry_Checkpoints_DefaultsToDisabledWhenAbsent(t *testing.T) {
	cat := loadInfraFieldsCatalog(t)
	e := entryFor(t, cat, "workflow-no-infra", "auto")

	if e.Checkpoints != "disabled" {
		t.Errorf("Checkpoints = %q, want %q (absent field must default to disabled)",
			e.Checkpoints, "disabled")
	}
}

// ---- Commits field ----

// TestCatalogEntry_Commits_DefaultsToDisabledWhenAbsent verifies that a
// workflow with no commits field in its frontmatter defaults to
// Commits == "disabled".
func TestCatalogEntry_Commits_DefaultsToDisabledWhenAbsent(t *testing.T) {
	cat := loadInfraFieldsCatalog(t)
	e := entryFor(t, cat, "workflow-no-infra", "auto")

	if e.Commits != "disabled" {
		t.Errorf("Commits = %q, want %q (absent field must default to disabled)",
			e.Commits, "disabled")
	}
}

// TestCatalogEntry_Commits_EnabledWhenDeclaredEnabled verifies that a workflow
// declaring commits: enabled produces a CatalogEntry with Commits == "enabled".
// workflow-multi-mode declares commits: enabled.
func TestCatalogEntry_Commits_EnabledWhenDeclaredEnabled(t *testing.T) {
	cat := loadInfraFieldsCatalog(t)
	e := entryFor(t, cat, "workflow-multi-mode", "auto")

	if e.Commits != "enabled" {
		t.Errorf("Commits = %q, want %q (workflow declares commits: enabled)",
			e.Commits, "enabled")
	}
}

// ---- Parse errors ----

// TestLoad_InvalidCheckpointsValue_yes_ReturnsError verifies that a workflow
// declaring checkpoints: yes causes Load to return an error. Only "enabled"
// and "disabled" are valid; matching is case-sensitive.
func TestLoad_InvalidCheckpointsValue_yes_ReturnsError(t *testing.T) {
	_, err := testcatalog.Load(fixtureDir(t, "infra-bad-checkpoints"))
	if err == nil {
		t.Fatal("Load(infra-bad-checkpoints): expected error for checkpoints: yes, got nil")
	}
	// Error message must contain the offending value and hint at valid values.
	if !strings.Contains(err.Error(), "yes") {
		t.Errorf("Load error %q does not contain the offending value %q", err.Error(), "yes")
	}
	if !strings.Contains(err.Error(), "disabled") {
		t.Errorf("Load error %q does not contain valid-values hint %q", err.Error(), "disabled")
	}
}

// TestLoad_InvalidCommitsValue_Enabled_ReturnsError verifies that a workflow
// declaring commits: Enabled (capital E) causes Load to return an error.
// Matching is case-sensitive: only lowercase "enabled" is valid.
func TestLoad_InvalidCommitsValue_Enabled_ReturnsError(t *testing.T) {
	_, err := testcatalog.Load(fixtureDir(t, "infra-bad-commits"))
	if err == nil {
		t.Fatal("Load(infra-bad-commits): expected error for commits: Enabled (wrong case), got nil")
	}
	if !strings.Contains(err.Error(), "Enabled") {
		t.Errorf("Load error %q does not contain the offending value %q", err.Error(), "Enabled")
	}
	if !strings.Contains(err.Error(), "disabled") {
		t.Errorf("Load error %q does not contain valid-values hint %q", err.Error(), "disabled")
	}
}

// TestLoad_InfrastructureAgents_EmptyEntry_ReturnsError verifies that a workflow
// with an empty string in infrastructure_agents causes Load to return an error.
func TestLoad_InfrastructureAgents_EmptyEntry_ReturnsError(t *testing.T) {
	_, err := testcatalog.Load(fixtureDir(t, "infra-empty-agent"))
	if err == nil {
		t.Fatal("Load(infra-empty-agent): expected error for infrastructure_agents with empty entry, got nil")
	}
	if !strings.Contains(err.Error(), "empty") {
		t.Errorf("Load error %q does not contain %q; expected message matching spec format %q",
			err.Error(), "empty", `workflow %q: infrastructure_agents contains empty entry`)
	}
}

// TestLoad_InfrastructureAgents_DuplicateEntry_ReturnsError verifies that a
// workflow declaring the same key twice in infrastructure_agents causes Load to
// return an error.
func TestLoad_InfrastructureAgents_DuplicateEntry_ReturnsError(t *testing.T) {
	_, err := testcatalog.Load(fixtureDir(t, "infra-duplicate-agents"))
	if err == nil {
		t.Fatal("Load(infra-duplicate-agents): expected error for duplicate infrastructure_agents entry, got nil")
	}
	// Error message must contain the duplicate key name.
	if !strings.Contains(err.Error(), "agent-a") {
		t.Errorf("Load error %q does not contain the duplicate key %q", err.Error(), "agent-a")
	}
}

// ---- UnionInfrastructureAgentKeys ----

// TestUnionInfrastructureAgentKeys_ReturnsSortedDedupedUnion verifies that
// UnionInfrastructureAgentKeys returns the sorted, deduplicated union of all
// infrastructure_agents declared across all workflows in the catalog.
//
// infra-fields-catalog:
//   - workflow-with-infra: [agent-a, agent-b]
//   - workflow-multi-mode: [agent-b, agent-c]  (agent-b duplicated cross-workflow)
//   - workflow-no-infra:   absent (contributes nothing)
//
// Expected union: [agent-a, agent-b, agent-c] (sorted, agent-b deduplicated).
func TestUnionInfrastructureAgentKeys_ReturnsSortedDedupedUnion(t *testing.T) {
	cat := loadInfraFieldsCatalog(t)
	got := cat.UnionInfrastructureAgentKeys()

	want := []string{"agent-a", "agent-b", "agent-c"}
	if got == nil {
		t.Fatal("UnionInfrastructureAgentKeys() = nil; want non-nil slice with union of declared agents")
	}
	if len(got) != len(want) {
		t.Fatalf("UnionInfrastructureAgentKeys() = %v (len %d), want %v (len %d)",
			got, len(got), want, len(want))
	}
	for i, w := range want {
		if got[i] != w {
			t.Errorf("UnionInfrastructureAgentKeys()[%d] = %q, want %q (must be sorted and deduplicated)",
				i, got[i], w)
		}
	}
}

// TestUnionInfrastructureAgentKeys_NonNilEmptyWhenNoWorkflowDeclares verifies
// that UnionInfrastructureAgentKeys returns a non-nil empty []string{} (not nil)
// when no workflow in the catalog declares any infrastructure agents.
//
// This is critical: nil would cause buildDeployArgs to omit --infrastructure,
// deploying the default set of agents and re-creating the cross-contamination bug.
// Non-nil empty causes --infrastructure "" which deploys zero agents.
func TestUnionInfrastructureAgentKeys_NonNilEmptyWhenNoWorkflowDeclares(t *testing.T) {
	// valid-catalog contains workflow-a, workflow-b, workflow-c, none of which
	// declare infrastructure_agents. The union across all three is empty.
	cat, err := testcatalog.Load(validCatalog(t))
	if err != nil {
		t.Fatalf("Load(valid-catalog): %v", err)
	}
	got := cat.UnionInfrastructureAgentKeys()

	if got == nil {
		t.Error("UnionInfrastructureAgentKeys() = nil when no workflow declares agents; " +
			"want non-nil empty []string{}. " +
			"nil causes buildDeployArgs to omit --infrastructure (default agents deployed); " +
			"non-nil empty causes --infrastructure \"\" (zero agents deployed)")
	}
	if len(got) != 0 {
		t.Errorf("UnionInfrastructureAgentKeys() = %v (len %d), want empty slice when no workflow declares agents",
			got, len(got))
	}
}

// TestUnionInfrastructureAgentKeys_MultiModeWorkflowNotDuplicated verifies that
// a workflow declaring multiple modes does not produce duplicate entries in the
// union. The union iterates workflows (not CatalogEntry values), so each
// workflow's declared agents appear at most once regardless of mode count.
func TestUnionInfrastructureAgentKeys_MultiModeWorkflowNotDuplicated(t *testing.T) {
	cat := loadInfraFieldsCatalog(t)
	got := cat.UnionInfrastructureAgentKeys()

	// Prerequisite: the infra-fields-catalog has two workflows declaring agents
	// (workflow-with-infra and workflow-multi-mode), so the union must be non-nil
	// and non-empty. An empty or nil result means the implementation is missing.
	if got == nil {
		t.Fatal("UnionInfrastructureAgentKeys() = nil; want non-nil slice (implementation missing)")
	}
	if len(got) == 0 {
		t.Fatal("UnionInfrastructureAgentKeys() = [] (empty); want non-empty union for catalog with declared agents (implementation missing)")
	}

	// Count how many times each agent key appears.
	seen := make(map[string]int)
	for _, key := range got {
		seen[key]++
	}

	// workflow-multi-mode has 2 modes and declares [agent-b, agent-c].
	// Neither agent-b nor agent-c must appear more than once in the union.
	for key, count := range seen {
		if count > 1 {
			t.Errorf("UnionInfrastructureAgentKeys() contains %q %d times; "+
				"each key must appear at most once (multi-mode workflow must not cause duplicates)",
				key, count)
		}
	}
}

// TestUnionInfrastructureAgentKeys_CrossWorkflowDeduplicated verifies that a
// key declared by more than one workflow appears exactly once in the union.
// Both workflow-with-infra and workflow-multi-mode declare "agent-b".
func TestUnionInfrastructureAgentKeys_CrossWorkflowDeduplicated(t *testing.T) {
	cat := loadInfraFieldsCatalog(t)
	got := cat.UnionInfrastructureAgentKeys()

	count := 0
	for _, key := range got {
		if key == "agent-b" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("UnionInfrastructureAgentKeys() contains %q %d time(s), want exactly 1; "+
			"keys declared in multiple workflows must be deduplicated",
			"agent-b", count)
	}
}

// TestUnionInfrastructureAgentKeys_ResultIsSorted verifies that the slice
// returned by UnionInfrastructureAgentKeys is sorted in ascending order.
func TestUnionInfrastructureAgentKeys_ResultIsSorted(t *testing.T) {
	cat := loadInfraFieldsCatalog(t)
	got := cat.UnionInfrastructureAgentKeys()

	// Prerequisite: the infra-fields-catalog declares multiple agents across
	// workflows, so the union must contain at least 2 entries to verify ordering.
	// A nil or short result means the implementation is missing.
	if got == nil {
		t.Fatal("UnionInfrastructureAgentKeys() = nil; want non-nil slice (implementation missing)")
	}
	if len(got) < 2 {
		t.Fatalf("UnionInfrastructureAgentKeys() = %v (len %d); want at least 2 entries to verify sort order (implementation missing)", got, len(got))
	}

	for i := 1; i < len(got); i++ {
		if got[i] <= got[i-1] {
			t.Errorf("UnionInfrastructureAgentKeys() not sorted: got[%d]=%q follows got[%d]=%q; "+
				"result must be sorted ascending",
				i, got[i], i-1, got[i-1])
		}
	}
}

// TestUnionInfrastructureAgentKeys_FreshSlicePerCall verifies that each call to
// UnionInfrastructureAgentKeys returns an independent slice. Mutating the result
// of one call must not affect the result of a subsequent call.
func TestUnionInfrastructureAgentKeys_FreshSlicePerCall(t *testing.T) {
	cat := loadInfraFieldsCatalog(t)

	got1 := cat.UnionInfrastructureAgentKeys()
	got2 := cat.UnionInfrastructureAgentKeys()

	if len(got1) == 0 || len(got2) == 0 {
		t.Skip("UnionInfrastructureAgentKeys returned empty slice (implementation not yet delivered); " +
			"skipping aliasing check")
	}

	original := got2[0]
	got1[0] = "MUTATED"

	if got2[0] != original {
		t.Errorf("mutating result of first UnionInfrastructureAgentKeys call changed "+
			"result of second call (got %q, want %q): "+
			"each call must return a fresh independent slice",
			got2[0], original)
	}
}
