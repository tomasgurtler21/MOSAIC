package descriptor_test

// Tests for the shared descriptor-driven algorithms exported from harness/descriptor:
//   - ApplyFrontmatterSpec: derives FrontmatterPlan from a descriptor's FrontmatterSpec and a request.
//   - ResolveTargetPath: expands a ScopedPaths template for a given artifact kind, scope, and GOOS.
//
// Coverage:
//   ApplyFrontmatterSpec:
//   - Fields declared in FrontmatterSpec.Add appear in FrontmatterPlan.Set.
//   - Fields listed in FrontmatterSpec.Drop appear in FrontmatterPlan.Remove.
//   - FrontmatterSpec.KeyOrder is reproduced in FrontmatterPlan.KeyOrder.
//   - The Add field's Key and Value are preserved in the plan's Set entry.
//
//   ResolveTargetPath:
//   - ScopeProject returns a path that contains the descriptor's project template.
//   - ScopeProject path ends with the expected artifact file name and extension.
//   - Any non-project scope returns domain.ErrUnsupportedScope.
//   - An empty scope (unreached by normalised callers) also returns domain.ErrUnsupportedScope.
//   - An artifact kind with Supported == false returns domain.ErrArtifactUnsupported.
//   - ResolveTargetPath never returns an empty string with a nil error.

import (
	"errors"
	"strings"
	"testing"

	"mosaic-deploy/internal/domain"
	"mosaic-deploy/internal/harness/descriptor"
)

// --- ApplyFrontmatterSpec ---

func TestApplyFrontmatterSpec_AddFieldsAppearsInSet(t *testing.T) {
	// The descriptor's FrontmatterSpec.Add list names fields that must be added to every
	// deployed agent. The returned FrontmatterPlan.Set must contain these fields so that
	// the caller (transform) can write them into frontmatter.
	d := loadFixture(t, "valid-full.yaml")
	// valid-full.yaml declares Add: [{Key: "custom-field", Value: ScalarValue("always-present")}]
	req := domain.FrontmatterRequest{
		Kind:     domain.ArtifactAgent,
		AgentKey: "test-agent",
	}

	plan, err := descriptor.ApplyFrontmatterSpec(d, req)

	if err != nil {
		t.Fatalf("ApplyFrontmatterSpec: %v", err)
	}
	var found bool
	for _, f := range plan.Set {
		if f.Key == "custom-field" {
			found = true
			break
		}
	}
	if !found {
		keys := make([]string, len(plan.Set))
		for i, f := range plan.Set {
			keys[i] = f.Key
		}
		t.Errorf("expected %q in FrontmatterPlan.Set; got keys: %v", "custom-field", keys)
	}
}

func TestApplyFrontmatterSpec_AddFieldPreservesKeyAndValue(t *testing.T) {
	// The Add field entry must arrive in Set with the exact Key and Value declared in the
	// descriptor. An implementation that strips the value or changes the key would fail here.
	d := loadFixture(t, "valid-full.yaml")
	req := domain.FrontmatterRequest{
		Kind:     domain.ArtifactAgent,
		AgentKey: "test-agent",
	}

	plan, err := descriptor.ApplyFrontmatterSpec(d, req)

	if err != nil {
		t.Fatalf("ApplyFrontmatterSpec: %v", err)
	}
	var customField *domain.FrontmatterField
	for i := range plan.Set {
		if plan.Set[i].Key == "custom-field" {
			customField = &plan.Set[i]
			break
		}
	}
	if customField == nil {
		t.Fatal("custom-field not found in FrontmatterPlan.Set")
	}
	if customField.Value.Kind != domain.KindScalar {
		t.Errorf("custom-field value kind: want %q, got %q", domain.KindScalar, customField.Value.Kind)
	}
	if customField.Value.Scalar != "always-present" {
		t.Errorf("custom-field value scalar: want %q, got %q", "always-present", customField.Value.Scalar)
	}
}

func TestApplyFrontmatterSpec_DropFieldsAppearsInRemove(t *testing.T) {
	// The descriptor's FrontmatterSpec.Drop list names fields to remove from every agent.
	// The returned FrontmatterPlan.Remove must contain these keys.
	// valid-full.yaml declares Drop: ["recommended_tier", "tier_rationale"].
	d := loadFixture(t, "valid-full.yaml")
	req := domain.FrontmatterRequest{
		Kind:     domain.ArtifactAgent,
		AgentKey: "test-agent",
	}

	plan, err := descriptor.ApplyFrontmatterSpec(d, req)

	if err != nil {
		t.Fatalf("ApplyFrontmatterSpec: %v", err)
	}
	wantRemoved := []string{"recommended_tier", "tier_rationale"}
	removed := make(map[string]bool, len(plan.Remove))
	for _, key := range plan.Remove {
		removed[key] = true
	}
	for _, want := range wantRemoved {
		if !removed[want] {
			t.Errorf("expected %q in FrontmatterPlan.Remove; got: %v", want, plan.Remove)
		}
	}
}

func TestApplyFrontmatterSpec_KeyOrderFromDescriptor(t *testing.T) {
	// The descriptor's FrontmatterSpec.KeyOrder declares the desired output key ordering.
	// The FrontmatterPlan.KeyOrder must contain these keys in the same order.
	// valid-full.yaml declares KeyOrder: ["id", "version", "name", "model", "tools", "custom-field"].
	d := loadFixture(t, "valid-full.yaml")
	req := domain.FrontmatterRequest{
		Kind:     domain.ArtifactAgent,
		AgentKey: "test-agent",
	}

	plan, err := descriptor.ApplyFrontmatterSpec(d, req)

	if err != nil {
		t.Fatalf("ApplyFrontmatterSpec: %v", err)
	}
	wantOrder := []string{"id", "version", "name", "model", "tools", "custom-field"}
	if len(plan.KeyOrder) < len(wantOrder) {
		t.Fatalf("FrontmatterPlan.KeyOrder has %d entries, want at least %d: %v",
			len(plan.KeyOrder), len(wantOrder), plan.KeyOrder)
	}
	// The declared keys must appear in the same order, though additional keys may follow.
	pos := 0
	for _, want := range wantOrder {
		found := false
		for pos < len(plan.KeyOrder) {
			if plan.KeyOrder[pos] == want {
				found = true
				pos++
				break
			}
			pos++
		}
		if !found {
			t.Errorf("key %q not found in KeyOrder in order; KeyOrder: %v", want, plan.KeyOrder)
		}
	}
}

// --- ResolveTargetPath ---

// resolveAlgorithmDescriptor returns the valid-full.yaml descriptor, which declares:
//
//	paths.agents.project: ".test/agents"
//	paths.agents.user.windows: "${APPDATA}/Test/agents"
//	paths.agents.user."": "~/.config/test/agents"  (fallback)
//	paths.hooks.supported: false
//	extensions.agent: ".test.md"
func resolveAlgorithmDescriptor(t *testing.T) *domain.HarnessDescriptor {
	t.Helper()
	return loadFixture(t, "valid-full.yaml")
}

func TestResolveTargetPath_ProjectScope_ContainsProjectTemplate(t *testing.T) {
	// ScopeProject must resolve to a path that starts with or contains the project template
	// declared in the descriptor. For agents, the template is ".test/agents".
	d := resolveAlgorithmDescriptor(t)
	req := domain.TargetPathRequest{
		Kind:  domain.ArtifactAgent,
		Key:   "test-runner",
		Scope: domain.ScopeProject,
		GOOS:  "linux",
	}

	path, err := descriptor.ResolveTargetPath(d, req)

	if err != nil {
		t.Fatalf("ResolveTargetPath (project scope): %v", err)
	}
	if path == "" {
		t.Fatal("ResolveTargetPath must never return an empty path with a nil error")
	}
	if !strings.Contains(path, ".test/agents") {
		t.Errorf("project-scope path should contain the project template %q; got %q", ".test/agents", path)
	}
}

func TestResolveTargetPath_ProjectScope_EndsWithArtifactFilename(t *testing.T) {
	// The returned path must include the artifact's file name with the harness extension.
	// For agent "test-runner" with extension ".test.md", the path must end with
	// "test-runner.test.md".
	d := resolveAlgorithmDescriptor(t)
	req := domain.TargetPathRequest{
		Kind:  domain.ArtifactAgent,
		Key:   "test-runner",
		Scope: domain.ScopeProject,
		GOOS:  "linux",
	}

	path, err := descriptor.ResolveTargetPath(d, req)

	if err != nil {
		t.Fatalf("ResolveTargetPath (project scope): %v", err)
	}
	wantSuffix := "test-runner.test.md"
	if !strings.HasSuffix(path, wantSuffix) {
		t.Errorf("project-scope path should end with %q; got %q", wantSuffix, path)
	}
}

func TestResolveTargetPath_NonProjectScope_ReturnsErrUnsupportedScope(t *testing.T) {
	// Any scope value other than ScopeProject must return domain.ErrUnsupportedScope.
	// MOSAIC deploys into the project workspace only; user-scope is not supported.
	// Uses domain.Scope("user") as a representative non-project scope value, expressed as
	// a raw string cast so this test compiles regardless of whether the ScopeUser constant
	// continues to exist.
	d := resolveAlgorithmDescriptor(t)
	req := domain.TargetPathRequest{
		Kind:  domain.ArtifactAgent,
		Key:   "test-runner",
		Scope: domain.Scope("user"),
		GOOS:  "linux",
	}

	path, err := descriptor.ResolveTargetPath(d, req)

	if err == nil {
		t.Fatalf("ResolveTargetPath for non-project scope returned %q with nil error; want domain.ErrUnsupportedScope", path)
	}
	if !errors.Is(err, domain.ErrUnsupportedScope) {
		t.Errorf("ResolveTargetPath for non-project scope: err=%v; want errors.Is(err, domain.ErrUnsupportedScope)", err)
	}
	if path != "" {
		t.Errorf("ResolveTargetPath for non-project scope must return empty path alongside error; got %q", path)
	}
}

func TestResolveTargetPath_EmptyScope_ReturnsErrUnsupportedScope(t *testing.T) {
	// An empty scope that has not been normalised by the app layer must return
	// domain.ErrUnsupportedScope. Normalising empty → ScopeProject is the app layer's
	// responsibility; path resolution treats every non-project value as unsupported.
	d := resolveAlgorithmDescriptor(t)
	req := domain.TargetPathRequest{
		Kind:  domain.ArtifactAgent,
		Key:   "test-runner",
		Scope: domain.Scope(""),
		GOOS:  "linux",
	}

	path, err := descriptor.ResolveTargetPath(d, req)

	if err == nil {
		t.Fatalf("ResolveTargetPath for empty scope returned %q with nil error; want domain.ErrUnsupportedScope", path)
	}
	if !errors.Is(err, domain.ErrUnsupportedScope) {
		t.Errorf("ResolveTargetPath for empty scope: err=%v; want errors.Is(err, domain.ErrUnsupportedScope)", err)
	}
	if path != "" {
		t.Errorf("ResolveTargetPath for empty scope must return empty path alongside error; got %q", path)
	}
}

func TestResolveTargetPath_UnsupportedArtifactKind_ReturnsErrArtifactUnsupported(t *testing.T) {
	// The descriptor sets paths.hooks.supported: false. Requesting a path for ArtifactHook
	// must return ErrArtifactUnsupported, not an empty string with nil error.
	// This invariant is stated in the HarnessModule contract (harness_module.go).
	d := resolveAlgorithmDescriptor(t)
	req := domain.TargetPathRequest{
		Kind:  domain.ArtifactHook,
		Key:   "some-hook",
		Scope: domain.ScopeProject,
		GOOS:  "linux",
	}

	path, err := descriptor.ResolveTargetPath(d, req)

	if err == nil {
		t.Fatalf("expected ErrArtifactUnsupported for unsupported artifact kind, got nil error (path=%q)", path)
	}
	if !errors.Is(err, domain.ErrArtifactUnsupported) {
		t.Errorf("expected errors.Is(err, domain.ErrArtifactUnsupported); got: %v", err)
	}
	if path != "" {
		t.Errorf("path must be empty when returning ErrArtifactUnsupported, got %q", path)
	}
}

func TestResolveTargetPath_NeverReturnsEmptyPathWithNilError(t *testing.T) {
	// An empty path with a nil error is an invalid response: callers cannot use an empty
	// path and have no error to inspect. This case is forbidden by the TargetPath contract.
	d := resolveAlgorithmDescriptor(t)
	req := domain.TargetPathRequest{
		Kind:  domain.ArtifactAgent,
		Key:   "any-agent",
		Scope: domain.ScopeProject,
		GOOS:  "linux",
	}

	path, err := descriptor.ResolveTargetPath(d, req)

	if err == nil && path == "" {
		t.Error("ResolveTargetPath must not return an empty path with a nil error")
	}
}
