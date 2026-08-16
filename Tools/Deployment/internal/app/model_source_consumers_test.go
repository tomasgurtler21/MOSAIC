package app_test

// model_source_consumers_test.go covers T9.2: both option-building consumers in resolve.go
// and transform_service_impl.go must present model options from the registry's effective
// catalog (the ModelCatalog hook-installed IDs), not from the descriptor's own declared list.
//
// Both consumers share the same expression:
//
//	consumer 1 (resolve.go):          buildModelOptions(module.Descriptor().Models.IDs)
//	consumer 2 (transform_service_impl.go): buildModelOptions(tgtModule.Descriptor().Models.IDs)
//
// The registry's ModelCatalog hook installs the effective catalog on Descriptor().Models before
// any consumer can read it. These tests verify that the effective catalog is in place on the
// module returned by Resolve(), because that is exactly the module both consumers receive.
//
// RED state: all tests reference registry.Options.ModelCatalog, which does not exist until
// I9.1 is implemented. Every test therefore fails to compile until then.
//
// Drift-back guard: after I9.2 retires the YAML models: blocks from the three CLI harnesses,
// any consumer that were to bypass the hook and read directly from the descriptor would
// receive an empty model list. These tests would catch that regression because the stub
// harnesses used here have EMPTY descriptor-declared Models, relying entirely on the hook
// to provide the effective catalog.

import (
	"testing"

	"mosaic-deploy/internal/domain"
	"mosaic-deploy/internal/harness/registry"
)

// ---------------------------------------------------------------------------
// Consumer test infrastructure
// ---------------------------------------------------------------------------

// consumerTestCatalog maps harness IDs to the model catalog the hook should install.
// The IDs are invented (prefixed "consumer-test-") and appear in no real YAML descriptor,
// so their presence in Descriptor().Models.IDs proves the hook was applied, not a
// coincidental match to the descriptor's own content.
var consumerTestCatalog = map[string]domain.ModelCatalog{
	"consumer-harness-a": {
		IDs:        []string{"consumer-test-model-1", "consumer-test-model-2"},
		FormatHint: "consumer-test-<hint>",
	},
	"consumer-harness-b": {
		IDs:        []string{"consumer-test-model-3"},
		FormatHint: "consumer-test-<target-hint>",
	},
}

// consumerTestHook returns a ModelCatalog hook backed by consumerTestCatalog. It is the
// same pattern the composition root uses: a hit returns the shared catalog; a miss returns
// the zero value so the descriptor-sourced catalog is preserved.
func consumerTestHook() func(string, domain.ModelCatalog) domain.ModelCatalog {
	return func(harnessID string, declared domain.ModelCatalog) domain.ModelCatalog {
		if cat, ok := consumerTestCatalog[harnessID]; ok {
			return cat
		}
		return domain.ModelCatalog{}
	}
}

// registerConsumerTestHarness registers a minimal stub builtin harness with the package-level
// registry. The harness has an EMPTY descriptor-declared Models (no ids, no format_hint),
// simulating the post-I9.2 state where the YAML models: block has been retired. The
// ModelCatalog hook is the only source of model data.
func registerConsumerTestHarness(t *testing.T, id string) {
	t.Helper()
	registry.Register(id, func(_ registry.BuiltinOptions) (domain.HarnessModule, error) {
		return &consumerStubModule{
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
				// Deliberately empty Models — the hook is the only source of model data.
				// This simulates the post-I9.2 state where YAML models: blocks are retired.
				Models: domain.ModelCatalog{},
				Frontmatter: domain.FrontmatterSpec{
					ToolsKey: "tools",
				},
			},
		}, nil
	})
}

// consumerStubModule is a minimal HarnessModule implementation used in consumer tests.
type consumerStubModule struct {
	ref  domain.HarnessRef
	desc *domain.HarnessDescriptor
}

func (m *consumerStubModule) Ref() domain.HarnessRef                    { return m.ref }
func (m *consumerStubModule) Descriptor() *domain.HarnessDescriptor      { return m.desc }
func (m *consumerStubModule) Close() error                               { return nil }
func (m *consumerStubModule) Tools(_ domain.ToolRequest) (domain.ToolResult, error) {
	return domain.ToolResult{}, nil
}
func (m *consumerStubModule) Frontmatter(_ domain.FrontmatterRequest) (domain.FrontmatterPlan, error) {
	return domain.FrontmatterPlan{}, nil
}
func (m *consumerStubModule) TargetPath(_ domain.TargetPathRequest) (string, error) {
	return "", domain.ErrArtifactUnsupported
}
func (m *consumerStubModule) Injection(_ domain.InjectionRequest) (string, bool) { return "", false }
func (m *consumerStubModule) HookPlan(_ domain.HookPlanRequest) (domain.HookPlan, error) {
	return domain.HookPlan{Supported: false, Reason: "stub"}, nil
}

// discoverConsumerTestRegistry builds a registry from consumerTestHook, using a fresh
// temp MosaicRoot. The caller must register the needed harnesses via
// registerConsumerTestHarness before calling this.
func discoverConsumerTestRegistry(t *testing.T, harnessIDs ...string) (registry.Registry, func()) {
	t.Helper()
	for _, id := range harnessIDs {
		registerConsumerTestHarness(t, id)
	}
	tempRoot := t.TempDir()
	reg, err := registry.Discover(registry.Options{
		MosaicRoot:   tempRoot,
		ModelCatalog: consumerTestHook(), // field does not exist → compile error (TDD RED)
	})
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	return reg, func() {} // cleanup is automatic via t.TempDir()
}

// ---------------------------------------------------------------------------
// T9.2 — Consumer 1: harness model selection (resolve.go)
// ---------------------------------------------------------------------------

// TestModelSourceConsumer_HarnessModelSelection_UsesHookInstalledIDs verifies that the
// module returned by the registry after the ModelCatalog hook is applied carries the
// hook's model IDs in Descriptor().Models.IDs. This field is the exact input to consumer 1:
//
//	harnessOptions := buildModelOptions(module.Descriptor().Models.IDs)   // resolve.go:361
//
// The test uses a stub harness with EMPTY descriptor-declared Models, so any IDs seen in
// the resolved module must come from the hook. If the hook is removed (or the field is not
// implemented), Descriptor().Models.IDs will be empty and the consumer will offer no options.
func TestModelSourceConsumer_HarnessModelSelection_UsesHookInstalledIDs(t *testing.T) {
	const harnessID = "consumer-harness-a"
	reg, _ := discoverConsumerTestRegistry(t, harnessID)

	m, err := reg.Resolve(harnessID)
	if err != nil {
		t.Fatalf("Resolve(%q): %v", harnessID, err)
	}

	// Both consumers read Descriptor().Models.IDs. Verify the hook-installed IDs are present.
	want := consumerTestCatalog[harnessID].IDs
	got := m.Descriptor().Models.IDs

	if len(got) == 0 {
		t.Fatalf("consumer 1 (harness model selection, resolve.go): "+
			"Descriptor().Models.IDs is empty for %q after ModelCatalog hook; "+
			"buildModelOptions(module.Descriptor().Models.IDs) would produce no options; "+
			"the hook must install the effective catalog before the module is returned by Resolve",
			harnessID)
	}

	// Verify the IDs come from the hook, not from the empty descriptor-declared Models.
	if len(got) != len(want) {
		t.Fatalf("consumer 1: Descriptor().Models.IDs has %d entries, want %d from hook: %v",
			len(got), len(want), want)
	}
	for i, wantID := range want {
		if got[i] != wantID {
			t.Errorf("consumer 1: Descriptor().Models.IDs[%d] = %q, want %q (from hook)",
				i, got[i], wantID)
		}
	}
}

// TestModelSourceConsumer_HarnessModelSelection_FormatHintFromHook verifies that the
// FormatHint on the hook-installed catalog is also visible to consumer 1.
func TestModelSourceConsumer_HarnessModelSelection_FormatHintFromHook(t *testing.T) {
	const harnessID = "consumer-harness-a"
	reg, _ := discoverConsumerTestRegistry(t, harnessID)

	m, err := reg.Resolve(harnessID)
	if err != nil {
		t.Fatalf("Resolve(%q): %v", harnessID, err)
	}

	want := consumerTestCatalog[harnessID].FormatHint
	got := m.Descriptor().Models.FormatHint

	if got != want {
		t.Errorf("consumer 1: Descriptor().Models.FormatHint = %q; want %q (from hook); "+
			"the format hint displayed alongside the model selection must also come from the hook",
			got, want)
	}
}

// ---------------------------------------------------------------------------
// T9.2 — Consumer 2: target model options (transform_service_impl.go)
// ---------------------------------------------------------------------------

// TestModelSourceConsumer_TargetModelOptions_UsesHookInstalledIDs verifies that the
// module returned by the registry after the ModelCatalog hook is applied carries the
// hook's model IDs in Descriptor().Models.IDs. This field is the exact input to consumer 2:
//
//	Options: buildModelOptions(tgtModule.Descriptor().Models.IDs)   // transform_service_impl.go:179
//
// The test uses a second stub harness (consumer-harness-b) to represent a "target harness",
// verifying that the target-model selection consumer also reads from the hook-installed catalog
// and not from a stale or empty descriptor-local list.
func TestModelSourceConsumer_TargetModelOptions_UsesHookInstalledIDs(t *testing.T) {
	const targetHarnessID = "consumer-harness-b"
	reg, _ := discoverConsumerTestRegistry(t, targetHarnessID)

	tgtModule, err := reg.Resolve(targetHarnessID)
	if err != nil {
		t.Fatalf("Resolve(%q): %v", targetHarnessID, err)
	}

	want := consumerTestCatalog[targetHarnessID].IDs
	got := tgtModule.Descriptor().Models.IDs

	if len(got) == 0 {
		t.Fatalf("consumer 2 (target model options, transform_service_impl.go): "+
			"Descriptor().Models.IDs is empty for %q after ModelCatalog hook; "+
			"buildModelOptions(tgtModule.Descriptor().Models.IDs) would produce no options; "+
			"the hook must install the effective catalog before the module is returned by Resolve",
			targetHarnessID)
	}

	if len(got) != len(want) {
		t.Fatalf("consumer 2: Descriptor().Models.IDs has %d entries, want %d from hook: %v",
			len(got), len(want), want)
	}
	for i, wantID := range want {
		if got[i] != wantID {
			t.Errorf("consumer 2: Descriptor().Models.IDs[%d] = %q, want %q (from hook)",
				i, got[i], wantID)
		}
	}
}

// TestModelSourceConsumer_BothConsumers_EmptyDescriptorModelsWithoutHook verifies the
// drift-back scenario: a harness with empty descriptor-declared Models produces an empty
// Descriptor().Models.IDs when no ModelCatalog hook is installed. This is the exact failure
// mode that would occur after I9.2 retires the YAML models: blocks without I9.1's hook in
// place. Both consumers would receive empty lists and offer no model options.
func TestModelSourceConsumer_BothConsumers_EmptyDescriptorModelsWithoutHook(t *testing.T) {
	const harnessID = "consumer-harness-a"
	registerConsumerTestHarness(t, harnessID)

	tempRoot := t.TempDir()
	// No ModelCatalog hook — the drift-back state.
	reg, err := registry.Discover(registry.Options{
		MosaicRoot:   tempRoot,
		ModelCatalog: nil, // field does not exist → compile error (TDD RED)
	})
	if err != nil {
		t.Fatalf("Discover without ModelCatalog hook: %v", err)
	}

	m, err := reg.Resolve(harnessID)
	if err != nil {
		t.Fatalf("Resolve(%q): %v", harnessID, err)
	}

	// Without the hook, the stub harness's empty descriptor-declared Models are returned.
	// Both consumers would produce empty option lists.
	got := m.Descriptor().Models.IDs
	if len(got) != 0 {
		t.Errorf("without ModelCatalog hook: Descriptor().Models.IDs = %v; want empty slice; "+
			"this baseline confirms the hook is the only source of model data in the post-I9.2 state",
			got)
	}
}
