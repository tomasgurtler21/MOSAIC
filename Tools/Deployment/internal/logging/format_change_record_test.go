package logging_test

// format_change_record_test.go specifies that the run log names a recorded formatting change
// together with the target path, in both sinks. Shared helpers (doRunWithAction,
// checkBothSinks) come from records_test.go.

import (
	"strings"
	"testing"

	"mosaic-deploy/internal/domain"
)

func formatChangeAction() domain.ActionRecord {
	ar := updateAction(domain.ArtifactRef{Kind: domain.ArtifactAgent, Key: "runner"}, "version", "1", "2")
	ar.FormatChange = &domain.FormatChange{
		BOMBefore:         true,
		LineEndingsBefore: domain.LineEndingCRLF,
		LineEndingsAfter:  domain.LineEndingLF,
	}
	return ar
}

func TestRecord_FormatChangeAppearsWithPathInBothSinks(t *testing.T) {
	ar := formatChangeAction()
	change := ar.FormatChange.String()
	if change == "" {
		t.Fatal("FormatChange.String() is empty; cannot assert the log text")
	}

	latest, history := doRunWithAction(t, ar)

	checkBothSinks(t, latest, history, ar.TargetPath, change)
}

func TestRecord_FormatChangeSitsOnTheActionLine(t *testing.T) {
	ar := formatChangeAction()
	change := ar.FormatChange.String()

	latest, _ := doRunWithAction(t, ar)

	var actionLine string
	for _, line := range strings.Split(latest, "\n") {
		if strings.Contains(line, "[action]") && strings.Contains(line, ar.TargetPath) {
			actionLine = line
		}
	}
	if actionLine == "" {
		t.Fatalf("no [action] line for %q in log:\n%s", ar.TargetPath, latest)
	}
	if change == "" || !strings.Contains(actionLine, change) {
		t.Errorf("action line %q does not contain the change text %q", actionLine, change)
	}
}

func TestRecord_NoFormatChangeLeavesActionLineWithoutFormatting(t *testing.T) {
	ar := updateAction(domain.ArtifactRef{Kind: domain.ArtifactAgent, Key: "runner"}, "version", "1", "2")

	latest, _ := doRunWithAction(t, ar)

	if strings.Contains(latest, "format_change") {
		t.Errorf("log mentions format_change although none was recorded:\n%s", latest)
	}
}
