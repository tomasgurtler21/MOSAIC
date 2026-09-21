package descriptor_test

// Tests for MinimalGenericToolSet properties and RenderMinimalToolGrant refusal behaviour.
//
// These tests do not need registry-wide coverage: they cover the descriptor package's
// own exported symbols and the error conditions the rendering entry point enforces via
// test-local stub modules. Registry-wide rendering assertions live in
// minimal_grant_registry_test.go.
//
// TDD RED: every test in this file references descriptor.MinimalGenericToolSet,
// descriptor.RenderMinimalToolGrant, descriptor.ErrMinimalGrantFieldAbsent and
// descriptor.ErrMinimalGrantValueEmpty, none of which exist until I14.1-I14.3 are
// implemented. The file compiles only after those implementations land; until then the
// build failure is the intended RED state.

import (
	"errors"
	"testing"

	"mosaic-deploy/internal/domain"
	"mosaic-deploy/internal/harness/descriptor"
)

// ---------------------------------------------------------------------------
// T14.1(a-c): MinimalGenericToolSet membership, disjointness and vocabulary
// ---------------------------------------------------------------------------

// TestMinimalGenericToolSet_IsExactMembers asserts that MinimalGenericToolSet contains
// exactly the three non-escalating read/search generics and nothing else.
func TestMinimalGenericToolSet_IsExactMembers(t *testing.T) {
	want := map[string]bool{
		"file_read":      true,
		"file_search":    true,
		"content_search": true,
	}

	got := descriptor.MinimalGenericToolSet

	if len(got) != len(want) {
		t.Errorf("MinimalGenericToolSet has %d members; want %d (%v)", len(got), len(want), got)
	}

	for _, name := range got {
		if !want[name] {
			t.Errorf("MinimalGenericToolSet contains unexpected member %q; "+
				"the set must be exactly file_read, file_search, content_search", name)
		}
	}

	// Check for missing members.
	gotSet := make(map[string]bool, len(got))
	for _, name := range got {
		gotSet[name] = true
	}
	for name := range want {
		if !gotSet[name] {
			t.Errorf("MinimalGenericToolSet is missing expected member %q", name)
		}
	}
}

// TestMinimalGenericToolSet_DisjointFromEscalatingSet asserts that no member of
// MinimalGenericToolSet appears in the escalating set. If the escalating set ever
// gains a new member that is also in the minimal set, this test fires and forces a
// deliberate review of the grant.
func TestMinimalGenericToolSet_DisjointFromEscalatingSet(t *testing.T) {
	// The escalating set is the fixed set whose presence drives workspace-write mode.
	// It is not exported from the descriptor package (it lives in the Codex module),
	// so we restate it here. A discrepancy between this literal and the module's own
	// set is a finding to report to the implementer, not a reason to change this test.
	escalating := map[string]bool{
		"file_write": true,
		"file_edit":  true,
		"terminal":   true,
		"subagent":   true,
	}

	for _, name := range descriptor.MinimalGenericToolSet {
		if escalating[name] {
			t.Errorf("MinimalGenericToolSet contains escalating tool %q; "+
				"the minimal set must be disjoint from the escalating set so that "+
				"a minimal grant never upgrades a sandbox to workspace-write mode", name)
		}
	}
}

// TestMinimalGenericToolSet_SubsetOfGenericVocabulary asserts that every member of
// MinimalGenericToolSet is a member of the known generic tool vocabulary.
// The vocabulary is stated in the descriptors and in the codex.go comment; restating
// it here means a vocabulary change that would orphan a minimal-set member is caught.
func TestMinimalGenericToolSet_SubsetOfGenericVocabulary(t *testing.T) {
	// The generic tool vocabulary: the union of all generic tool names across every
	// harness descriptor. Listed here as a cross-check; if a descriptor adds a new
	// generic tool name and that name should be in the minimal set, this test will not
	// catch its absence -- but it will catch a minimal-set member that fell out of
	// every descriptor.
	knownVocabulary := map[string]bool{
		"file_read":        true,
		"file_write":       true,
		"file_edit":        true,
		"file_search":      true,
		"content_search":   true,
		"terminal":         true,
		"user_interaction": true,
		"subagent":         true,
		"skill":            true,
		"web_fetch":        true,
		"web_search":       true,
	}

	for _, name := range descriptor.MinimalGenericToolSet {
		if !knownVocabulary[name] {
			t.Errorf("MinimalGenericToolSet member %q is not in the known generic tool vocabulary; "+
				"the minimal set must be a subset of the vocabulary that harness descriptors map", name)
		}
	}
}

// ---------------------------------------------------------------------------
// T14.3(a,b,c,d): refusal cases for RenderMinimalToolGrant, using stub modules
// ---------------------------------------------------------------------------

// toolsKeyDescriptor builds a minimal HarnessDescriptor that declares the given tools
// key. It is the smallest descriptor needed to satisfy RenderMinimalToolGrant's
// structural checks without exercising any harness-specific logic.
func toolsKeyDescriptor(toolsKey string) *domain.HarnessDescriptor {
	return &domain.HarnessDescriptor{
		ID:          "stub-harness",
		DisplayName: "Stub Harness",
		Frontmatter: domain.FrontmatterSpec{
			ToolsKey: toolsKey,
		},
	}
}

// noToolsKeyDescriptor builds a minimal HarnessDescriptor that declares NO tools key,
// modelling the Codex-like case where capability is expressed through another field.
func noToolsKeyDescriptor() *domain.HarnessDescriptor {
	return &domain.HarnessDescriptor{
		ID:          "stub-no-key-harness",
		DisplayName: "Stub No-Key Harness",
		Frontmatter: domain.FrontmatterSpec{
			ToolsKey: "", // no tools key
		},
	}
}

// stubbedToolsModule is a minimal HarnessModule whose Tools() behaviour is injected
// at construction time. All other methods return safe zero values or the module's own
// descriptor, satisfying the interface without exercising production code.
type stubbedToolsModule struct {
	ref    domain.HarnessRef
	desc   *domain.HarnessDescriptor
	toolsFn func(req domain.ToolRequest) (domain.ToolResult, error)
}

func (m *stubbedToolsModule) Ref() domain.HarnessRef                                    { return m.ref }
func (m *stubbedToolsModule) Descriptor() *domain.HarnessDescriptor                     { return m.desc }
func (m *stubbedToolsModule) Close() error                                               { return nil }
func (m *stubbedToolsModule) Injection(_ domain.InjectionRequest) (string, bool)        { return "", false }
func (m *stubbedToolsModule) Frontmatter(_ domain.FrontmatterRequest) (domain.FrontmatterPlan, error) {
	return domain.FrontmatterPlan{}, nil
}
func (m *stubbedToolsModule) TargetPath(_ domain.TargetPathRequest) (string, error) {
	return "", domain.ErrArtifactUnsupported
}
func (m *stubbedToolsModule) HookPlan(_ domain.HookPlanRequest) (domain.HookPlan, error) {
	return domain.HookPlan{Supported: false, Reason: "stub"}, nil
}
func (m *stubbedToolsModule) Tools(req domain.ToolRequest) (domain.ToolResult, error) {
	return m.toolsFn(req)
}

// newStub constructs a stubbedToolsModule with the given descriptor and Tools function.
func newStub(desc *domain.HarnessDescriptor, toolsFn func(domain.ToolRequest) (domain.ToolResult, error)) *stubbedToolsModule {
	return &stubbedToolsModule{
		ref: domain.HarnessRef{
			ID:          desc.ID,
			DisplayName: desc.DisplayName,
			Tier:        domain.TierBuiltin,
			Usable:      true,
		},
		desc:    desc,
		toolsFn: toolsFn,
	}
}

// T14.3(a): Absence refusal -- tools key absent from rendered result.
//
// When the target declares a tools key and Tools() returns no field with that key,
// RenderMinimalToolGrant must refuse with ErrMinimalGrantFieldAbsent.
func TestRenderMinimalToolGrant_RefusesAbsentToolsKey(t *testing.T) {
	// Stub whose Tools() returns no fields at all, even though the descriptor declares
	// tools_key: "tools". This simulates a module override that drops the field.
	stub := newStub(
		toolsKeyDescriptor("tools"),
		func(req domain.ToolRequest) (domain.ToolResult, error) {
			// Return a result with no fields -- the declared key is absent.
			resolutions := make([]domain.ToolResolution, len(req.Generic))
			for i, g := range req.Generic {
				resolutions[i] = domain.ToolResolution{
					Generic: g,
					Outcome: domain.ToolMapped,
				}
			}
			return domain.ToolResult{Fields: nil, Resolutions: resolutions}, nil
		},
	)

	_, err := descriptor.RenderMinimalToolGrant(stub, "test-agent")
	if err == nil {
		t.Fatal("RenderMinimalToolGrant: got nil error for a stub that returns no fields; " +
			"want ErrMinimalGrantFieldAbsent when the declared tools key is absent from the result")
	}
	if !errors.Is(err, descriptor.ErrMinimalGrantFieldAbsent) {
		t.Errorf("RenderMinimalToolGrant: got %v; want errors.Is(err, ErrMinimalGrantFieldAbsent); "+
			"a module that drops the declared tools key must cause the entry point to fail with "+
			"the named absent-key sentinel", err)
	}
}

// T14.3(b): Value-level refusal -- tools key present but value is empty scalar.
//
// An empty comma-separated scalar is the concrete "inherit everything" hazard: Claude
// Code reads a missing or empty tools list as unrestricted. A presence-only check
// would pass this, so a value-level check is required alongside it.
func TestRenderMinimalToolGrant_RefusesEmptyScalar(t *testing.T) {
	stub := newStub(
		toolsKeyDescriptor("tools"),
		func(req domain.ToolRequest) (domain.ToolResult, error) {
			resolutions := make([]domain.ToolResolution, len(req.Generic))
			for i, g := range req.Generic {
				resolutions[i] = domain.ToolResolution{Generic: g, Outcome: domain.ToolMapped}
			}
			// Return the declared tools key with an empty scalar value -- the hazard form.
			return domain.ToolResult{
				Fields: []domain.FrontmatterField{
					{Key: "tools", Value: domain.ScalarValue("", domain.QuotePlain)},
				},
				Resolutions: resolutions,
			}, nil
		},
	)

	_, err := descriptor.RenderMinimalToolGrant(stub, "test-agent")
	if err == nil {
		t.Fatal("RenderMinimalToolGrant: got nil error for a stub returning an empty scalar tools value; " +
			"want ErrMinimalGrantValueEmpty; an empty scalar is the 'inherit everything' hazard")
	}
	if !errors.Is(err, descriptor.ErrMinimalGrantValueEmpty) {
		t.Errorf("RenderMinimalToolGrant: got %v; want errors.Is(err, ErrMinimalGrantValueEmpty); "+
			"an empty scalar value must be refused with the named value-empty sentinel", err)
	}
}

// T14.3(b): Value-level refusal -- tools key present but value is empty list.
//
// An empty list is empty in the list-shape's own syntax. It is treated the same as an
// empty scalar: present-but-empty must not be accepted.
func TestRenderMinimalToolGrant_RefusesEmptyList(t *testing.T) {
	stub := newStub(
		toolsKeyDescriptor("tools"),
		func(req domain.ToolRequest) (domain.ToolResult, error) {
			resolutions := make([]domain.ToolResolution, len(req.Generic))
			for i, g := range req.Generic {
				resolutions[i] = domain.ToolResolution{Generic: g, Outcome: domain.ToolMapped}
			}
			// Return the declared tools key with an empty list value.
			return domain.ToolResult{
				Fields: []domain.FrontmatterField{
					{Key: "tools", Value: domain.ListValue(nil, domain.ListBlock)},
				},
				Resolutions: resolutions,
			}, nil
		},
	)

	_, err := descriptor.RenderMinimalToolGrant(stub, "test-agent")
	if err == nil {
		t.Fatal("RenderMinimalToolGrant: got nil error for a stub returning an empty list tools value; " +
			"want ErrMinimalGrantValueEmpty; an empty list must be refused with the value-empty sentinel")
	}
	if !errors.Is(err, descriptor.ErrMinimalGrantValueEmpty) {
		t.Errorf("RenderMinimalToolGrant: got %v; want errors.Is(err, ErrMinimalGrantValueEmpty)", err)
	}
}

// T14.3(b): Value-level refusal -- permission map with no allow entry.
//
// A permission map whose every pair is "deny" grants nothing; it is empty in the
// permission shape's own syntax. It must be refused even though the tools key is
// present, because a mapping of all-deny is as dangerous as an empty list.
func TestRenderMinimalToolGrant_RefusesAllDenyPermissionMap(t *testing.T) {
	stub := newStub(
		toolsKeyDescriptor("permission"),
		func(req domain.ToolRequest) (domain.ToolResult, error) {
			resolutions := make([]domain.ToolResolution, len(req.Generic))
			for i, g := range req.Generic {
				resolutions[i] = domain.ToolResolution{Generic: g, Outcome: domain.ToolMapped}
			}
			// Return the declared tools key with a permission map that has no allow entry.
			pairs := []domain.FieldPair{
				{Key: "read", Value: domain.ScalarValue(string(domain.Deny), domain.QuotePlain)},
				{Key: "write", Value: domain.ScalarValue(string(domain.Deny), domain.QuotePlain)},
			}
			return domain.ToolResult{
				Fields: []domain.FrontmatterField{
					{Key: "permission", Value: domain.MappingValue(pairs)},
				},
				Resolutions: resolutions,
			}, nil
		},
	)

	_, err := descriptor.RenderMinimalToolGrant(stub, "test-agent")
	if err == nil {
		t.Fatal("RenderMinimalToolGrant: got nil error for a stub returning all-deny permission map; " +
			"want ErrMinimalGrantValueEmpty; a mapping with no allow entry is empty in permission syntax")
	}
	if !errors.Is(err, descriptor.ErrMinimalGrantValueEmpty) {
		t.Errorf("RenderMinimalToolGrant: got %v; want errors.Is(err, ErrMinimalGrantValueEmpty)", err)
	}
}

// T14.3(b): Whitespace-only scalar is also refused.
//
// A scalar that contains only whitespace is empty in the scalar syntax even though it
// is not the zero-length string. The Claude Code comma-separated form with only
// whitespace would still be read as unrestricted.
func TestRenderMinimalToolGrant_RefusesWhitespaceScalar(t *testing.T) {
	stub := newStub(
		toolsKeyDescriptor("tools"),
		func(req domain.ToolRequest) (domain.ToolResult, error) {
			resolutions := make([]domain.ToolResolution, len(req.Generic))
			for i, g := range req.Generic {
				resolutions[i] = domain.ToolResolution{Generic: g, Outcome: domain.ToolMapped}
			}
			return domain.ToolResult{
				Fields: []domain.FrontmatterField{
					{Key: "tools", Value: domain.ScalarValue("   ", domain.QuotePlain)},
				},
				Resolutions: resolutions,
			}, nil
		},
	)

	_, err := descriptor.RenderMinimalToolGrant(stub, "test-agent")
	if err == nil {
		t.Fatal("RenderMinimalToolGrant: got nil error for a stub returning whitespace-only scalar; " +
			"want ErrMinimalGrantValueEmpty; whitespace-only is empty in scalar syntax")
	}
	if !errors.Is(err, descriptor.ErrMinimalGrantValueEmpty) {
		t.Errorf("RenderMinimalToolGrant: got %v; want errors.Is(err, ErrMinimalGrantValueEmpty)", err)
	}
}

// TestRenderMinimalToolGrant_AcceptsPermissionMapWithAllow verifies that a permission
// map with at least one allow entry is accepted (non-empty in permission syntax).
//
// This distinguishes "all deny" (refused) from "has at least one allow" (accepted).
// The permission shape's explicit-none form, if it exists, must not be confused with
// the empty form.
//
// Note on explicit-none coverage: the acceptance test for the target's explicit "none"
// form (where its syntax has one, distinct from refused empty forms) is intentionally
// deferred. None of the five currently registered harnesses defines an explicit "none"
// token for the permission shape. When a harness is added that introduces such a form,
// a new stub test should be written to prove it is accepted rather than refused as an
// empty grant. Until then, the explicit-none codepath is not exercised, and this comment
// is the tracking marker so the gap is visible rather than silently overlooked.
func TestRenderMinimalToolGrant_AcceptsPermissionMapWithAllow(t *testing.T) {
	stub := newStub(
		toolsKeyDescriptor("permission"),
		func(req domain.ToolRequest) (domain.ToolResult, error) {
			resolutions := make([]domain.ToolResolution, len(req.Generic))
			for i, g := range req.Generic {
				resolutions[i] = domain.ToolResolution{Generic: g, Outcome: domain.ToolMapped}
			}
			// A permission map with at least one allow entry: non-empty in permission syntax.
			pairs := []domain.FieldPair{
				{Key: "read", Value: domain.ScalarValue(string(domain.Allow), domain.QuotePlain)},
				{Key: "write", Value: domain.ScalarValue(string(domain.Deny), domain.QuotePlain)},
			}
			return domain.ToolResult{
				Fields: []domain.FrontmatterField{
					{Key: "permission", Value: domain.MappingValue(pairs)},
				},
				Resolutions: resolutions,
			}, nil
		},
	)

	_, err := descriptor.RenderMinimalToolGrant(stub, "test-agent")
	if err != nil {
		t.Errorf("RenderMinimalToolGrant: got unexpected error %v for a stub returning a "+
			"permission map with at least one allow entry; a non-empty permission map must be accepted", err)
	}
}

// T14.3(c): A non-empty scalar value is accepted.
//
// A scalar containing tool names is non-empty in scalar syntax and must be accepted.
func TestRenderMinimalToolGrant_AcceptsNonEmptyScalar(t *testing.T) {
	stub := newStub(
		toolsKeyDescriptor("tools"),
		func(req domain.ToolRequest) (domain.ToolResult, error) {
			resolutions := make([]domain.ToolResolution, len(req.Generic))
			for i, g := range req.Generic {
				resolutions[i] = domain.ToolResolution{Generic: g, Outcome: domain.ToolMapped}
			}
			return domain.ToolResult{
				Fields: []domain.FrontmatterField{
					{Key: "tools", Value: domain.ScalarValue("Read, Glob, Grep", domain.QuotePlain)},
				},
				Resolutions: resolutions,
			}, nil
		},
	)

	_, err := descriptor.RenderMinimalToolGrant(stub, "test-agent")
	if err != nil {
		t.Errorf("RenderMinimalToolGrant: got unexpected error %v for a stub returning a "+
			"non-empty scalar tools value; a present non-empty scalar must be accepted", err)
	}
}

// T14.3(c): A non-empty list value is accepted.
func TestRenderMinimalToolGrant_AcceptsNonEmptyList(t *testing.T) {
	stub := newStub(
		toolsKeyDescriptor("tools"),
		func(req domain.ToolRequest) (domain.ToolResult, error) {
			resolutions := make([]domain.ToolResolution, len(req.Generic))
			for i, g := range req.Generic {
				resolutions[i] = domain.ToolResolution{Generic: g, Outcome: domain.ToolMapped}
			}
			items := []domain.FieldValue{
				domain.ScalarValue("read", domain.QuotePlain),
				domain.ScalarValue("search", domain.QuotePlain),
			}
			return domain.ToolResult{
				Fields: []domain.FrontmatterField{
					{Key: "tools", Value: domain.ListValue(items, domain.ListBlock)},
				},
				Resolutions: resolutions,
			}, nil
		},
	)

	_, err := descriptor.RenderMinimalToolGrant(stub, "test-agent")
	if err != nil {
		t.Errorf("RenderMinimalToolGrant: got unexpected error %v for a stub returning a "+
			"non-empty list tools value; a present non-empty list must be accepted", err)
	}
}

// T14.3: A harness that declares NO tools key does not trigger the tools-key refusals.
//
// When ToolsKeyDeclared is false (Codex-like), neither ErrMinimalGrantFieldAbsent nor
// ErrMinimalGrantValueEmpty applies. Returning empty Fields is allowed in this case:
// a module with no tools key states capability through another field, and if that
// module returns no fields at all, placement 2 (contracttest) catches it, not this
// entry point.
//
// This test proves the guard was narrowed to tools-key-only: a stub declaring no
// tools key and returning empty Fields must pass placement 1 without error.
func TestRenderMinimalToolGrant_NoToolsKeyEmptyFieldsPassesPlacement1(t *testing.T) {
	stub := newStub(
		noToolsKeyDescriptor(),
		func(req domain.ToolRequest) (domain.ToolResult, error) {
			resolutions := make([]domain.ToolResolution, len(req.Generic))
			for i, g := range req.Generic {
				resolutions[i] = domain.ToolResolution{Generic: g, Outcome: domain.ToolMapped}
			}
			// Return empty Fields: no sandbox_mode, no tools, nothing.
			// This is the stub that proves placement 1 does NOT guard the no-tools-key case.
			return domain.ToolResult{Fields: nil, Resolutions: resolutions}, nil
		},
	)

	grant, err := descriptor.RenderMinimalToolGrant(stub, "test-agent")
	if err != nil {
		t.Errorf("RenderMinimalToolGrant: got unexpected error %v for a no-tools-key stub "+
			"returning empty Fields; the entry point must not refuse this case -- "+
			"ToolsKeyDeclared=false means the tools-key refusals are disabled; "+
			"if this guard fires, the partition contract is violated", err)
	}

	// The result should reflect ToolsKeyDeclared=false.
	if grant.ToolsKeyDeclared {
		t.Error("RenderMinimalToolGrant: ToolsKeyDeclared=true for a stub with no tools key; "+
			"want ToolsKeyDeclared=false; the flag must reflect the descriptor, not the Fields content")
	}
}
