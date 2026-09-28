package tui

// Run selection shows a run whose artifact has an unusable run identity as
// refused, with the reason it was refused, and never as a selectable choice.

import (
	"context"
	"testing"

	tuicommon "mosaic-common/tui"
	"mosaic-run/internal/domain"
	"mosaic-run/internal/runscan"
	"mosaic-run/internal/runselect"
	"mosaic-run/internal/tui/screens/setup"
)

const (
	invalidIdentityRunID  = "20260601T090000Z-1234"
	invalidIdentityDetail = "malformed run_id"
)

func TestRunSelectScreen_InvalidRunIdentity_RenderedWithDetail(t *testing.T) {
	// Arrange
	q := runselect.Question{Choices: []runselect.Choice{
		newRunChoiceFixture(),
		newTestResumeChoice("20260701T120000Z-a3f9"),
		{
			ID:         invalidIdentityRunID,
			Kind:       runselect.ChoiceUnresumable,
			Run:        runscan.RunInfo{RunID: invalidIdentityRunID},
			Selectable: false,
			Reason:     runscan.ReasonInvalidRunIdentity,
			Detail:     invalidIdentityDetail,
		},
	}}
	s := setup.NewRunSelectScreen(q, 100, 24, stylesFromTheme(tuicommon.DefaultTheme()))

	// Act
	view := s.View()

	// Assert
	if !containsStr(view, invalidIdentityRunID) {
		t.Errorf("run with an invalid identity must be shown, not hidden.\nview:\n%s", view)
	}
	if !containsStr(view, invalidIdentityDetail) {
		t.Errorf("view must show why the run was refused (%q).\nview:\n%s", invalidIdentityDetail, view)
	}
}

func TestRunSelect_ScanWithInvalidRunIdentity_ShowsRefusalReason(t *testing.T) {
	// Arrange: the model adapts the scan itself, so the detail must survive it.
	sess := &stubNavSession{outcome: domain.RunOutcome{Status: domain.RunCompleted, Message: "ok"}}
	m := newRootModel(context.Background(), sess, Options{
		Theme: tuicommon.DefaultTheme(),
		ScanResult: &runscan.ScanResult{
			Candidates: []runscan.RunCandidate{
				newTestCandidate("20260701T120000Z-a3f9", "/ws/Orchestration-20260701T120000Z-a3f9"),
			},
			Unresumable: []runscan.UnresumableRun{{
				RunInfo: runscan.RunInfo{RunID: invalidIdentityRunID, FolderPath: "/ws/Orchestration-" + invalidIdentityRunID},
				Reason:  runscan.ReasonInvalidRunIdentity,
				Detail:  invalidIdentityDetail,
			}},
		},
	})

	// Act
	view := m.View()

	// Assert
	if m.screen != screenRunSelect {
		t.Fatalf("precondition: screen = %v, want screenRunSelect", m.screen)
	}
	if !containsStr(view, invalidIdentityRunID) || !containsStr(view, invalidIdentityDetail) {
		t.Errorf("view must show the refused run %s and why (%q).\nview:\n%s", invalidIdentityRunID, invalidIdentityDetail, view)
	}
}
