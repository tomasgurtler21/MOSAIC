package domain_test

// Tests for the path form recorded in the Execution Log Inputs column and the
// Artifacts registry.

import (
	"testing"

	"mosaic-run/internal/domain"
)

func TestRecordedArtifactPath(t *testing.T) {
	cases := []struct {
		name  string
		runID string
		in    string
		want  string
	}{
		{"prefixed path loses the run folder", pathRunID, "Orchestration-" + pathRunID + "/Stage-2/Plan.md", "Stage-2/Plan.md"},
		{"backslash prefixed path is returned with forward slashes", pathRunID, "Orchestration-" + pathRunID + `\Stage-2\Plan.md`, "Stage-2/Plan.md"},
		{"unprefixed path is unchanged", pathRunID, "Stage-2/Plan.md", "Stage-2/Plan.md"},
		{"project file with backslashes is unchanged byte for byte", pathRunID, `src\main.go`, `src\main.go`},
		{"other run prefix is kept", pathRunID, "Orchestration-other-run/Plan.md", "Orchestration-other-run/Plan.md"},
		{"empty run id returns the path unchanged", "", "Orchestration-" + pathRunID + "/Plan.md", "Orchestration-" + pathRunID + "/Plan.md"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := domain.RecordedArtifactPath(tc.runID, tc.in)

			if got != tc.want {
				t.Errorf("RecordedArtifactPath(%q, %q) = %q, want %q", tc.runID, tc.in, got, tc.want)
			}
		})
	}
}
