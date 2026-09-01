package opencode

// export_test.go exposes internal test helpers to the external opencode_test package.
// These helpers are compiled only during 'go test' and are never part of the production binary.
// Following the standard Go export_test.go pattern, exported symbols defined here are visible
// to package opencode_test but not to other packages.

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
//	<MosaicRoot>/Agents/OpenCode/HarnessInjections.md and
//	<MosaicRoot>/Agents/OpenCode/HarnessInjectionsOrchestrator.md.
func NewWithOptsForTesting(_ testing.TB, opts registry.BuiltinOptions) (domain.HarnessModule, error) {
	return New(opts)
}

// DescriptorForTesting parses the embedded opencode.yaml descriptor and
// returns the resulting HarnessDescriptor. It is used by consistency tests
// that compare the embedded YAML's path values against the shared harness
// catalog without requiring an on-disk fixture directory.
func DescriptorForTesting(t testing.TB) *domain.HarnessDescriptor {
	t.Helper()
	desc, err := descriptor.Parse(embeddedDescriptor, "builtin:opencode")
	if err != nil {
		t.Fatalf("DescriptorForTesting: parse embedded opencode descriptor: %v", err)
	}
	return desc
}
