package runselect_test

// runselect_test.go drives the TDD RED phase for the runselect package: the
// single selection decision shared by the CLI, the CLI entry point, and the
// TUI entry point.
//
// These tests compile against the placeholder declarations in runselect.go
// and fail because those placeholders return zero values. They become GREEN
// when Resolve, Answer, and Announce are implemented per the contract in
// Development/Designs (ContractsDesign.md, "runselect.Resolve").

import (
	"time"

	"mosaic-run/internal/domain"
	"mosaic-run/internal/runscan"
	"mosaic-run/internal/runselect"
)

// ---------------------------------------------------------------------------
// Test fixtures
// ---------------------------------------------------------------------------

func candidate(runID, folder string) runscan.RunCandidate {
	return runscan.RunCandidate{
		RunInfo: runscan.RunInfo{
			RunID:       runID,
			FolderPath:  folder,
			LastUpdated: time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC),
			Workflow:    "test-workflow",
			Task:        "test task",
			Phase:       "EXECUTION",
			Stage:       "Stage-1",
			LastAgent:   "impl#1",
		},
	}
}

func unresumable(runID, folder string) runscan.UnresumableRun {
	return runscan.UnresumableRun{
		RunInfo: runscan.RunInfo{
			RunID:       runID,
			FolderPath:  folder,
			LastUpdated: time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC),
			Phase:       "COMPLETED",
		},
		Reason: runscan.ReasonCompleted,
	}
}

// countingMinter returns a Minter that records how many times it was called
// and returns distinct, deterministic identities on each call.
func countingMinter() (m runselect.Minter, calls *int) {
	n := 0
	calls = &n
	m = func() (string, string) {
		n++
		id := "20260801T000000Z-000" + string(rune('0'+n))
		return id, "/work/" + domain.RunScopedFolder(id)
	}
	return m, calls
}

const testWorkDir = "/work"
