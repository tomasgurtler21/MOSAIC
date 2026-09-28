package cli_test

import (
	"os"
	"strings"
	"testing"

	"mosaic-run/internal/cli"
)

const otherRunID = "20260101T000000Z-abcd"

// chdirTo makes dir the working directory for the test.
func chdirTo(t *testing.T, dir string) {
	t.Helper()
	origDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("os.Getwd: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("os.Chdir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(origDir) })
}

// --run naming a run whose artifact has an unusable run identity is refused
// before the session starts, and the message says what is wrong.
func TestRunFlag_InvalidRunIdentity_RefusedNamingTheProblem(t *testing.T) {
	tests := []struct {
		name      string
		runIDLine string
		mentions  string
	}{
		{"run_id key absent", "", "run_id"},
		{"run_id empty", `run_id: ""`, "run_id"},
		{"run_id malformed", "run_id: not-a-run-id", "malformed"},
		{"run_id names a different folder", "run_id: " + otherRunID, otherRunID},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Arrange
			rootDir := t.TempDir()
			writeRunArtifactWithRunIDLine(t, rootDir, testRunID, tc.runIDLine)
			chdirTo(t, rootDir)
			sess := &scriptedSession{}

			// Act
			code, _, errOut := runCLI(t, []string{
				"run", "--workflow", "w1", "--task", "do work", "--run", testRunID,
			}, sess)

			// Assert
			if code != cli.ExitUsage && code != cli.ExitRefused {
				t.Errorf("exit code = %d, want ExitUsage or ExitRefused", code)
			}
			if !strings.Contains(errOut, tc.mentions) {
				t.Errorf("stderr %q does not name the problem (want it to mention %q)", errOut, tc.mentions)
			}
			if sess.called {
				t.Error("session.Start must not be called for a run with an invalid identity")
			}
		})
	}
}

// Without --run or --new-run the CLI refuses and lists what it found; a run
// with an unusable identity is listed as refused, with the reason, and is not
// offered for --run.
func TestSelection_InvalidRunIdentity_ListedAsRefusedNotOffered(t *testing.T) {
	// Arrange
	rootDir := t.TempDir()
	writeRunArtifactWithRunIDLine(t, rootDir, cliRunID1, "run_id: not-a-run-id")
	writeResumableRunArtifact(t, rootDir, cliRunID2)
	chdirTo(t, rootDir)
	sess := &scriptedSession{}

	// Act
	code, _, errOut := runCLI(t, []string{"run", "--workflow", "w1", "--task", "do work"}, sess)

	// Assert
	if code != cli.ExitUsage {
		t.Errorf("exit code = %d, want ExitUsage", code)
	}
	if sess.called {
		t.Error("session.Start must not be called when selection is unsettled")
	}
	offered, refused, found := strings.Cut(errOut, "cannot be resumed")
	if !found {
		t.Fatalf("stderr %q does not list the refused run under \"cannot be resumed\"", errOut)
	}
	if !strings.Contains(offered, cliRunID2) {
		t.Errorf("stderr %q does not offer the current run %s", errOut, cliRunID2)
	}
	if strings.Contains(offered, cliRunID1) {
		t.Errorf("run %s has an invalid identity and must not be offered for --run; stderr: %q", cliRunID1, errOut)
	}
	if !strings.Contains(refused, cliRunID1) || !strings.Contains(refused, "malformed") {
		t.Errorf("refused listing %q must name run %s and the problem", refused, cliRunID1)
	}
}
