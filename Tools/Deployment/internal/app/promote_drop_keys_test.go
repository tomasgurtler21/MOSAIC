package app

// promote_drop_keys_test.go covers the promoteHarnessDropKeys pure function, which
// derives the harness-specific frontmatter drop-set from a resolved harness module's
// descriptor keys and its own FrontmatterPlan.Set. Tests are in package app (not
// package app_test) because promoteHarnessDropKeys is unexported.
//
// Verified behaviours:
//
// OpenCode-shaped harness (AC2.2, AC2.3):
//   - The model key from Descriptor().Frontmatter.ModelKey is included in the drop set.
//   - The tools key from Descriptor().Frontmatter.ToolsKey is included in the drop set.
//   - Each key from the harness's own FrontmatterPlan.Set is included in the drop set.
//   - FrontmatterSpec.Drop and FrontmatterPlan.Remove are deliberately excluded (AD-3).
//
// Differently-shaped harness (AC2.4):
//   - A harness with different ModelKey, ToolsKey, and plan-added keys produces its own
//     drop set, not OpenCode's set.
//
// Protected generic keys (AD-4, AC2.6):
//   - id, version, name, role, and description are never present in the returned drop set,
//     even when the harness's descriptor keys or plan-added keys collide with them.
//
// Agent-key pass-through (AD-15):
//   - The agentKey parameter is forwarded verbatim to FrontmatterRequest.AgentKey so a
//     harness that shapes its plan per artifact (e.g. "orchestrator" vs regular keys)
//     contributes the correct plan-added keys for the promoted agent.
//
// Error handling:
//   - When the module's Frontmatter call fails, the returned drop set contains only the
//     descriptor keys (model key and tools key), and the error is returned so the caller
//     can decide how to proceed.
//
// Edge cases:
//   - An empty ModelKey or ToolsKey is silently ignored.
//   - An empty string among the FrontmatterPlan.Set key names is silently ignored.
//   - A module with no mappings (empty descriptor) returns an empty set without error.

import (
	"errors"
	"testing"

	"mosaic-common/docformat"
	"mosaic-deploy/internal/domain"
	"mosaic-deploy/internal/harness/descriptor"
)

// ---------------------------------------------------------------------------
// Stub harness module for promoteHarnessDropKeys tests
// ---------------------------------------------------------------------------

// configuredFrontmatterModule is a HarnessModule stub whose Frontmatter method returns
// a fixed plan and optionally a fixed error. It is used for promoteHarnessDropKeys unit
// tests so each test can fully control what the module contributes to the drop set without
// depending on a real harness implementation.
type configuredFrontmatterModule struct {
	desc       domain.HarnessDescriptor
	planSet    []domain.FrontmatterField // keys returned in FrontmatterPlan.Set
	planRemove []string                  // keys returned in FrontmatterPlan.Remove; must NOT enter the drop set (AD-3)
	planErr    error                     // error returned by Frontmatter; nil means success
	agentKeys  []string                  // records AgentKey from each Frontmatter call; for pass-through assertions
}

func (m *configuredFrontmatterModule) Ref() domain.HarnessRef { return domain.HarnessRef{} }
func (m *configuredFrontmatterModule) Descriptor() *domain.HarnessDescriptor {
	return &m.desc
}
func (m *configuredFrontmatterModule) Tools(req domain.ToolRequest) (domain.ToolResult, error) {
	return domain.ToolResult{}, nil
}
func (m *configuredFrontmatterModule) Frontmatter(req domain.FrontmatterRequest) (domain.FrontmatterPlan, error) {
	m.agentKeys = append(m.agentKeys, req.AgentKey)
	if m.planErr != nil {
		return domain.FrontmatterPlan{}, m.planErr
	}
	return domain.FrontmatterPlan{Set: m.planSet, Remove: m.planRemove}, nil
}
func (m *configuredFrontmatterModule) TargetPath(req domain.TargetPathRequest) (string, error) {
	return req.Key + ".md", nil
}
func (m *configuredFrontmatterModule) Injection(_ domain.InjectionRequest) (string, bool) {
	return "", false
}
func (m *configuredFrontmatterModule) HookPlan(_ domain.HookPlanRequest) (domain.HookPlan, error) {
	return domain.HookPlan{Supported: false}, nil
}
func (m *configuredFrontmatterModule) Close() error { return nil }

// openCodeShapedModule returns a configuredFrontmatterModule that mimics the OpenCode harness
// contract: ModelKey="model", ToolsKey="permission", and a plan that adds "mode". The descriptor
// also carries non-empty Drop (["name", "tools", ...]) to verify those are NOT included (AD-3).
func openCodeShapedModule() *configuredFrontmatterModule {
	return &configuredFrontmatterModule{
		desc: domain.HarnessDescriptor{
			ID: "opencode-shaped",
			Frontmatter: domain.FrontmatterSpec{
				ModelKey: "model",
				ToolsKey: "permission",
				Drop:     []string{"name", "recommended_tier", "tier_rationale", "required_skills", "tools"},
			},
		},
		planSet: []domain.FrontmatterField{
			{Key: "mode", Value: domain.ScalarValue("subagent", domain.QuotePlain)},
		},
	}
}

// differentlyShapedModule returns a configuredFrontmatterModule with its own model key,
// tools key, and plan keys — none of which match OpenCode's. Used to verify AC2.4.
func differentlyShapedModule() *configuredFrontmatterModule {
	return &configuredFrontmatterModule{
		desc: domain.HarnessDescriptor{
			ID: "differently-shaped",
			Frontmatter: domain.FrontmatterSpec{
				ModelKey: "llm_model",
				ToolsKey: "allowed_tools",
			},
		},
		planSet: []domain.FrontmatterField{
			{Key: "execution_mode", Value: domain.ScalarValue("autonomous", domain.QuotePlain)},
			{Key: "custom_flag", Value: domain.ScalarValue("true", domain.QuotePlain)},
		},
	}
}

// ---------------------------------------------------------------------------
// Tests for promoteHarnessDropKeys
// ---------------------------------------------------------------------------

// TestPromoteHarnessDropKeys_ModelKeyIsIncluded verifies that the harness's declared
// ModelKey (the key it writes the model to in a deployed agent) is included in the drop
// set. A generic promoted file must not carry the harness-specific model field.
func TestPromoteHarnessDropKeys_ModelKeyIsIncluded(t *testing.T) {
	mod := openCodeShapedModule() // ModelKey="model"

	got, err := promoteHarnessDropKeys(mod, domain.ArtifactAgent, "test-agent")
	if err != nil {
		t.Fatalf("promoteHarnessDropKeys returned unexpected error: %v", err)
	}

	if !got["model"] {
		t.Errorf("drop set does not contain ModelKey %q; it must be included because the harness writes the model to this key", "model")
	}
}

// TestPromoteHarnessDropKeys_ToolsKeyIsIncluded verifies that the harness's declared
// ToolsKey (the key it writes the tool entries to in a deployed agent) is included in the
// drop set. The harness-side tool representation belongs to the deployed form only.
func TestPromoteHarnessDropKeys_ToolsKeyIsIncluded(t *testing.T) {
	mod := openCodeShapedModule() // ToolsKey="permission"

	got, err := promoteHarnessDropKeys(mod, domain.ArtifactAgent, "test-agent")
	if err != nil {
		t.Fatalf("promoteHarnessDropKeys returned unexpected error: %v", err)
	}

	if !got["permission"] {
		t.Errorf("drop set does not contain ToolsKey %q; it must be included because the harness writes tool entries to this key", "permission")
	}
}

// TestPromoteHarnessDropKeys_PlanSetKeysAreIncluded verifies that every key appearing in
// FrontmatterPlan.Set is included in the drop set. The OpenCode module adds "mode" in its
// plan; that key was added by the harness at deploy time and must not survive into a generic
// source.
func TestPromoteHarnessDropKeys_PlanSetKeysAreIncluded(t *testing.T) {
	mod := openCodeShapedModule() // plan adds "mode"

	got, err := promoteHarnessDropKeys(mod, domain.ArtifactAgent, "test-agent")
	if err != nil {
		t.Fatalf("promoteHarnessDropKeys returned unexpected error: %v", err)
	}

	if !got["mode"] {
		t.Errorf("drop set does not contain plan-added key %q; every key in FrontmatterPlan.Set must be in the drop set", "mode")
	}
}

// TestPromoteHarnessDropKeys_OpenCodeShapedHarness_DropsModeModelAndPermission verifies
// the complete OpenCode-shaped drop set in one assertion: model, permission, and mode are
// all present. This mirrors AC2.2 at the derivation level (the service layer) rather than
// at the generation-core level.
func TestPromoteHarnessDropKeys_OpenCodeShapedHarness_DropsModeModelAndPermission(t *testing.T) {
	mod := openCodeShapedModule()

	got, err := promoteHarnessDropKeys(mod, domain.ArtifactAgent, "test-agent")
	if err != nil {
		t.Fatalf("promoteHarnessDropKeys returned unexpected error: %v", err)
	}

	for _, key := range []string{"model", "permission", "mode"} {
		if !got[key] {
			t.Errorf("OpenCode-shaped harness drop set is missing %q; got: %v", key, got)
		}
	}
}

// TestPromoteHarnessDropKeys_FrontmatterSpecDropExcluded verifies that keys listed in
// FrontmatterSpec.Drop are NOT included in the returned drop set. AD-3: those keys name
// generic fields the harness removes at deploy time — exactly the fields promote is trying
// to restore. Including them would remove the recovered fields a second time.
func TestPromoteHarnessDropKeys_FrontmatterSpecDropExcluded(t *testing.T) {
	// The OpenCode-shaped module has Drop = ["name", "recommended_tier", ...].
	mod := openCodeShapedModule()

	got, err := promoteHarnessDropKeys(mod, domain.ArtifactAgent, "test-agent")
	if err != nil {
		t.Fatalf("promoteHarnessDropKeys returned unexpected error: %v", err)
	}

	// FrontmatterSpec.Drop lists generic fields the harness removes; they must NOT be in the set.
	for _, key := range mod.desc.Frontmatter.Drop {
		if got[key] {
			t.Errorf("drop set contains FrontmatterSpec.Drop entry %q; AD-3 forbids this — the spec Drop names generic fields promote wants back", key)
		}
	}
}

// TestPromoteHarnessDropKeys_FrontmatterPlanRemoveExcluded verifies that keys in
// FrontmatterPlan.Remove are NOT included in the returned drop set. AD-3: Remove names
// generic fields the harness drops at deploy time — exactly the fields promote is trying
// to restore — so including them would double-drop recovered generic fields.
//
// The module's planRemove carries "tools" and "recommended_tier", which are NOT in
// ModelKey, ToolsKey, or planSet. The test uses positive guard assertions to confirm the
// module's real keys are returned, ruling out a vacuous pass from an empty result.
func TestPromoteHarnessDropKeys_FrontmatterPlanRemoveExcluded(t *testing.T) {
	mod := &configuredFrontmatterModule{
		desc: domain.HarnessDescriptor{
			Frontmatter: domain.FrontmatterSpec{
				ModelKey: "llm_model",
				ToolsKey: "tool_list",
			},
		},
		planSet: []domain.FrontmatterField{
			{Key: "agent_mode", Value: domain.ScalarValue("fast", domain.QuotePlain)},
		},
		// planRemove names generic fields the harness strips at deploy time. AD-3 forbids
		// promoteHarnessDropKeys from unioning these into the derived drop set.
		planRemove: []string{"tools", "recommended_tier"},
	}

	got, err := promoteHarnessDropKeys(mod, domain.ArtifactAgent, "my-agent")
	if err != nil {
		t.Fatalf("promoteHarnessDropKeys returned unexpected error: %v", err)
	}

	// Guard: confirm the real drop-set keys (ModelKey, ToolsKey, planSet) are present so the
	// test does not pass vacuously if the implementation returns an empty map.
	for _, required := range []string{"llm_model", "tool_list", "agent_mode"} {
		if !got[required] {
			t.Errorf("drop set is missing expected key %q; guard assertion failed — cannot trust the Remove-exclusion assertions below", required)
		}
	}

	// The Remove keys name generic fields and must NOT enter the drop set (AD-3).
	for _, unwanted := range mod.planRemove {
		if got[unwanted] {
			t.Errorf("drop set contains FrontmatterPlan.Remove entry %q; AD-3 forbids this — Remove names generic fields promote wants to restore", unwanted)
		}
	}
}

// TestPromoteHarnessDropKeys_DifferentlyShapedHarness_DropsItsOwnKeys verifies that
// a harness with different model/tools/plan keys produces its own drop set, containing
// those keys and not OpenCode's. This is AC2.4 at the derivation level.
func TestPromoteHarnessDropKeys_DifferentlyShapedHarness_DropsItsOwnKeys(t *testing.T) {
	mod := differentlyShapedModule() // ModelKey="llm_model", ToolsKey="allowed_tools", plan adds "execution_mode" and "custom_flag"

	got, err := promoteHarnessDropKeys(mod, domain.ArtifactAgent, "some-agent")
	if err != nil {
		t.Fatalf("promoteHarnessDropKeys returned unexpected error: %v", err)
	}

	// Its own keys must be in the set.
	for _, key := range []string{"llm_model", "allowed_tools", "execution_mode", "custom_flag"} {
		if !got[key] {
			t.Errorf("differently-shaped harness drop set is missing its own key %q; got: %v", key, got)
		}
	}

	// OpenCode's keys must NOT be in the set.
	for _, opencodeKey := range []string{"model", "permission", "mode"} {
		if got[opencodeKey] {
			t.Errorf("differently-shaped harness drop set contains OpenCode key %q; only its own keys must be present", opencodeKey)
		}
	}
}

// TestPromoteHarnessDropKeys_ProtectedGenericKeysExcluded verifies that id, version, name,
// role, and description are never in the returned drop set, even when the harness's
// ModelKey, ToolsKey, or plan-added keys collide with one of these names (AD-4, AC2.6).
func TestPromoteHarnessDropKeys_ProtectedGenericKeysExcluded(t *testing.T) {
	// A pathological harness whose declared keys collide with all five protected fields.
	mod := &configuredFrontmatterModule{
		desc: domain.HarnessDescriptor{
			Frontmatter: domain.FrontmatterSpec{
				ModelKey: "id",          // collides with protected key
				ToolsKey: "description", // collides with protected key
			},
		},
		planSet: []domain.FrontmatterField{
			{Key: "version", Value: domain.ScalarValue("3.0.0", domain.QuotePlain)}, // collides
			{Key: "name", Value: domain.ScalarValue("bad", domain.QuotePlain)},       // collides
			{Key: "role", Value: domain.ScalarValue("orchestrator", domain.QuotePlain)}, // collides
		},
	}

	got, err := promoteHarnessDropKeys(mod, domain.ArtifactAgent, "agent")
	if err != nil {
		t.Fatalf("promoteHarnessDropKeys returned unexpected error: %v", err)
	}

	for _, protected := range []string{"id", "version", "name", "role", "description"} {
		if got[protected] {
			t.Errorf("protected key %q is in the drop set; protected keys must never be removed by the harness-derived set (AD-4)", protected)
		}
	}
}

// TestPromoteHarnessDropKeys_MixedProtectedAndNonProtectedKeys verifies that the protection
// logic inspects each declared key individually rather than short-circuiting on the first
// collision. When a harness has one key that collides with a protected field and another that
// does not, only the colliding key is excluded; the non-colliding key must appear in the set.
// This proves selective exclusion and guards against an implementation that always returns an
// empty set whenever any key is protected (AC2.6).
func TestPromoteHarnessDropKeys_MixedProtectedAndNonProtectedKeys(t *testing.T) {
	// ModelKey collides with a protected field; ToolsKey does not.
	mod := &configuredFrontmatterModule{
		desc: domain.HarnessDescriptor{
			Frontmatter: domain.FrontmatterSpec{
				ModelKey: "id",         // collides with protected key — must be excluded
				ToolsKey: "permission", // non-colliding, non-protected — must survive
			},
		},
		planSet: []domain.FrontmatterField{
			{Key: "mode", Value: domain.ScalarValue("subagent", domain.QuotePlain)}, // non-colliding — must survive
		},
	}

	got, err := promoteHarnessDropKeys(mod, domain.ArtifactAgent, "agent")
	if err != nil {
		t.Fatalf("promoteHarnessDropKeys returned unexpected error: %v", err)
	}

	// The protected key must be excluded even though it is declared as ModelKey.
	if got["id"] {
		t.Errorf("protected key %q appears in the drop set; it must be excluded even when declared as ModelKey (AD-4)", "id")
	}

	// The non-colliding descriptor key must survive in the set.
	if !got["permission"] {
		t.Errorf("non-colliding ToolsKey %q is absent from the drop set; only keys that collide with protected fields are excluded, not all declared keys", "permission")
	}

	// The non-colliding plan key must also survive.
	if !got["mode"] {
		t.Errorf("non-colliding plan-added key %q is absent from the drop set; plan keys that do not collide with protected fields must be included", "mode")
	}
}

// TestPromoteHarnessDropKeys_DuplicateKeyAcrossSources_WellFormed verifies that when a
// harness uses the same string for ModelKey, ToolsKey, and a plan-set key, the returned map
// remains well-formed (contains the key exactly once with value true) and no panic or error
// occurs. This is a cheap edge case for the union derivation path.
func TestPromoteHarnessDropKeys_DuplicateKeyAcrossSources_WellFormed(t *testing.T) {
	const sharedKey = "harness_token"
	mod := &configuredFrontmatterModule{
		desc: domain.HarnessDescriptor{
			Frontmatter: domain.FrontmatterSpec{
				ModelKey: sharedKey,
				ToolsKey: sharedKey,
			},
		},
		planSet: []domain.FrontmatterField{
			{Key: sharedKey, Value: domain.ScalarValue("v1", domain.QuotePlain)},
		},
	}

	got, err := promoteHarnessDropKeys(mod, domain.ArtifactAgent, "some-agent")
	if err != nil {
		t.Fatalf("promoteHarnessDropKeys returned unexpected error for duplicate key across sources: %v", err)
	}

	if !got[sharedKey] {
		t.Errorf("drop set is missing %q; a key shared across ModelKey, ToolsKey, and plan-set must still be in the derived set", sharedKey)
	}

	// Map semantics guarantee no duplicate entries, so no extra check is needed beyond presence.
}

// TestPromoteHarnessDropKeys_AgentKeyForwardedToFrontmatterRequest verifies that the
// agentKey parameter is passed verbatim to FrontmatterRequest.AgentKey so that a harness
// which shapes its plan per artifact (e.g. "orchestrator" adds different keys) contributes
// the correct set for the promoted agent. This pins the AD-15 pass-through.
func TestPromoteHarnessDropKeys_AgentKeyForwardedToFrontmatterRequest(t *testing.T) {
	mod := openCodeShapedModule()

	agentKey := "orchestrator"
	_, err := promoteHarnessDropKeys(mod, domain.ArtifactAgent, agentKey)
	if err != nil {
		t.Fatalf("promoteHarnessDropKeys returned unexpected error: %v", err)
	}

	if len(mod.agentKeys) == 0 {
		t.Fatal("module.Frontmatter was not called; cannot verify AgentKey pass-through")
	}
	if mod.agentKeys[0] != agentKey {
		t.Errorf("FrontmatterRequest.AgentKey = %q, want %q; the agentKey must be forwarded verbatim (AD-15)", mod.agentKeys[0], agentKey)
	}
}

// TestPromoteHarnessDropKeys_OrchestratorAgentKey_ContainsOrchestratorPlanKeys verifies that
// when agentKey is "orchestrator" and the harness returns a different plan for that key (such
// as adding "orchestrator_injections_version"), those additional plan-added keys appear in
// the drop set. This is the concrete AD-15 pass-through scenario.
func TestPromoteHarnessDropKeys_OrchestratorAgentKey_ContainsOrchestratorPlanKeys(t *testing.T) {
	// Build a module that returns a larger Set for agentKey="orchestrator".
	mod := &configuredFrontmatterModule{
		desc: domain.HarnessDescriptor{
			Frontmatter: domain.FrontmatterSpec{
				ModelKey: "model",
				ToolsKey: "permission",
			},
		},
	}

	// Override Frontmatter to return different plans for orchestrator vs regular agents.
	orchMod := &orchestratorAwareModule{
		configuredFrontmatterModule: mod,
		orchestratorExtraKey:        "orchestrator_injections_version",
	}

	got, err := promoteHarnessDropKeys(orchMod, domain.ArtifactAgent, "orchestrator")
	if err != nil {
		t.Fatalf("promoteHarnessDropKeys returned unexpected error: %v", err)
	}

	if !got["orchestrator_injections_version"] {
		t.Errorf("drop set is missing orchestrator-only plan key %q; when agentKey is %q and the harness adds this key, it must appear in the drop set",
			"orchestrator_injections_version", "orchestrator")
	}
}

// orchestratorAwareModule wraps configuredFrontmatterModule to return a different
// FrontmatterPlan.Set when AgentKey is "orchestrator". This models real harnesses like
// OpenCode that add orchestrator_injections_version only for the orchestrator agent.
type orchestratorAwareModule struct {
	*configuredFrontmatterModule
	orchestratorExtraKey string
}

func (m *orchestratorAwareModule) Frontmatter(req domain.FrontmatterRequest) (domain.FrontmatterPlan, error) {
	m.agentKeys = append(m.agentKeys, req.AgentKey)
	set := []domain.FrontmatterField{
		{Key: "mode", Value: domain.ScalarValue("subagent", domain.QuotePlain)},
	}
	if req.AgentKey == "orchestrator" && m.orchestratorExtraKey != "" {
		set = append(set, domain.FrontmatterField{
			Key:   m.orchestratorExtraKey,
			Value: domain.ScalarValue("1.0.0", domain.QuotePlain),
		})
	}
	return domain.FrontmatterPlan{Set: set}, nil
}

// TestPromoteHarnessDropKeys_FrontmatterCallFails_ReturnsDescriptorKeysAndError verifies
// that when the module's Frontmatter call returns an error, promoteHarnessDropKeys still
// returns the descriptor-level keys (ModelKey and ToolsKey) so the caller has a partial
// set, and the error is returned for the caller to decide how to handle it.
func TestPromoteHarnessDropKeys_FrontmatterCallFails_ReturnsDescriptorKeysAndError(t *testing.T) {
	frontmatterErr := errors.New("frontmatter plan is unavailable")
	mod := &configuredFrontmatterModule{
		desc: domain.HarnessDescriptor{
			Frontmatter: domain.FrontmatterSpec{
				ModelKey: "model",
				ToolsKey: "permission",
			},
		},
		planErr: frontmatterErr,
	}

	got, err := promoteHarnessDropKeys(mod, domain.ArtifactAgent, "some-agent")

	// The error must be returned so the caller can decide.
	if err == nil {
		t.Fatal("promoteHarnessDropKeys returned nil error when Frontmatter failed; the error must be returned")
	}
	if !errors.Is(err, frontmatterErr) {
		t.Errorf("returned error = %v; want it to wrap the Frontmatter failure %v", err, frontmatterErr)
	}

	// The descriptor keys must still be in the set even though the plan failed.
	if !got["model"] {
		t.Errorf("drop set is missing descriptor ModelKey %q after Frontmatter failure; descriptor keys must be returned even when plan derivation fails", "model")
	}
	if !got["permission"] {
		t.Errorf("drop set is missing descriptor ToolsKey %q after Frontmatter failure; descriptor keys must be returned even when plan derivation fails", "permission")
	}
}

// TestPromoteHarnessDropKeys_EmptyModelKey_Ignored verifies that an empty ModelKey is
// silently ignored and does not add an empty string to the drop set.
func TestPromoteHarnessDropKeys_EmptyModelKey_Ignored(t *testing.T) {
	mod := &configuredFrontmatterModule{
		desc: domain.HarnessDescriptor{
			Frontmatter: domain.FrontmatterSpec{
				ModelKey: "", // empty — must be ignored
				ToolsKey: "tool_entries",
			},
		},
		planSet: []domain.FrontmatterField{
			{Key: "agent_flag", Value: domain.ScalarValue("on", domain.QuotePlain)},
		},
	}

	got, err := promoteHarnessDropKeys(mod, domain.ArtifactAgent, "my-agent")
	if err != nil {
		t.Fatalf("promoteHarnessDropKeys returned unexpected error: %v", err)
	}

	if got[""] {
		t.Error("drop set contains an empty string key; empty ModelKey must be silently ignored")
	}

	// Non-empty keys must still be present.
	if !got["tool_entries"] {
		t.Error("drop set is missing ToolsKey; non-empty descriptor keys must still be included")
	}
	if !got["agent_flag"] {
		t.Error("drop set is missing plan-added key; plan keys must still be included")
	}
}

// TestPromoteHarnessDropKeys_EmptyToolsKey_Ignored verifies that an empty ToolsKey is
// silently ignored and does not add an empty string to the drop set.
func TestPromoteHarnessDropKeys_EmptyToolsKey_Ignored(t *testing.T) {
	mod := &configuredFrontmatterModule{
		desc: domain.HarnessDescriptor{
			Frontmatter: domain.FrontmatterSpec{
				ModelKey: "llm_model",
				ToolsKey: "", // empty — must be ignored
			},
		},
		planSet: []domain.FrontmatterField{},
	}

	got, err := promoteHarnessDropKeys(mod, domain.ArtifactAgent, "my-agent")
	if err != nil {
		t.Fatalf("promoteHarnessDropKeys returned unexpected error: %v", err)
	}

	if got[""] {
		t.Error("drop set contains an empty string key; empty ToolsKey must be silently ignored")
	}

	if !got["llm_model"] {
		t.Error("drop set is missing ModelKey; non-empty descriptor keys must still be included")
	}
}

// TestPromoteHarnessDropKeys_EmptyPlanSetKey_Ignored verifies that an empty key name in
// FrontmatterPlan.Set does not add an empty string to the drop set.
func TestPromoteHarnessDropKeys_EmptyPlanSetKey_Ignored(t *testing.T) {
	mod := &configuredFrontmatterModule{
		desc: domain.HarnessDescriptor{
			Frontmatter: domain.FrontmatterSpec{
				ModelKey: "model",
				ToolsKey: "tools_key",
			},
		},
		planSet: []domain.FrontmatterField{
			{Key: "", Value: domain.ScalarValue("nokey", domain.QuotePlain)},  // empty key — must be ignored
			{Key: "valid_key", Value: domain.ScalarValue("value", domain.QuotePlain)},
		},
	}

	got, err := promoteHarnessDropKeys(mod, domain.ArtifactAgent, "some-agent")
	if err != nil {
		t.Fatalf("promoteHarnessDropKeys returned unexpected error: %v", err)
	}

	if got[""] {
		t.Error("drop set contains an empty string key; empty key names in FrontmatterPlan.Set must be silently ignored")
	}

	if !got["valid_key"] {
		t.Error("drop set is missing valid plan-added key; non-empty plan keys must still be included")
	}
}

// TestPromoteHarnessDropKeys_EmptyDescriptor_ReturnsEmptySetNoError verifies that a
// module with an empty descriptor (no ModelKey, no ToolsKey) and an empty plan returns
// an empty drop set without an error. This is the zero-module case.
func TestPromoteHarnessDropKeys_EmptyDescriptor_ReturnsEmptySetNoError(t *testing.T) {
	mod := &configuredFrontmatterModule{
		desc: domain.HarnessDescriptor{
			Frontmatter: domain.FrontmatterSpec{
				// ModelKey and ToolsKey are both empty strings.
			},
		},
		planSet: []domain.FrontmatterField{},
	}

	got, err := promoteHarnessDropKeys(mod, domain.ArtifactAgent, "some-agent")
	if err != nil {
		t.Fatalf("promoteHarnessDropKeys returned unexpected error for empty descriptor: %v", err)
	}

	// Empty-string keys must not appear even in the zero case.
	if got[""] {
		t.Error("drop set contains an empty string key for an empty descriptor")
	}

	// The set should be empty (or contain no meaningful keys) for an empty descriptor.
	for k := range got {
		if k != "" {
			t.Errorf("drop set contains unexpected key %q for an empty descriptor; expected an empty set", k)
		}
	}
}

// ---------------------------------------------------------------------------
// Tests for buildPromoteHarnessDropSet (shared drop-set and classifier derivation)
// ---------------------------------------------------------------------------

// TestBuildPromoteHarnessDropSet_DropKeysMatchPromoteHarnessDropKeys verifies that
// buildPromoteHarnessDropSet's DropKeys field equals the result of promoteHarnessDropKeys for
// the same inputs. This pins the delegation contract: the two functions must agree because
// promoteHarnessDropKeys delegates to buildPromoteHarnessDropSet.
func TestBuildPromoteHarnessDropSet_DropKeysMatchPromoteHarnessDropKeys(t *testing.T) {
	modules := []*configuredFrontmatterModule{
		openCodeShapedModule(),
		differentlyShapedModule(),
	}

	for _, mod := range modules {
		t.Run(mod.desc.ID, func(t *testing.T) {
			wantKeys, wantErr := promoteHarnessDropKeys(mod, domain.ArtifactAgent, "test-agent")
			// Reset agentKeys so the second call records its own invocations independently.
			mod.agentKeys = nil

			gotSet, gotErr := buildPromoteHarnessDropSet(mod, domain.ArtifactAgent, "test-agent")

			if wantErr != gotErr {
				t.Errorf("error mismatch: promoteHarnessDropKeys returned %v, buildPromoteHarnessDropSet returned %v", wantErr, gotErr)
			}
			for k := range wantKeys {
				if !gotSet.DropKeys[k] {
					t.Errorf("buildPromoteHarnessDropSet.DropKeys missing key %q that promoteHarnessDropKeys includes", k)
				}
			}
			for k := range gotSet.DropKeys {
				if !wantKeys[k] {
					t.Errorf("buildPromoteHarnessDropSet.DropKeys has extra key %q not in promoteHarnessDropKeys result", k)
				}
			}
		})
	}
}

// TestBuildPromoteHarnessDropSet_ClassifierIsNonZero verifies that buildPromoteHarnessDropSet
// returns a usable FieldClassifier alongside the drop keys. The classifier is tested by
// asserting it correctly classifies the harness's own declared keys as ClassHarness and
// an invented key as ClassUnknown.
func TestBuildPromoteHarnessDropSet_ClassifierIsNonZero(t *testing.T) {
	mod := openCodeShapedModule() // ModelKey="model", ToolsKey="permission", plan adds "mode"

	set, err := buildPromoteHarnessDropSet(mod, domain.ArtifactAgent, "test-agent")
	if err != nil {
		t.Fatalf("buildPromoteHarnessDropSet returned unexpected error: %v", err)
	}

	// The classifier must recognise the harness's own non-MOSAIC keys as ClassHarness.
	// "permission" is the ToolsKey (non-MOSAIC), so it must be ClassHarness or ClassDivertedTool.
	if cls := set.Classifier.Classify("permission"); cls != descriptor.ClassHarness {
		t.Errorf("Classifier.Classify(%q) = %q, want ClassHarness; the classifier must be built from the harness descriptor", "permission", cls)
	}

	// An arbitrary unknown key must be ClassUnknown.
	if cls := set.Classifier.Classify("invented_key_xyz"); cls != descriptor.ClassUnknown {
		t.Errorf("Classifier.Classify(%q) = %q, want ClassUnknown; unknown keys must classify as unknown", "invented_key_xyz", cls)
	}
}

// TestBuildPromoteHarnessDropSet_FrontmatterCalledOnce verifies that buildPromoteHarnessDropSet
// calls module.Frontmatter exactly once, not twice. A single call ensures both the drop-set
// and the classifier are derived from the same plan, with no risk of divergence.
func TestBuildPromoteHarnessDropSet_FrontmatterCalledOnce(t *testing.T) {
	mod := openCodeShapedModule()

	_, _ = buildPromoteHarnessDropSet(mod, domain.ArtifactAgent, "test-agent")

	if len(mod.agentKeys) != 1 {
		t.Errorf("module.Frontmatter called %d time(s); want exactly 1 call — both drop-set and classifier must share a single plan", len(mod.agentKeys))
	}
}

// TestPromoteHarnessDropKeys_ReturnedSetIsUsableByBuildGenericAgent verifies the end-to-end
// integration of promoteHarnessDropKeys and buildGenericAgent: the drop set returned by the
// derivation function is accepted directly as PromoteInput.DropKeys, causing the harness-
// specific keys to be absent from the generated generic file. This tests the contract
// boundary between the two functions without a real harness or registry.
func TestPromoteHarnessDropKeys_ReturnedSetIsUsableByBuildGenericAgent(t *testing.T) {
	mod := openCodeShapedModule() // produces {model, permission, mode}

	dropKeys, err := promoteHarnessDropKeys(mod, domain.ArtifactAgent, "my-agent")
	if err != nil {
		t.Fatalf("promoteHarnessDropKeys: %v", err)
	}

	// Build a source that carries all three OpenCode harness keys.
	src := []byte("---\n" +
		"transform_version: \"2.1.0\"\n" +
		"version: \"1.0.0\"\n" +
		"id: \"5\"\n" +
		"name: my-agent\n" +
		"description: End-to-end drop test agent\n" +
		"role: subagent\n" +
		"mode: subagent\n" +
		"model: github-copilot/claude-sonnet-4-6\n" +
		"permission:\n" +
		"  read: allow\n" +
		"  write: deny\n" +
		"---\n" +
		"<Identity type=\"core\">\n" +
		"Content.\n" +
		"</Identity>\n")

	out, buildErr := buildGenericAgent(PromoteInput{
		Source:    src,
		NumericID: "5",
		Key:       "my-agent",
		Role:      domain.RoleSubagent,
		Version:   "1.0.0",
		DropKeys:  dropKeys,
	})
	if buildErr != nil {
		t.Fatalf("buildGenericAgent: %v", buildErr)
	}

	doc, parseErr := docformat.Parse(out)
	if parseErr != nil {
		t.Fatalf("generated output failed to parse: %v", parseErr)
	}
	fm := doc.Frontmatter()

	// Guard: ensure output is populated.
	if _, present := fm.Get("id"); !present {
		t.Fatal("generated frontmatter is missing `id`; output may be empty")
	}

	// The OpenCode harness keys must be absent.
	for _, key := range []string{"mode", "model", "permission"} {
		if _, present := fm.Get(key); present {
			t.Errorf("harness key %q is present in generated frontmatter; promoteHarnessDropKeys must derive a set that causes buildGenericAgent to remove it", key)
		}
	}

	// Generic fields must survive.
	for _, key := range []string{"description", "name"} {
		if _, present := fm.Get(key); !present {
			t.Errorf("generic field %q is absent from generated frontmatter; it must not be removed by the harness-derived drop set", key)
		}
	}
}
