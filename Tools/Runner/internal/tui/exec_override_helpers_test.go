package tui

// exec_override_helpers_test.go holds the shared fixtures used by the
// executable-override test files split out of the former
// exec_override_test.go: the synthetic launch-failure error builder and a
// rootModel constructor for routing tests.
//
// Tests are in package tui (internal) so they can access the unexported
// rootModel fields (screen, execOverrideScreen, launchFailureAttempt,
// selections) and the unexported message types (runDoneMsg, runErrorMsg).

import (
	"context"
	"fmt"

	tuicommon "mosaic-common/tui"
	"mosaic-run/internal/domain"
)

// launchFailureErr returns a *domain.HarnessLaunchError wrapping a synthetic
// cause, exactly as the harness adapters will produce on a failed Start().
func launchFailureErr(harnessID, exe string) *domain.HarnessLaunchError {
	return &domain.HarnessLaunchError{
		Harness:    harnessID,
		Executable: exe,
		Err:        fmt.Errorf("exec: no such file or directory"),
	}
}

// newOverrideTestModel creates a rootModel whose stub session returns a
// completed outcome, suitable for receiving synthetic messages in routing tests.
func newOverrideTestModel() *rootModel {
	sess := &stubNavSession{outcome: domain.RunOutcome{Status: domain.RunCompleted}}
	return newRootModel(context.Background(), sess, Options{
		Theme: tuicommon.DefaultTheme(),
	})
}
