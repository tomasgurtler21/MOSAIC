package cli

import (
	"fmt"
	"io"
	"os"

	commonharness "mosaic-common/harness"

	"mosaic-run/internal/harness"
)

// validateHarnessFlag validates --harness against Runner's one accepted set
// (the tool-local test double plus every catalog-declared CLI harness), so a
// catalog addition is accepted here without an edit.
func validateHarnessFlag(harnessFlag string, errOut io.Writer) (exitCode int, ok bool) {
	if !harness.Accepts(harnessFlag) {
		fmt.Fprintf(errOut, "error: invalid --harness value %q; valid values: %s\n", harnessFlag, harness.FlagValueList())
		return ExitUsage, false
	}
	return ExitSuccess, true
}

// discoverOrchestratorPath auto-discovers the orchestrator-script.md path from
// the harness convention. For CLI-backed harnesses, this verifies the
// workspace is deployed for the selected harness. For non-CLI harnesses (e.g.
// the "fake" test double), discovery is skipped and the returned path is left
// empty, since those harnesses have no agents directory convention.
func discoverOrchestratorPath(harnessFlag string, errOut io.Writer) (path string, exitCode int, ok bool) {
	if !commonharness.IsCLIHarness(harnessFlag) {
		return "", ExitSuccess, true
	}

	workDir, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(errOut, "error: getting working directory: %v\n", err)
		return "", ExitUsage, false
	}

	orchPath, err := harness.DiscoverOrchestrator(workDir, harnessFlag)
	if err != nil {
		fmt.Fprintf(errOut, "error: %v\n", err)
		return "", ExitRefused, false
	}
	return orchPath, ExitSuccess, true
}
