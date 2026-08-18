package transform_test

// role_forwarding_test.go tests that transform.Apply forwards the Role field from
// transform.Request into the domain.FrontmatterRequest it passes to the module's
// Frontmatter method.
//
// Coverage:
//   - When transform.Request.Role is set, the FrontmatterRequest.Role passed to
//     module.Frontmatter equals transform.Request.Role.
//   - When transform.Request.Role is empty (zero value), FrontmatterRequest.Role
//     is also the zero value.
//
// RED: currently fails because transform.Apply does not set FrontmatterRequest.Role.

import (
	"testing"

	"mosaic-deploy/internal/domain"
	"mosaic-deploy/internal/transform"
)

// roleCapturingModule wraps an inner HarnessModule and records the FrontmatterRequest
// passed to its Frontmatter method. All other calls are delegated to the inner module.
type roleCapturingModule struct {
	inner         domain.HarnessModule
	capturedFMReq *domain.FrontmatterRequest
}

func (m *roleCapturingModule) Ref() domain.HarnessRef { return m.inner.Ref() }

func (m *roleCapturingModule) Descriptor() *domain.HarnessDescriptor { return m.inner.Descriptor() }

func (m *roleCapturingModule) Close() error { return m.inner.Close() }

func (m *roleCapturingModule) Tools(req domain.ToolRequest) (domain.ToolResult, error) {
	return m.inner.Tools(req)
}

func (m *roleCapturingModule) Frontmatter(req domain.FrontmatterRequest) (domain.FrontmatterPlan, error) {
	// Capture a copy of the request so the test can inspect it after Apply returns.
	captured := req
	m.capturedFMReq = &captured
	return m.inner.Frontmatter(req)
}

func (m *roleCapturingModule) TargetPath(req domain.TargetPathRequest) (string, error) {
	return m.inner.TargetPath(req)
}

func (m *roleCapturingModule) Injection(req domain.InjectionRequest) (string, bool) {
	return m.inner.Injection(req)
}

func (m *roleCapturingModule) HookPlan(req domain.HookPlanRequest) (domain.HookPlan, error) {
	return m.inner.HookPlan(req)
}

// roleFMSource is a minimal generic agent source suitable for the fixture harness
// descriptor used in role-forwarding tests. It includes the fields that the fixture
// descriptor's drop list references (name, recommended_tier, tier_rationale) and a
// plain identity block that the transform body parser can handle.
const roleFMSource = `---
id: role-test-agent
version: 1.0.0
name: role-test-agent
description: Agent for role forwarding tests
model: {model-identifier}
tools: [file_read]
recommended_tier: LOW
tier_rationale: minimal test
required_skills: []
---

<Identity type="core">
Role forwarding test agent.
</Identity>
`

// TestApply_Role_ForwardedToFrontmatterRequest verifies that the Role value set on
// transform.Request reaches the FrontmatterRequest passed to the module's Frontmatter
// method. This ensures the harness can make role-conditional decisions (e.g., setting
// user-invocable based on role) based on accurate role information from the caller.
//
// RED: currently fails because transform.Apply constructs FrontmatterRequest without
// setting the Role field, leaving it as the zero value ("") regardless of req.Role.
func TestApply_Role_ForwardedToFrontmatterRequest(t *testing.T) {
	spy := &roleCapturingModule{inner: newFixtureModule(t)}
	req := transform.Request{
		Source: []byte(roleFMSource),
		Kind:   domain.ArtifactAgent,
		Key:    "role-test-agent",
		Module: spy,
		Model:  fixtureModel(),
		Scope:  domain.ScopeProject,
		Role:   domain.RoleOrchestrator,
	}

	_, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	if spy.capturedFMReq == nil {
		t.Fatal("Frontmatter was never called by transform.Apply")
	}
	if spy.capturedFMReq.Role != domain.RoleOrchestrator {
		t.Errorf("FrontmatterRequest.Role = %q, want %q (transform.Apply must forward req.Role)",
			spy.capturedFMReq.Role, domain.RoleOrchestrator)
	}
}

// TestApply_EmptyRole_ForwardedToFrontmatterRequest verifies that when transform.Request.Role
// is the zero value, FrontmatterRequest.Role is also the zero value. This establishes that
// the forwarding is a pass-through, not a coercion.
//
// Note: This test CANNOT fail in TDD RED phase by design. Before implementation,
// transform.Apply never sets FrontmatterRequest.Role, so it defaults to the zero value ("").
// The test asserts FrontmatterRequest.Role == domain.AgentRole(""), which is trivially true
// both before and after implementation (zero → zero is indistinguishable from not-forwarded → zero).
// The test provides documentation value and is validated indirectly by the RED-failing companion
// test TestApply_Role_ForwardedToFrontmatterRequest. It does not provide a RED-phase signal.
func TestApply_EmptyRole_ForwardedToFrontmatterRequest(t *testing.T) {
	spy := &roleCapturingModule{inner: newFixtureModule(t)}
	req := transform.Request{
		Source: []byte(roleFMSource),
		Kind:   domain.ArtifactAgent,
		Key:    "role-test-agent",
		Module: spy,
		Model:  fixtureModel(),
		Scope:  domain.ScopeProject,
		// Role intentionally not set — zero value.
	}

	_, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	if spy.capturedFMReq == nil {
		t.Fatal("Frontmatter was never called by transform.Apply")
	}
	if spy.capturedFMReq.Role != domain.AgentRole("") {
		t.Errorf("FrontmatterRequest.Role = %q for empty req.Role, want empty string (zero value forwarded as-is)",
			spy.capturedFMReq.Role)
	}
}
