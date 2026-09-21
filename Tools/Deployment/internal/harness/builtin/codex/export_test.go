package codex

// export_test.go exposes internal test helpers to the external codex_test package.
// These helpers are compiled only during 'go test' and are never part of the production binary.
// Following the standard Go export_test.go pattern, exported symbols defined here are visible
// to package codex_test but not to other packages.

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
//	<MosaicRoot>/Catalog/HarnessInjections/Codex/HarnessInjections.md and
//	<MosaicRoot>/Catalog/HarnessInjections/Codex/HarnessInjectionsOrchestrator.md.
func NewWithOptsForTesting(_ testing.TB, opts registry.BuiltinOptions) (domain.HarnessModule, error) {
	return New(opts)
}

// DescriptorForTesting parses the embedded codex.yaml descriptor and returns the
// resulting HarnessDescriptor. It is used by tests that need access to the raw
// descriptor data without constructing a full module (for example, to inspect
// the drop list, key order, and model IDs without loading injection content).
func DescriptorForTesting(t testing.TB) *domain.HarnessDescriptor {
	t.Helper()
	desc, err := descriptor.Parse(embeddedDescriptor, "builtin:codex")
	if err != nil {
		t.Fatalf("DescriptorForTesting: parse embedded codex descriptor: %v", err)
	}
	return desc
}
