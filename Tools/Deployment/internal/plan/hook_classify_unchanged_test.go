package plan_test

// hook_classify_unchanged_test.go covers classifyHookItem for the ActionUnchanged and
// ActionUpdate outcomes, focusing on the manifest-version-based staleness path introduced
// by the deployed-state presence fix.
//
// T3.3 — already-deployed hook bundle is not classified as new:
//
//   The pre-fix defect is that probeDeployedArtifact always yields Present: false for
//   directories, making every hook bundle appear absent and therefore classify as ActionCreate.
//   After the fix, probeDeployedHookBundle yields Present: true for a non-empty directory, and
//   classifyHookItem uses HookStalenessFromRecorded(entry.Version, hook.Version) rather than
//   HookStaleness(deployed, hook) + !deployed.HasVersionInfo() fallback.
//
//   These tests exercise that behaviour end-to-end through plan.Planner.Build, using a
//   manually constructed DeployedState that matches what probeDeployedHookBundle will return:
//
//     - Present: true
//     - ContentHash: manifest.Hash(nil)  (the executor's nil-hash convention for hooks)
//     - All version fields empty          (no in-file version marker for a directory)
//
//   With that DeployedState and a manifest entry whose Version matches the source bundle
//   version, the plan item must be ActionUnchanged.
//
//   Without the fix (current code), the same setup produces ActionUpdate because:
//     - HookStaleness compares deployed.Version ("") against hook.Version ("1.0") → delta
//     - OR !deployed.HasVersionInfo() is true → fallback to update
//
// These tests will fail (ActionUpdate instead of ActionUnchanged) until I3.3 is implemented,
// which is the intended TDD RED state.

import (
	"context"
	"strings"
	"testing"

	"mosaic-deploy/internal/domain"
	"mosaic-deploy/internal/manifest"
	"mosaic-deploy/internal/plan"
)

// ---------------------------------------------------------------------------
// Helpers for hook classification tests
// ---------------------------------------------------------------------------

// hookProbeState returns a DeployedArtifactState as probeDeployedHookBundle will produce for a
// present hook bundle: Present true, ContentHash == manifest.Hash(nil), all version fields empty.
// This is the standard deployed state for a hook whose target directory exists and is non-empty.
func hookProbeState() domain.DeployedArtifactState {
	return domain.DeployedArtifactState{
		Present:     true,
		ContentHash: manifest.Hash(nil),
		// All version fields intentionally empty: hook directories carry no in-file version marker.
	}
}

// buildInputWithHook returns a plan.Input ready for Build with one hook bundle included.
// targetPath is the harness-computed path for the hook. deployedState and snap control the
// classification scenario.
func buildInputWithHook(
	hook domain.HookBundle,
	targetPath string,
	snap manifest.Snapshot,
	deployedState map[string]domain.DeployedArtifactState,
) plan.Input {
	cat := &fakeCatalog{
		orchestrator: makeOrchestrator(),
		hooks:        []domain.HookBundle{hook},
	}

	// Custom target path function: hooks get the explicit targetPath; others get defaults.
	module := newFakeModule()
	module.TargetPathFn = func(req domain.TargetPathRequest) (string, error) {
		if req.Kind == domain.ArtifactHook && req.Key == hook.Key {
			return targetPath, nil
		}
		switch req.Kind {
		case domain.ArtifactAgent:
			return "agents/" + req.Key + ".md", nil
		case domain.ArtifactSkill:
			return "skills/" + req.Key + "/SKILL.md", nil
		}
		return req.Key, nil
	}

	return plan.Input{
		Catalog:       cat,
		Module:        module,
		Mode:          domain.ModeDeployWorkspace,
		WorkspacePath: "/fake/workspace",
		Scope:         domain.ScopeProject,
		GOOS:          "linux",
		Manifest:      snap,
		HookIDs:       []string{hook.Key},
		Models: map[string]domain.ModelSelection{
			"orchestrator": {ModelID: "test-model", Origin: domain.OriginHarnessList},
		},
		DeployedState: deployedState,
	}
}

// hookManifestEntry returns a ManifestEntry for the given hook bundle at the given target path,
// using manifest.Hash(nil) as the content hash (the executor's convention for hook bundles).
func hookManifestEntry(hook domain.HookBundle, targetPath string) domain.ManifestEntry {
	return domain.ManifestEntry{
		Ref:        domain.ArtifactRef{Kind: domain.ArtifactHook, Key: hook.Key},
		TargetPath: targetPath,
		Version:    hook.Version, // recorded version matches source → not stale
		ContentHash: manifest.Hash(nil),
	}
}

// ---------------------------------------------------------------------------
// T3.3 — already-deployed hook bundle classifies as ActionUnchanged
// ---------------------------------------------------------------------------

// TestBuild_PresentHookBundleWithMatchingVersion_ClassifiesAsUnchanged verifies that when a
// hook bundle's target directory is present (Present: true, ContentHash: manifest.Hash(nil),
// all version fields empty) and the manifest's recorded version matches the source bundle
// version, the plan item classifies as ActionUnchanged with an empty Stale slice and a Reason
// that does not mention missing version stamps.
//
// This is the direct regression lock for FR-16: an already-deployed, unmodified hook bundle
// must not be reported as a new create or a spurious update on every subsequent run.
func TestBuild_PresentHookBundleWithMatchingVersion_ClassifiesAsUnchanged(t *testing.T) {
	const hookTarget = "hooks/pre-commit"
	hook := makeHookBundle("pre-commit", "1.0")
	entry := hookManifestEntry(hook, hookTarget)
	snap := presentSnapshot(domain.Manifest{Entries: []domain.ManifestEntry{entry}})
	deployed := map[string]domain.DeployedArtifactState{
		hookTarget: hookProbeState(),
	}

	in := buildInputWithHook(hook, hookTarget, snap, deployed)
	p, err := plan.New().Build(context.Background(), in)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	item, ok := findItem(p.Items, "pre-commit")
	if !ok {
		t.Fatal("plan has no item for hook pre-commit")
	}

	if item.Action != domain.ActionUnchanged {
		t.Errorf("hook with matching manifest version: Action = %q, want ActionUnchanged; "+
			"a present, unmodified hook bundle must not be reported as stale or new",
			item.Action)
	}

	if len(item.Stale) != 0 {
		t.Errorf("expected empty Stale slice for ActionUnchanged hook, got %v", item.Stale)
	}
}

// TestBuild_PresentHookBundleWithMatchingVersion_ReasonIsEmpty verifies that the Reason field
// is empty for an ActionUnchanged hook. An unchanged item has nothing to explain to the user.
func TestBuild_PresentHookBundleWithMatchingVersion_ReasonIsEmpty(t *testing.T) {
	const hookTarget = "hooks/pre-commit"
	hook := makeHookBundle("pre-commit", "1.0")
	entry := hookManifestEntry(hook, hookTarget)
	snap := presentSnapshot(domain.Manifest{Entries: []domain.ManifestEntry{entry}})
	deployed := map[string]domain.DeployedArtifactState{
		hookTarget: hookProbeState(),
	}

	in := buildInputWithHook(hook, hookTarget, snap, deployed)
	p, err := plan.New().Build(context.Background(), in)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	item, ok := findItem(p.Items, "pre-commit")
	if !ok {
		t.Fatal("plan has no item for hook pre-commit")
	}
	if item.Action != domain.ActionUnchanged {
		t.Skipf("skipping Reason check: Action = %q (expected ActionUnchanged)", item.Action)
	}

	if item.Reason != "" {
		t.Errorf("Reason = %q, want empty for ActionUnchanged hook", item.Reason)
	}
}

// TestBuild_PresentHookBundleWithMatchingVersion_ReasonDoesNotMentionVersionStamps verifies
// that the Reason does not contain the "no readable version stamps" clause that the pre-fix
// step-7 fallback injected. After the fix, the manifest-recorded version is the sole version
// signal for hooks; the absence of on-disk version stamps is not surfaced to the user.
func TestBuild_PresentHookBundleWithMatchingVersion_ReasonDoesNotMentionVersionStamps(t *testing.T) {
	const hookTarget = "hooks/pre-commit"
	hook := makeHookBundle("pre-commit", "1.0")
	entry := hookManifestEntry(hook, hookTarget)
	snap := presentSnapshot(domain.Manifest{Entries: []domain.ManifestEntry{entry}})
	deployed := map[string]domain.DeployedArtifactState{
		hookTarget: hookProbeState(),
	}

	in := buildInputWithHook(hook, hookTarget, snap, deployed)
	p, err := plan.New().Build(context.Background(), in)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	item, ok := findItem(p.Items, "pre-commit")
	if !ok {
		t.Fatal("plan has no item for hook pre-commit")
	}

	if strings.Contains(item.Reason, "no readable version stamps") {
		t.Errorf("Reason %q must not contain 'no readable version stamps'; "+
			"the no-version-info fallback must be bypassed on the hook path",
			item.Reason)
	}
}

// ---------------------------------------------------------------------------
// T3.3 — advanced source version classifies as ActionUpdate
// ---------------------------------------------------------------------------

// TestBuild_PresentHookBundleWithAdvancedVersion_ClassifiesAsUpdate verifies that when the
// source bundle version has advanced beyond the manifest's recorded version, the plan item
// classifies as ActionUpdate with exactly one "version" delta.
//
// This is the companion to the ActionUnchanged test: the manifest-version comparison must
// correctly detect staleness when it exists, so the fix does not mask legitimate updates.
func TestBuild_PresentHookBundleWithAdvancedVersion_ClassifiesAsUpdate(t *testing.T) {
	const hookTarget = "hooks/pre-commit"
	hook := makeHookBundle("pre-commit", "2.0") // source at v2.0
	// Manifest records v1.0 — the previously deployed version.
	entry := domain.ManifestEntry{
		Ref:         domain.ArtifactRef{Kind: domain.ArtifactHook, Key: "pre-commit"},
		TargetPath:  hookTarget,
		Version:     "1.0", // recorded version differs from source
		ContentHash: manifest.Hash(nil),
	}
	snap := presentSnapshot(domain.Manifest{Entries: []domain.ManifestEntry{entry}})
	deployed := map[string]domain.DeployedArtifactState{
		hookTarget: hookProbeState(),
	}

	in := buildInputWithHook(hook, hookTarget, snap, deployed)
	p, err := plan.New().Build(context.Background(), in)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	item, ok := findItem(p.Items, "pre-commit")
	if !ok {
		t.Fatal("plan has no item for hook pre-commit")
	}

	if item.Action != domain.ActionUpdate {
		t.Errorf("hook with advanced source version: Action = %q, want ActionUpdate", item.Action)
	}
}

// TestBuild_PresentHookBundleWithAdvancedVersion_ExactlyOneVersionDelta verifies that the
// ActionUpdate item for an advanced hook version contains exactly one delta with Field "version".
func TestBuild_PresentHookBundleWithAdvancedVersion_ExactlyOneVersionDelta(t *testing.T) {
	const hookTarget = "hooks/pre-commit"
	hook := makeHookBundle("pre-commit", "2.0")
	entry := domain.ManifestEntry{
		Ref:         domain.ArtifactRef{Kind: domain.ArtifactHook, Key: "pre-commit"},
		TargetPath:  hookTarget,
		Version:     "1.0",
		ContentHash: manifest.Hash(nil),
	}
	snap := presentSnapshot(domain.Manifest{Entries: []domain.ManifestEntry{entry}})
	deployed := map[string]domain.DeployedArtifactState{
		hookTarget: hookProbeState(),
	}

	in := buildInputWithHook(hook, hookTarget, snap, deployed)
	p, err := plan.New().Build(context.Background(), in)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	item, ok := findItem(p.Items, "pre-commit")
	if !ok {
		t.Fatal("plan has no item for hook pre-commit")
	}
	if item.Action != domain.ActionUpdate {
		t.Skipf("skipping delta check: Action = %q (expected ActionUpdate)", item.Action)
	}

	if len(item.Stale) != 1 {
		t.Fatalf("expected exactly 1 stale delta for an advanced hook version, got %d: %v",
			len(item.Stale), item.Stale)
	}
	if item.Stale[0].Field != "version" {
		t.Errorf("stale delta Field = %q, want %q", item.Stale[0].Field, "version")
	}
}

// ---------------------------------------------------------------------------
// AC2.3 — Hook hash mismatch retains ActionConflict (Step 4 not removed for hooks)
// ---------------------------------------------------------------------------

// TestBuild_HookHashMismatch_StillClassifiesAsConflict verifies that when a hook bundle's
// deployed content hash differs from the hash recorded in the manifest, the plan item
// classifies as ActionConflict. The Step 4 whole-file hash check is retained in
// classifyHookItem -- Stage 2 removes Step 4 only from classifyAgentItem and
// classifySkillItem. This test guards against accidental removal of the hook hash check
// during the Stage 2 implementation and must pass both before and after I2.1.
func TestBuild_HookHashMismatch_StillClassifiesAsConflict(t *testing.T) {
	const hookTarget = "hooks/pre-commit"
	const recordedHash = "sha256:recorded-hook-hash"
	const currentHash = "sha256:modified-hook-hash" // differs from recordedHash -- hash mismatch

	hook := makeHookBundle("pre-commit", "1.0")

	entry := domain.ManifestEntry{
		Ref:         domain.ArtifactRef{Kind: domain.ArtifactHook, Key: "pre-commit"},
		TargetPath:  hookTarget,
		Version:     hook.Version,
		ContentHash: recordedHash,
	}
	snap := presentSnapshot(domain.Manifest{Entries: []domain.ManifestEntry{entry}})

	// Deployed hash differs from the manifest-recorded hash.
	// For hooks, Step 4 is retained: this must still produce ActionConflict.
	deployed := map[string]domain.DeployedArtifactState{
		hookTarget: {
			Present:     true,
			ContentHash: currentHash,
		},
	}

	in := buildInputWithHook(hook, hookTarget, snap, deployed)
	p, err := plan.New().Build(context.Background(), in)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	item, ok := findItem(p.Items, "pre-commit")
	if !ok {
		t.Fatal("plan has no item for hook pre-commit")
	}

	if item.Action != domain.ActionConflict {
		t.Errorf("hook with hash mismatch: Action = %q, want ActionConflict; "+
			"the Step 4 hash check must be retained for hooks -- only agents and skills "+
			"have Step 4 removed in Stage 2. A hook hash mismatch must still classify as "+
			"ActionConflict to guard against accidental local modification of hook files.",
			item.Action)
	}
}

// TestBuild_PresentHookBundleWithAdvancedVersion_ReasonDoesNotMentionVersionStamps verifies
// that even the ActionUpdate case does not include the "no readable version stamps" clause.
// The no-version-info fallback must be dropped on the hook path regardless of whether the hook
// is stale or unchanged.
func TestBuild_PresentHookBundleWithAdvancedVersion_ReasonDoesNotMentionVersionStamps(t *testing.T) {
	const hookTarget = "hooks/pre-commit"
	hook := makeHookBundle("pre-commit", "2.0")
	entry := domain.ManifestEntry{
		Ref:         domain.ArtifactRef{Kind: domain.ArtifactHook, Key: "pre-commit"},
		TargetPath:  hookTarget,
		Version:     "1.0",
		ContentHash: manifest.Hash(nil),
	}
	snap := presentSnapshot(domain.Manifest{Entries: []domain.ManifestEntry{entry}})
	deployed := map[string]domain.DeployedArtifactState{
		hookTarget: hookProbeState(),
	}

	in := buildInputWithHook(hook, hookTarget, snap, deployed)
	p, err := plan.New().Build(context.Background(), in)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	item, ok := findItem(p.Items, "pre-commit")
	if !ok {
		t.Fatal("plan has no item for hook pre-commit")
	}

	if strings.Contains(item.Reason, "no readable version stamps") {
		t.Errorf("Reason %q must not contain 'no readable version stamps' on the hook path; "+
			"only the version delta reason should appear", item.Reason)
	}
}
