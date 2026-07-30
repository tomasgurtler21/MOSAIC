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
	// Candidates are the resumable (non-completed) runs, ordered by LastUpdated descending.
	Candidates []RunCandidate

	// CompletedCount is the number of completed runs found and excluded from Candidates.
	CompletedCount int
}

// RunCandidate represents one resumable run folder found during scanning.
type RunCandidate struct {
	// RunID is the run identifier extracted from the folder name.
	RunID string

	// FolderPath is the absolute path to the Orchestration-{run_id}/ folder.
	FolderPath string

	// LastUpdated is the last_updated timestamp from the artifact's frontmatter.
	// Zero value if the artifact could not be parsed (the run is still surfaced as resumable).
	LastUpdated time.Time

	// Workflow is the workflow identifier from the artifact's frontmatter.
	// Empty if the artifact could not be parsed.
	Workflow string

	// Task is the task description from the artifact's frontmatter.
	// Empty if the artifact could not be parsed.
	Task string

	// ParseError is non-nil when the Orchestration.md could not be read or parsed.
	// The candidate is still treated as resumable (surfaced, not hidden).
	ParseError error
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
			RunID:      runID,
			FolderPath: folderPath,
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
			result.CompletedCount++
			continue
		}

		// Resumable candidate: populate metadata from frontmatter.
		candidate.LastUpdated = state.LastUpdated
		candidate.Workflow = string(state.Workflow)
		candidate.Task = state.Task
		result.Candidates = append(result.Candidates, candidate)
	}

	// Sort candidates by LastUpdated descending (most recent first).
	// Candidates with a zero LastUpdated (unparseable artifacts) sort last.
	sort.Slice(result.Candidates, func(i, j int) bool {
		return result.Candidates[i].LastUpdated.After(result.Candidates[j].LastUpdated)
	})

	return result, nil
}
