package registry_test

// registry_modelcatalog_test.go tests the ModelCatalog hook in registry.Options (T9.1).
//
// The ModelCatalog hook, when non-nil, is called once per successfully constructed module
// during Discover. It receives the harness id and the descriptor's own declared model catalog,
// and returns the effective catalog for that module.
//
// Returning a catalog with an empty IDs list means "no shared entry for this harness; keep
// the descriptor's own declared catalog unchanged". This preserves a non-CLI-backed harness
// (like vscodeghcp), a descriptor-tier harness, and an external harness on their own model
// data rather than emptying them.
//
// All tests in this file reference registry.Options.ModelCatalog, which does not exist until
// I9.1 is implemented. Every test therefore fails to compile until then — that is the intended
// TDD RED state.

import (
	"testing"

	"mosaic-deploy/internal/domain"
	"mosaic-deploy/internal/harness/registry"
)

// ---------------------------------------------------------------------------
// newModelCatalogModule — a HarnessModule whose descriptor carries an explicit
// model catalog, so tests can assert whether that catalog is preserved or replaced.
// ---------------------------------------------------------------------------

// newModelCatalogModule creates a built-in stub module with the given harness id and initial
// model catalog. The module's descriptor pointer is returned directly by Descriptor(), so
// callers may observe downstream effects of the registry hook on descriptor.Models.
func newModelCatalogModule(id string, models domain.ModelCatalog) domain.HarnessModule {
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
			Models:        models,
			Frontmatter: domain.FrontmatterSpec{
				ToolsKey: "tools",
			},
		},
	}
}

// modelCatalogHookRecorder captures invocations of the ModelCatalog hook so tests can
// assert which harness ids were received and what declared catalogs were passed.
type modelCatalogHookCall struct {
	harnessID string
	declared  domain.ModelCatalog
}

type modelCatalogHookRecorder struct {
	calls  []modelCatalogHookCall
	result domain.ModelCatalog // value the hook returns; empty IDs means "use descriptor's own"
}

func (r *modelCatalogHookRecorder) hook() func(string, domain.ModelCatalog) domain.ModelCatalog {
	return func(harnessID string, declared domain.ModelCatalog) domain.ModelCatalog {
		r.calls = append(r.calls, modelCatalogHookCall{harnessID: harnessID, declared: declared})
		return r.result
	}
}

// ---------------------------------------------------------------------------
// T9.1 — ModelCatalog hook behaviour during Discover
// ---------------------------------------------------------------------------

// TestDiscover_ModelCatalogHookNil_DescriptorModelsUntouched verifies that when the
// ModelCatalog hook is nil (the no-config case), the descriptor's own model catalog is not
// modified. This is the regression guard that proves harnesses with no configured shared
// catalog produce byte-identical output to before this change.
func TestDiscover_ModelCatalogHookNil_DescriptorModelsUntouched(t *testing.T) {
	root := makeRoot(t)
	id := uniqueID(t, "mcnil")

	originalModels := domain.ModelCatalog{
		IDs:        []string{"original-model-a", "original-model-b"},
		FormatHint: "original-<hint>",
	}
	mod := newModelCatalogModule(id, originalModels)
	registry.Register(id, func(_ registry.BuiltinOptions) (domain.HarnessModule, error) { return mod, nil })

	// ModelCatalog is nil — the no-config case. The field does not exist yet, so this
	// struct literal fails to compile until I9.1 adds it.
	reg, err := registry.Discover(registry.Options{
		MosaicRoot:   root,
		ModelCatalog: nil, // field does not exist → compile error (TDD RED)
	})
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}

	m, err := reg.Resolve(id)
	if err != nil {
		t.Fatalf("Resolve(%q): %v", id, err)
	}

	got := m.Descriptor().Models
	if len(got.IDs) != 2 {
		t.Fatalf("Descriptor().Models.IDs: got %d entries, want 2; "+
			"a nil ModelCatalog hook must leave the descriptor's own catalog untouched",
			len(got.IDs))
	}
	if got.IDs[0] != "original-model-a" || got.IDs[1] != "original-model-b" {
		t.Errorf("Descriptor().Models.IDs = %v; want [original-model-a original-model-b]; "+
			"nil hook must preserve the descriptor's declared model catalog",
			got.IDs)
	}
	if got.FormatHint != "original-<hint>" {
		t.Errorf("Descriptor().Models.FormatHint = %q; want %q; "+
			"nil hook must preserve the descriptor's declared format hint",
			got.FormatHint, "original-<hint>")
	}
}

// TestDiscover_ModelCatalogHook_CalledWithHarnessID verifies that the hook receives the
// harness id as its first argument, so the hook can look up per-harness catalog data
// without a separate join step.
func TestDiscover_ModelCatalogHook_CalledWithHarnessID(t *testing.T) {
	root := makeRoot(t)
	id := uniqueID(t, "mcid")

	mod := newModelCatalogModule(id, domain.ModelCatalog{})
	registry.Register(id, func(_ registry.BuiltinOptions) (domain.HarnessModule, error) { return mod, nil })

	rec := &modelCatalogHookRecorder{result: domain.ModelCatalog{}}
	_, err := registry.Discover(registry.Options{
		MosaicRoot:   root,
		ModelCatalog: rec.hook(), // field does not exist → compile error (TDD RED)
	})
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}

	if len(rec.calls) == 0 {
		t.Fatal("ModelCatalog hook was never called; " +
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
		t.Errorf("ModelCatalog hook was not called with harness id %q; got calls with ids: %v",
			id, modelCatalogHookCallIDs(rec.calls))
	}
}

// TestDiscover_ModelCatalogHook_CalledWithDeclaredModels verifies that the hook receives
// the descriptor's own declared model catalog as its second argument. This is the input the
// hook needs to decide whether to override or fall back to descriptor-sourced data.
func TestDiscover_ModelCatalogHook_CalledWithDeclaredModels(t *testing.T) {
	root := makeRoot(t)
	id := uniqueID(t, "mcdecl")

	declaredModels := domain.ModelCatalog{
		IDs:        []string{"descriptor-model-x"},
		FormatHint: "descriptor-hint",
	}
	mod := newModelCatalogModule(id, declaredModels)
	registry.Register(id, func(_ registry.BuiltinOptions) (domain.HarnessModule, error) { return mod, nil })

	rec := &modelCatalogHookRecorder{result: domain.ModelCatalog{}}
	_, err := registry.Discover(registry.Options{
		MosaicRoot:   root,
		ModelCatalog: rec.hook(), // field does not exist → compile error (TDD RED)
	})
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}

	var call *modelCatalogHookCall
	for i := range rec.calls {
		if rec.calls[i].harnessID == id {
			call = &rec.calls[i]
			break
		}
	}
	if call == nil {
		t.Fatalf("ModelCatalog hook was not called with harness id %q; calls: %v",
			id, modelCatalogHookCallIDs(rec.calls))
	}
	if len(call.declared.IDs) == 0 {
		t.Fatal("hook received empty declared IDs; " +
			"the descriptor's own declared model catalog must be passed as the second argument")
	}
	if call.declared.IDs[0] != "descriptor-model-x" {
		t.Errorf("hook received declared.IDs[0] = %q, want %q",
			call.declared.IDs[0], "descriptor-model-x")
	}
	if call.declared.FormatHint != "descriptor-hint" {
		t.Errorf("hook received declared.FormatHint = %q, want %q",
			call.declared.FormatHint, "descriptor-hint")
	}
}

// TestDiscover_ModelCatalogHook_NonEmptyIDsInstalledOnDescriptor verifies that when the hook
// returns a catalog with a non-empty IDs list, that catalog is installed on the module's
// Descriptor().Models. After Discover, calling Resolve(id).Descriptor().Models must return the
// hook's result, not the descriptor's original declared catalog.
func TestDiscover_ModelCatalogHook_NonEmptyIDsInstalledOnDescriptor(t *testing.T) {
	root := makeRoot(t)
	id := uniqueID(t, "mcinstall")

	// Module starts with descriptor-sourced model IDs.
	descriptorModels := domain.ModelCatalog{
		IDs:        []string{"old-descriptor-model"},
		FormatHint: "old-hint",
	}
	mod := newModelCatalogModule(id, descriptorModels)
	registry.Register(id, func(_ registry.BuiltinOptions) (domain.HarnessModule, error) { return mod, nil })

	// The hook returns a catalog with fresh shared-catalog IDs.
	sharedCatalog := domain.ModelCatalog{
		IDs:        []string{"shared-model-1", "shared-model-2", "shared-model-3"},
		FormatHint: "shared-<format>",
	}
	rec := &modelCatalogHookRecorder{result: sharedCatalog}
	reg, err := registry.Discover(registry.Options{
		MosaicRoot:   root,
		ModelCatalog: rec.hook(), // field does not exist → compile error (TDD RED)
	})
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}

	m, err := reg.Resolve(id)
	if err != nil {
		t.Fatalf("Resolve(%q): %v", id, err)
	}

	got := m.Descriptor().Models
	if len(got.IDs) != 3 {
		t.Fatalf("Descriptor().Models.IDs: got %d entries, want 3 (the hook's return value); "+
			"hook result must replace the descriptor's own model catalog",
			len(got.IDs))
	}
	for i, want := range sharedCatalog.IDs {
		if got.IDs[i] != want {
			t.Errorf("Descriptor().Models.IDs[%d] = %q, want %q", i, got.IDs[i], want)
		}
	}
	if got.FormatHint != "shared-<format>" {
		t.Errorf("Descriptor().Models.FormatHint = %q; want %q; "+
			"hook result's FormatHint must also be installed",
			got.FormatHint, "shared-<format>")
	}
}

// TestDiscover_ModelCatalogHook_EmptyIDsPreservesDescriptorModels verifies that when the
// hook returns a catalog with an empty IDs list, the descriptor's own declared model catalog
// is preserved. An empty IDs list signals "no shared entry for this harness; use descriptor's
// own declared catalog unchanged" — the mechanism that keeps vscodeghcp and descriptor-tier
// harnesses on their own model data rather than emptying them.
func TestDiscover_ModelCatalogHook_EmptyIDsPreservesDescriptorModels(t *testing.T) {
	root := makeRoot(t)
	id := uniqueID(t, "mcempty")

	originalModels := domain.ModelCatalog{
		IDs:        []string{"harness-own-model-1", "harness-own-model-2"},
		FormatHint: "harness-own-hint",
	}
	mod := newModelCatalogModule(id, originalModels)
	registry.Register(id, func(_ registry.BuiltinOptions) (domain.HarnessModule, error) { return mod, nil })

	// Hook returns a catalog with empty IDs — signals "use descriptor's own catalog".
	rec := &modelCatalogHookRecorder{result: domain.ModelCatalog{IDs: nil, FormatHint: ""}}
	reg, err := registry.Discover(registry.Options{
		MosaicRoot:   root,
		ModelCatalog: rec.hook(), // field does not exist → compile error (TDD RED)
	})
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}

	m, err := reg.Resolve(id)
	if err != nil {
		t.Fatalf("Resolve(%q): %v", id, err)
	}

	got := m.Descriptor().Models
	if len(got.IDs) != 2 {
		t.Fatalf("Descriptor().Models.IDs: got %d entries, want 2 (the descriptor's own catalog); "+
			"an empty-IDs hook return must preserve the descriptor's declared catalog",
			len(got.IDs))
	}
	if got.IDs[0] != "harness-own-model-1" || got.IDs[1] != "harness-own-model-2" {
		t.Errorf("Descriptor().Models.IDs = %v; want [harness-own-model-1 harness-own-model-2]; "+
			"empty-IDs hook return must leave the descriptor's declared catalog intact",
			got.IDs)
	}
	if got.FormatHint != "harness-own-hint" {
		t.Errorf("Descriptor().Models.FormatHint = %q; want %q; "+
			"empty-IDs hook return must preserve the descriptor's declared format hint",
			got.FormatHint, "harness-own-hint")
	}
}

// TestDiscover_ModelCatalogHook_OriginalDescriptorNotPermanentlyMutated verifies that the
// hook's overlay does not permanently mutate the inner module's descriptor. The same module
// instance can be discovered multiple times (e.g., in tests) and each invocation must see
// the original descriptor-sourced catalog, not a prior hook's overlay.
func TestDiscover_ModelCatalogHook_OriginalDescriptorNotPermanentlyMutated(t *testing.T) {
	root := makeRoot(t)
	id := uniqueID(t, "mcmutate")

	originalModels := domain.ModelCatalog{
		IDs:        []string{"original-model"},
		FormatHint: "original-format",
	}
	mod := newModelCatalogModule(id, originalModels)
	registry.Register(id, func(_ registry.BuiltinOptions) (domain.HarnessModule, error) { return mod, nil })

	// First Discover: hook installs a different catalog.
	overrideCatalog := domain.ModelCatalog{
		IDs:        []string{"override-model-x", "override-model-y"},
		FormatHint: "override-format",
	}
	rec1 := &modelCatalogHookRecorder{result: overrideCatalog}
	reg1, err := registry.Discover(registry.Options{
		MosaicRoot:   root,
		ModelCatalog: rec1.hook(), // field does not exist → compile error (TDD RED)
	})
	if err != nil {
		t.Fatalf("first Discover: %v", err)
	}
	m1, err := reg1.Resolve(id)
	if err != nil {
		t.Fatalf("first Resolve(%q): %v", id, err)
	}
	if len(m1.Descriptor().Models.IDs) != 2 {
		t.Fatalf("first Discover: want 2 IDs from hook, got %d", len(m1.Descriptor().Models.IDs))
	}

	// Second Discover: nil hook. Must see original descriptor-sourced catalog, not the prior override.
	reg2, err := registry.Discover(registry.Options{
		MosaicRoot:   root,
		ModelCatalog: nil, // field does not exist → compile error (TDD RED)
	})
	if err != nil {
		t.Fatalf("second Discover: %v", err)
	}
	m2, err := reg2.Resolve(id)
	if err != nil {
		t.Fatalf("second Resolve(%q): %v", id, err)
	}

	got := m2.Descriptor().Models
	if len(got.IDs) != 1 || got.IDs[0] != "original-model" {
		t.Errorf("second Discover (nil hook): Descriptor().Models.IDs = %v; want [original-model]; "+
			"a prior hook must not permanently mutate the inner descriptor",
			got.IDs)
	}
}

// TestDiscover_ModelCatalogHook_AppliedToDescriptorOnlyHarness verifies that the hook is
// called for descriptor-only (on-disk) harnesses as well as built-in ones. Every
// successfully constructed module must be passed through the hook regardless of provision tier.
func TestDiscover_ModelCatalogHook_AppliedToDescriptorOnlyHarness(t *testing.T) {
	root := makeRoot(t)
	id := uniqueID(t, "mcondisc")
	addDescriptorOnly(t, root, id, "Descriptor-only harness for model catalog test")

	called := false
	var calledWithID string
	_, err := registry.Discover(registry.Options{
		MosaicRoot: root,
		// field does not exist → compile error (TDD RED)
		ModelCatalog: func(harnessID string, declared domain.ModelCatalog) domain.ModelCatalog {
			called = true
			calledWithID = harnessID
			return domain.ModelCatalog{} // no override; keep descriptor catalog
		},
	})
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}

	if !called {
		t.Fatal("ModelCatalog hook was not called for a descriptor-only harness; " +
			"the hook must be applied to every successfully constructed module, " +
			"including descriptor-only harnesses")
	}
	if calledWithID != id {
		t.Errorf("hook was called with harness id %q, want %q", calledWithID, id)
	}
}

// modelCatalogHookCallIDs extracts the harness ids from recorded hook calls for diagnostic
// messages.
func modelCatalogHookCallIDs(calls []modelCatalogHookCall) []string {
	ids := make([]string, len(calls))
	for i, c := range calls {
		ids[i] = c.harnessID
	}
	return ids
}
