package transform_test

// injection_agentkey_test.go: Tests for Stage 4 — verifying that the transform pipeline
// correctly passes Request.Key as InjectionRequest.AgentKey to the module's Injection() call,
// and that agent-specific content flows through to the output.
//
// Test coverage (per plan task T4.2):
//
//  - Plumbing: transform.Apply passes Request.Key as AgentKey to Injection() via InjectionRequest.
//  - Content routing: orchestrator agent receives orchestrator-only content; subagents do not.
//
// RED STATE:
//   The tests in this file will NOT COMPILE until I4.7 updates transform/injection.go to pass
//   domain.InjectionRequest{Name: name, AgentKey: req.Key} instead of calling Injection(name).
//   This compilation failure is the TDD RED indicator for the pipeline plumbing change.
//
//   After I4.7 (injection.go updated):
//   - TestApply_HarnessInjection_PassesAgentKeyToModule:  PASSES (spy verifies plumbing)
//   - TestApply_SubagentKey_PassedToInjection:            PASSES (spy verifies plumbing)
//   - TestApply_OrchestratorContent_InOutput:             PASSES (spy returns orch content)
//   - TestApply_SubagentContent_DoesNotContainOrchestratorContent: PASSES

import (
	"bytes"
	"testing"

	"mosaic-common/docformat"
	"mosaic-deploy/internal/domain"
	"mosaic-deploy/internal/transform"
)

// sourceWithHarnessConstraints is a source document containing an <HarnessConstraints type="project">
// harness-class injection region, used to exercise the injection plumbing in the pipeline.
const sourceWithHarnessConstraints = `---
id: 1
version: 1.0.0
name: agent-with-harness-injection
description: Agent for testing agent-key injection passthrough
model: {model-identifier}
tools: [file_read]
recommended_tier: LOW
tier_rationale: minimal
required_skills: []
---

<Constraints type="core">
## Constraints

<HarnessConstraints type="managed">
</HarnessConstraints>
</Constraints>
`

// spyInjectionModule wraps the fixture module and overrides Injection to:
//  1. Record every InjectionRequest it receives (for plumbing assertions)
//  2. Return agent-key-specific content for HarnessConstraints (for content assertions)
//
// All other interface methods are delegated to the embedded fixture module.
type spyInjectionModule struct {
	base              domain.HarnessModule
	calls             []domain.InjectionRequest
	contentByAgentKey map[string]string // AgentKey → content returned for HarnessConstraints
}

func newSpyInjectionModule(t *testing.T, contentByAgentKey map[string]string) *spyInjectionModule {
	t.Helper()
	return &spyInjectionModule{
		base:              newFixtureModule(t),
		contentByAgentKey: contentByAgentKey,
	}
}

func (m *spyInjectionModule) Ref() domain.HarnessRef { return m.base.Ref() }
func (m *spyInjectionModule) Descriptor() *domain.HarnessDescriptor {
	return m.base.Descriptor()
}
func (m *spyInjectionModule) Close() error { return m.base.Close() }
func (m *spyInjectionModule) Tools(req domain.ToolRequest) (domain.ToolResult, error) {
	return m.base.Tools(req)
}
func (m *spyInjectionModule) Frontmatter(req domain.FrontmatterRequest) (domain.FrontmatterPlan, error) {
	return m.base.Frontmatter(req)
}
func (m *spyInjectionModule) TargetPath(req domain.TargetPathRequest) (string, error) {
	return m.base.TargetPath(req)
}
func (m *spyInjectionModule) HookPlan(req domain.HookPlanRequest) (domain.HookPlan, error) {
	return m.base.HookPlan(req)
}

// Injection records the request and returns agent-key-specific content for HarnessConstraints.
// For all other injection names, it delegates to the base fixture module.
func (m *spyInjectionModule) Injection(req domain.InjectionRequest) (string, bool) {
	m.calls = append(m.calls, req)
	if req.Name == "HarnessConstraints" {
		content, exists := m.contentByAgentKey[req.AgentKey]
		if exists {
			return content, true
		}
		// No entry for this agent key → return empty declared injection (ok=true).
		return "", true
	}
	// Delegate non-HarnessConstraints injections to the base module.
	return m.base.Injection(req)
}

// lastCallForName returns the last InjectionRequest recorded for the given injection name,
// or the zero value if no such call was recorded.
func (m *spyInjectionModule) lastCallForName(name string) (domain.InjectionRequest, bool) {
	for i := len(m.calls) - 1; i >= 0; i-- {
		if m.calls[i].Name == name {
			return m.calls[i], true
		}
	}
	return domain.InjectionRequest{}, false
}

// TestApply_HarnessInjection_PassesAgentKeyToModule verifies the plumbing: when Apply is
// called with Key="orchestrator", applyHarnessInjection must forward "orchestrator" as
// InjectionRequest.AgentKey to the module's Injection() call.
//
// RED: Does not compile until I4.7 updates injection.go to call
//
//	req.Module.Injection(domain.InjectionRequest{Name: name, AgentKey: req.Key})
//
// instead of the old bare-string call.
func TestApply_HarnessInjection_PassesAgentKeyToModule(t *testing.T) {
	const agentKey = "orchestrator"
	spy := newSpyInjectionModule(t, map[string]string{
		"orchestrator": "orchestrator-specific content",
		"":             "",
	})

	req := transform.Request{
		Source: []byte(sourceWithHarnessConstraints),
		Kind:   domain.ArtifactAgent,
		Key:    agentKey,
		Module: spy,
		Model:  fixtureModel(),
		Scope:  domain.ScopeProject,
	}

	_, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	// The pipeline must have called Injection with AgentKey matching Request.Key.
	call, found := spy.lastCallForName("HarnessConstraints")
	if !found {
		t.Fatal("Injection was never called with Name=HarnessConstraints; expected the pipeline to call Injection for every harness-class injection region in the source")
	}
	if call.AgentKey != agentKey {
		t.Errorf("Injection was called with AgentKey=%q; want %q (must match transform.Request.Key)", call.AgentKey, agentKey)
	}
}

// TestApply_SubagentKey_PassedToInjection verifies the plumbing for a non-orchestrator key:
// when Apply is called with a subagent key, the module receives that key as AgentKey.
func TestApply_SubagentKey_PassedToInjection(t *testing.T) {
	const agentKey = "contracts-reviewer"
	spy := newSpyInjectionModule(t, map[string]string{
		"":             "shared content",
		"orchestrator": "orchestrator-specific content",
	})

	req := transform.Request{
		Source: []byte(sourceWithHarnessConstraints),
		Kind:   domain.ArtifactAgent,
		Key:    agentKey,
		Module: spy,
		Model:  fixtureModel(),
		Scope:  domain.ScopeProject,
	}

	_, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	call, found := spy.lastCallForName("HarnessConstraints")
	if !found {
		t.Fatal("Injection was never called with Name=HarnessConstraints")
	}
	if call.AgentKey != agentKey {
		t.Errorf("Injection called with AgentKey=%q; want %q (must match transform.Request.Key)", call.AgentKey, agentKey)
	}
}

// TestApply_OrchestratorContent_InOutput verifies the end-to-end data path: when the module
// returns orchestrator-specific content for AgentKey="orchestrator", that content appears
// byte-identically in the HarnessConstraints injection region of the transform output.
func TestApply_OrchestratorContent_InOutput(t *testing.T) {
	const orchContent = "skills path: cwd/.claude/skills\nTask tool: fill message field only"
	spy := newSpyInjectionModule(t, map[string]string{
		"orchestrator": orchContent,
		"":             "",
	})

	req := transform.Request{
		Source: []byte(sourceWithHarnessConstraints),
		Kind:   domain.ArtifactAgent,
		Key:    "orchestrator",
		Module: spy,
		Model:  fixtureModel(),
		Scope:  domain.ScopeProject,
	}

	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	// Parse the output and locate the HarnessConstraints injection region.
	outDoc, err := docformat.Parse(result.Output)
	if err != nil {
		t.Fatalf("parse output: %v", err)
	}
	node, ok := outDoc.Body().Deployed("HarnessConstraints")
	if !ok {
		t.Fatal("HarnessConstraints injection absent from output document")
	}
	if !bytes.Contains(node.Content(), []byte(orchContent)) {
		t.Errorf("output HarnessConstraints does not contain orchestrator content\ngot:  %q\nwant: content containing %q", node.Content(), orchContent)
	}
}

// TestApply_SubagentContent_DoesNotContainOrchestratorContent verifies that subagent
// agents do not receive orchestrator-only content, even when the same module would return
// it for the orchestrator key.
func TestApply_SubagentContent_DoesNotContainOrchestratorContent(t *testing.T) {
	const orchOnlyContent = "ORCHESTRATOR-ONLY-MARKER-DO-NOT-LEAK"
	spy := newSpyInjectionModule(t, map[string]string{
		"orchestrator":  orchOnlyContent,
		"some-subagent": "shared content only",
		"":              "shared content only",
	})

	req := transform.Request{
		Source: []byte(sourceWithHarnessConstraints),
		Kind:   domain.ArtifactAgent,
		Key:    "some-subagent",
		Module: spy,
		Model:  fixtureModel(),
		Scope:  domain.ScopeProject,
	}

	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	// The subagent output must not contain the orchestrator-only marker.
	if bytes.Contains(result.Output, []byte(orchOnlyContent)) {
		t.Errorf("subagent output contains orchestrator-only content %q; subagents must receive only shared content", orchOnlyContent)
	}
}
