package runscan_test

// A scanned run reports whether its artifact already records the runner
// settings. A native-created artifact does not, and its first Runner resume
// has to obtain them.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mosaic-run/internal/runscan"
)

func scanSingleCandidate(t *testing.T, mutate func(dir string)) runscan.RunInfo {
	t.Helper()
	rootDir := t.TempDir()
	dir := newTestRunFolder(t, rootDir, runID1)
	writeArtifact(t, dir, "EXECUTION", time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	if mutate != nil {
		mutate(dir)
	}

	result, err := runscan.NewDirScanner().Scan(rootDir)

	if err != nil {
		t.Fatalf("Scan() unexpected error: %v", err)
	}
	if len(result.Candidates) != 1 {
		t.Fatalf("Candidates = %d, want 1", len(result.Candidates))
	}
	return result.Candidates[0].RunInfo
}

func TestScan_NativeCreatedArtifact_RunnerSettingsNotRecorded(t *testing.T) {
	info := scanSingleCandidate(t, nil)

	if info.RunnerSettingsRecorded {
		t.Error("RunnerSettingsRecorded = true, want false for an artifact without runner_mode")
	}
}

func TestScan_RunnerCreatedArtifact_RunnerSettingsRecorded(t *testing.T) {
	info := scanSingleCandidate(t, func(dir string) {
		path := filepath.Join(dir, "Orchestration.md")
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read artifact: %v", err)
		}
		recorded := "runner_mode: auto\nrunner_pre_consultation: enabled\nrunner_manual_resolution: disabled\n"
		edited := strings.Replace(string(data), "current_state:", recorded+"current_state:", 1)
		if err := os.WriteFile(path, []byte(edited), 0o600); err != nil {
			t.Fatalf("write artifact: %v", err)
		}
	})

	if !info.RunnerSettingsRecorded {
		t.Error("RunnerSettingsRecorded = false, want true for an artifact that records runner_mode")
	}
}

func TestScan_UnparseableArtifact_RunnerSettingsNotRecorded(t *testing.T) {
	rootDir := t.TempDir()
	newTestRunFolder(t, rootDir, runID1) // no Orchestration.md

	result, err := runscan.NewDirScanner().Scan(rootDir)

	if err != nil || len(result.Candidates) != 1 {
		t.Fatalf("Scan() = %v candidates, err %v; want 1 candidate", len(result.Candidates), err)
	}
	if result.Candidates[0].RunnerSettingsRecorded {
		t.Error("RunnerSettingsRecorded = true, want false when the artifact cannot be parsed")
	}
}

// ---------------------------------------------------------------------------
// Commit setup pending
// ---------------------------------------------------------------------------

// setCommitFrontmatter inserts commit keys ahead of current_state.
func setCommitFrontmatter(lines string) func(t *testing.T, dir string) {
	return func(t *testing.T, dir string) {
		path := filepath.Join(dir, "Orchestration.md")
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read artifact: %v", err)
		}
		edited := strings.Replace(string(data), "current_state:", lines+"current_state:", 1)
		if err := os.WriteFile(path, []byte(edited), 0o600); err != nil {
			t.Fatalf("write artifact: %v", err)
		}
	}
}

func TestScan_CommitSetupPending(t *testing.T) {
	tests := []struct {
		name string
		keys string
		want bool
	}{
		{"commits disabled", "", false},
		{"commits enabled without branch", "commits: enabled\n", true},
		{"commits enabled with branch", "commits: enabled\ncommit_branch: mosaic/run/x\n", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			mutate := setCommitFrontmatter(tc.keys)

			info := scanSingleCandidate(t, func(dir string) { mutate(t, dir) })

			if info.CommitSetupPending != tc.want {
				t.Errorf("CommitSetupPending = %v, want %v", info.CommitSetupPending, tc.want)
			}
		})
	}
}
