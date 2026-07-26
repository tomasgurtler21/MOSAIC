package app

import "errors"

// ErrPlanNotConfirmed is returned by DeployNew and Update when the user declines or cancels
// the plan review. It is a distinguishable sentinel that the TUI can test with errors.Is to
// restart the selection flow rather than showing the done/error screen. Every genuine failure
// (planner error, executor error, harness resolution error, etc.) is distinct from this value.
var ErrPlanNotConfirmed = errors.New("deployment plan was not confirmed")
