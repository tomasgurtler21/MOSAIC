package claudecode

// export_test.go exposes internal test helpers to the external claudecode_test package.
// These helpers are compiled only during 'go test' and are never part of the production binary.
// Following the standard Go export_test.go pattern, exported symbols defined here are visible
// to package claudecode_test but not to other packages.

import (
	"testing"

	"mosaic-deploy/internal/domain"
	"mosaic-deploy/internal/harness/descriptor"
	"mosaic-deploy/internal/harness/registry"
)

// NewWithOptsForTesting calls New(opts) and returns the module. It is the test-facing
// entry point for the run-time construction path:
//
//	opts.MosaicRoot must point to a directory containing
//	<MosaicRoot>/Agents/Claude Code/HarnessInjections.md and
//	<MosaicRoot>/Agents/Claude Code/HarnessInjectionsOrchestrator.md.
func NewWithOptsForTesting(_ testing.TB, opts registry.BuiltinOptions) (domain.HarnessModule, error) {
	return New(opts)
}

// NewWithMapsForTesting creates a claudecode module configured with caller-supplied injection
// maps for testing the Injection method's merge behavior. shared maps canonical injection
// names to shared (subagent-level) content; orchContent maps names to orchestrator-only
// content.
//
// This constructor is used by tests that need to assert the exact merge output (including the
// "\n\n" separator) without depending on the on-disk HarnessInjections.md or
// HarnessInjectionsOrchestrator.md files, which may have empty content for the injection
// names being tested.
func NewWithMapsForTesting(t testing.TB, shared, orchContent map[string]string) domain.HarnessModule {
	t.Helper()
	desc, err := descriptor.Parse(embeddedDescriptor, "builtin:claude-code")
	if err != nil {
		t.Fatalf("NewWithMapsForTesting: parse embedded claude-code descriptor: %v", err)
	}
	ref := domain.HarnessRef{
		ID:          desc.ID,
		DisplayName: desc.DisplayName,
		Tier:        domain.TierBuiltin,
		Usable:      true,
	}
	return &module{ref: ref, desc: desc, injections: shared, orchInjections: orchContent}
}

// DescriptorForTesting parses the embedded claude-code.yaml descriptor and
// returns the resulting HarnessDescriptor. It is used by consistency tests
// that compare the embedded YAML's path values against the shared harness
// catalog without requiring an on-disk fixture directory.
func DescriptorForTesting(t testing.TB) *domain.HarnessDescriptor {
	t.Helper()
	desc, err := descriptor.Parse(embeddedDescriptor, "builtin:claude-code")
	if err != nil {
		t.Fatalf("DescriptorForTesting: parse embedded claude-code descriptor: %v", err)
	}
	return desc
}
