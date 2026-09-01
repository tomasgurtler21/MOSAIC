package harness

import "path/filepath"

// SnapshotDirPath computes the run-scoped agent snapshot directory path.
//
// The snapshot directory is a sibling of the regular agents directory: it
// shares the same parent directory but carries the run ID in its name. For
// example:
//
//	agentsDir = ".opencode/agents"
//	runID     = "20260727T170000Z-a3f9"
//	result    = "{workDir}/.opencode/agents-runner-20260727T170000Z-a3f9"
//
// This naming pattern matches domain.RunScopedFolder.
func SnapshotDirPath(workDir, agentsDir, runID string) string {
	return filepath.Join(workDir, filepath.Dir(agentsDir), filepath.Base(agentsDir)+"-runner-"+runID)
}
