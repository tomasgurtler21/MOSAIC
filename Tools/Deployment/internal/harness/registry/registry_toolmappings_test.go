package registry_test

// registry_toolmappings_test.go tests the ToolMappings hook in registry.Options (T4.1).
//
// The ToolMappings hook, when non-nil, is called once per successfully constructed module
// during Discover. It receives the harness id and the descriptor's own declared tool mappings,
// and returns the effective mapping set to install on that module's descriptor before the
// module becomes reachable through Resolve.
//
// All tests in this file reference registry.Options.ToolMappings, which does not exist until
// I4.1 is implemented. Every test therefore fails to compile until then — that is the intended
// TDD RED state.

import (
	"testing"

	"mosaic-deploy/internal/domain"
	"mosaic-deploy/internal/harness/registry"
)

// ---------------------------------------------------------------------------
// moduleWithMappings — a HarnessModule whose descriptor carries explicit tool
// mappings, so tests can assert whether those mappings are preserved or replaced.
// ---------------------------------------------------------------------------

type moduleWithMappings struct {
	ref  domain.HarnessRef
	desc *domain.HarnessDescriptor
}

func (m *moduleWithMappings) Ref() domain.HarnessRef                    { return m.ref }
func (m *moduleWithMappings) Descriptor() *domain.HarnessDescriptor      { return m.desc }
func (m *moduleWithMappings) Close() error                               { return nil }
func (m *moduleWithMappings) Tools(_ domain.ToolRequest) (domain.ToolResult, error) {
	return domain.ToolResult{}, nil
}
func (m *moduleWithMappings) Frontmatter(_ domain.FrontmatterRequest) (domain.FrontmatterPlan, error) {
	return domain.FrontmatterPlan{}, nil
}
func (m *moduleWithMappings) TargetPath(_ domain.TargetPathRequest) (string, error) {
	return "", domain.ErrArtifactUnsupported
}
func (m *moduleWithMappings) Injection(_ domain.InjectionRequest) (string, bool) { return "", false }
func (m *moduleWithMappings) HookPlan(_ domain.HookPlanRequest) (domain.HookPlan, error) {
	return domain.HookPlan{Supported: false, Reason: "stub"}, nil
}

// newMappingsModule creates a built-in stub module with the given harness id and initial
// tool mappings. The module's descriptor pointer is returned directly by Descriptor(), so
// callers may mutate it (as the registry hook does) to observe downstream effects.
func newMappingsModule(id string, mappings []domain.ToolMapping) domain.HarnessModule {
	return &moduleWithMappings{
		ref: domain.HarnessRef{
			ID:          id,
			DisplayName: id + "-display",
			Tier:        domain.TierBuiltin,
			Usable:      true,
		},
		desc: &domain.HarnessDescriptor{
			SchemaVersion: "1",
			ID:            id,
			DisplayName:   id + "-display",
			Tools: domain.ToolSpec{
				Shape:    domain.ShapeList,
				Mappings: mappings,
			},
			Frontmatter: domain.FrontmatterSpec{
				ToolsKey: "tools",
			},
		},
	}
}

// ---------------------------------------------------------------------------
// hookRecorder captures invocations of the ToolMappings hook so tests can assert
// which harness ids were received and what declared mappings were passed.
// ---------------------------------------------------------------------------

type hookCall struct {
	harnessID string
	declared  []domain.ToolMapping
}

type hookRecorder struct {
	calls  []hookCall
	result []domain.ToolMapping // value the hook returns; nil means "leave unchanged"
}

func (r *hookRecorder) hook() func(string, []domain.ToolMapping) []domain.ToolMapping {
	return func(harnessID string, declared []domain.ToolMapping) []domain.ToolMapping {
		r.calls = append(r.calls, hookCall{harnessID: harnessID, declared: declared})
		return r.result
	}
}

// hookCallIDs extracts the harness ids from recorded hook calls for diagnostic messages.
func hookCallIDs(calls []hookCall) []string {
	ids := make([]string, len(calls))
	for i, c := range calls {
		ids[i] = c.harnessID
	}
	return ids
}

// ---------------------------------------------------------------------------
// T4.1 — ToolMappings hook behaviour during Discover
// ---------------------------------------------------------------------------

// TestDiscover_ToolMappingsHookNil_DescriptorMappingsUntouched verifies that when the
// ToolMappings hook is nil (the no-config case), the descriptor's own tool mappings are
// not modified. This is the regression guard that proves harnesses and agents with no
// configured destination mappings produce byte-identical output to before this change.
func TestDiscover_ToolMappingsHookNil_DescriptorMappingsUntouched(t *testing.T) {
	root := makeRoot(t)
	id := uniqueID(t, "hooknil")

	originalMappings := []domain.ToolMapping{
		{
			Generic: "file_read",
			Destinations: []domain.ToolDestination{
				{Kind: domain.DestMain, Names: []string{"ReadFile"}},
			},
		},
	}
	mod := newMappingsModule(id, originalMappings)
	registry.Register(id, func(_ registry.BuiltinOptions) (domain.HarnessModule, error) { return mod, nil })

	// ToolMappings is nil — the no-config case. The field does not exist yet, so this
	// struct literal fails to compile until I4.1 adds it.
	reg, err := registry.Discover(registry.Options{
		MosaicRoot:   root,
		ToolMappings: nil, // field does not exist → compile error (TDD RED)
	})
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}

	m, err := reg.Resolve(id)
	if err != nil {
		t.Fatalf("Resolve(%q): %v", id, err)
	}

	got := m.Descriptor().Tools.Mappings
	if len(got) != 1 {
		t.Fatalf("Descriptor().Tools.Mappings: got %d entries, want 1; "+
			"a nil ToolMappings hook must leave the descriptor's own mappings untouched",
			len(got))
	}
	if got[0].Generic != "file_read" {
		t.Errorf("Descriptor().Tools.Mappings[0].Generic = %q, want %q; "+
			"nil hook must preserve the descriptor's declared mappings",
			got[0].Generic, "file_read")
	}
}

// TestDiscover_ToolMappingsHook_CalledWithHarnessID verifies that the hook receives the
// harness id as its first argument. The hook uses the id to look up harness-specific
// mappings in the loaded project and user config without a separate join step.
func TestDiscover_ToolMappingsHook_CalledWithHarnessID(t *testing.T) {
	root := makeRoot(t)
	id := uniqueID(t, "hookid")

	mod := newMappingsModule(id, nil)
	registry.Register(id, func(_ registry.BuiltinOptions) (domain.HarnessModule, error) { return mod, nil })

	rec := &hookRecorder{result: nil}
	reg, err := registry.Discover(registry.Options{
		MosaicRoot:   root,
		ToolMappings: rec.hook(), // field does not exist → compile error (TDD RED)
	})
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	// Resolve to ensure the module's construction path was exercised.
	_, _ = reg.Resolve(id)

	if len(rec.calls) == 0 {
		t.Fatal("ToolMappings hook was never called; " +
			"Discover must call the hook for each successfully constructed module")
	}
	found := false
	for _, c := range rec.calls {
		if c.harnessID == id {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("ToolMappings hook was not called with harness id %q; "+
			"got calls with ids: %v", id, hookCallIDs(rec.calls))
	}
}

// TestDiscover_ToolMappingsHook_CalledWithDeclaredMappings verifies that the hook receives
// the descriptor's own declared tool mappings as its second argument. This is the input the
// hook needs to compute the effective merge (user > project > descriptor).
func TestDiscover_ToolMappingsHook_CalledWithDeclaredMappings(t *testing.T) {
	root := makeRoot(t)
	id := uniqueID(t, "hookdecl")

	declared := []domain.ToolMapping{
		{
			Generic: "user_interaction",
			Destinations: []domain.ToolDestination{
				{Kind: domain.DestMain, Names: []string{"AskUserQuestion"}},
			},
		},
	}
	mod := newMappingsModule(id, declared)
	registry.Register(id, func(_ registry.BuiltinOptions) (domain.HarnessModule, error) { return mod, nil })

	rec := &hookRecorder{result: nil}
	_, err := registry.Discover(registry.Options{
		MosaicRoot:   root,
		ToolMappings: rec.hook(), // field does not exist → compile error (TDD RED)
	})
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}

	if len(rec.calls) == 0 {
		t.Fatal("ToolMappings hook was never called")
	}
	var call *hookCall
	for i := range rec.calls {
		if rec.calls[i].harnessID == id {
			call = &rec.calls[i]
			break
		}
	}
	if call == nil {
		t.Fatalf("ToolMappings hook was not called with harness id %q; calls: %v",
			id, hookCallIDs(rec.calls))
	}
	if len(call.declared) == 0 {
		t.Fatal("hook received empty declared mappings; " +
			"the descriptor's own declared mappings must be passed as the second argument")
	}
	if call.declared[0].Generic != "user_interaction" {
		t.Errorf("hook received declared[0].Generic = %q, want %q",
			call.declared[0].Generic, "user_interaction")
	}
}

// TestDiscover_ToolMappingsHook_ReturnValueInstalledOnDescriptor verifies that the mapping
// set returned by the hook is installed on the module's descriptor. After Discover, calling
// Resolve(id).Descriptor().Tools.Mappings must return the hook's result, not the descriptor's
// original declared set. This is the mechanism that lets config-declared mappings reach the
// module without modifying the descriptor source on disk.
func TestDiscover_ToolMappingsHook_ReturnValueInstalledOnDescriptor(t *testing.T) {
	root := makeRoot(t)
	id := uniqueID(t, "hookinstall")

	// Module starts with no mappings — simulating a harness whose descriptor declares
	// no user_interaction mapping (which is correct: FR-4 leaves it to user config).
	mod := newMappingsModule(id, nil)
	registry.Register(id, func(_ registry.BuiltinOptions) (domain.HarnessModule, error) { return mod, nil })

	// The hook returns a multi-destination mapping: both a main destination (adding the
	// tool to the harness's built-in tools field) and a field destination (producing a
	// separate mcp_servers frontmatter key).
	configMappings := []domain.ToolMapping{
		{
			Generic: "user_interaction",
			Destinations: []domain.ToolDestination{
				{Kind: domain.DestMain, Names: []string{"AskUser"}},
				{Kind: domain.DestField, Field: "mcp_servers", Format: domain.FormatListBlock, Names: []string{"user-feedback"}},
			},
		},
	}
	rec := &hookRecorder{result: configMappings}
	reg, err := registry.Discover(registry.Options{
		MosaicRoot:   root,
		ToolMappings: rec.hook(), // field does not exist → compile error (TDD RED)
	})
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}

	m, err := reg.Resolve(id)
	if err != nil {
		t.Fatalf("Resolve(%q): %v", id, err)
	}

	got := m.Descriptor().Tools.Mappings
	if len(got) == 0 {
		t.Fatalf("Descriptor().Tools.Mappings is empty; " +
			"the hook's return value must be installed on the descriptor before Resolve returns the module")
	}
	if len(got) != 1 {
		t.Fatalf("Descriptor().Tools.Mappings: got %d entries, want 1 (the hook's return value); "+
			"hook result must replace the descriptor's own mapping set", len(got))
	}
	if got[0].Generic != "user_interaction" {
		t.Errorf("Descriptor().Tools.Mappings[0].Generic = %q, want %q",
			got[0].Generic, "user_interaction")
	}
	if len(got[0].Destinations) != 2 {
		t.Errorf("Descriptor().Tools.Mappings[0].Destinations: got %d entries, want 2 "+
			"(one DestMain and one DestField)", len(got[0].Destinations))
	}
}

// TestDiscover_ToolMappingsHook_ReturningNilLeavesDescriptorMappingsUntouched verifies
// that when the hook returns nil, the descriptor's own declared mappings are preserved.
// A nil return signals "apply no override for this harness" — the config declares nothing
// for it, so the descriptor's own mapping set is the effective one.
func TestDiscover_ToolMappingsHook_ReturningNilLeavesDescriptorMappingsUntouched(t *testing.T) {
	root := makeRoot(t)
	id := uniqueID(t, "hooknilret")

	original := []domain.ToolMapping{
		{
			Generic: "bash",
			Destinations: []domain.ToolDestination{
				{Kind: domain.DestMain, Names: []string{"Bash"}},
			},
		},
	}
	mod := newMappingsModule(id, original)
	registry.Register(id, func(_ registry.BuiltinOptions) (domain.HarnessModule, error) { return mod, nil })

	// Hook returns nil → leave the descriptor's declared mappings unchanged.
	rec := &hookRecorder{result: nil}
	reg, err := registry.Discover(registry.Options{
		MosaicRoot:   root,
		ToolMappings: rec.hook(), // field does not exist → compile error (TDD RED)
	})
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}

	m, err := reg.Resolve(id)
	if err != nil {
		t.Fatalf("Resolve(%q): %v", id, err)
	}

	got := m.Descriptor().Tools.Mappings
	if len(got) != 1 || got[0].Generic != "bash" {
		t.Errorf("Descriptor().Tools.Mappings = %v; want [{Generic:bash ...}]; "+
			"a nil hook return must leave the descriptor's declared mappings intact",
			got)
	}
}

// TestDiscover_ToolMappingsHook_AppliedToDescriptorOnlyHarness verifies that the hook is
// called for descriptor-only (on-disk) harnesses as well as built-in ones. Every
// successfully constructed module must be passed through the hook regardless of provision tier.
func TestDiscover_ToolMappingsHook_AppliedToDescriptorOnlyHarness(t *testing.T) {
	root := makeRoot(t)
	id := uniqueID(t, "hookondisc")
	addDescriptorOnly(t, root, id, "Descriptor-only harness")

	called := false
	var calledWithID string
	reg, err := registry.Discover(registry.Options{
		MosaicRoot: root,
		// field does not exist → compile error (TDD RED)
		ToolMappings: func(harnessID string, declared []domain.ToolMapping) []domain.ToolMapping {
			called = true
			calledWithID = harnessID
			return nil // no override; keep descriptor mappings
		},
	})
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	_, _ = reg.Resolve(id)

	if !called {
		t.Fatal("ToolMappings hook was not called for a descriptor-only harness; " +
			"the hook must be applied to every successfully constructed module, " +
			"including descriptor-only and external harnesses")
	}
	if calledWithID != id {
		t.Errorf("hook was called with harness id %q, want %q", calledWithID, id)
	}
}
