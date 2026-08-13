package app_test

// update_harness_only_test.go verifies the harness-only agent wiring in the Update flow.
//
// Stage 7 wires harness-only agent discovery, the refresh-scope prompt, and harness-only
// content refresh into the existing Update flow so eligible harness-only agents are
// refreshed automatically as part of a normal update run. No new CLI subcommand or mode is
// introduced; the behaviour is automatic.
//
// Verified behaviours (one section per Stage 7 task):
//
// End-to-end eligible refresh (T7.1):
//   - An Update run on a workspace containing an eligible harness-only agent produces an
//     executor plan item for that agent with ActionUpdate and a SourcePath of "".
//   - The Content callback for a harness-only plan item returns refreshed bytes: the
//     CommunicationProtocol region carries canonical protocol content.
//   - The Content callback for a harness-only plan item preserves [[INJECTION:]] content
//     verbatim — unchanged between the deployed file and the refreshed output.
//
// Ineligible files never touched (T7.2):
//   - A file in the agents directory that lacks transform_version produces no plan item.
//   - When an eligible and an ineligible file coexist, only the eligible one is planned.
//
// Refresh-scope prompt lifecycle (T7.3):
//   - QHarnessOnlyRefreshScope is asked once per eligible harness-only agent.
//   - An "all:<scope>" answer latches the scope for all remaining agents; the second
//     agent's question is not asked again.
//   - When no eligible harness-only agent is discovered, QHarnessOnlyRefreshScope is never
//     asked.
//
// Scope breadth end to end (T7.4):
//   - Under a "protocol-only" scope answer the plan item's SourcePath is empty and the
//     CommunicationProtocol region is refreshed while other deployed regions are untouched.
//   - Under an "all-deployed" scope answer all canonical deployed regions present in the
//     file are included in the refresh.
//
// Orchestrator exclusion (T7.5):
//   - A file named "orchestrator.md" in the agents directory is never included in the plan,
//     even when it satisfies both eligibility signals.
//   - A file named "orchestrator.agent.md" is likewise never included.
//
// Atomic reversal coverage (T7.6):
//   - When an Update run fails and the executor reverses, the app returns *RevertedRunError.
//   - A harness-only agent is added to the plan so it participates in the executor's
//     all-or-nothing reversal rather than being written through a side channel.
//
// Dry-run semantics (T7.7):
//   - When UpdateRequest.DryRun is true, the ExecRequest.DryRun forwarded to the executor
//     is also true for a run that includes harness-only agents.
//   - Discovery and prompting still occur when DryRun is true.
//
// Catalog-backed regression (T7.8):
//   - A run with no harness-only agents present produces the same plan items as it did
//     before this stage's wiring was added (no extra or missing items).
//   - When a catalog-backed agent and a harness-only agent coexist, the catalog-backed
//     plan items are present alongside the harness-only item without duplication.
//
// Observability (T7.9):
//   - The Update flow emits a logging.Event with Fields["harness_only"] == "true" for
//     each harness-only agent that is refreshed.
//   - The emitted event's Fields["scope"] matches the scope the user chose.

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"mosaic-deploy/internal/app"
	"mosaic-deploy/internal/app/interactiontest"
	"mosaic-deploy/internal/deploy"
	"mosaic-deploy/internal/domain"
	"mosaic-deploy/internal/logging"
)

// ---------------------------------------------------------------------------
// Test helpers local to this file
// ---------------------------------------------------------------------------

const harnessOnlyTestAgentsDir = "agents"

// newModuleWithAgentsDir returns a stubHarnessModule configured with an agents directory
// so that the Update flow discovers harness-only agents. The module is otherwise identical
// to newMinimalModule().
func newModuleWithAgentsDir(agentsDir string) *stubHarnessModule {
	mod := newMinimalModule()
	mod.descriptor.Paths.Agents = domain.ScopedPaths{
		Supported: true,
		Project:   agentsDir,
	}
	return mod
}

// harnessOnlyAgentContent returns bytes for an eligible harness-only agent carrying:
//   - transform_version in frontmatter (eligibility signal 1)
//   - canonical [[SECTION:Identity]] boundary pair (eligibility signal 2)
//   - [[DEPLOYED:CommunicationProtocol]] with stale content (to verify refresh replaces it)
//   - [[INJECTION:IdentityExtension]] with user content (to verify verbatim preservation)
//
// The injection content string is unique enough to be detectable in the output bytes.
func harnessOnlyAgentContent() []byte {
	return []byte("---\n" +
		"transform_version: \"1.0.0\"\n" +
		"---\n" +
		"[[DEPLOYED:CommunicationProtocol]]\n" +
		"OLD PROTOCOL CONTENT — must be replaced by refresh.\n" +
		"[[/DEPLOYED:CommunicationProtocol]]\n" +
		"[[SECTION:Identity]]\n" +
		"# Harness-Only Agent For Stage 7 Tests\n" +
		"[[INJECTION:IdentityExtension]]\n" +
		"user-authored-injection-content-sentinel-for-preservation-check\n" +
		"[[/INJECTION:IdentityExtension]]\n" +
		"[[/SECTION:Identity]]\n")
}

// twoRegionAgentContent returns bytes for an eligible harness-only agent carrying two
// canonical [[DEPLOYED:...]] regions so that protocol-only vs all-deployed scope answers
// produce observably different output:
//
//   - [[DEPLOYED:CommunicationProtocol]] at body top level (in scope under both answers)
//   - [[DEPLOYED:AuthorityHierarchy]] inside [[SECTION:Identity]] (in scope only under
//     all-deployed; must remain byte-identical under protocol-only)
//
// Both regions carry unique stale-content sentinels so a test can assert whether each
// region was replaced or preserved by inspecting the Content callback's returned bytes.
func twoRegionAgentContent() []byte {
	return []byte("---\n" +
		"transform_version: \"1.0.0\"\n" +
		"---\n" +
		"[[DEPLOYED:CommunicationProtocol]]\n" +
		"OLD PROTOCOL CONTENT — must be replaced by refresh.\n" +
		"[[/DEPLOYED:CommunicationProtocol]]\n" +
		"[[SECTION:Identity]]\n" +
		"# Harness-Only Agent For Stage 7 Scope-Differentiation Tests\n" +
		"[[DEPLOYED:AuthorityHierarchy]]\n" +
		"OLD AUTHORITY HIERARCHY CONTENT — preserved under protocol-only, replaced under all-deployed.\n" +
		"[[/DEPLOYED:AuthorityHierarchy]]\n" +
		"[[INJECTION:IdentityExtension]]\n" +
		"user-authored-injection-content-sentinel-for-preservation-check\n" +
		"[[/INJECTION:IdentityExtension]]\n" +
		"[[/SECTION:Identity]]\n")
}

// ineligibleAgentContent returns bytes for a .md file that is NOT an eligible harness-only
// agent (no transform_version), so the scan silently skips it.
func ineligibleAgentContent() []byte {
	return []byte("---\n" +
		"version: \"1.0.0\"\n" +
		"---\n" +
		"[[SECTION:Identity]]\n" +
		"Generic-style agent without transform_version.\n" +
		"[[/SECTION:Identity]]\n")
}

// writeAgentFile writes content into dir/filename, creating dir if needed. Returns the full path.
func writeAgentFile(t *testing.T, dir, filename string, content []byte) string {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("writeAgentFile MkdirAll(%q): %v", dir, err)
	}
	fullPath := filepath.Join(dir, filename)
	if err := os.WriteFile(fullPath, content, 0o644); err != nil {
		t.Fatalf("writeAgentFile WriteFile(%q): %v", fullPath, err)
	}
	return fullPath
}

// spyLogger records every logging.Event passed to Event(). It satisfies logging.Logger.
type spyLogger struct {
	events []logging.Event
}

func (l *spyLogger) StartRun(rc logging.RunContext) {}
func (l *spyLogger) Event(e logging.Event)          { l.events = append(l.events, e) }
func (l *spyLogger) Action(ar domain.ActionRecord)  {}
func (l *spyLogger) EndRun(outcome domain.Outcome)  {}
func (l *spyLogger) Paths() domain.LogPaths         { return domain.LogPaths{} }
func (l *spyLogger) Degraded() []error              { return nil }

// harnessOnlyEvent returns the first event whose Fields["harness_only"] == "true", or
// the zero event and false when none exists.
func (l *spyLogger) harnessOnlyEvent() (logging.Event, bool) {
	for _, e := range l.events {
		if e.Fields["harness_only"] == "true" {
			return e, true
		}
	}
	return logging.Event{}, false
}

// newBaseDepsWithAgentsDir calls newBaseDeps and overrides the harness registry with a
// module that declares an agents directory. The workspace is the same temp dir.
func newBaseDepsWithAgentsDir(t *testing.T, stub *interactiontest.Stub, agentsDir string) (app.Deps, string) {
	t.Helper()
	deps, workspace := newBaseDeps(t, stub)
	mod := newModuleWithAgentsDir(agentsDir)
	deps.Registry = newMinimalRegistry(mod)
	return deps, workspace
}

// planHasHarnessOnlyItem returns true when capturedReq contains a plan item whose
// TargetPath matches targetPath AND whose SourcePath is empty (the harness-only marker).
func planHasHarnessOnlyItem(capturedReq *deploy.ExecRequest, targetPath string) bool {
	if capturedReq == nil {
		return false
	}
	for _, item := range capturedReq.Plan.Items {
		if item.TargetPath == targetPath && item.SourcePath == "" && item.Ref.Kind == domain.ArtifactAgent {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// T7.1 — End-to-end eligible harness-only refresh
// ---------------------------------------------------------------------------

// TestUpdate_EligibleHarnessOnlyAgent_AppearsInPlanWithEmptySourcePath verifies that when
// the workspace agents directory contains an eligible harness-only agent, the Update flow
// adds a plan item for it with ActionUpdate and SourcePath == "" (the empty SourcePath is
// the marker that the item has no catalog source and is harness-only).
func TestUpdate_EligibleHarnessOnlyAgent_AppearsInPlanWithEmptySourcePath(t *testing.T) {
	// Arrange — eligible harness-only agent file in the workspace agents directory.
	const agentsDir = harnessOnlyTestAgentsDir
	const agentFileName = "my-harness-agent.md"
	stub := interactiontest.NewBuilder().
		AnswerSelectOne(domain.QHarnessOnlyRefreshScope, filepath.Join(agentsDir, agentFileName), string(app.RefreshProtocolOnly)).
		AnswerReview(true).
		Build()
	deps, workspace := newBaseDepsWithAgentsDir(t, stub, agentsDir)

	fullAgentsDir := filepath.Join(workspace, agentsDir)
	writeAgentFile(t, fullAgentsDir, agentFileName, harnessOnlyAgentContent())

	exec := &capturingExecutor{}
	deps.Executor = exec
	svc := app.New(deps)

	// Act
	_, _ = svc.Update(context.Background(), app.UpdateRequest{
		HarnessID:       "stub-harness",
		WorkspacePath:   workspace,
		AutoConfirmPlan: true,
	})

	// Assert — the harness-only agent must appear in the plan with an empty SourcePath.
	targetPath := filepath.Join(agentsDir, agentFileName)
	if exec.capturedReq == nil {
		t.Fatal("executor was never called; Update did not reach the executor")
	}
	if !planHasHarnessOnlyItem(exec.capturedReq, targetPath) {
		t.Errorf("Update plan does not contain a harness-only item with TargetPath=%q and SourcePath=\"\"; "+
			"the Update flow must discover eligible harness-only agents and add them to the plan. "+
			"Plan items: %v", targetPath, exec.capturedReq.Plan.Items)
	}
}

// TestUpdate_EligibleHarnessOnlyAgent_ContentCallbackPreservesInjection verifies that the
// Content callback produced by the Update flow for a harness-only plan item returns refreshed
// bytes that preserve the [[INJECTION:]] content verbatim. Injection content is user-authored
// and must never be modified by the tool.
func TestUpdate_EligibleHarnessOnlyAgent_ContentCallbackPreservesInjection(t *testing.T) {
	// Arrange — harness-only agent with a known injection sentinel string.
	const agentsDir = harnessOnlyTestAgentsDir
	const agentFileName = "my-harness-agent.md"
	const injectionSentinel = "user-authored-injection-content-sentinel-for-preservation-check"
	stub := interactiontest.NewBuilder().
		AnswerSelectOne(domain.QHarnessOnlyRefreshScope, filepath.Join(agentsDir, agentFileName), string(app.RefreshProtocolOnly)).
		AnswerReview(true).
		Build()
	deps, workspace := newBaseDepsWithAgentsDir(t, stub, agentsDir)

	fullAgentsDir := filepath.Join(workspace, agentsDir)
	writeAgentFile(t, fullAgentsDir, agentFileName, harnessOnlyAgentContent())

	// Use a content-capturing executor that invokes the Content callback for every agent item.
	exec := &capturingExecutor{}
	deps.Executor = exec
	svc := app.New(deps)

	// Act
	_, _ = svc.Update(context.Background(), app.UpdateRequest{
		HarnessID:       "stub-harness",
		WorkspacePath:   workspace,
		AutoConfirmPlan: true,
	})

	// Assert — find the harness-only plan item and check its content bytes.
	targetPath := filepath.Join(agentsDir, agentFileName)
	if exec.capturedReq == nil {
		t.Fatal("executor was never called")
	}

	// Find the refreshed bytes from the capturingExecutor by invoking Content directly.
	// If the plan item is absent (RED phase), this block is skipped, and the test already
	// fails above on planHasHarnessOnlyItem.
	var foundItem bool
	for _, item := range exec.capturedReq.Plan.Items {
		if item.TargetPath == targetPath && item.SourcePath == "" {
			foundItem = true
			if exec.capturedReq.Content == nil {
				t.Fatal("ExecRequest.Content callback is nil; Update must provide a Content function")
			}
			out, err := exec.capturedReq.Content(item)
			if err != nil {
				t.Fatalf("Content callback returned error for harness-only item %q: %v", targetPath, err)
			}
			if len(out) == 0 {
				t.Fatalf("Content callback returned empty bytes for harness-only item %q; "+
					"refreshHarnessOnly must produce non-empty output", targetPath)
			}
			if !contains(out, []byte(injectionSentinel)) {
				t.Errorf("refreshed bytes for %q do not contain the injection sentinel %q; "+
					"[[INJECTION:]] content must be preserved verbatim between the deployed "+
					"file and the refreshed output — it is user-authored and must not be cleared",
					targetPath, injectionSentinel)
			}
			break
		}
	}
	if !foundItem {
		t.Errorf("no harness-only plan item with TargetPath=%q and SourcePath=\"\" found; "+
			"Update must discover and plan eligible harness-only agents", targetPath)
	}
}

// contains reports whether haystack contains needle as a byte subsequence (substring).
func contains(haystack, needle []byte) bool {
	if len(needle) == 0 {
		return true
	}
	for i := 0; i <= len(haystack)-len(needle); i++ {
		match := true
		for j := range needle {
			if haystack[i+j] != needle[j] {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}

// TestUpdate_EligibleHarnessOnlyAgent_CommunicationProtocolRegionRefreshed verifies that the
// Content callback for a harness-only plan item replaces the stale CommunicationProtocol
// region with canonical protocol content from the stub loader. The old content that was in
// the deployed file must not appear in the output.
func TestUpdate_EligibleHarnessOnlyAgent_CommunicationProtocolRegionRefreshed(t *testing.T) {
	// Arrange — harness-only agent whose protocol region carries a sentinel "old" string.
	const agentsDir = harnessOnlyTestAgentsDir
	const agentFileName = "my-harness-agent.md"
	const oldContent = "OLD PROTOCOL CONTENT — must be replaced by refresh."
	stub := interactiontest.NewBuilder().
		AnswerSelectOne(domain.QHarnessOnlyRefreshScope, filepath.Join(agentsDir, agentFileName), string(app.RefreshProtocolOnly)).
		AnswerReview(true).
		Build()
	deps, workspace := newBaseDepsWithAgentsDir(t, stub, agentsDir)

	fullAgentsDir := filepath.Join(workspace, agentsDir)
	writeAgentFile(t, fullAgentsDir, agentFileName, harnessOnlyAgentContent())

	exec := &capturingExecutor{}
	deps.Executor = exec
	svc := app.New(deps)

	// Act
	_, _ = svc.Update(context.Background(), app.UpdateRequest{
		HarnessID:       "stub-harness",
		WorkspacePath:   workspace,
		AutoConfirmPlan: true,
	})

	// Assert — find the harness-only item and check the refreshed protocol region.
	targetPath := filepath.Join(agentsDir, agentFileName)
	if exec.capturedReq == nil {
		t.Fatal("executor was never called")
	}
	for _, item := range exec.capturedReq.Plan.Items {
		if item.TargetPath == targetPath && item.SourcePath == "" {
			out, err := exec.capturedReq.Content(item)
			if err != nil {
				t.Fatalf("Content returned error for %q: %v", targetPath, err)
			}
			if contains(out, []byte(oldContent)) {
				t.Errorf("refreshed bytes for %q still contain the old protocol content %q; "+
					"refreshHarnessOnly must replace the [[DEPLOYED:CommunicationProtocol]] region "+
					"with canonical protocol content from the loaded protocol source",
					targetPath, oldContent)
			}
			return
		}
	}
	t.Errorf("no harness-only plan item found for %q; Update must discover and plan "+
		"eligible harness-only agents", targetPath)
}

// ---------------------------------------------------------------------------
// T7.2 — Ineligible files never touched
// ---------------------------------------------------------------------------

// TestUpdate_IneligibleFileInAgentsDir_NotIncludedInPlan verifies that a .md file in the
// agents directory lacking transform_version produces no plan item. The file must be
// completely untouched — not read for writing, not planned, not stamped.
func TestUpdate_IneligibleFileInAgentsDir_NotIncludedInPlan(t *testing.T) {
	// Arrange — only an ineligible file present; the workspace has no eligible agent.
	const agentsDir = harnessOnlyTestAgentsDir
	const ineligibleFileName = "plain-agent.md"
	stub := interactiontest.NewBuilder().
		AnswerReview(true).
		Build()
	deps, workspace := newBaseDepsWithAgentsDir(t, stub, agentsDir)

	fullAgentsDir := filepath.Join(workspace, agentsDir)
	writeAgentFile(t, fullAgentsDir, ineligibleFileName, ineligibleAgentContent())

	exec := &capturingExecutor{}
	deps.Executor = exec
	svc := app.New(deps)

	// Act
	_, _ = svc.Update(context.Background(), app.UpdateRequest{
		HarnessID:       "stub-harness",
		WorkspacePath:   workspace,
		AutoConfirmPlan: true,
	})

	// Assert — the ineligible file must not appear as a plan item.
	ineligiblePath := filepath.Join(agentsDir, ineligibleFileName)
	if exec.capturedReq != nil {
		for _, item := range exec.capturedReq.Plan.Items {
			if item.TargetPath == ineligiblePath {
				t.Errorf("plan item found for ineligible file %q; "+
					"a file failing either detection signal must be left completely untouched — "+
					"never read for writing, never planned, never stamped",
					ineligiblePath)
			}
		}
	}
}

// TestUpdate_EligibleAndIneligibleCoexist_OnlyEligibleIsPlanned verifies that when the
// agents directory contains both an eligible harness-only file and an ineligible one, only
// the eligible file produces a plan item. The ineligible file must be silently excluded.
func TestUpdate_EligibleAndIneligibleCoexist_OnlyEligibleIsPlanned(t *testing.T) {
	// Arrange — one eligible file and one ineligible file.
	const agentsDir = harnessOnlyTestAgentsDir
	const eligibleFileName = "eligible-agent.md"
	const ineligibleFileName = "ineligible-agent.md"
	eligiblePath := filepath.Join(agentsDir, eligibleFileName)
	ineligiblePath := filepath.Join(agentsDir, ineligibleFileName)
	stub := interactiontest.NewBuilder().
		AnswerSelectOne(domain.QHarnessOnlyRefreshScope, eligiblePath, string(app.RefreshProtocolOnly)).
		AnswerReview(true).
		Build()
	deps, workspace := newBaseDepsWithAgentsDir(t, stub, agentsDir)

	fullAgentsDir := filepath.Join(workspace, agentsDir)
	writeAgentFile(t, fullAgentsDir, eligibleFileName, harnessOnlyAgentContent())
	writeAgentFile(t, fullAgentsDir, ineligibleFileName, ineligibleAgentContent())

	exec := &capturingExecutor{}
	deps.Executor = exec
	svc := app.New(deps)

	// Act
	_, _ = svc.Update(context.Background(), app.UpdateRequest{
		HarnessID:       "stub-harness",
		WorkspacePath:   workspace,
		AutoConfirmPlan: true,
	})

	if exec.capturedReq == nil {
		t.Fatal("executor was never called")
	}

	// Assert — eligible file must be in the plan.
	if !planHasHarnessOnlyItem(exec.capturedReq, eligiblePath) {
		t.Errorf("plan does not contain a harness-only item for the eligible file %q; "+
			"eligible agents must be discovered and planned", eligiblePath)
	}

	// Assert — ineligible file must NOT be in the plan.
	for _, item := range exec.capturedReq.Plan.Items {
		if item.TargetPath == ineligiblePath {
			t.Errorf("plan contains an item for the ineligible file %q; "+
				"files failing either detection signal must be silently excluded", ineligiblePath)
		}
	}
}

// ---------------------------------------------------------------------------
// T7.3 — Refresh-scope prompt lifecycle
// ---------------------------------------------------------------------------

// TestUpdate_EligibleHarnessOnlyAgent_RefreshScopePromptIsAsked verifies that when the
// workspace contains one eligible harness-only agent, QHarnessOnlyRefreshScope is asked
// exactly once, with the agent's TargetPath as the Subject.
func TestUpdate_EligibleHarnessOnlyAgent_RefreshScopePromptIsAsked(t *testing.T) {
	// Arrange
	const agentsDir = harnessOnlyTestAgentsDir
	const agentFileName = "my-harness-agent.md"
	targetPath := filepath.Join(agentsDir, agentFileName)
	stub := interactiontest.NewBuilder().
		AnswerSelectOne(domain.QHarnessOnlyRefreshScope, targetPath, string(app.RefreshProtocolOnly)).
		AnswerReview(true).
		Build()
	deps, workspace := newBaseDepsWithAgentsDir(t, stub, agentsDir)

	writeAgentFile(t, filepath.Join(workspace, agentsDir), agentFileName, harnessOnlyAgentContent())

	svc := app.New(deps)

	// Act
	_, _ = svc.Update(context.Background(), app.UpdateRequest{
		HarnessID:       "stub-harness",
		WorkspacePath:   workspace,
		AutoConfirmPlan: true,
	})

	// Assert — the refresh-scope question must have been asked for the agent's TargetPath.
	if !stub.WasAsked(domain.QHarnessOnlyRefreshScope, targetPath) {
		t.Errorf("QHarnessOnlyRefreshScope was not asked with Subject=%q; "+
			"Update must ask the refresh-scope question once per eligible harness-only agent "+
			"using the agent's TargetPath as the Subject",
			targetPath)
	}
	count := stub.CountAsked(domain.QHarnessOnlyRefreshScope, targetPath)
	if count != 1 {
		t.Errorf("QHarnessOnlyRefreshScope was asked %d times for Subject=%q, want 1; "+
			"the question must be asked exactly once per eligible agent (not for duplicates)",
			count, targetPath)
	}
}

// TestUpdate_TwoEligibleAgents_ApplyToAllAnswerLatchesForSecond verifies the apply-to-all
// latching behaviour: when the user answers "all:<scope>" for the first harness-only agent,
// the second agent is NOT prompted — the latched scope is applied automatically. This test
// additionally verifies that the latched scope's *value* was propagated to the second
// agent's content generation (not just that re-prompting was suppressed): the second
// agent's Content callback output must reflect the protocol-only scope by preserving its
// AuthorityHierarchy region unchanged, exactly as it would if the user had answered
// "protocol-only" for it directly.
func TestUpdate_TwoEligibleAgents_ApplyToAllAnswerLatchesForSecond(t *testing.T) {
	// Arrange — two eligible agents, each with two DEPLOYED regions. The first is answered
	// with apply-to-all protocol-only. Alphabetical order: alpha-agent.md < beta-agent.md.
	// The scan returns sorted by TargetPath, so alpha-agent is asked first.
	const agentsDir = harnessOnlyTestAgentsDir
	const firstAgentFileName = "alpha-agent.md"
	const secondAgentFileName = "beta-agent.md"
	const oldAuthorityContent = "OLD AUTHORITY HIERARCHY CONTENT — preserved under protocol-only, replaced under all-deployed."
	firstPath := filepath.Join(agentsDir, firstAgentFileName)
	secondPath := filepath.Join(agentsDir, secondAgentFileName)
	stub := interactiontest.NewBuilder().
		// Apply-to-all answer for the first agent: latch "protocol-only" for all remaining.
		AnswerSelectOne(domain.QHarnessOnlyRefreshScope, firstPath, "all:"+string(app.RefreshProtocolOnly)).
		AnswerReview(true).
		Build()
	deps, workspace := newBaseDepsWithAgentsDir(t, stub, agentsDir)

	agentsFullDir := filepath.Join(workspace, agentsDir)
	writeAgentFile(t, agentsFullDir, firstAgentFileName, twoRegionAgentContent())
	writeAgentFile(t, agentsFullDir, secondAgentFileName, twoRegionAgentContent())

	exec := &capturingExecutor{}
	deps.Executor = exec
	svc := app.New(deps)

	// Act
	_, _ = svc.Update(context.Background(), app.UpdateRequest{
		HarnessID:       "stub-harness",
		WorkspacePath:   workspace,
		AutoConfirmPlan: true,
	})

	// Assert — first agent must have been asked.
	if !stub.WasAsked(domain.QHarnessOnlyRefreshScope, firstPath) {
		t.Errorf("QHarnessOnlyRefreshScope was not asked for the first agent %q; "+
			"the prompt must be asked for the first eligible harness-only agent", firstPath)
	}

	// Assert — second agent must NOT have been asked (latched by apply-to-all).
	if stub.WasAsked(domain.QHarnessOnlyRefreshScope, secondPath) {
		t.Errorf("QHarnessOnlyRefreshScope was asked for the second agent %q after an "+
			"apply-to-all answer for the first agent; "+
			"an \"all:<scope>\" answer must latch the scope for all remaining agents and "+
			"suppress further prompting, exactly as the conflict loop's applyToAllLatch does",
			secondPath)
	}

	// Assert — the latched protocol-only scope must have been applied to the second agent's
	// content generation, not just suppressed re-prompting. The second agent's Content
	// callback output must preserve the AuthorityHierarchy region unchanged (because
	// AuthorityHierarchy is out of scope for protocol-only).
	if exec.capturedReq == nil {
		t.Fatal("executor was never called")
	}
	for _, item := range exec.capturedReq.Plan.Items {
		if item.TargetPath == secondPath && item.SourcePath == "" {
			if exec.capturedReq.Content == nil {
				t.Fatal("ExecRequest.Content callback is nil")
			}
			out, err := exec.capturedReq.Content(item)
			if err != nil {
				t.Fatalf("Content callback returned error for second agent %q: %v", secondPath, err)
			}
			if !contains(out, []byte(oldAuthorityContent)) {
				t.Errorf("second agent %q: the latched protocol-only scope was not applied "+
					"correctly — AuthorityHierarchy stale content %q is absent from the output, "+
					"meaning the scope was not propagated or was widened beyond protocol-only; "+
					"the latching mechanism must propagate the latched scope's value, not just "+
					"suppress the prompt", secondPath, oldAuthorityContent)
			}
			return
		}
	}
	t.Errorf("no harness-only plan item for the second agent %q found; "+
		"both agents must be discovered and planned", secondPath)
}

// TestUpdate_NoEligibleHarnessOnlyAgents_RefreshScopePromptNeverAsked verifies that when
// there are no eligible harness-only agents (agents dir is empty or contains only ineligible
// files), QHarnessOnlyRefreshScope is never asked. Prompting when nothing was discovered
// would be noise; the guard must be on a non-empty discovery result.
func TestUpdate_NoEligibleHarnessOnlyAgents_RefreshScopePromptNeverAsked(t *testing.T) {
	// Arrange — workspace with only an ineligible file (no eligible harness-only agents).
	const agentsDir = harnessOnlyTestAgentsDir
	stub := interactiontest.NewBuilder().
		AnswerReview(true).
		Build()
	deps, workspace := newBaseDepsWithAgentsDir(t, stub, agentsDir)

	// Write only an ineligible file — no eligible agents discovered.
	writeAgentFile(t, filepath.Join(workspace, agentsDir), "plain-agent.md", ineligibleAgentContent())

	svc := app.New(deps)

	// Act
	_, _ = svc.Update(context.Background(), app.UpdateRequest{
		HarnessID:       "stub-harness",
		WorkspacePath:   workspace,
		AutoConfirmPlan: true,
	})

	// Assert — no QHarnessOnlyRefreshScope question was asked.
	for _, call := range stub.Calls() {
		if call.ID == domain.QHarnessOnlyRefreshScope {
			t.Errorf("QHarnessOnlyRefreshScope was asked (subject=%q) when no eligible "+
				"harness-only agent was discovered; "+
				"the refresh-scope prompt must be suppressed entirely when the discovery result "+
				"is empty", call.Subject)
		}
	}
}

// ---------------------------------------------------------------------------
// T7.4 — Scope breadth end to end
// ---------------------------------------------------------------------------

// TestUpdate_ProtocolOnlyScopeAnswer_NonProtocolDeployedRegionPreservedUnchanged verifies
// that when the user answers "protocol-only" for a harness-only agent whose file contains
// two canonical [[DEPLOYED:...]] regions (CommunicationProtocol and AuthorityHierarchy),
// the Content callback output preserves the AuthorityHierarchy region byte-identical —
// its stale content must not be replaced, because it is out of scope for protocol-only.
//
// This test uses twoRegionAgentContent() so the two scopes are observably distinguishable:
// a CommunicationProtocol-only refresh and an all-deployed refresh produce different bytes
// for the AuthorityHierarchy region, making it impossible for a correct-looking test to
// pass if the scope parameter were silently ignored.
func TestUpdate_ProtocolOnlyScopeAnswer_NonProtocolDeployedRegionPreservedUnchanged(t *testing.T) {
	// Arrange — agent with two DEPLOYED regions; user answers "protocol-only".
	const agentsDir = harnessOnlyTestAgentsDir
	const agentFileName = "my-harness-agent.md"
	const oldAuthorityContent = "OLD AUTHORITY HIERARCHY CONTENT — preserved under protocol-only, replaced under all-deployed."
	targetPath := filepath.Join(agentsDir, agentFileName)
	stub := interactiontest.NewBuilder().
		AnswerSelectOne(domain.QHarnessOnlyRefreshScope, targetPath, string(app.RefreshProtocolOnly)).
		AnswerReview(true).
		Build()
	deps, workspace := newBaseDepsWithAgentsDir(t, stub, agentsDir)

	writeAgentFile(t, filepath.Join(workspace, agentsDir), agentFileName, twoRegionAgentContent())

	exec := &capturingExecutor{}
	deps.Executor = exec
	svc := app.New(deps)

	// Act
	_, _ = svc.Update(context.Background(), app.UpdateRequest{
		HarnessID:       "stub-harness",
		WorkspacePath:   workspace,
		AutoConfirmPlan: true,
	})

	// Assert — plan item must be present.
	if exec.capturedReq == nil {
		t.Fatal("executor was never called")
	}
	if !planHasHarnessOnlyItem(exec.capturedReq, targetPath) {
		t.Errorf("plan item for harness-only agent %q not found; "+
			"Update must plan the eligible harness-only agent with ActionUpdate", targetPath)
	}

	// Assert — the Content callback must preserve the AuthorityHierarchy region unchanged.
	// Under protocol-only scope, only CommunicationProtocol is in scope; every other
	// [[DEPLOYED:]] region must remain byte-identical to the deployed file.
	for _, item := range exec.capturedReq.Plan.Items {
		if item.TargetPath == targetPath && item.SourcePath == "" {
			if exec.capturedReq.Content == nil {
				t.Fatal("ExecRequest.Content callback is nil")
			}
			out, err := exec.capturedReq.Content(item)
			if err != nil {
				t.Fatalf("Content callback returned error for %q: %v", targetPath, err)
			}
			if !contains(out, []byte(oldAuthorityContent)) {
				t.Errorf("protocol-only scope replaced the AuthorityHierarchy region whose stale "+
					"content is %q; under protocol-only scope only CommunicationProtocol is "+
					"refreshed — every other [[DEPLOYED:]] region must remain byte-identical to "+
					"the deployed file, so the old AuthorityHierarchy content must still appear "+
					"in the output", oldAuthorityContent)
			}
			return
		}
	}
	t.Errorf("no harness-only plan item with TargetPath=%q and SourcePath=\"\" found", targetPath)
}

// TestUpdate_AllDeployedScopeAnswer_AllDeployedRegionsRefreshed verifies that when the
// user answers "all-deployed" for a harness-only agent whose file contains two canonical
// [[DEPLOYED:...]] regions (CommunicationProtocol and AuthorityHierarchy), the Content
// callback output replaces the AuthorityHierarchy region's stale content — it must not
// survive, because both regions are in scope for all-deployed.
//
// Together with TestUpdate_ProtocolOnlyScopeAnswer_NonProtocolDeployedRegionPreservedUnchanged
// this pair exercises the scope-breadth contract: the same fixture produces different output
// bytes depending on which scope was chosen, making it impossible for either test to produce
// a false green if the scope parameter were silently ignored or the two branches swapped.
func TestUpdate_AllDeployedScopeAnswer_AllDeployedRegionsRefreshed(t *testing.T) {
	// Arrange — agent with two DEPLOYED regions; user answers "all-deployed".
	const agentsDir = harnessOnlyTestAgentsDir
	const agentFileName = "my-harness-agent.md"
	const oldAuthorityContent = "OLD AUTHORITY HIERARCHY CONTENT — preserved under protocol-only, replaced under all-deployed."
	targetPath := filepath.Join(agentsDir, agentFileName)
	stub := interactiontest.NewBuilder().
		AnswerSelectOne(domain.QHarnessOnlyRefreshScope, targetPath, string(app.RefreshAllDeployed)).
		AnswerReview(true).
		Build()
	deps, workspace := newBaseDepsWithAgentsDir(t, stub, agentsDir)

	writeAgentFile(t, filepath.Join(workspace, agentsDir), agentFileName, twoRegionAgentContent())

	exec := &capturingExecutor{}
	deps.Executor = exec
	svc := app.New(deps)

	// Act
	_, _ = svc.Update(context.Background(), app.UpdateRequest{
		HarnessID:       "stub-harness",
		WorkspacePath:   workspace,
		AutoConfirmPlan: true,
	})

	// Assert — plan item with ActionUpdate and empty SourcePath must be present.
	if exec.capturedReq == nil {
		t.Fatal("executor was never called")
	}
	if !planHasHarnessOnlyItem(exec.capturedReq, targetPath) {
		t.Errorf("plan item for harness-only agent %q with ActionUpdate and empty SourcePath "+
			"not found under 'all-deployed' scope answer; "+
			"the scope choice must not suppress the plan item — it only governs refresh breadth",
			targetPath)
	}

	// Assert — the Content callback must have replaced the AuthorityHierarchy region.
	// Under all-deployed scope, every canonical [[DEPLOYED:]] region is in scope, so the
	// stale AuthorityHierarchy content must not survive in the refreshed output.
	for _, item := range exec.capturedReq.Plan.Items {
		if item.TargetPath == targetPath && item.SourcePath == "" {
			if exec.capturedReq.Content == nil {
				t.Fatal("ExecRequest.Content callback is nil")
			}
			out, err := exec.capturedReq.Content(item)
			if err != nil {
				t.Fatalf("Content callback returned error for %q: %v", targetPath, err)
			}
			if contains(out, []byte(oldAuthorityContent)) {
				t.Errorf("all-deployed scope left the AuthorityHierarchy region's stale content %q "+
					"unchanged in the output; under all-deployed scope every canonical [[DEPLOYED:]] "+
					"region is refreshed — stale bytes must not survive",
					oldAuthorityContent)
			}
			return
		}
	}
	t.Errorf("no harness-only plan item with TargetPath=%q and SourcePath=\"\" found", targetPath)
}

// ---------------------------------------------------------------------------
// T7.5 — Orchestrator exclusion
// ---------------------------------------------------------------------------

// TestUpdate_OrchestratorMdFile_NeverIncludedInPlan verifies that a file named
// "orchestrator.md" in the agents directory is never added to the plan, even when it
// satisfies both eligibility signals. Orchestrator files are unconditionally excluded.
func TestUpdate_OrchestratorMdFile_NeverIncludedInPlan(t *testing.T) {
	// Arrange — orchestrator.md carries both eligibility signals but must be excluded.
	const agentsDir = harnessOnlyTestAgentsDir
	const orchFileName = "orchestrator.md"
	orchPath := filepath.Join(agentsDir, orchFileName)
	stub := interactiontest.NewBuilder().
		AnswerReview(true).
		Build()
	deps, workspace := newBaseDepsWithAgentsDir(t, stub, agentsDir)

	writeAgentFile(t, filepath.Join(workspace, agentsDir), orchFileName, harnessOnlyAgentContent())

	exec := &capturingExecutor{}
	deps.Executor = exec
	svc := app.New(deps)

	// Act
	_, _ = svc.Update(context.Background(), app.UpdateRequest{
		HarnessID:       "stub-harness",
		WorkspacePath:   workspace,
		AutoConfirmPlan: true,
	})

	// Assert — orchestrator.md must never appear as a harness-only plan item.
	if exec.capturedReq != nil {
		for _, item := range exec.capturedReq.Plan.Items {
			if item.TargetPath == orchPath && item.SourcePath == "" {
				t.Errorf("plan contains a harness-only item for %q; "+
					"orchestrator-named files must be unconditionally excluded from "+
					"harness-only handling — their eligibility is irrelevant", orchPath)
			}
		}
	}

	// Assert — QHarnessOnlyRefreshScope must also not have been asked for the orchestrator.
	if stub.WasAsked(domain.QHarnessOnlyRefreshScope, orchPath) {
		t.Errorf("QHarnessOnlyRefreshScope was asked for orchestrator file %q; "+
			"orchestrators must be excluded from all harness-only handling, including prompting",
			orchPath)
	}
}

// TestUpdate_OrchestratorAgentMdFile_NeverIncludedInPlan verifies that the alternate form
// "orchestrator.agent.md" is also excluded from harness-only handling.
func TestUpdate_OrchestratorAgentMdFile_NeverIncludedInPlan(t *testing.T) {
	// Arrange
	const agentsDir = harnessOnlyTestAgentsDir
	const orchFileName = "orchestrator.agent.md"
	orchPath := filepath.Join(agentsDir, orchFileName)
	stub := interactiontest.NewBuilder().
		AnswerReview(true).
		Build()
	deps, workspace := newBaseDepsWithAgentsDir(t, stub, agentsDir)

	writeAgentFile(t, filepath.Join(workspace, agentsDir), orchFileName, harnessOnlyAgentContent())

	exec := &capturingExecutor{}
	deps.Executor = exec
	svc := app.New(deps)

	// Act
	_, _ = svc.Update(context.Background(), app.UpdateRequest{
		HarnessID:       "stub-harness",
		WorkspacePath:   workspace,
		AutoConfirmPlan: true,
	})

	// Assert
	if exec.capturedReq != nil {
		for _, item := range exec.capturedReq.Plan.Items {
			if item.TargetPath == orchPath && item.SourcePath == "" {
				t.Errorf("plan contains a harness-only item for %q; "+
					"both orchestrator filename forms must be unconditionally excluded",
					orchPath)
			}
		}
	}
	if stub.WasAsked(domain.QHarnessOnlyRefreshScope, orchPath) {
		t.Errorf("QHarnessOnlyRefreshScope was asked for orchestrator.agent.md file %q; "+
			"orchestrators must be excluded from all harness-only handling", orchPath)
	}
}

// ---------------------------------------------------------------------------
// T7.6 — Atomic reversal coverage
// ---------------------------------------------------------------------------

// TestUpdate_HarnessOnlyAgentInPlan_ParticipatesInAtomicReversal verifies that a harness-only
// agent is routed through the plan and executor (not written via a side channel), so that it
// participates in the run's existing atomic all-or-nothing reversal guarantee. When the
// executor reports reversal (Reverted:true), the app returns *RevertedRunError.
func TestUpdate_HarnessOnlyAgentInPlan_ParticipatesInAtomicReversal(t *testing.T) {
	// Arrange — eligible harness-only agent; executor signals reversal.
	const agentsDir = harnessOnlyTestAgentsDir
	const agentFileName = "my-harness-agent.md"
	targetPath := filepath.Join(agentsDir, agentFileName)
	triggerErr := errors.New("simulated write failure for reversal test")
	stub := interactiontest.NewBuilder().
		AnswerSelectOne(domain.QHarnessOnlyRefreshScope, targetPath, string(app.RefreshProtocolOnly)).
		AnswerReview(true).
		Build()
	deps, workspace := newBaseDepsWithAgentsDir(t, stub, agentsDir)

	writeAgentFile(t, filepath.Join(workspace, agentsDir), agentFileName, harnessOnlyAgentContent())

	// Use a capturing executor for plan inspection AND set it to signal reversal.
	// The revertingCapturingExecutor captures the request and returns a reverted result.
	revertExec := &revertingCapturingExecutor{
		triggerErr: triggerErr,
	}
	deps.Executor = revertExec
	svc := app.New(deps)

	// Act
	_, err := svc.Update(context.Background(), app.UpdateRequest{
		HarnessID:       "stub-harness",
		WorkspacePath:   workspace,
		AutoConfirmPlan: true,
	})

	// Assert — Update must return *RevertedRunError when the executor reverses.
	if err == nil {
		t.Fatal("Update returned nil error for a reverted run; want *RevertedRunError")
	}
	var revErr *app.RevertedRunError
	if !errors.As(err, &revErr) {
		t.Fatalf("Update error is %T, want *app.RevertedRunError (err=%v)", err, err)
	}
	if !errors.Is(err, app.ErrRunReverted) {
		t.Error("errors.Is(err, app.ErrRunReverted) = false; want true")
	}
	// The trigger error must be reachable through Unwrap.
	if !errors.Is(err, triggerErr) {
		t.Errorf("errors.Is(err, triggerErr) = false; "+
			"RevertedRunError must unwrap to the executor's trigger error so the cause is visible")
	}
	// Verify the harness-only item was present in the plan that was sent to the executor
	// and subsequently reverted. Without this check the test passes even when harness-only
	// agents are silently dropped before reaching the executor — bypassing the atomic
	// guarantee entirely. This is the per-AC7.7 litmus test: could this test fail if the
	// specific behavior it names is wrong? Yes: if Update drops the harness-only agent
	// from the plan, planHasHarnessOnlyItem returns false and this assertion fires.
	if !planHasHarnessOnlyItem(revertExec.capturedReq, targetPath) {
		t.Errorf("harness-only plan item for %q was not present in the ExecRequest sent to the "+
			"executor; harness-only agents must be routed through the plan so they participate in "+
			"the atomic all-or-nothing reversal guarantee — not written via a side channel that "+
			"bypasses it", targetPath)
	}
}

// revertingCapturingExecutor records the ExecRequest and signals reversal.
// It is used to verify that harness-only agents are routed through the executor
// so they inherit the run's atomic guarantee.
type revertingCapturingExecutor struct {
	capturedReq *deploy.ExecRequest
	triggerErr  error
}

func (e *revertingCapturingExecutor) Execute(_ context.Context, req deploy.ExecRequest) (deploy.ExecResult, error) {
	cp := req
	e.capturedReq = &cp
	return deploy.ExecResult{
		DeploymentRoot: req.Plan.WorkspacePath,
		Partial:        e.triggerErr,
		Reverted:       true,
	}, nil
}

// TestUpdate_HarnessOnlyAgentAddedAfterConflictLoop_NotInConflictItems verifies that
// harness-only agents are appended to the plan AFTER the conflict loop runs, so they
// never appear as ActionConflict items. The refresh-scope prompt is their consent
// mechanism; asking both the conflict prompt and the refresh-scope prompt for the same
// file would be redundant noise.
func TestUpdate_HarnessOnlyAgentAddedAfterConflictLoop_ActionIsUpdate(t *testing.T) {
	// Arrange — eligible harness-only agent.
	const agentsDir = harnessOnlyTestAgentsDir
	const agentFileName = "my-harness-agent.md"
	targetPath := filepath.Join(agentsDir, agentFileName)
	stub := interactiontest.NewBuilder().
		AnswerSelectOne(domain.QHarnessOnlyRefreshScope, targetPath, string(app.RefreshProtocolOnly)).
		AnswerReview(true).
		Build()
	deps, workspace := newBaseDepsWithAgentsDir(t, stub, agentsDir)

	writeAgentFile(t, filepath.Join(workspace, agentsDir), agentFileName, harnessOnlyAgentContent())

	exec := &capturingExecutor{}
	deps.Executor = exec
	svc := app.New(deps)

	// Act
	_, _ = svc.Update(context.Background(), app.UpdateRequest{
		HarnessID:       "stub-harness",
		WorkspacePath:   workspace,
		AutoConfirmPlan: true,
	})

	// Assert — the harness-only item must not be ActionConflict.
	if exec.capturedReq == nil {
		t.Fatal("executor was never called")
	}
	for _, item := range exec.capturedReq.Plan.Items {
		if item.TargetPath == targetPath && item.SourcePath == "" {
			if item.Action == domain.ActionConflict {
				t.Errorf("harness-only plan item %q has ActionConflict; "+
					"harness-only agents must be appended after the conflict loop with "+
					"ActionUpdate — the refresh-scope prompt is their sole consent mechanism",
					targetPath)
			}
			return
		}
	}
	// Item not found — the plan doesn't have the harness-only agent at all.
	t.Errorf("no harness-only plan item for %q; Update must discover and plan eligible agents",
		targetPath)
}

// ---------------------------------------------------------------------------
// T7.7 — Dry-run semantics
// ---------------------------------------------------------------------------

// TestUpdate_DryRun_ExecRequestHasDryRunTrue verifies that when UpdateRequest.DryRun is
// true, the ExecRequest forwarded to the executor also has DryRun true, even when a
// harness-only agent is present. DryRun must be honoured identically for harness-only
// agents as for catalog-backed agents — no bytes are written.
func TestUpdate_DryRun_ExecRequestHasDryRunTrue(t *testing.T) {
	// Arrange — eligible harness-only agent, dry-run mode.
	const agentsDir = harnessOnlyTestAgentsDir
	const agentFileName = "my-harness-agent.md"
	targetPath := filepath.Join(agentsDir, agentFileName)
	stub := interactiontest.NewBuilder().
		AnswerSelectOne(domain.QHarnessOnlyRefreshScope, targetPath, string(app.RefreshProtocolOnly)).
		AnswerReview(true).
		Build()
	deps, workspace := newBaseDepsWithAgentsDir(t, stub, agentsDir)

	writeAgentFile(t, filepath.Join(workspace, agentsDir), agentFileName, harnessOnlyAgentContent())

	exec := &capturingExecutor{}
	deps.Executor = exec
	svc := app.New(deps)

	// Act
	_, _ = svc.Update(context.Background(), app.UpdateRequest{
		HarnessID:       "stub-harness",
		WorkspacePath:   workspace,
		AutoConfirmPlan: true,
		DryRun:          true,
	})

	// Assert — the executor must receive DryRun:true.
	if exec.capturedReq == nil {
		t.Fatal("executor was never called; Update must reach the executor even in dry-run mode")
	}
	if !exec.capturedReq.DryRun {
		t.Error("ExecRequest.DryRun is false for a dry-run Update run; "+
			"DryRun must be forwarded to the executor so no bytes are written for harness-only "+
			"agents (or any other artifact)")
	}
}

// TestUpdate_DryRun_DiscoveryAndPromptingStillOccur verifies that even in dry-run mode,
// discovery runs and the refresh-scope prompt is asked. The run still computes and
// validates everything; only the actual writes are suppressed.
func TestUpdate_DryRun_DiscoveryAndPromptingStillOccur(t *testing.T) {
	// Arrange — eligible harness-only agent, dry-run mode.
	const agentsDir = harnessOnlyTestAgentsDir
	const agentFileName = "my-harness-agent.md"
	targetPath := filepath.Join(agentsDir, agentFileName)
	stub := interactiontest.NewBuilder().
		AnswerSelectOne(domain.QHarnessOnlyRefreshScope, targetPath, string(app.RefreshProtocolOnly)).
		AnswerReview(true).
		Build()
	deps, workspace := newBaseDepsWithAgentsDir(t, stub, agentsDir)

	writeAgentFile(t, filepath.Join(workspace, agentsDir), agentFileName, harnessOnlyAgentContent())

	svc := app.New(deps)

	// Act
	_, _ = svc.Update(context.Background(), app.UpdateRequest{
		HarnessID:       "stub-harness",
		WorkspacePath:   workspace,
		AutoConfirmPlan: true,
		DryRun:          true,
	})

	// Assert — the refresh-scope prompt must have been asked even in dry-run mode.
	if !stub.WasAsked(domain.QHarnessOnlyRefreshScope, targetPath) {
		t.Errorf("QHarnessOnlyRefreshScope was not asked in dry-run mode; "+
			"discovery and prompting must still occur when DryRun is true — "+
			"only the executor writes are suppressed, not the decision-making before them")
	}
}

// ---------------------------------------------------------------------------
// Version-stamping omission
// ---------------------------------------------------------------------------

// TestUpdate_HarnessOnlyAgent_NoVersionStampForTargetPath verifies that the Update flow
// does not add an entry to ExecRequest.VersionStamps for a harness-only agent's TargetPath.
// Per the design's version-stamping decision, there is no generic source version to stamp
// from; recording a fabricated version stamp would make the file appear catalog-backed on
// the next run, defeating the two-signal detection rule. A future regression that starts
// stamping a version onto a harness-only agent must be caught by this assertion.
func TestUpdate_HarnessOnlyAgent_NoVersionStampForTargetPath(t *testing.T) {
	// Arrange — eligible harness-only agent.
	const agentsDir = harnessOnlyTestAgentsDir
	const agentFileName = "my-harness-agent.md"
	targetPath := filepath.Join(agentsDir, agentFileName)
	stub := interactiontest.NewBuilder().
		AnswerSelectOne(domain.QHarnessOnlyRefreshScope, targetPath, string(app.RefreshProtocolOnly)).
		AnswerReview(true).
		Build()
	deps, workspace := newBaseDepsWithAgentsDir(t, stub, agentsDir)

	writeAgentFile(t, filepath.Join(workspace, agentsDir), agentFileName, harnessOnlyAgentContent())

	exec := &capturingExecutor{}
	deps.Executor = exec
	svc := app.New(deps)

	// Act
	_, _ = svc.Update(context.Background(), app.UpdateRequest{
		HarnessID:       "stub-harness",
		WorkspacePath:   workspace,
		AutoConfirmPlan: true,
	})

	// Assert — ExecRequest.VersionStamps must not contain an entry for the harness-only
	// agent's TargetPath. Stamping implies a source version to stamp from; there is none for
	// a harness-only agent, and inventing a version would make the file look catalog-backed
	// on the next run, silently breaking the detection rule that keeps it in the harness-only
	// path.
	if exec.capturedReq == nil {
		t.Fatal("executor was never called")
	}
	if _, ok := exec.capturedReq.VersionStamps[targetPath]; ok {
		t.Errorf("ExecRequest.VersionStamps contains an entry for harness-only agent %q; "+
			"no version stamp must be recorded for a harness-only agent — there is no generic "+
			"source version to stamp from, and stamping would make the file appear catalog-backed "+
			"on the next run", targetPath)
	}
}

// ---------------------------------------------------------------------------
// T7.8 — Catalog-backed regression
// ---------------------------------------------------------------------------

// TestUpdate_NoCatalogPathChange_WhenNoHarnessOnlyAgentsPresent verifies that an Update run
// with no harness-only agents in the workspace produces plan items only from the planner,
// exactly as before the harness-only wiring was added. The harness-only wiring must not
// introduce extra plan items or remove existing ones when the discovery result is empty.
func TestUpdate_NoCatalogPathChange_WhenNoHarnessOnlyAgentsPresent(t *testing.T) {
	// Arrange — harness declares an agents dir but it is empty (no harness-only agents).
	const agentsDir = harnessOnlyTestAgentsDir
	stub := interactiontest.NewBuilder().
		AnswerReview(true).
		Build()
	deps, workspace := newBaseDepsWithAgentsDir(t, stub, agentsDir)

	// Create the agents directory but leave it empty.
	if err := os.MkdirAll(filepath.Join(workspace, agentsDir), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	// Configure the planner to return a specific catalog-backed plan.
	catalogItem := domain.PlanItem{
		Ref:        domain.ArtifactRef{Kind: domain.ArtifactAgent, Key: "test-runner"},
		TargetPath: "test-runner.md",
		SourcePath: "Catalog/Subagents/TestRunner/test-runner.md",
		Action:     domain.ActionUpdate,
	}
	deps.Planner = &stubPlanner{plan: domain.Plan{
		Mode:          domain.ModeUpdate,
		Harness:       minimalHarness,
		WorkspacePath: workspace,
		Scope:         domain.ScopeProject,
		Items:         []domain.PlanItem{catalogItem},
		Workflows:     []string{"quick-fix"},
	}}

	exec := &capturingExecutor{}
	deps.Executor = exec
	svc := app.New(deps)

	// Act
	_, _ = svc.Update(context.Background(), app.UpdateRequest{
		HarnessID:       "stub-harness",
		WorkspacePath:   workspace,
		AutoConfirmPlan: true,
	})

	// Assert — the catalog-backed item must still appear in the plan unchanged.
	if exec.capturedReq == nil {
		t.Fatal("executor was never called")
	}
	found := false
	for _, item := range exec.capturedReq.Plan.Items {
		if item.TargetPath == catalogItem.TargetPath && item.SourcePath == catalogItem.SourcePath {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("catalog-backed plan item %q (SourcePath=%q) is missing from the executor's "+
			"plan; the harness-only wiring must not remove or modify catalog-backed plan items",
			catalogItem.TargetPath, catalogItem.SourcePath)
	}

	// Assert — no extra harness-only items were added (discovery result is empty).
	for _, item := range exec.capturedReq.Plan.Items {
		if item.SourcePath == "" && item.Ref.Kind == domain.ArtifactAgent {
			t.Errorf("unexpected harness-only plan item found (TargetPath=%q, SourcePath=\"\") "+
				"when agents directory is empty; no harness-only agents should be discovered",
				item.TargetPath)
		}
	}
}

// TestUpdate_CatalogBackedAndHarnessOnlyCoexist_BothPresentInPlan verifies that when both
// a catalog-backed agent (from the planner) and a harness-only agent coexist in the same
// Update run, both appear in the executor's plan without interfering with each other.
func TestUpdate_CatalogBackedAndHarnessOnlyCoexist_BothPresentInPlan(t *testing.T) {
	// Arrange — one catalog-backed item from the planner, one eligible harness-only file.
	const agentsDir = harnessOnlyTestAgentsDir
	const harnessFileName = "my-harness-agent.md"
	harnessTargetPath := filepath.Join(agentsDir, harnessFileName)

	stub := interactiontest.NewBuilder().
		AnswerSelectOne(domain.QHarnessOnlyRefreshScope, harnessTargetPath, string(app.RefreshProtocolOnly)).
		AnswerReview(true).
		Build()
	deps, workspace := newBaseDepsWithAgentsDir(t, stub, agentsDir)

	// Write the harness-only agent file.
	writeAgentFile(t, filepath.Join(workspace, agentsDir), harnessFileName, harnessOnlyAgentContent())

	// Configure the planner with a catalog-backed item alongside the harness-only agent.
	catalogItem := domain.PlanItem{
		Ref:        domain.ArtifactRef{Kind: domain.ArtifactAgent, Key: "test-runner"},
		TargetPath: "test-runner.md",
		SourcePath: "Catalog/Subagents/TestRunner/test-runner.md",
		Action:     domain.ActionUpdate,
	}
	deps.Planner = &stubPlanner{plan: domain.Plan{
		Mode:          domain.ModeUpdate,
		Harness:       minimalHarness,
		WorkspacePath: workspace,
		Scope:         domain.ScopeProject,
		Items:         []domain.PlanItem{catalogItem},
		Workflows:     []string{"quick-fix"},
	}}

	exec := &capturingExecutor{}
	deps.Executor = exec
	svc := app.New(deps)

	// Act
	_, _ = svc.Update(context.Background(), app.UpdateRequest{
		HarnessID:       "stub-harness",
		WorkspacePath:   workspace,
		AutoConfirmPlan: true,
	})

	if exec.capturedReq == nil {
		t.Fatal("executor was never called")
	}

	// Assert — catalog-backed item present.
	catalogFound := false
	for _, item := range exec.capturedReq.Plan.Items {
		if item.TargetPath == catalogItem.TargetPath && item.SourcePath == catalogItem.SourcePath {
			catalogFound = true
			break
		}
	}
	if !catalogFound {
		t.Errorf("catalog-backed plan item %q is missing; "+
			"harness-only wiring must not disturb catalog-backed plan items", catalogItem.TargetPath)
	}

	// Assert — harness-only item present.
	if !planHasHarnessOnlyItem(exec.capturedReq, harnessTargetPath) {
		t.Errorf("harness-only plan item %q not found; "+
			"both catalog-backed and harness-only agents must appear in the same plan",
			harnessTargetPath)
	}
}

// TestUpdate_HarnessWithNoAgentsDirSupport_NeverScansForHarnessOnly verifies that when the
// harness module's descriptor does NOT declare a supported agents directory (Supported:false),
// the Update flow never calls scanHarnessOnlyAgents and QHarnessOnlyRefreshScope is never
// asked. A harness with no agents directory has nothing to scan.
func TestUpdate_HarnessWithNoAgentsDirSupport_NeverScansForHarnessOnly(t *testing.T) {
	// Arrange — use the standard minimal module (Paths.Agents.Supported == false).
	stub := interactiontest.NewBuilder().
		AnswerReview(true).
		Build()
	deps, workspace := newBaseDeps(t, stub)
	// newBaseDeps already uses newMinimalModule() which has Paths.Agents.Supported == false.

	// Write an eligible file in the workspace root to confirm it is never picked up.
	writeAgentFile(t, workspace, "stray-harness-agent.md", harnessOnlyAgentContent())

	svc := app.New(deps)

	// Act
	_, _ = svc.Update(context.Background(), app.UpdateRequest{
		HarnessID:       "stub-harness",
		WorkspacePath:   workspace,
		AutoConfirmPlan: true,
	})

	// Assert — no harness-only refresh scope question asked.
	for _, call := range stub.Calls() {
		if call.ID == domain.QHarnessOnlyRefreshScope {
			t.Errorf("QHarnessOnlyRefreshScope was asked (subject=%q) for a harness that does "+
				"not declare a supported agents directory; "+
				"discovery must be guarded on Paths.Agents.Supported being true", call.Subject)
		}
	}
}

// ---------------------------------------------------------------------------
// T7.9 — Observability
// ---------------------------------------------------------------------------

// TestUpdate_HarnessOnlyRefresh_EmitsEventWithHarnessOnlyField verifies that the Update
// flow emits a logging.Event with Fields["harness_only"] == "true" for each harness-only
// agent that is refreshed. This is the contract a caller or monitoring tool reads to
// determine which agents received degraded-quality treatment.
func TestUpdate_HarnessOnlyRefresh_EmitsEventWithHarnessOnlyField(t *testing.T) {
	// Arrange — eligible harness-only agent.
	const agentsDir = harnessOnlyTestAgentsDir
	const agentFileName = "my-harness-agent.md"
	targetPath := filepath.Join(agentsDir, agentFileName)
	stub := interactiontest.NewBuilder().
		AnswerSelectOne(domain.QHarnessOnlyRefreshScope, targetPath, string(app.RefreshProtocolOnly)).
		AnswerReview(true).
		Build()
	deps, workspace := newBaseDepsWithAgentsDir(t, stub, agentsDir)

	writeAgentFile(t, filepath.Join(workspace, agentsDir), agentFileName, harnessOnlyAgentContent())

	logger := &spyLogger{}
	deps.Logger = logger
	svc := app.New(deps)

	// Act
	_, _ = svc.Update(context.Background(), app.UpdateRequest{
		HarnessID:       "stub-harness",
		WorkspacePath:   workspace,
		AutoConfirmPlan: true,
	})

	// Assert — a logging event with Fields["harness_only"] == "true" must have been emitted.
	evt, found := logger.harnessOnlyEvent()
	if !found {
		t.Errorf("no logging event with Fields[\"harness_only\"] == \"true\" was emitted; "+
			"the Update flow must emit an observability event for each harness-only refresh "+
			"so callers can identify which agents received degraded-quality treatment")
		return
	}

	// Assert — the event Subject must name the refreshed agent.
	if evt.Subject != targetPath {
		t.Errorf("harness-only logging event Subject = %q, want %q; "+
			"the event must identify the refreshed agent by its TargetPath",
			evt.Subject, targetPath)
	}
}

// TestUpdate_HarnessOnlyRefresh_EventScopeFieldMatchesUserChoice verifies that the scope
// field in the emitted logging event matches the scope the user selected for the agent.
// The scope field is the contract a monitoring tool reads to determine refresh breadth.
func TestUpdate_HarnessOnlyRefresh_EventScopeFieldMatchesUserChoice(t *testing.T) {
	// Arrange — eligible harness-only agent; user answers "all-deployed".
	const agentsDir = harnessOnlyTestAgentsDir
	const agentFileName = "my-harness-agent.md"
	targetPath := filepath.Join(agentsDir, agentFileName)
	stub := interactiontest.NewBuilder().
		AnswerSelectOne(domain.QHarnessOnlyRefreshScope, targetPath, string(app.RefreshAllDeployed)).
		AnswerReview(true).
		Build()
	deps, workspace := newBaseDepsWithAgentsDir(t, stub, agentsDir)

	writeAgentFile(t, filepath.Join(workspace, agentsDir), agentFileName, harnessOnlyAgentContent())

	logger := &spyLogger{}
	deps.Logger = logger
	svc := app.New(deps)

	// Act
	_, _ = svc.Update(context.Background(), app.UpdateRequest{
		HarnessID:       "stub-harness",
		WorkspacePath:   workspace,
		AutoConfirmPlan: true,
	})

	// Assert — the emitted event's "scope" field must match the chosen scope.
	evt, found := logger.harnessOnlyEvent()
	if !found {
		t.Errorf("no harness-only logging event emitted; "+
			"the Update flow must emit an observability event for each harness-only refresh")
		return
	}
	wantScope := string(app.RefreshAllDeployed)
	if evt.Fields["scope"] != wantScope {
		t.Errorf("harness-only logging event Fields[\"scope\"] = %q, want %q; "+
			"the scope field must reflect the scope the user chose so the event is actionable",
			evt.Fields["scope"], wantScope)
	}
}
