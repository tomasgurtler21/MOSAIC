package app_test

// update_scan_harness_only_test.go verifies the Stage 4 requirement that, after the
// workspace-scan-driven classifier is wired in, the harness-only and catalog-match paths
// are mutually exclusive (disjoint by construction).
//
// All tests in this file represent the TDD RED phase. The current implementation uses
// scanHarnessOnlyAgents which excludes catalog-keyed files by *filename key* only. A
// renamed deployed file — one whose numeric id matches a catalog agent but whose filename key
// does not — currently passes the filename-key exclusion and is incorrectly offered as a
// harness-only agent. After Stage 4 wires scanWorkspaceAgents, the numeric-id match removes
// such files from the harness-only path, and the tests below correctly FAIL against the
// current code.
//
// Verified behaviours:
//
// Catalog-matched renamed file never in harness-only (T4.4a — RED):
//   - A deployed file whose numeric id matches a catalog agent but whose filename is different
//     from the agent's catalog key must NOT trigger QHarnessOnlyRefreshScope. After Stage 4,
//     the file is classified Matched by the workspace scan and is never offered as harness-only.
//   - Currently FAILS: scanHarnessOnlyAgents uses filename-key exclusion, so the renamed file
//     passes the check and the prompt IS asked.
//
// Unmatched eligible file still produces harness-only path (T4.4b — regression):
//   - A deployed file that fails BOTH catalog lookups but passes the two-signal eligibility
//     check (transform_version + canonical section) must still trigger QHarnessOnlyRefreshScope
//     and produce a plan item when the user answers. This asserts the harness-only path is
//     preserved after Stage 4's classifier change.
//
// Mixed: renamed catalog-matched and unmatched eligible coexist (T4.4c — RED):
//   - When the workspace has both a renamed catalog-matched file and an unmatched eligible
//     file, QHarnessOnlyRefreshScope must be asked exactly once (for the unmatched file only).
//   - Currently FAILS: the prompt is asked twice (for both files), because the renamed file
//     is not excluded by the filename-key check.

import (
	"context"
	"path/filepath"
	"testing"

	"mosaic-deploy/internal/app"
	"mosaic-deploy/internal/app/interactiontest"
	"mosaic-deploy/internal/domain"
)

// ---------------------------------------------------------------------------
// Test helpers local to this file
// ---------------------------------------------------------------------------

const scanHarnessOnlyTestAgentsDir = "agents"

// renamedCatalogAgentContent returns bytes for a deployed file that:
//   - carries numeric id "1" in frontmatter (matching minimalAgent.NumericID)
//   - carries transform_version (harness-only eligibility signal 1)
//   - carries a canonical <Identity type="core"> section (harness-only eligibility signal 2)
//
// The file is named "renamed-test-runner.md" — its filename key is "renamed-test-runner",
// which is NOT in the catalog by key. So the current scanHarnessOnlyAgents (filename-key
// exclusion) does NOT exclude it, but the Stage 4 scanWorkspaceAgents (numeric-id match)
// WILL classify it as Matched and exclude it from the harness-only path.
func renamedCatalogAgentContent() []byte {
	return []byte("---\n" +
		"id: \"1\"\n" +
		"transform_version: \"1.0.0\"\n" +
		"version: \"1.0.0\"\n" +
		"---\n" +
		"<Identity type=\"core\">\n" +
		"Renamed deployed file — numeric id matches catalog, filename key does not.\n" +
		"</Identity>\n")
}

// unmatchedEligibleContent returns bytes for an eligible harness-only agent with no numeric
// id and no filename-key match. This file should always be in the harness-only path.
func unmatchedEligibleContent() []byte {
	return []byte("---\n" +
		"transform_version: \"1.0.0\"\n" +
		"version: \"2.0.0\"\n" +
		"---\n" +
		"<Identity type=\"core\">\n" +
		"Genuinely harness-only agent with no catalog counterpart.\n" +
		"</Identity>\n")
}

// hasRefreshScopeCallForSubject reports whether stub.Calls() contains a QHarnessOnlyRefreshScope
// call whose Subject matches the given targetPath.
func hasRefreshScopeCallForSubject(stub *interactiontest.Stub, targetPath string) bool {
	for _, c := range stub.Calls() {
		if c.ID == domain.QHarnessOnlyRefreshScope && c.Subject == targetPath {
			return true
		}
	}
	return false
}

// countRefreshScopeCalls returns the total number of QHarnessOnlyRefreshScope calls recorded
// by the stub, across all subjects.
func countRefreshScopeCalls(stub *interactiontest.Stub) int {
	n := 0
	for _, c := range stub.Calls() {
		if c.ID == domain.QHarnessOnlyRefreshScope {
			n++
		}
	}
	return n
}

// ---------------------------------------------------------------------------
// T4.4a — Catalog-matched renamed file never triggers harness-only prompt (RED)
// ---------------------------------------------------------------------------

// TestUpdate_RenamedCatalogAgent_RefreshScopeNotAsked verifies that when a deployed file
// matches a catalog agent by numeric id but has a different filename (renamed), the Update
// flow does NOT ask QHarnessOnlyRefreshScope for that file.
//
// Current behaviour (RED): the file's filename key "renamed-test-runner" is not in
// catalogAgentKeys (which only contains "test-runner"), so scanHarnessOnlyAgents includes the
// file in harnessOnlyAgents, and QHarnessOnlyRefreshScope IS asked.
//
// Expected behaviour (GREEN after Stage 4): scanWorkspaceAgents resolves the numeric id "1"
// to "test-runner" (Matched), so the file never reaches the harness-only path, and
// QHarnessOnlyRefreshScope is NOT asked for it.
func TestUpdate_RenamedCatalogAgent_RefreshScopeNotAsked(t *testing.T) {
	// Arrange — workspace has a renamed catalog-backed file "renamed-test-runner.md".
	const agentsDir = scanHarnessOnlyTestAgentsDir
	const renamedFilename = "renamed-test-runner.md"

	// Do not script a refresh-scope answer for the renamed file so its absence from
	// stub.Calls() is unambiguous. (Unscripted questions return SkippedOne by default,
	// so if the question IS asked, the flow continues without error but the call is recorded.)
	stub := interactiontest.NewBuilder().Build()
	deps, workspace := newBaseDepsWithAgentsDir(t, stub, agentsDir)

	// Write the renamed deployed file into the workspace agents directory.
	writeAgentFile(t, filepath.Join(workspace, agentsDir), renamedFilename, renamedCatalogAgentContent())

	svc := app.New(deps)

	// Act
	_, _ = svc.Update(context.Background(), app.UpdateRequest{
		HarnessID:       "stub-harness",
		WorkspacePath:   workspace,
		AutoConfirmPlan: true,
	})

	// Assert — QHarnessOnlyRefreshScope must NOT have been asked for "renamed-test-runner.md".
	targetPath := filepath.Join(agentsDir, renamedFilename)
	if hasRefreshScopeCallForSubject(stub, targetPath) {
		t.Errorf("QHarnessOnlyRefreshScope was asked for %q; "+
			"a deployed file whose numeric id (1) matches a catalog agent (test-runner) "+
			"must be classified as Matched, not as harness-only, and must never trigger "+
			"the harness-only refresh-scope prompt",
			targetPath)
	}
}

// ---------------------------------------------------------------------------
// T4.4b — Unmatched eligible file still in harness-only path (regression)
// ---------------------------------------------------------------------------

// TestUpdate_UnmatchedEligibleAgent_RefreshScopeStillAsked verifies that an unmatched
// harness-only eligible file (no numeric id, no filename-key match in catalog) still triggers
// QHarnessOnlyRefreshScope after Stage 4. This is a regression test: the Stage 4 classifier
// change must not remove genuinely harness-only files from the prompt path.
func TestUpdate_UnmatchedEligibleAgent_RefreshScopeStillAsked(t *testing.T) {
	// Arrange — workspace has an unmatched eligible file "external-tool.md".
	const agentsDir = scanHarnessOnlyTestAgentsDir
	const eligibleFilename = "external-tool.md"

	targetPath := filepath.Join(agentsDir, eligibleFilename)
	stub := interactiontest.NewBuilder().
		AnswerSelectOne(domain.QHarnessOnlyRefreshScope, targetPath, string(app.RefreshProtocolOnly)).
		Build()
	deps, workspace := newBaseDepsWithAgentsDir(t, stub, agentsDir)

	writeAgentFile(t, filepath.Join(workspace, agentsDir), eligibleFilename, unmatchedEligibleContent())

	svc := app.New(deps)

	// Act
	_, _ = svc.Update(context.Background(), app.UpdateRequest{
		HarnessID:       "stub-harness",
		WorkspacePath:   workspace,
		AutoConfirmPlan: true,
	})

	// Assert — QHarnessOnlyRefreshScope must have been asked for "external-tool.md".
	if !hasRefreshScopeCallForSubject(stub, targetPath) {
		t.Errorf("QHarnessOnlyRefreshScope was NOT asked for %q; "+
			"an unmatched harness-only eligible file must still be prompted for refresh scope "+
			"after Stage 4 — the classifier change must not remove genuinely harness-only files "+
			"from the harness-only path", targetPath)
	}
}

// ---------------------------------------------------------------------------
// T4.4c — Mixed workspace: one call only, for the unmatched file (RED)
// ---------------------------------------------------------------------------

// TestUpdate_MixedWorkspace_RefreshScopeAskedOnceForUnmatchedOnly verifies that when the
// agents directory contains both a renamed catalog-matched file and an unmatched eligible
// file, QHarnessOnlyRefreshScope is asked exactly once — for the unmatched file — and never
// for the renamed catalog-matched file.
//
// Current behaviour (RED): the prompt is asked twice, for both files, because the renamed
// file passes the filename-key exclusion check in scanHarnessOnlyAgents.
//
// Expected behaviour (GREEN after Stage 4): the renamed file is classified as Matched by
// numeric-id lookup and is excluded from the harness-only path, so the prompt is asked once.
func TestUpdate_MixedWorkspace_RefreshScopeAskedOnceForUnmatchedOnly(t *testing.T) {
	// Arrange — workspace has both a renamed catalog-backed file and an unmatched eligible file.
	const agentsDir = scanHarnessOnlyTestAgentsDir
	const renamedFilename = "renamed-test-runner.md"
	const eligibleFilename = "external-tool.md"

	renamedTargetPath := filepath.Join(agentsDir, renamedFilename)
	eligibleTargetPath := filepath.Join(agentsDir, eligibleFilename)

	stub := interactiontest.NewBuilder().
		AnswerSelectOne(domain.QHarnessOnlyRefreshScope, eligibleTargetPath, string(app.RefreshProtocolOnly)).
		Build()
	deps, workspace := newBaseDepsWithAgentsDir(t, stub, agentsDir)

	writeAgentFile(t, filepath.Join(workspace, agentsDir), renamedFilename, renamedCatalogAgentContent())
	writeAgentFile(t, filepath.Join(workspace, agentsDir), eligibleFilename, unmatchedEligibleContent())

	svc := app.New(deps)

	// Act
	_, _ = svc.Update(context.Background(), app.UpdateRequest{
		HarnessID:       "stub-harness",
		WorkspacePath:   workspace,
		AutoConfirmPlan: true,
	})

	// Assert 1 — QHarnessOnlyRefreshScope must NOT have been asked for the renamed file.
	if hasRefreshScopeCallForSubject(stub, renamedTargetPath) {
		t.Errorf("QHarnessOnlyRefreshScope was asked for %q (renamed catalog-matched file); "+
			"a file whose numeric id matches a catalog agent must be classified as Matched, "+
			"never as harness-only, and must not trigger the refresh-scope prompt",
			renamedTargetPath)
	}

	// Assert 2 — QHarnessOnlyRefreshScope must have been asked for the unmatched eligible file.
	if !hasRefreshScopeCallForSubject(stub, eligibleTargetPath) {
		t.Errorf("QHarnessOnlyRefreshScope was NOT asked for %q (unmatched eligible file); "+
			"a genuinely harness-only file must still trigger the refresh-scope prompt "+
			"after Stage 4", eligibleTargetPath)
	}

	// Assert 3 — total call count must be 1 (only for the unmatched file).
	if n := countRefreshScopeCalls(stub); n != 1 {
		t.Errorf("QHarnessOnlyRefreshScope was asked %d times, want 1; "+
			"only the unmatched eligible file should trigger the prompt, "+
			"not the renamed catalog-matched file", n)
	}
}

// ---------------------------------------------------------------------------
// T4.4d — Catalog match by numeric id gates harness-only even when filename key absent
// ---------------------------------------------------------------------------

// TestUpdate_NumericIDCatalogMatch_ExcludesFromHarnessOnly verifies that numeric-id
// matching is sufficient to exclude a file from the harness-only path, even when the file's
// filename key is completely absent from the catalog. This pins the correct priority of the
// two-step lookup: numeric id first, filename key fallback second. The harness-only path
// only runs when BOTH lookups fail.
func TestUpdate_NumericIDCatalogMatch_ExcludesFromHarnessOnly(t *testing.T) {
	// Arrange — "completely-unknown-name.md" has id "1" (matches test-runner by numeric id)
	// but its filename key "completely-unknown-name" has no catalog entry.
	const agentsDir = scanHarnessOnlyTestAgentsDir
	const unknownFilename = "completely-unknown-name.md"

	stub := interactiontest.NewBuilder().Build()
	deps, workspace := newBaseDepsWithAgentsDir(t, stub, agentsDir)

	writeAgentFile(t, filepath.Join(workspace, agentsDir), unknownFilename, renamedCatalogAgentContent())

	svc := app.New(deps)

	// Act
	_, _ = svc.Update(context.Background(), app.UpdateRequest{
		HarnessID:       "stub-harness",
		WorkspacePath:   workspace,
		AutoConfirmPlan: true,
	})

	// Assert — QHarnessOnlyRefreshScope must NOT have been asked for this file.
	targetPath := filepath.Join(agentsDir, unknownFilename)
	if hasRefreshScopeCallForSubject(stub, targetPath) {
		t.Errorf("QHarnessOnlyRefreshScope was asked for %q; "+
			"numeric-id match (id=1 → test-runner) must exclude the file from the "+
			"harness-only path even when its filename key has no catalog entry",
			targetPath)
	}
}

// ---------------------------------------------------------------------------
// T4.4e — os.DirFS guard: no harness-only prompt when agents dir not configured
// ---------------------------------------------------------------------------

// TestUpdate_NoAgentsDir_NoRefreshScopePromptAsked verifies that when the harness module
// does not declare an agents directory (Paths.Agents.Supported == false), no
// QHarnessOnlyRefreshScope call is made. This asserts that the scan guard is still respected
// after Stage 4.
func TestUpdate_NoAgentsDir_NoRefreshScopePromptAsked(t *testing.T) {
	// Arrange — standard minimal deps, no agents dir configured (default module).
	stub := interactiontest.NewBuilder().Build()
	deps, workspace := newBaseDeps(t, stub)

	svc := app.New(deps)

	// Act
	_, _ = svc.Update(context.Background(), app.UpdateRequest{
		HarnessID:       "stub-harness",
		WorkspacePath:   workspace,
		AutoConfirmPlan: true,
	})

	// Assert — QHarnessOnlyRefreshScope must never have been asked.
	if n := countRefreshScopeCalls(stub); n != 0 {
		t.Errorf("QHarnessOnlyRefreshScope was asked %d times, want 0; "+
			"when no agents directory is configured, no harness-only scan runs "+
			"and the refresh-scope prompt must never appear", n)
	}
}
