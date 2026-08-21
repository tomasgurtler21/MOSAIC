package descriptor_test

// Tests for the shared descriptor-driven algorithms exported from harness/descriptor:
//   - ApplyFrontmatterSpec: derives FrontmatterPlan from a descriptor's FrontmatterSpec and a request.
//   - ResolveTargetPath: expands a ScopedPaths template for a given artifact kind, scope, and GOOS.
//   - ResolveRoleConditionalFields: resolves role-conditional entries to FrontmatterField values.
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
//
//   ResolveRoleConditionalFields (TDD RED - implementation pending):
//   - A matching role produces one FrontmatterField with the role's value.
//   - An empty role produces no fields.
//   - An unrecognized role string produces no fields.
//   - All four standard roles (subagent, orchestrator, utility, standalone) resolve correctly.
//   - Input order is preserved in the returned slice.
//   - An empty entry slice produces an empty result regardless of role.

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

// TestApplyFrontmatterSpec_RoleConditionalEntry_AppendedForMatchingRole verifies that a
// descriptor whose FrontmatterSpec.RoleConditionalAdd is non-empty produces a resolved
// FrontmatterField in FrontmatterPlan.Set when the request's Role matches an entry. This
// covers the descriptor-only adapter path: any module that delegates frontmatter shaping
// entirely to ApplyFrontmatterSpec (rather than a custom Frontmatter method) must have
// role-conditional fields resolved here.
//
// The descriptor is built programmatically (not from a fixture file) so the test does not
// depend on the YAML loader supporting value_by_role — the loader and the resolution are
// separate concerns.
//
// RED: currently fails because ApplyFrontmatterSpec ignores req (including req.Role) and
// returns only d.Frontmatter.Add entries. After implementation (I2.5), it calls
// ResolveRoleConditionalFields and appends the results to FrontmatterPlan.Set.
func TestApplyFrontmatterSpec_RoleConditionalEntry_AppendedForMatchingRole(t *testing.T) {
	// Build a descriptor with a single role-conditional entry: user-invocable is true for
	// orchestrator and false for subagent.
	d := &domain.HarnessDescriptor{
		Frontmatter: domain.FrontmatterSpec{
			RoleConditionalAdd: []domain.RoleConditionalField{
				{
					Key: "user-invocable",
					ValueByRole: map[domain.AgentRole]domain.FieldValue{
						domain.RoleOrchestrator: domain.ScalarValue("true", domain.QuotePlain),
						domain.RoleSubagent:     domain.ScalarValue("false", domain.QuotePlain),
					},
				},
			},
		},
	}
	req := domain.FrontmatterRequest{
		Kind:     domain.ArtifactAgent,
		AgentKey: "test-agent",
		Role:     domain.RoleOrchestrator,
	}

	plan, err := descriptor.ApplyFrontmatterSpec(d, req)

	if err != nil {
		t.Fatalf("ApplyFrontmatterSpec: %v", err)
	}
	var found bool
	var foundValue string
	for _, f := range plan.Set {
		if f.Key == "user-invocable" {
			found = true
			foundValue = f.Value.Scalar
			break
		}
	}
	if !found {
		keys := make([]string, len(plan.Set))
		for i, f := range plan.Set {
			keys[i] = f.Key
		}
		t.Errorf("user-invocable not found in FrontmatterPlan.Set for orchestrator role; "+
			"ApplyFrontmatterSpec must resolve RoleConditionalAdd entries; Set keys: %v", keys)
		return
	}
	if foundValue != "true" {
		t.Errorf("user-invocable value for orchestrator role: want %q, got %q", "true", foundValue)
	}
}

// TestApplyFrontmatterSpec_RoleConditionalEntry_AbsentForNonMatchingRole verifies that when
// the request's Role does not appear in a RoleConditionalAdd entry's ValueByRole map, the
// field is omitted from FrontmatterPlan.Set. This is the fallback behavior for unknown/zero
// roles described in the design.
//
// RED: currently ApplyFrontmatterSpec ignores req.Role entirely, but since it also ignores
// RoleConditionalAdd entirely, the field is absent for the wrong reason (the whole
// role-conditional resolution is missing). After implementation, it must be absent because
// the role has no matching entry.
func TestApplyFrontmatterSpec_RoleConditionalEntry_AbsentForNonMatchingRole(t *testing.T) {
	// Only orchestrator has a value; subagent is not in the map.
	d := &domain.HarnessDescriptor{
		Frontmatter: domain.FrontmatterSpec{
			RoleConditionalAdd: []domain.RoleConditionalField{
				{
					Key: "orchestrator-only-field",
					ValueByRole: map[domain.AgentRole]domain.FieldValue{
						domain.RoleOrchestrator: domain.ScalarValue("present", domain.QuotePlain),
					},
				},
			},
		},
	}
	req := domain.FrontmatterRequest{
		Kind:     domain.ArtifactAgent,
		AgentKey: "test-agent",
		Role:     domain.RoleSubagent, // not in ValueByRole
	}

	plan, err := descriptor.ApplyFrontmatterSpec(d, req)

	if err != nil {
		t.Fatalf("ApplyFrontmatterSpec: %v", err)
	}
	for _, f := range plan.Set {
		if f.Key == "orchestrator-only-field" {
			t.Errorf("orchestrator-only-field appeared in FrontmatterPlan.Set for subagent role; "+
				"role-conditional fields must be omitted when the role has no matching entry; "+
				"Set: %v", plan.Set)
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

// --- ResolveRoleConditionalFields (TDD RED - implementation pending) ---

// roleConditionalEntry is a helper that builds a single-entry RoleConditionalField
// covering all four standard roles, mapping each to its own distinct scalar value.
// Used across multiple resolution tests to reduce boilerplate.
func allRolesEntry() domain.RoleConditionalField {
	return domain.RoleConditionalField{
		Key: "user-invocable",
		ValueByRole: map[domain.AgentRole]domain.FieldValue{
			domain.RoleSubagent:     domain.ScalarValue("false", domain.QuotePlain),
			domain.RoleOrchestrator: domain.ScalarValue("true", domain.QuotePlain),
			domain.RoleUtility:      domain.ScalarValue("true", domain.QuotePlain),
			domain.RoleStandalone:   domain.ScalarValue("true", domain.QuotePlain),
		},
	}
}

// TestResolveRoleConditionalFields_MatchingRole_ProducesField verifies that when the
// role has a corresponding entry in ValueByRole, exactly one FrontmatterField is returned
// with the key and value declared for that role.
// RED: the stub returns nil, so len(result) == 0 and the test fails.
func TestResolveRoleConditionalFields_MatchingRole_ProducesField(t *testing.T) {
	entries := []domain.RoleConditionalField{allRolesEntry()}

	result := descriptor.ResolveRoleConditionalFields(entries, domain.RoleOrchestrator)

	if len(result) != 1 {
		t.Fatalf("ResolveRoleConditionalFields with matching role: want 1 field, got %d", len(result))
	}
	if result[0].Key != "user-invocable" {
		t.Errorf("result[0].Key: want %q, got %q", "user-invocable", result[0].Key)
	}
	if result[0].Value.Scalar != "true" {
		t.Errorf("result[0].Value.Scalar for orchestrator: want %q, got %q", "true", result[0].Value.Scalar)
	}
}

// TestResolveRoleConditionalFields_EmptyRole_ProducesNoFields verifies that an empty
// (zero-value) role produces no output fields. The zero role is not a valid key, so
// the fallback behavior is to omit the entry.
func TestResolveRoleConditionalFields_EmptyRole_ProducesNoFields(t *testing.T) {
	entries := []domain.RoleConditionalField{allRolesEntry()}

	result := descriptor.ResolveRoleConditionalFields(entries, domain.AgentRole(""))

	if len(result) != 0 {
		t.Errorf("ResolveRoleConditionalFields with empty role: want 0 fields, got %d: %v", len(result), result)
	}
}

// TestResolveRoleConditionalFields_UnknownRole_ProducesNoFields verifies that a role
// string not present in any entry's ValueByRole map produces no output fields.
func TestResolveRoleConditionalFields_UnknownRole_ProducesNoFields(t *testing.T) {
	entries := []domain.RoleConditionalField{allRolesEntry()}

	result := descriptor.ResolveRoleConditionalFields(entries, domain.AgentRole("not-a-real-role"))

	if len(result) != 0 {
		t.Errorf("ResolveRoleConditionalFields with unknown role: want 0 fields, got %d: %v", len(result), result)
	}
}

// TestResolveRoleConditionalFields_EmptyEntries_ProducesNoFields verifies that an empty
// input slice always produces an empty result regardless of role.
func TestResolveRoleConditionalFields_EmptyEntries_ProducesNoFields(t *testing.T) {
	result := descriptor.ResolveRoleConditionalFields(nil, domain.RoleOrchestrator)
	if len(result) != 0 {
		t.Errorf("ResolveRoleConditionalFields with nil entries: want 0 fields, got %d", len(result))
	}

	result = descriptor.ResolveRoleConditionalFields([]domain.RoleConditionalField{}, domain.RoleOrchestrator)
	if len(result) != 0 {
		t.Errorf("ResolveRoleConditionalFields with empty entries: want 0 fields, got %d", len(result))
	}
}

// TestResolveRoleConditionalFields_AllStandardRoles_ResolveCorrectly is a table-driven
// test covering all four standard AgentRole constants. Each role resolves to the value
// declared for it in allRolesEntry(): subagent→false, orchestrator/utility/standalone→true.
// RED: the stub returns nil for all cases, causing the test to fail for all rows.
func TestResolveRoleConditionalFields_AllStandardRoles_ResolveCorrectly(t *testing.T) {
	entries := []domain.RoleConditionalField{allRolesEntry()}

	tests := []struct {
		role          domain.AgentRole
		wantScalar    string
	}{
		{domain.RoleSubagent, "false"},
		{domain.RoleOrchestrator, "true"},
		{domain.RoleUtility, "true"},
		{domain.RoleStandalone, "true"},
	}

	for _, tc := range tests {
		t.Run(string(tc.role), func(t *testing.T) {
			result := descriptor.ResolveRoleConditionalFields(entries, tc.role)
			if len(result) != 1 {
				t.Fatalf("role %q: want 1 field, got %d", tc.role, len(result))
			}
			if result[0].Value.Scalar != tc.wantScalar {
				t.Errorf("role %q: want scalar %q, got %q", tc.role, tc.wantScalar, result[0].Value.Scalar)
			}
		})
	}
}

// TestResolveRoleConditionalFields_InputOrderPreserved verifies that when multiple
// role-conditional entries are provided, the returned fields appear in the same order
// as the input entries (for a role that matches all of them).
// RED: stub returns nil.
func TestResolveRoleConditionalFields_InputOrderPreserved(t *testing.T) {
	firstEntry := domain.RoleConditionalField{
		Key: "first-key",
		ValueByRole: map[domain.AgentRole]domain.FieldValue{
			domain.RoleOrchestrator: domain.ScalarValue("first-value", domain.QuotePlain),
		},
	}
	secondEntry := domain.RoleConditionalField{
		Key: "second-key",
		ValueByRole: map[domain.AgentRole]domain.FieldValue{
			domain.RoleOrchestrator: domain.ScalarValue("second-value", domain.QuotePlain),
		},
	}
	entries := []domain.RoleConditionalField{firstEntry, secondEntry}

	result := descriptor.ResolveRoleConditionalFields(entries, domain.RoleOrchestrator)

	if len(result) != 2 {
		t.Fatalf("want 2 resolved fields, got %d", len(result))
	}
	if result[0].Key != "first-key" {
		t.Errorf("result[0].Key: want %q, got %q", "first-key", result[0].Key)
	}
	if result[1].Key != "second-key" {
		t.Errorf("result[1].Key: want %q, got %q", "second-key", result[1].Key)
	}
}
