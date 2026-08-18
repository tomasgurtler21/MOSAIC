// Package runscan scans the working directory for Orchestration-{run_id}/ folders
// and classifies each as resumable or completed.
package runscan

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"mosaic-run/internal/artifact"
	"mosaic-run/internal/domain"
)

// RunScanner scans the working directory for Orchestration-{run_id}/ folders
// and classifies each as resumable or completed.
type RunScanner interface {
	// Scan enumerates immediate children of rootDir matching the
	// Orchestration-{run_id}/ naming pattern. For each match, it reads
	// Orchestration.md and checks current_state.phase to classify the run
	// as completed (phase == "COMPLETED", case-insensitive) or resumable.
	//
	// A folder whose Orchestration.md is missing, unreadable, or unparseable
	// is classified as resumable (surfaced to user, not hidden).
	//
	// Returns candidates sorted by last_updated descending (most recent first).
	// Completed runs are excluded from the returned candidate list.
	Scan(rootDir string) (ScanResult, error)
}

// ScanResult is the outcome of scanning the working directory for run folders.
type ScanResult struct {
	// Candidates are the resumable runs, ordered by LastUpdated descending.
	Candidates []RunCandidate

	// Unresumable are the runs found but not offered, each with its reason,
	// ordered by LastUpdated descending.
	Unresumable []UnresumableRun
}

// CompletedCount returns the number of unresumable runs whose reason is
// ReasonCompleted.
func (r ScanResult) CompletedCount() int {
	count := 0
	for _, u := range r.Unresumable {
		if u.Reason == ReasonCompleted {
			count++
		}
	}
	return count
}

// RunInfo is the identifying metadata carried for every run folder found by
// a scan, resumable or not.
type RunInfo struct {
	// RunID is the run identifier extracted from the folder name.
	RunID string

	// FolderPath is the absolute path to the Orchestration-{run_id}/ folder.
	FolderPath string

	// LastUpdated is the last_updated timestamp from the artifact's frontmatter.
	// Zero value if the artifact could not be parsed.
	LastUpdated time.Time

	// Workflow is the workflow identifier from the artifact's frontmatter.
	// Empty if the artifact could not be parsed.
	Workflow string

	// Task is the task description from the artifact's frontmatter.
	// Empty if the artifact could not be parsed.
	Task string

	// Phase is current_state.phase as recorded. Empty if the artifact could
	// not be parsed.
	Phase string

	// Stage is current_state.stage as recorded. Empty when none.
	Stage string

	// LastAgent is current_state.last_agent. Empty before any invocation.
	LastAgent string
}

// RunCandidate represents one resumable run folder found during scanning.
type RunCandidate struct {
	RunInfo

	// ParseError is non-nil when the Orchestration.md could not be read or parsed.
	// The candidate is still treated as resumable (surfaced, not hidden).
	ParseError error
}

// UnresumableRun is one run folder that was found and will not be offered
// for resumption, carried with the reason.
type UnresumableRun struct {
	RunInfo

	// Reason states why the run cannot be resumed. Always set.
	Reason UnresumableReason
}

// UnresumableReason is why a run folder is not offered for resumption.
type UnresumableReason string

// ReasonCompleted is the only reason a run is unresumable: its
// current_state.phase is COMPLETED.
const ReasonCompleted UnresumableReason = "completed"

// Description returns a human-readable phrase for display next to the run,
// suitable for both a TUI list row and a CLI line. Never empty, including
// for an unrecognised value.
func (r UnresumableReason) Description() string {
	switch r {
	case ReasonCompleted:
		return "completed"
	default:
		return "unresumable"
	}
}

// dirScanner is the production RunScanner that reads from the file system.
type dirScanner struct{}

// NewDirScanner creates a RunScanner that scans the file system for run folders.
// It enumerates immediate children of the given root directory, parses each
// matching Orchestration.md to determine completion status, and returns the
// set of resumable runs ordered by recency.
func NewDirScanner() RunScanner {
	return &dirScanner{}
}

// Scan enumerates immediate children of rootDir matching Orchestration-{run_id}/
// and classifies each run as resumable or completed.
func (s *dirScanner) Scan(rootDir string) (ScanResult, error) {
	entries, err := os.ReadDir(rootDir)
	if err != nil {
		return ScanResult{}, err
	}

	var result ScanResult

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		runID, ok := domain.ParseRunFolder(entry.Name())
		if !ok {
			continue
		}

		// Validate the run_id format: must match \d{8}T\d{6}Z-[0-9a-f]{4}.
		if !domain.IsValidRunID(runID) {
			continue
		}

		folderPath := filepath.Join(rootDir, entry.Name())
		artifactPath := filepath.Join(folderPath, "Orchestration.md")

		candidate := RunCandidate{
			RunInfo: RunInfo{
				RunID:      runID,
				FolderPath: folderPath,
			},
		}

		// Try to read the artifact file.
		data, readErr := os.ReadFile(artifactPath)
		if readErr != nil {
			candidate.ParseError = readErr
			result.Candidates = append(result.Candidates, candidate)
			continue
		}

		// Try to parse the artifact.
		state, parseErr := artifact.Parse(data)
		if parseErr != nil {
			candidate.ParseError = parseErr
			result.Candidates = append(result.Candidates, candidate)
			continue
		}

		// Check whether the run is completed (case-insensitive).
		if strings.EqualFold(state.CurrentState.Phase, "COMPLETED") {
			unresumable := UnresumableRun{
				RunInfo: RunInfo{
					RunID:       runID,
					FolderPath:  folderPath,
					LastUpdated: state.LastUpdated,
					Workflow:    string(state.Workflow),
					Task:        state.Task,
					Phase:       state.CurrentState.Phase,
					Stage:       state.CurrentState.Stage,
					LastAgent:   state.CurrentState.LastAgent,
				},
				Reason: ReasonCompleted,
			}
			result.Unresumable = append(result.Unresumable, unresumable)
			continue
		}

		// Resumable candidate: populate metadata from frontmatter.
		candidate.LastUpdated = state.LastUpdated
		candidate.Workflow = string(state.Workflow)
		candidate.Task = state.Task
		candidate.Phase = state.CurrentState.Phase
		candidate.Stage = state.CurrentState.Stage
		candidate.LastAgent = state.CurrentState.LastAgent
		result.Candidates = append(result.Candidates, candidate)
	}

	// Sort candidates by LastUpdated descending (most recent first).
	// Candidates with a zero LastUpdated (unparseable artifacts) sort last.
	sort.Slice(result.Candidates, func(i, j int) bool {
		return result.Candidates[i].LastUpdated.After(result.Candidates[j].LastUpdated)
	})

	// Sort unresumable entries by LastUpdated descending, independently of
	// Candidates ordering.
	sort.Slice(result.Unresumable, func(i, j int) bool {
		return result.Unresumable[i].LastUpdated.After(result.Unresumable[j].LastUpdated)
	})

	return result, nil
}
