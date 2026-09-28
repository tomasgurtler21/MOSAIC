package testdeploy_test

// Tests for the deploy invoker package. The test suite covers:
//   - Argument construction: correct flags for mosaic-deploy deploy subcommand
//   - Workflows flag guard: --workflows always emitted to prevent silent empty deployment
//   - Multi-harness: one subprocess invocation per harness, each with the correct harness ID
//   - Exit-code-to-error mapping: success, failure, usage error
//   - ErrDeployFailed wrapping: errors.Is and errors.As work correctly
//   - Binary-not-found handling: ErrToolUnavailable with diagnosable path
//   - Timeout and cancellation: ErrTimedOut vs caller cancellation
//
// No real mosaic-deploy binary is present in these tests. Every interaction
// with the subprocess goes through the Invoke seam. The seam returns stderr as
// well as stdout so the "tool's message reaches the caller verbatim" property
// is assertable without any process involved.
//
// These tests are written for the TDD RED phase. They compile and run, but
// fail because the stub implementation has no real logic. When the real
// implementation is written, all tests must pass.

import (
	"context"

	"mosaic-run/internal/testdeploy"
)

// The deployment tool's stable exit codes, mirrored here as plain ints.
// This test file must not import mosaic-deploy to learn its own outcome
// contract — only the stable exit-code meanings are shared, per the
// no-import convention.
const (
	exitSuccess = 0
	exitFailure = 1
	exitUsage   = 3
)

// newDeployer constructs a Deployer with the given Options. Every test
// supplies its own Invoke seam; ExecutablePath is set to a stable sentinel so
// tests can assert it is passed through to the seam.
func newDeployer(opts testdeploy.Options) *testdeploy.Deployer {
	if opts.ExecutablePath == "" {
		opts.ExecutablePath = "mosaic-deploy"
	}
	return testdeploy.New(opts)
}

// minimalDeploy calls Deploy with the smallest valid argument set for a single
// harness deployment. Tests that care about specific field values call Deploy
// directly. infrastructureKeys is nil (absent flag semantics).
func minimalDeploy(d *testdeploy.Deployer) error {
	return d.Deploy(
		context.Background(),
		"/catalog",               // catalogFolder
		"/mosaic",                // mosaicRoot
		"/workspace",             // workspace
		[]string{"auto"},         // harnesses
		[]string{"smoke-single"}, // workflows
		nil,                      // infrastructureKeys: nil = absent flag
	)
}

// =============================================================================
// Helpers
// =============================================================================

// containsFlag reports whether args contains the flag followed by value as
// consecutive elements.
func containsFlag(args []string, flag, value string) bool {
	for i, arg := range args {
		if arg == flag && i+1 < len(args) && args[i+1] == value {
			return true
		}
	}
	return false
}

// flagIndex returns the index of flag in args, or -1 if not found.
func flagIndex(args []string, flag string) int {
	for i, arg := range args {
		if arg == flag {
			return i
		}
	}
	return -1
}
