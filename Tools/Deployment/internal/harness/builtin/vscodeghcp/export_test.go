package vscodeghcp

// export_test.go exposes internal test helpers to the external vscodeghcp_test package.
// These helpers are compiled only during 'go test' and are never part of the production binary.
// Following the standard Go export_test.go pattern, exported symbols defined here are visible
// to package vscodeghcp_test but not to other packages.

import (
	"testing"

	"mosaic-deploy/internal/domain"
	"mosaic-deploy/internal/harness/registry"
)

// NewWithOptsForTesting calls New(opts) and returns the module. It is the test-facing
// entry point for the run-time construction path:
//
//	opts.MosaicRoot must point to a directory containing
//	<MosaicRoot>/Agents/VS code GHCP/HarnessInjections.md and
//	<MosaicRoot>/Agents/VS code GHCP/HarnessInjectionsOrchestrator.md.
func NewWithOptsForTesting(_ testing.TB, opts registry.BuiltinOptions) (domain.HarnessModule, error) {
	return New(opts)
}
