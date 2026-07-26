package deploy

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"mosaic-deploy/internal/domain"
	"mosaic-deploy/internal/logging"
)

// deployHooks writes hook bundle files and applies registration steps.
// Hook files are copied to <deployRoot>/<HookPlan.TargetDir>/<HookFile.TargetName>.
// Registration steps follow the write-if-absent / TODO-if-present policy (AC17.5):
//   - !Performable: always emit GapManualStep; never write anything.
//   - Performable, target absent: write the fragment; emit no gap.
//   - Performable, target exists: never modify; emit GapHookRegistration.
//
// Registration targets are relative to the workspace, not the deployment root,
// because they are user-owned config files (e.g. .claude/settings.json).
func (e *executor) deployHooks(deployRoot, workspace string, hooks []domain.HookPlan, dryRun bool) []domain.Gap {
	var gaps []domain.Gap

	for _, hp := range hooks {
		if !hp.Supported {
			continue
		}

		if !dryRun {
			// Copy hook files to the target directory under the deployment root.
			targetDir := filepath.Join(deployRoot, hp.TargetDir)
			for _, hf := range hp.Files {
				content, err := os.ReadFile(hf.SourcePath)
				if err != nil {
					// Source file unreadable: this indicates a catalog integrity problem.
					// Log the error and emit a manual-step gap so the user is informed and
					// the hook file does not silently disappear from the deployment (AC17.6).
					e.log.Event(logging.Event{
						Time:    time.Now(),
						Level:   logging.LevelError,
						Kind:    "hook",
						Subject: hf.SourcePath,
						Message: fmt.Sprintf("hook source file unreadable, skipping: %s", err),
					})
					gaps = append(gaps, domain.Gap{
						Kind:    domain.GapManualStep,
						Subject: hf.TargetName,
						Detail:  fmt.Sprintf("hook source file %q could not be read and was not deployed: %s", hf.SourcePath, err),
					})
					continue
				}
				dest := filepath.Join(targetDir, hf.TargetName)
				_ = mkdirAndWrite(dest, content)
			}

			// Apply registration steps.
			for _, step := range hp.Registration {
				if g := applyRegistrationStep(step, workspace); g != nil {
					gaps = append(gaps, *g)
				}
			}
		}
	}

	return gaps
}

// applyRegistrationStep enforces the hook registration policy for one step.
// It returns a Gap when the step produces a TODO item, or nil when the fragment
// was written successfully (no user action required).
func applyRegistrationStep(step domain.RegistrationStep, workspace string) *domain.Gap {
	if !step.Performable {
		// Tool cannot perform this step at all (e.g. a user-level editor setting).
		// Always emit a manual-step gap.
		return &domain.Gap{
			Kind:     domain.GapManualStep,
			Subject:  step.ID,
			Detail:   step.Instruction,
			Fragment: step.Fragment,
		}
	}

	// Performable: check whether the target file already exists.
	targetPath := filepath.Join(workspace, step.TargetPath)
	if _, err := os.Stat(targetPath); err == nil {
		// Target exists: never modify; emit a hook-registration gap so the user
		// can paste the fragment manually (AC17.5).
		return &domain.Gap{
			Kind:     domain.GapHookRegistration,
			Subject:  step.ID,
			Detail:   step.Instruction,
			Fragment: step.Fragment,
		}
	}

	// Target absent: write the fragment (AC17.5).
	_ = mkdirAndWrite(targetPath, []byte(step.Fragment))
	return nil
}
