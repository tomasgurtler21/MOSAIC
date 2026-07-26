package app

// summary.go assembles the RunSummary returned by both use cases (I18.5) from the executor's
// result and the accumulated todo collector, so both frontends render identical information.

import (
	"os"
	"path/filepath"

	"mosaic-deploy/internal/deploy"
	"mosaic-deploy/internal/domain"
)

// buildSummary derives the run outcome from the executor result and assembles the RunSummary.
func (s *service) buildSummary(mode domain.RunMode, harness domain.HarnessRef, workspace string, result deploy.ExecResult) domain.RunSummary {
	hasSkip := false
	for _, a := range result.Actions {
		if a.Taken == domain.TakenSkipped {
			hasSkip = true
			break
		}
	}

	outcome := domain.OutcomeSuccess
	switch {
	case result.Partial != nil:
		outcome = domain.OutcomeFailed
	case result.Fallback != domain.FallbackNone || hasSkip:
		outcome = domain.OutcomeCompletedWithGaps
	}

	var degraded []string
	for _, e := range s.deps.Logger.Degraded() {
		degraded = append(degraded, e.Error())
	}

	return domain.RunSummary{
		Mode:           mode,
		Harness:        harness,
		WorkspacePath:  workspace,
		DeploymentRoot: result.DeploymentRoot,
		Fallback:       result.Fallback,
		Actions:        result.Actions,
		Todos:          s.deps.Todo.Items(),
		TodoFilePath:   result.TodoFilePath,
		Logs:           s.deps.Logger.Paths(),
		LogDegraded:    degraded,
		Outcome:        outcome,
	}
}

// readDeployedFile returns the bytes of the currently-deployed file at
// <workspace>/<targetPath>, or nil when the file cannot be read (absent on first deploy).
func readDeployedFile(workspace, targetPath string) []byte {
	data, err := os.ReadFile(filepath.Join(workspace, targetPath))
	if err != nil {
		return nil
	}
	return data
}
