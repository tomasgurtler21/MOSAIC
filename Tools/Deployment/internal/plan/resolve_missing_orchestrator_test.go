package plan_test

// resolve_missing_orchestrator_test.go covers refusal behavior when artifact resolution
// or plan computation is attempted against a catalogue that carries no orchestrator agent
// definition.
//
// Full-deployment-mode refusal (T2.1):
//
//   - ResolveArtifactsFrom returns ErrMissingOrchestrator (via errors.Is) when the
//     catalogue's orchestrator agent has an empty key and ExcludeOrchestrator is false.
//   - The wrapped error message includes the catalogue root path, naming where the
//     missing orchestrator was sought.
//   - The refusal fires for every workspace mode (deploy-workspace, update-workspace,
//     update-workflows, promote-to-generic, transform-harness) because each maps to
//     ExcludeOrchestrator == false via OrchestratorExcludedFor.
//   - plan.Build propagates the same refusal when called in a full-deployment mode.
//
// Orchestrator-excluding mode pass-through (T2.2, regression guards):
//
//   - ModeDeployAgents and ModeDeployHooks set ExcludeOrchestrator to true and must
//     resolve successfully against an orchestrator-less catalogue exactly as they do today.
//   - No ErrMissingOrchestrator is ever returned on those paths.
//
// Orchestrator-present invariants (T2.3, regression guards):
//
//   - A catalogue carrying a real (non-empty-key) orchestrator never returns
//     ErrMissingOrchestrator in any mode or selection shape.
//   - No successful resolution ever returns an ArtifactSet containing an agent with
//     an empty key.

import (
	"context"
	"errors"
	"strings"
	"testing"

	"mosaic-deploy/internal/domain"
	"mosaic-deploy/internal/plan"
)

// ---------------------------------------------------------------------------
// Shared fixture helpers
// ---------------------------------------------------------------------------

// orchestratorlessCatalog returns a fakeCatalog with no orchestrator (the orchestrator
// field is the zero value, resulting in Orchestrator().Key == "").
// The catalogue root is set to the given path so error-message assertions can check it.
func orchestratorlessCatalog(root string) *fakeCatalog {
	return &fakeCatalog{
		root: root,
		// orchestrator field is deliberately left at its zero value (domain.Agent{}).
	}
}

// orchestratorlessCatalogWithWorkflow returns an orchestrator-less fakeCatalog that also
// carries one worker agent and one workflow referencing it. Used in scenarios that exercise
// the workflow-resolution path in addition to the orchestrator-refusal path.
func orchestratorlessCatalogWithWorkflow(root string) *fakeCatalog {
	worker := makeAgent("worker-a", "1.0")
	return &fakeCatalog{
		root:    root,
		workers: []domain.Agent{worker},
		workflows: []domain.Workflow{
			makeWorkflow("my-workflow", "worker-a"),
		},
		// orchestrator field is deliberately left at its zero value.
	}
}

// ---------------------------------------------------------------------------
// T2.1 — Full-deployment-mode refusal
// ---------------------------------------------------------------------------

// TestResolveArtifactsFrom_OrchestratorAbsent_FullMode_ReturnsErrMissingOrchestrator
// verifies that ResolveArtifactsFrom refuses (via plan.ErrMissingOrchestrator) when the
// catalogue's orchestrator has an empty key and ExcludeOrchestrator is false. An absent
// orchestrator must never silently produce an artifact set containing an empty-key agent.
func TestResolveArtifactsFrom_OrchestratorAbsent_FullMode_ReturnsErrMissingOrchestrator(t *testing.T) {
	cat := orchestratorlessCatalog("/fake/catalogue")

	_, err := plan.ResolveArtifactsFrom(cat, plan.Selection{
		ExcludeOrchestrator: false,
	})

	if err == nil {
		t.Fatal("ResolveArtifactsFrom with orchestrator-less catalogue: expected error, got nil; " +
			"a full-mode deployment must refuse when no orchestrator is defined rather than " +
			"silently producing an artifact set with an empty-key agent")
	}
	if !errors.Is(err, plan.ErrMissingOrchestrator) {
		t.Errorf("ResolveArtifactsFrom: got error %v, want errors.Is(err, plan.ErrMissingOrchestrator); " +
			"the error must wrap ErrMissingOrchestrator so callers can identify this specific refusal", err)
	}
}

// TestResolveArtifactsFrom_OrchestratorAbsent_FullMode_ErrorMessageNamesCatalogueRoot
// verifies that the error returned for an absent orchestrator names the catalogue root path
// where the orchestrator was sought. Operators must be able to identify which catalogue
// folder was inspected when diagnosing the failure.
func TestResolveArtifactsFrom_OrchestratorAbsent_FullMode_ErrorMessageNamesCatalogueRoot(t *testing.T) {
	const catalogueRoot = "/path/to/my/catalogue"
	cat := orchestratorlessCatalog(catalogueRoot)

	_, err := plan.ResolveArtifactsFrom(cat, plan.Selection{
		ExcludeOrchestrator: false,
	})

	if err == nil {
		t.Fatal("ResolveArtifactsFrom with orchestrator-less catalogue: expected error, got nil; " +
			"cannot check error message content when no error was returned")
	}
	if !strings.Contains(err.Error(), catalogueRoot) {
		t.Errorf("error message %q does not contain catalogue root %q; "+
			"the message must name where the orchestrator was sought so operators can diagnose the failure",
			err.Error(), catalogueRoot)
	}
}

// TestResolveArtifactsFrom_OrchestratorAbsent_AllFullModes_Refuse verifies that the
// refusal fires for every workspace-oriented mode — not just one specific mode. Each mode
// that maps to OrchestratorExcludedFor == false must produce ErrMissingOrchestrator when
// the catalogue has no orchestrator.
func TestResolveArtifactsFrom_OrchestratorAbsent_AllFullModes_Refuse(t *testing.T) {
	fullModes := []domain.RunMode{
		domain.ModeDeployWorkspace,
		domain.ModeUpdateWorkspace,
		domain.ModeUpdateWorkflows,
		domain.ModePromoteToGeneric,
		domain.ModeTransformHarness,
	}

	for _, mode := range fullModes {
		t.Run(string(mode), func(t *testing.T) {
			cat := orchestratorlessCatalog("/fake/catalogue")
			excludeOrch := plan.OrchestratorExcludedFor(mode)

			_, err := plan.ResolveArtifactsFrom(cat, plan.Selection{
				ExcludeOrchestrator: excludeOrch,
			})

			if excludeOrch {
				// This mode excludes the orchestrator; no refusal expected.
				if errors.Is(err, plan.ErrMissingOrchestrator) {
					t.Errorf("mode %q: OrchestratorExcludedFor returned true but got ErrMissingOrchestrator; "+
						"orchestrator-excluding modes must never refuse an orchestrator-less catalogue", mode)
				}
				return
			}

			// Full-deployment mode: refusal required.
			if err == nil {
				t.Errorf("mode %q: expected ErrMissingOrchestrator, got nil; "+
					"every mode where OrchestratorExcludedFor returns false must refuse an orchestrator-less catalogue", mode)
				return
			}
			if !errors.Is(err, plan.ErrMissingOrchestrator) {
				t.Errorf("mode %q: got error %v, want errors.Is(err, plan.ErrMissingOrchestrator)", mode, err)
			}
		})
	}
}

// TestResolveArtifactsFrom_OrchestratorAbsent_FullMode_WithWorkflows_Refuses verifies that
// the orchestrator refusal fires even when workflows are selected. The refusal is not
// bypassed by having workflow agents in the selection.
func TestResolveArtifactsFrom_OrchestratorAbsent_FullMode_WithWorkflows_Refuses(t *testing.T) {
	cat := orchestratorlessCatalogWithWorkflow("/fake/catalogue")

	_, err := plan.ResolveArtifactsFrom(cat, plan.Selection{
		WorkflowIDs:         []string{"my-workflow"},
		ExcludeOrchestrator: false,
	})

	if err == nil {
		t.Fatal("ResolveArtifactsFrom with orchestrator-less catalogue and workflow selection: " +
			"expected ErrMissingOrchestrator, got nil; " +
			"workflow selections must not bypass the orchestrator-absent refusal")
	}
	if !errors.Is(err, plan.ErrMissingOrchestrator) {
		t.Errorf("got error %v, want errors.Is(err, plan.ErrMissingOrchestrator)", err)
	}
}

// TestBuild_OrchestratorAbsent_FullMode_ReturnsErrMissingOrchestrator verifies that
// plan.Build propagates ErrMissingOrchestrator from ResolveArtifactsFrom. No workspace
// files are written when Build returns early with an error, satisfying the module's
// no-writes-before-plan invariant.
func TestBuild_OrchestratorAbsent_FullMode_ReturnsErrMissingOrchestrator(t *testing.T) {
	cat := orchestratorlessCatalog("/fake/catalogue")
	module := newFakeModule()
	p := plan.New()

	in := plan.Input{
		Catalog:       cat,
		Module:        module,
		Mode:          domain.ModeDeployWorkspace,
		WorkspacePath: t.TempDir(),
		Scope:         domain.ScopeProject,
		GOOS:          "linux",
		Manifest:      absentSnapshot(),
		WorkflowIDs:   []string{},
	}

	_, err := p.Build(context.Background(), in)

	if err == nil {
		t.Fatal("plan.Build with orchestrator-less catalogue in ModeDeployWorkspace: expected error, got nil; " +
			"Build must refuse and return early — no artifact set can be produced for a full-mode deployment " +
			"without an orchestrator, which ensures no workspace writes follow this refusal")
	}
	if !errors.Is(err, plan.ErrMissingOrchestrator) {
		t.Errorf("plan.Build: got error %v, want errors.Is(err, plan.ErrMissingOrchestrator)", err)
	}
}

// ---------------------------------------------------------------------------
// T2.2 — Orchestrator-excluding mode pass-through (regression guards)
// ---------------------------------------------------------------------------
// These tests assert existing behavior that must be preserved by the Stage 2 change.
// They start GREEN and must remain GREEN after the implementation lands.

// TestResolveArtifactsFrom_OrchestratorAbsent_ModeDeployAgents_Succeeds verifies that
// a catalogue without an orchestrator resolves successfully under ModeDeployAgents, which
// sets ExcludeOrchestrator to true via OrchestratorExcludedFor. The new refusal must not
// be introduced on this path.
func TestResolveArtifactsFrom_OrchestratorAbsent_ModeDeployAgents_Succeeds(t *testing.T) {
	standalone := makeStandaloneAgent("session-logger")
	cat := &fakeCatalog{
		root:             "/fake/catalogue",
		standaloneAgents: []domain.Agent{standalone},
		// orchestrator: zero value — the key invariant of this scenario.
	}

	_, err := plan.ResolveArtifactsFrom(cat, plan.Selection{
		StandaloneAgentIDs:  []string{"session-logger"},
		ExcludeOrchestrator: plan.OrchestratorExcludedFor(domain.ModeDeployAgents),
	})

	if errors.Is(err, plan.ErrMissingOrchestrator) {
		t.Error("ResolveArtifactsFrom with ExcludeOrchestrator=true (ModeDeployAgents) returned " +
			"ErrMissingOrchestrator; orchestrator-excluding modes must succeed with an orchestrator-less " +
			"catalogue exactly as today — the new refusal guard must be inside the ExcludeOrchestrator==false branch")
	}
	if err != nil {
		t.Errorf("ResolveArtifactsFrom (ModeDeployAgents, no orchestrator): unexpected error %v; "+
			"orchestrator-excluding modes must not be affected by the new refusal", err)
	}
}

// TestResolveArtifactsFrom_OrchestratorAbsent_ModeDeployHooks_Succeeds verifies that
// a catalogue without an orchestrator resolves successfully under ModeDeployHooks.
func TestResolveArtifactsFrom_OrchestratorAbsent_ModeDeployHooks_Succeeds(t *testing.T) {
	hookBundle := makeHookBundle("my-hooks", "1.0")
	cat := &fakeCatalog{
		root:  "/fake/catalogue",
		hooks: []domain.HookBundle{hookBundle},
		// orchestrator: zero value.
	}

	_, err := plan.ResolveArtifactsFrom(cat, plan.Selection{
		HookIDs:             []string{"my-hooks"},
		ExcludeOrchestrator: plan.OrchestratorExcludedFor(domain.ModeDeployHooks),
	})

	if errors.Is(err, plan.ErrMissingOrchestrator) {
		t.Error("ResolveArtifactsFrom with ExcludeOrchestrator=true (ModeDeployHooks) returned " +
			"ErrMissingOrchestrator; ModeDeployHooks must succeed with an orchestrator-less catalogue " +
			"exactly as today — the ExcludeOrchestrator path must not be affected by the new guard")
	}
	if err != nil {
		t.Errorf("ResolveArtifactsFrom (ModeDeployHooks, no orchestrator): unexpected error %v", err)
	}
}

// TestOrchestratorExcludedFor_ExcludingModes_NeverRefuse verifies that every mode for
// which OrchestratorExcludedFor returns true passes through an orchestrator-less catalogue
// without ErrMissingOrchestrator. This is the single declarative guard for the full set
// of excluding modes — if new modes are added and mapped to true, they inherit this guard.
func TestOrchestratorExcludedFor_ExcludingModes_NeverRefuse(t *testing.T) {
	excludingModes := []domain.RunMode{
		domain.ModeDeployAgents,
		domain.ModeDeployHooks,
	}

	for _, mode := range excludingModes {
		t.Run(string(mode), func(t *testing.T) {
			cat := orchestratorlessCatalog("/fake/catalogue")

			_, err := plan.ResolveArtifactsFrom(cat, plan.Selection{
				ExcludeOrchestrator: plan.OrchestratorExcludedFor(mode),
			})

			if errors.Is(err, plan.ErrMissingOrchestrator) {
				t.Errorf("mode %q (ExcludeOrchestrator=true): got ErrMissingOrchestrator; "+
					"orchestrator-excluding modes must not refuse an orchestrator-less catalogue", mode)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// T2.3 — Orchestrator-present invariants (regression guards)
// ---------------------------------------------------------------------------
// These tests assert existing behavior that must be preserved. They also pin the invariant
// that no successful resolution ever contains an agent with an empty key.

// TestResolveArtifactsFrom_OrchestratorPresent_FullMode_Succeeds verifies that a catalogue
// carrying a real orchestrator resolves without error in a full-deployment mode. The new
// ErrMissingOrchestrator refusal must not fire when the orchestrator is present.
func TestResolveArtifactsFrom_OrchestratorPresent_FullMode_Succeeds(t *testing.T) {
	cat := &fakeCatalog{
		root:         "/fake/catalogue",
		orchestrator: makeOrchestrator(),
	}

	_, err := plan.ResolveArtifactsFrom(cat, plan.Selection{
		ExcludeOrchestrator: false,
	})

	if errors.Is(err, plan.ErrMissingOrchestrator) {
		t.Error("ResolveArtifactsFrom with a real orchestrator returned ErrMissingOrchestrator; " +
			"the refusal must fire only when the orchestrator agent key is empty, not for a present orchestrator")
	}
	if err != nil {
		t.Errorf("ResolveArtifactsFrom with real orchestrator: unexpected error %v; "+
			"a catalogue carrying an orchestrator must resolve unchanged", err)
	}
}

// TestResolveArtifactsFrom_OrchestratorPresent_OrchestratorInResultSet verifies that a
// real orchestrator appears in the resolved artifact set. The guard introduced for missing
// orchestrators must not affect normal resolution when an orchestrator is present.
func TestResolveArtifactsFrom_OrchestratorPresent_OrchestratorInResultSet(t *testing.T) {
	orc := makeOrchestrator()
	cat := &fakeCatalog{
		root:         "/fake/catalogue",
		orchestrator: orc,
	}

	set, err := plan.ResolveArtifactsFrom(cat, plan.Selection{
		ExcludeOrchestrator: false,
	})
	if err != nil {
		t.Fatalf("ResolveArtifactsFrom with real orchestrator: unexpected error %v", err)
	}

	if !containsAgent(set.Agents, orc.Key) {
		t.Errorf("ArtifactSet.Agents does not contain orchestrator %q; "+
			"a present orchestrator must be included in the resolved set unchanged", orc.Key)
	}
}

// TestResolveArtifactsFrom_OrchestratorPresent_NoEmptyKeyAgents verifies that no agent
// with an empty key appears in any successfully resolved artifact set. An empty-key agent
// would produce a deployment action with no agent identity and must never reach the executor.
func TestResolveArtifactsFrom_OrchestratorPresent_NoEmptyKeyAgents(t *testing.T) {
	worker := makeAgent("worker-a", "1.0")
	cat := &fakeCatalog{
		root:         "/fake/catalogue",
		orchestrator: makeOrchestrator(),
		workers:      []domain.Agent{worker},
		workflows:    []domain.Workflow{makeWorkflow("my-workflow", "worker-a")},
	}

	set, err := plan.ResolveArtifactsFrom(cat, plan.Selection{
		WorkflowIDs:         []string{"my-workflow"},
		ExcludeOrchestrator: false,
	})
	if err != nil {
		t.Fatalf("ResolveArtifactsFrom: unexpected error %v", err)
	}

	for _, a := range set.Agents {
		if a.Key == "" {
			t.Errorf("ArtifactSet.Agents contains an agent with an empty key; "+
				"no deployment action must ever be emitted for an agent with no key — "+
				"this would produce a deployment action with an empty agent identity; "+
				"current agents in set: %v", agentKeys(set.Agents))
		}
	}
}

// TestResolveArtifactsFrom_ExcludingMode_NoEmptyKeyAgents verifies that no empty-key agent
// appears in the result of an orchestrator-excluding resolution. Even when the catalogue has
// no orchestrator and the mode excludes it, the resulting set must have no empty keys.
func TestResolveArtifactsFrom_ExcludingMode_NoEmptyKeyAgents(t *testing.T) {
	standalone := makeStandaloneAgent("session-logger")
	cat := &fakeCatalog{
		root:             "/fake/catalogue",
		standaloneAgents: []domain.Agent{standalone},
		// orchestrator: zero value (key == "") — excluded by ExcludeOrchestrator.
	}

	set, err := plan.ResolveArtifactsFrom(cat, plan.Selection{
		StandaloneAgentIDs:  []string{"session-logger"},
		ExcludeOrchestrator: true,
	})
	if err != nil {
		t.Fatalf("ResolveArtifactsFrom with ExcludeOrchestrator=true: unexpected error %v", err)
	}

	for _, a := range set.Agents {
		if a.Key == "" {
			t.Errorf("ArtifactSet.Agents contains an empty-key agent even with ExcludeOrchestrator=true; "+
				"the orchestrator must be fully excluded, not included with a blank key; "+
				"current agents in set: %v", agentKeys(set.Agents))
		}
	}
}

// TestResolveArtifactsFrom_OrchestratorPresent_AllModes_NeverRefuses verifies that a
// catalogue carrying a real orchestrator never triggers ErrMissingOrchestrator in any
// mode, including every workspace mode and every orchestrator-excluding mode.
func TestResolveArtifactsFrom_OrchestratorPresent_AllModes_NeverRefuses(t *testing.T) {
	allModes := []domain.RunMode{
		domain.ModeDeployWorkspace,
		domain.ModeUpdateWorkspace,
		domain.ModeUpdateWorkflows,
		domain.ModePromoteToGeneric,
		domain.ModeTransformHarness,
		domain.ModeDeployAgents,
		domain.ModeDeployHooks,
	}

	for _, mode := range allModes {
		t.Run(string(mode), func(t *testing.T) {
			cat := &fakeCatalog{
				root:         "/fake/catalogue",
				orchestrator: makeOrchestrator(),
			}
			excludeOrch := plan.OrchestratorExcludedFor(mode)

			_, err := plan.ResolveArtifactsFrom(cat, plan.Selection{
				ExcludeOrchestrator: excludeOrch,
			})

			if errors.Is(err, plan.ErrMissingOrchestrator) {
				t.Errorf("mode %q: got ErrMissingOrchestrator even though a real orchestrator is present; "+
					"the refusal must fire only for a truly absent orchestrator (empty key), "+
					"not for a catalogue that carries a valid orchestrator definition", mode)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Helpers local to this file
// ---------------------------------------------------------------------------

// agentKeys returns the keys of all agents in the slice, for use in diagnostic messages.
func agentKeys(agents []domain.Agent) []string {
	keys := make([]string, len(agents))
	for i, a := range agents {
		keys[i] = a.Key
	}
	return keys
}
