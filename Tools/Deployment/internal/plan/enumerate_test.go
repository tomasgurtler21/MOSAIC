package plan_test

// enumerate_test.go covers the pure target-path enumeration helper EnumerateTargetPaths.
//
// Verified invariants:
//
//   Agents: an agent in the ArtifactSet gets a corresponding PlannedPath entry.
//   Skills: a skill in the ArtifactSet gets a corresponding PlannedPath entry.
//   Hooks (supported): a hook whose TargetPath resolves successfully is included.
//   Hooks (unsupported): a hook whose TargetPath returns an error is silently omitted.
//   Agent failure: an agent whose TargetPath returns an error propagates the error.
//   Skill failure: a skill whose TargetPath returns an error propagates the error.
//   Order: result order is agents, then skills, then hooks, within each group preserving
//          ArtifactSet order.
//   Path lookup: PlannedPaths.Path finds the correct entry by ArtifactRef.
//   Pure: no file-system access — the test uses no temp directory.

import (
	"errors"
	"testing"

	"mosaic-deploy/internal/domain"
	"mosaic-deploy/internal/plan"
)

// ---------------------------------------------------------------------------
// Agent path enumeration
// ---------------------------------------------------------------------------

// TestEnumerateTargetPaths_SingleAgent_ReturnsAgentPath verifies that one agent in the set
// produces one PlannedPath with the correct TargetPath.
func TestEnumerateTargetPaths_SingleAgent_ReturnsAgentPath(t *testing.T) {
	set := plan.ArtifactSet{
		Agents: []domain.Agent{
			makeAgent("test-runner", "1.0"),
		},
	}
	mod := newFakeModule()

	paths, err := plan.EnumerateTargetPaths(set, mod, domain.ScopeProject, "linux")

	if err != nil {
		t.Fatalf("EnumerateTargetPaths: %v", err)
	}
	ref := domain.ArtifactRef{Kind: domain.ArtifactAgent, Key: "test-runner"}
	p, ok := paths.Path(ref)
	if !ok {
		t.Fatal("expected a planned path for agent test-runner, not found")
	}
	if p == "" {
		t.Error("planned path for agent must be non-empty")
	}
	// The fakeModule's default path for an agent is "agents/<key>.md".
	want := "agents/test-runner.md"
	if p != want {
		t.Errorf("agent path = %q, want %q", p, want)
	}
}

// TestEnumerateTargetPaths_MultipleAgents_AllIncluded verifies that each agent in the set
// produces exactly one PlannedPath entry.
func TestEnumerateTargetPaths_MultipleAgents_AllIncluded(t *testing.T) {
	set := plan.ArtifactSet{
		Agents: []domain.Agent{
			makeAgent("agent-a", "1.0"),
			makeAgent("agent-b", "1.0"),
		},
	}
	mod := newFakeModule()

	paths, err := plan.EnumerateTargetPaths(set, mod, domain.ScopeProject, "linux")

	if err != nil {
		t.Fatalf("EnumerateTargetPaths: %v", err)
	}
	if len(paths) != 2 {
		t.Errorf("expected 2 PlannedPath entries, got %d", len(paths))
	}
	for _, key := range []string{"agent-a", "agent-b"} {
		ref := domain.ArtifactRef{Kind: domain.ArtifactAgent, Key: key}
		if _, ok := paths.Path(ref); !ok {
			t.Errorf("no planned path for agent %q", key)
		}
	}
}

// ---------------------------------------------------------------------------
// Skill path enumeration
// ---------------------------------------------------------------------------

// TestEnumerateTargetPaths_SingleSkill_ReturnsSkillPath verifies that one skill in the set
// produces one PlannedPath with the correct TargetPath.
func TestEnumerateTargetPaths_SingleSkill_ReturnsSkillPath(t *testing.T) {
	set := plan.ArtifactSet{
		Skills: []domain.Skill{
			makeSkill("lean-tdd", "1.0"),
		},
	}
	mod := newFakeModule()

	paths, err := plan.EnumerateTargetPaths(set, mod, domain.ScopeProject, "linux")

	if err != nil {
		t.Fatalf("EnumerateTargetPaths: %v", err)
	}
	ref := domain.ArtifactRef{Kind: domain.ArtifactSkill, Key: "lean-tdd"}
	p, ok := paths.Path(ref)
	if !ok {
		t.Fatal("expected a planned path for skill lean-tdd, not found")
	}
	// fakeModule default: "skills/<key>/SKILL.md"
	want := "skills/lean-tdd/SKILL.md"
	if p != want {
		t.Errorf("skill path = %q, want %q", p, want)
	}
}

// ---------------------------------------------------------------------------
// Hook path enumeration
// ---------------------------------------------------------------------------

// TestEnumerateTargetPaths_SupportedHook_IncludedInResult verifies that a hook bundle whose
// TargetPath resolves successfully appears in the PlannedPaths.
func TestEnumerateTargetPaths_SupportedHook_IncludedInResult(t *testing.T) {
	set := plan.ArtifactSet{
		Hooks: []domain.HookBundle{
			makeHookBundle("pre-tool", "1.0"),
		},
	}
	mod := newFakeModule()

	paths, err := plan.EnumerateTargetPaths(set, mod, domain.ScopeProject, "linux")

	if err != nil {
		t.Fatalf("EnumerateTargetPaths: %v", err)
	}
	ref := domain.ArtifactRef{Kind: domain.ArtifactHook, Key: "pre-tool"}
	if _, ok := paths.Path(ref); !ok {
		t.Error("supported hook must appear in PlannedPaths")
	}
}

// TestEnumerateTargetPaths_UnsupportedHook_SilentlyOmitted verifies that a hook bundle whose
// TargetPath returns any error is silently omitted from the result — this matches plan.Build's
// "skip unsupported hook" behaviour and keeps the probe from attempting to read a non-existent
// bundle path.
func TestEnumerateTargetPaths_UnsupportedHook_SilentlyOmitted(t *testing.T) {
	set := plan.ArtifactSet{
		Hooks: []domain.HookBundle{
			makeHookBundle("unsupported-hook", "1.0"),
		},
	}
	// Configure the fakeModule to return ErrArtifactUnsupported for the hook.
	mod := newFakeModule()
	mod.TargetPathFn = func(req domain.TargetPathRequest) (string, error) {
		if req.Kind == domain.ArtifactHook {
			return "", domain.ErrArtifactUnsupported
		}
		// Default for other kinds.
		return "", nil
	}

	paths, err := plan.EnumerateTargetPaths(set, mod, domain.ScopeProject, "linux")

	if err != nil {
		t.Fatalf("unsupported hook must not cause EnumerateTargetPaths to error, got: %v", err)
	}
	ref := domain.ArtifactRef{Kind: domain.ArtifactHook, Key: "unsupported-hook"}
	if _, ok := paths.Path(ref); ok {
		t.Error("unsupported hook must be silently omitted, but was found in PlannedPaths")
	}
	if len(paths) != 0 {
		t.Errorf("expected empty result for unsupported-only hook set, got %d entries", len(paths))
	}
}

// TestEnumerateTargetPaths_MixedHookSupport_OnlySupportedIncluded verifies that when some
// hooks are supported and some are not, only the supported ones appear in the result.
func TestEnumerateTargetPaths_MixedHookSupport_OnlySupportedIncluded(t *testing.T) {
	set := plan.ArtifactSet{
		Hooks: []domain.HookBundle{
			makeHookBundle("supported-hook", "1.0"),
			makeHookBundle("unsupported-hook", "1.0"),
		},
	}
	mod := newFakeModule()
	mod.TargetPathFn = func(req domain.TargetPathRequest) (string, error) {
		if req.Kind == domain.ArtifactHook && req.Key == "unsupported-hook" {
			return "", domain.ErrArtifactUnsupported
		}
		switch req.Kind {
		case domain.ArtifactAgent:
			return "agents/" + req.Key + ".md", nil
		case domain.ArtifactSkill:
			return "skills/" + req.Key + "/SKILL.md", nil
		case domain.ArtifactHook:
			return "hooks/" + req.Key, nil
		}
		return "", domain.ErrArtifactUnsupported
	}

	paths, err := plan.EnumerateTargetPaths(set, mod, domain.ScopeProject, "linux")

	if err != nil {
		t.Fatalf("EnumerateTargetPaths: %v", err)
	}
	supportedRef := domain.ArtifactRef{Kind: domain.ArtifactHook, Key: "supported-hook"}
	unsupportedRef := domain.ArtifactRef{Kind: domain.ArtifactHook, Key: "unsupported-hook"}
	if _, ok := paths.Path(supportedRef); !ok {
		t.Error("supported-hook must be included in PlannedPaths")
	}
	if _, ok := paths.Path(unsupportedRef); ok {
		t.Error("unsupported-hook must be omitted from PlannedPaths")
	}
}

// ---------------------------------------------------------------------------
// Error propagation for agents and skills
// ---------------------------------------------------------------------------

// TestEnumerateTargetPaths_AgentPathFails_ReturnsError verifies that when TargetPath returns
// an error for an agent, EnumerateTargetPaths propagates the error (unlike hooks, which are
// silently omitted).
func TestEnumerateTargetPaths_AgentPathFails_ReturnsError(t *testing.T) {
	set := plan.ArtifactSet{
		Agents: []domain.Agent{
			makeAgent("failing-agent", "1.0"),
		},
	}
	failErr := errors.New("agent path resolution failed")
	mod := newFakeModule()
	mod.TargetPathFn = func(req domain.TargetPathRequest) (string, error) {
		if req.Kind == domain.ArtifactAgent {
			return "", failErr
		}
		return "", nil
	}

	_, err := plan.EnumerateTargetPaths(set, mod, domain.ScopeProject, "linux")

	if err == nil {
		t.Fatal("expected error when agent TargetPath fails, got nil")
	}
	if !errors.Is(err, failErr) {
		t.Errorf("error %v does not wrap the original failure %v", err, failErr)
	}
}

// TestEnumerateTargetPaths_SkillPathFails_ReturnsError verifies that when TargetPath returns
// an error for a skill, EnumerateTargetPaths propagates the error.
func TestEnumerateTargetPaths_SkillPathFails_ReturnsError(t *testing.T) {
	set := plan.ArtifactSet{
		Skills: []domain.Skill{
			makeSkill("failing-skill", "1.0"),
		},
	}
	failErr := errors.New("skill path resolution failed")
	mod := newFakeModule()
	mod.TargetPathFn = func(req domain.TargetPathRequest) (string, error) {
		if req.Kind == domain.ArtifactSkill {
			return "", failErr
		}
		return "", nil
	}

	_, err := plan.EnumerateTargetPaths(set, mod, domain.ScopeProject, "linux")

	if err == nil {
		t.Fatal("expected error when skill TargetPath fails, got nil")
	}
	if !errors.Is(err, failErr) {
		t.Errorf("error %v does not wrap the original failure %v", err, failErr)
	}
}

// ---------------------------------------------------------------------------
// Mixed artifact set and ordering
// ---------------------------------------------------------------------------

// TestEnumerateTargetPaths_MixedSet_AllArtifactsIncluded verifies that when the set contains
// agents, skills, and a supported hook, all three appear in the result.
func TestEnumerateTargetPaths_MixedSet_AllArtifactsIncluded(t *testing.T) {
	set := plan.ArtifactSet{
		Agents: []domain.Agent{makeAgent("worker", "1.0")},
		Skills: []domain.Skill{makeSkill("lean-tdd", "1.0")},
		Hooks:  []domain.HookBundle{makeHookBundle("pre-push", "1.0")},
	}
	mod := newFakeModule()

	paths, err := plan.EnumerateTargetPaths(set, mod, domain.ScopeProject, "linux")

	if err != nil {
		t.Fatalf("EnumerateTargetPaths: %v", err)
	}
	if len(paths) != 3 {
		t.Errorf("expected 3 planned paths (1 agent + 1 skill + 1 hook), got %d", len(paths))
	}
	for _, tc := range []struct {
		kind domain.ArtifactKind
		key  string
	}{
		{domain.ArtifactAgent, "worker"},
		{domain.ArtifactSkill, "lean-tdd"},
		{domain.ArtifactHook, "pre-push"},
	} {
		ref := domain.ArtifactRef{Kind: tc.kind, Key: tc.key}
		if _, ok := paths.Path(ref); !ok {
			t.Errorf("no planned path for %s %q", tc.kind, tc.key)
		}
	}
}

// TestEnumerateTargetPaths_MixedSetWithUnsupportedHook_UnsupportedOmitted verifies that in
// a mixed set, only the unsupported hook is omitted while agents, skills, and supported hooks
// are all included.
func TestEnumerateTargetPaths_MixedSetWithUnsupportedHook_UnsupportedOmitted(t *testing.T) {
	set := plan.ArtifactSet{
		Agents: []domain.Agent{makeAgent("worker", "1.0")},
		Skills: []domain.Skill{makeSkill("lean-tdd", "1.0")},
		Hooks: []domain.HookBundle{
			makeHookBundle("supported", "1.0"),
			makeHookBundle("unsupported", "1.0"),
		},
	}
	mod := newFakeModule()
	mod.TargetPathFn = func(req domain.TargetPathRequest) (string, error) {
		if req.Kind == domain.ArtifactHook && req.Key == "unsupported" {
			return "", domain.ErrArtifactUnsupported
		}
		switch req.Kind {
		case domain.ArtifactAgent:
			return "agents/" + req.Key + ".md", nil
		case domain.ArtifactSkill:
			return "skills/" + req.Key + "/SKILL.md", nil
		case domain.ArtifactHook:
			return "hooks/" + req.Key, nil
		}
		return "", domain.ErrArtifactUnsupported
	}

	paths, err := plan.EnumerateTargetPaths(set, mod, domain.ScopeProject, "linux")

	if err != nil {
		t.Fatalf("EnumerateTargetPaths: %v", err)
	}
	// Expect: 1 agent + 1 skill + 1 supported hook = 3
	if len(paths) != 3 {
		t.Errorf("expected 3 planned paths, got %d", len(paths))
	}
	unsupportedRef := domain.ArtifactRef{Kind: domain.ArtifactHook, Key: "unsupported"}
	if _, ok := paths.Path(unsupportedRef); ok {
		t.Error("unsupported hook must not appear in the result")
	}
}

// ---------------------------------------------------------------------------
// Ordering guarantee: agents before skills before hooks
// ---------------------------------------------------------------------------

// TestEnumerateTargetPaths_OrderingGuarantee_AgentsBeforeSkillsBeforeHooks verifies that the
// result places every agent entry before any skill entry, and every skill entry before any hook
// entry. This ordering guarantee is documented in the PlannedPaths type comment and is relied
// upon by the probe, which iterates paths and keys results by target-path string.
func TestEnumerateTargetPaths_OrderingGuarantee_AgentsBeforeSkillsBeforeHooks(t *testing.T) {
	set := plan.ArtifactSet{
		Agents: []domain.Agent{
			makeAgent("agent-a", "1.0"),
			makeAgent("agent-b", "1.0"),
		},
		Skills: []domain.Skill{
			makeSkill("skill-x", "1.0"),
		},
		Hooks: []domain.HookBundle{
			makeHookBundle("hook-y", "1.0"),
		},
	}
	mod := newFakeModule()

	paths, err := plan.EnumerateTargetPaths(set, mod, domain.ScopeProject, "linux")

	if err != nil {
		t.Fatalf("EnumerateTargetPaths: %v", err)
	}
	if len(paths) != 4 {
		t.Fatalf("expected 4 planned paths (2 agents + 1 skill + 1 hook), got %d", len(paths))
	}

	// Determine the index range occupied by each kind.
	maxAgentIdx := -1
	minSkillIdx := len(paths) // sentinel: "no skills seen"
	maxSkillIdx := -1
	minHookIdx := len(paths) // sentinel: "no hooks seen"
	for i, pp := range paths {
		switch pp.Ref.Kind {
		case domain.ArtifactAgent:
			if i > maxAgentIdx {
				maxAgentIdx = i
			}
		case domain.ArtifactSkill:
			if i < minSkillIdx {
				minSkillIdx = i
			}
			if i > maxSkillIdx {
				maxSkillIdx = i
			}
		case domain.ArtifactHook:
			if i < minHookIdx {
				minHookIdx = i
			}
		}
	}

	// All agents must appear before all skills.
	if maxAgentIdx != -1 && minSkillIdx != len(paths) && maxAgentIdx >= minSkillIdx {
		t.Errorf("agent at index %d appears at or after first skill at index %d; "+
			"the ordering guarantee requires all agents before any skill", maxAgentIdx, minSkillIdx)
	}

	// All skills must appear before all hooks.
	if maxSkillIdx != -1 && minHookIdx != len(paths) && maxSkillIdx >= minHookIdx {
		t.Errorf("skill at index %d appears at or after first hook at index %d; "+
			"the ordering guarantee requires all skills before any hook", maxSkillIdx, minHookIdx)
	}

	// Transitively: all agents must appear before all hooks (covered implicitly by the two
	// checks above, but stated explicitly for clarity in failure output).
	if maxAgentIdx != -1 && minHookIdx != len(paths) && maxAgentIdx >= minHookIdx {
		t.Errorf("agent at index %d appears at or after first hook at index %d; "+
			"the ordering guarantee requires all agents before any hook", maxAgentIdx, minHookIdx)
	}
}

// ---------------------------------------------------------------------------
// Empty input
// ---------------------------------------------------------------------------

// TestEnumerateTargetPaths_EmptySet_ReturnsEmptyResult verifies that an empty ArtifactSet
// produces a non-nil empty PlannedPaths, not nil.
func TestEnumerateTargetPaths_EmptySet_ReturnsEmptyResult(t *testing.T) {
	set := plan.ArtifactSet{}
	mod := newFakeModule()

	paths, err := plan.EnumerateTargetPaths(set, mod, domain.ScopeProject, "linux")

	if err != nil {
		t.Fatalf("EnumerateTargetPaths on empty set: %v", err)
	}
	if paths == nil {
		t.Error("expected non-nil empty PlannedPaths for empty artifact set")
	}
	if len(paths) != 0 {
		t.Errorf("expected 0 planned paths for empty set, got %d", len(paths))
	}
}

// ---------------------------------------------------------------------------
// PlannedPaths.Path lookup
// ---------------------------------------------------------------------------

// TestPlannedPaths_PathLookup_FoundAndNotFound verifies that PlannedPaths.Path returns the
// correct path when the ref is present and (false, "") when absent.
func TestPlannedPaths_PathLookup_FoundAndNotFound(t *testing.T) {
	paths := plan.PlannedPaths{
		{
			Ref:        domain.ArtifactRef{Kind: domain.ArtifactAgent, Key: "agent-x"},
			TargetPath: "agents/agent-x.md",
		},
	}

	p, ok := paths.Path(domain.ArtifactRef{Kind: domain.ArtifactAgent, Key: "agent-x"})
	if !ok {
		t.Fatal("expected Path to find agent-x, got false")
	}
	if p != "agents/agent-x.md" {
		t.Errorf("Path() = %q, want %q", p, "agents/agent-x.md")
	}

	_, ok = paths.Path(domain.ArtifactRef{Kind: domain.ArtifactAgent, Key: "missing"})
	if ok {
		t.Error("expected Path to return false for absent ref, got true")
	}
}
