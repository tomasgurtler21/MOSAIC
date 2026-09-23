package snapshot

import (
	"fmt"
	"os"
	"path/filepath"

	"mosaic-run/internal/domain"
)

// WriteRecoveryMarker writes the human-readable recovery marker file
// (RUNNER-RECOVERY.txt) into agentsDir. Uses .txt extension to avoid
// being indexed as an agent.
//
// The marker contains the backup location and step-by-step manual restore
// instructions telling the user to COPY files from the backup directory
// (overwrite), then delete the backup directory, then delete this marker.
// The instructions do NOT say to move, swap, or otherwise restructure the
// agents directory.
//
// Returns *domain.RefusalError on write failure.
func WriteRecoveryMarker(agentsDir, backupDir string) error {
	content := fmt.Sprintf(`MOSAIC Runner Recovery Marker
===========================================

The MOSAIC Runner was interrupted while transforming agent files in-place.
The agents directory may contain partially or fully transformed files.

Backup location: %s

Manual recovery steps:
  1. COPY all .md files from the backup directory into the agents directory,
     overwriting existing files:
       From: %s
       To:   %s

  2. Delete the backup directory:
       %s

  3. Delete this marker file (%s)
`, backupDir, backupDir, agentsDir, backupDir, RecoveryMarkerFileName)

	path := filepath.Join(agentsDir, RecoveryMarkerFileName)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return &domain.RefusalError{
			Component: "snapshot",
			Resource:  path,
			Reason:    fmt.Sprintf("cannot write recovery marker: %v", err),
		}
	}
	return nil
}
