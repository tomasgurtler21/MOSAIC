package app

import (
	"errors"
	"fmt"
	"strings"
)

// ErrPlanNotConfirmed is returned by DeployNew and Update when the user declines or cancels
// the plan review. It is a distinguishable sentinel that the TUI can test with errors.Is to
// restart the selection flow rather than showing the done/error screen. Every genuine failure
// (planner error, executor error, harness resolution error, etc.) is distinct from this value.
var ErrPlanNotConfirmed = errors.New("deployment plan was not confirmed")

// ErrRunReverted is the sentinel every reversed run matches under errors.Is. Frontends test
// against it to render "nothing was deployed, the workspace is unchanged, retry the run"
// rather than a deployment summary.
var ErrRunReverted = errors.New("deployment run failed and the workspace was restored")

// ErrModelSelectionCancelled is returned when the user cancels a tier-model or agent-model
// question — Esc in the TUI, which arrives as domain.Cancelled. It aborts the whole run:
// no config is persisted and no plan is executed. It is a distinguishable sentinel so a
// frontend can tell a deliberate user abort from a genuine failure and render accordingly.
// Both ErrModelSelectionCancelled and ErrSelectionCancelled are rendered by the TUI as
// the cancelled outcome ("Deployment cancelled."), not the failure outcome.
var ErrModelSelectionCancelled = errors.New("model selection was cancelled")

// ErrSelectionCancelled is returned when the user cancels one of the five multi-select
// selection questions — workflows, utility agents, infrastructure agents, standalone
// agents, hook bundles. Esc in the TUI arrives as domain.Cancelled and produces this.
// It aborts the whole run: no further question is asked, no config is persisted, and no
// plan is executed. It is a distinguishable sentinel so a frontend can tell a deliberate
// user abort from a genuine failure: the TUI renders it, and ErrModelSelectionCancelled,
// as the cancelled outcome rather than the failure outcome.
//
// Cancellation is the ONLY status that produces it. domain.SkippedOne, domain.SkippedAll,
// and a transport error from Interaction.SelectMany all keep their existing behaviour of
// yielding an empty selection and letting the run continue, so non-interactive CLI runs
// that supply no pre-answer for these questions are unaffected.
var ErrSelectionCancelled = errors.New("selection was cancelled")

// RevertedRunError is returned by a flow whose execution ran in atomic mode, failed, and was
// reversed. It carries the original cause so the user sees why the run failed, and the paths
// the reversal could not restore so the user can recover them from backups if any exist.
type RevertedRunError struct {
	// Cause is the executor's triggering failure (ExecResult.Partial). Never nil.
	Cause error
	// UnrestoredPaths lists absolute paths the reversal could not restore. Empty when the
	// reversal was clean, which is the normal case.
	UnrestoredPaths []string
}

// Error states that the workspace was restored and the run can simply be repeated, names the
// original cause, and lists any unrestored paths.
func (e *RevertedRunError) Error() string {
	msg := fmt.Sprintf("deployment run failed and the workspace was restored: %v", e.Cause)
	if len(e.UnrestoredPaths) > 0 {
		msg += "; unrestored paths: " + strings.Join(e.UnrestoredPaths, ", ")
	}
	return msg
}

// Unwrap returns Cause so errors.Is/errors.As reach the underlying failure.
func (e *RevertedRunError) Unwrap() error {
	return e.Cause
}

// Is matches ErrRunReverted and nothing else, so callers can match the category without a
// type assertion. The comparison is identity against the sentinel value only.
func (e *RevertedRunError) Is(target error) bool {
	return target == ErrRunReverted
}
