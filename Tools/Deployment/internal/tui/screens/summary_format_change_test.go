package screens_test

// summary_format_change_test.go specifies that the summary screen shows a recorded formatting
// change, with the target path, under the entry of the created/updated/backed-up action.

import (
	"strings"
	"testing"

	"mosaic-deploy/internal/domain"
	"mosaic-deploy/internal/tui/screens"
)

const fcSummaryPath = "/workspace/.ai/Agents/test-runner.agent.md"

func summaryWithFormatChange(taken domain.ActionTaken, fc *domain.FormatChange) domain.RunSummary {
	s := cleanSummary()
	s.Actions = []domain.ActionRecord{{
		Ref:          agentRef("test-runner"),
		TargetPath:   fcSummaryPath,
		Taken:        taken,
		FormatChange: fc,
	}}
	return s
}

func crlfToLFChange() *domain.FormatChange {
	return &domain.FormatChange{
		BOMBefore:         true,
		LineEndingsBefore: domain.LineEndingCRLF,
		LineEndingsAfter:  domain.LineEndingLF,
	}
}

func TestSummaryScreen_ShowsFormatChangeWithPath(t *testing.T) {
	for _, taken := range []domain.ActionTaken{domain.TakenCreated, domain.TakenUpdated, domain.TakenBackedUp} {
		t.Run(string(taken), func(t *testing.T) {
			s := screens.NewSummaryScreen(summaryWithFormatChange(taken, crlfToLFChange()), 200, 40, plainStyles())

			view := collapseWhitespace(s.View())

			change := crlfToLFChange().String()
			if change == "" || !strings.Contains(view, change) {
				t.Errorf("summary view does not show change text %q:\n%s", change, s.View())
			}
			if !strings.Contains(view, fcSummaryPath) {
				t.Errorf("summary view does not show target path %q next to the change:\n%s", fcSummaryPath, s.View())
			}
		})
	}
}

func TestSummaryScreen_NoFormatLineWhenNoChangeRecorded(t *testing.T) {
	s := screens.NewSummaryScreen(summaryWithFormatChange(domain.TakenUpdated, nil), 200, 40, plainStyles())

	view := collapseWhitespace(s.View())

	if strings.Contains(view, "formatting") {
		t.Errorf("summary view shows a formatting line although none was recorded:\n%s", s.View())
	}
}
