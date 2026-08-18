package tui

// RunIdentityMinter mints identity for a run started from inside the TUI.
// It returns a newly minted run_id and the matching run-scoped folder path,
// which is absolute and whose final element is Orchestration-{runID}.
// Minting cannot fail; implementations must always return a usable pair.
type RunIdentityMinter func() (runID string, runFolder string)
