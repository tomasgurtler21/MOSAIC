package tui

// stopgate_forcequit_test.go verifies that force-quit stays available while the
// graceful-stop confirmation gate is open.
//
// The gate only resolves on an explicit confirm or cancel key; every other key
// leaves it pending. That makes ctrl+c the user's guaranteed escape if anything
// about the resolution goes wrong, so being trapped in an unresolvable
// confirmation is the failure this file exists to prevent. The assertion lives
// here rather than in the screens package because ctrl+c is handled at the root
// model, ahead of any screen delegation, and is not observable from the screen.

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"mosaic-run/internal/domain"
)

// TestFlow_CtrlC_WhileStopGatePending_CancelsAndQuits asserts that ctrl+c
// pressed while the confirmation gate is open still cancels the context and
// issues a quit command.
func TestFlow_CtrlC_WhileStopGatePending_CancelsAndQuits(t *testing.T) {
	m := newFlowModel(domain.RunOutcome{Status: domain.RunCompleted})
	m.progressScreen = newProgressScreen(m)
	m.screen = screenProgress

	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}})
	if !m.progressScreen.ConfirmPending() {
		t.Fatalf("ConfirmPending() = false after 's'; the gate must be open for this test to mean anything")
	}

	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})

	if m.ctx.Err() == nil {
		t.Error("ctx.Err() = nil after ctrl+c while the confirmation gate was pending; force-quit must remain available")
	}
	if cmd == nil {
		t.Error("cmd = nil after ctrl+c while the confirmation gate was pending; want tea.Quit (non-nil)")
	}
}
