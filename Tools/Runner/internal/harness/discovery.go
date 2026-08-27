package harness

import (
	"fmt"
	"os"
	"path/filepath"

	commonharness "mosaic-common/harness"

	"mosaic-run/internal/domain"
)

// DiscoverOrchestrator computes the expected path to the script-mode
// orchestrator agent file from the harness convention and verifies the file
// exists.
//
// The path is: {workDir}/{agentsDir}/orchestrator-script.md
//
// Returns the absolute path on success.
// Returns *domain.RefusalError with Component "harness" if:
//   - harnessID is not a known CLI harness (unknown identity)
//   - the computed path does not exist on disk (workspace not deployed)
func DiscoverOrchestrator(workDir, harnessID string) (string, error) {
	entry, ok := commonharness.LookupCLIHarness(harnessID)
	if !ok {
		return "", &domain.RefusalError{
			Component: "harness",
			Resource:  harnessID,
			Reason:    fmt.Sprintf("unknown harness %s: cannot determine agents directory", harnessID),
		}
	}

	orchPath := filepath.Join(workDir, entry.AgentsDir, "orchestrator-script.md")
	if _, err := os.Stat(orchPath); err != nil {
		if os.IsNotExist(err) {
			return "", &domain.RefusalError{
				Component: "harness",
				Resource:  orchPath,
				Reason:    fmt.Sprintf("workspace not deployed for harness %s: expected orchestrator-script at %s", harnessID, orchPath),
			}
		}
		return "", &domain.RefusalError{
			Component: "harness",
			Resource:  orchPath,
			Reason:    fmt.Sprintf("cannot access expected orchestrator-script: %v", err),
		}
	}

	return orchPath, nil
}
