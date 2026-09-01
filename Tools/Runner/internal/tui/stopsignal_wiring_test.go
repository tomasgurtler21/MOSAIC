package tui

// stopsignal_wiring_test.go verifies that rootModel.updateProgress signals
// the Stage-1 session.StopSignal on a confirmed graceful stop, instead of
// cancelling m.ctx (the pre-Stage-2 mechanism removed by this stage). ctx
// must remain uncancelled by a confirmed graceful stop so it can be reused
// for resume in Stage 3.
//
// RED phase: updateProgress still calls m.ctxCancel() on GracefulStop() and
// never touches a StopSignal, so every test below fails until I2.3 rewires
// it. These tests also depend on progress_confirm_test.go's I2.1/I2.2
// confirmation state machine landing, since the confirm sequence
// ('s' then 'y'/'n') only produces a confirmed/cancelled stop once that
// state machine exists.

import (
	"context"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	tuicommon "mosaic-common/tui"
	"mosaic-run/internal/domain"
	"mosaic-run/internal/session"
)

// newFlowModelWithStopSignal builds a rootModel wired to the given
// StopSignal, mirroring newFlowModel in flow_test.go but injecting
// Options.StopSignal so tests can observe Request()/Reset() calls.
func newFlowModelWithStopSignal(outcome domain.RunOutcome, stopSignal *session.StopSignal) *rootModel {
	sess := &stubFlowSession{outcome: outcome}
	return newRootModel(context.Background(), sess, Options{
		Theme:      tuicommon.DefaultTheme(),
		StopSignal: stopSignal,
	})
}

// TestFlow_ConfirmedStop_SignalsStopSignal_NotCtxCancel asserts that a
// confirmed graceful stop ('s' then 'y') calls the StopSignal's Request()
// method and leaves m.ctx uncancelled.
func TestFlow_ConfirmedStop_SignalsStopSignal_NotCtxCancel(t *testing.T) {
	stopSignal := session.NewStopSignal()
	m := newFlowModelWithStopSignal(domain.RunOutcome{Status: domain.RunCompleted}, stopSignal)
	m.progressScreen = newProgressScreen(m)
	m.screen = screenProgress

	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}})
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})

	if !stopSignal.Requested() {
		t.Error("stopSignal.Requested() = false after a confirmed graceful stop; want true")
	}
	if m.ctx.Err() != nil {
		t.Errorf("m.ctx.Err() = %v after a confirmed graceful stop; want nil (ctx must remain usable for resume)", m.ctx.Err())
	}
}

// TestFlow_PendingConfirmation_DoesNotSignalStopSignalOrCancelCtx asserts
// that merely pressing 's' (pending, unconfirmed) neither signals the
// StopSignal nor cancels ctx.
func TestFlow_PendingConfirmation_DoesNotSignalStopSignalOrCancelCtx(t *testing.T) {
	stopSignal := session.NewStopSignal()
	m := newFlowModelWithStopSignal(domain.RunOutcome{Status: domain.RunCompleted}, stopSignal)
	m.progressScreen = newProgressScreen(m)
	m.screen = screenProgress

	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}})

	if stopSignal.Requested() {
		t.Error("stopSignal.Requested() = true after only 's' pressed (pending, unconfirmed); want false")
	}
	if m.ctx.Err() != nil {
		t.Errorf("m.ctx.Err() = %v while confirmation is pending; want nil", m.ctx.Err())
	}
}

// TestFlow_NilOptionsStopSignal_DefaultsAndDoesNotPanic asserts that a
// rootModel built without Options.StopSignal set (nil) does not panic on a
// confirmed graceful stop -- ContractsDesign.md specifies newRootModel
// normalises a nil StopSignal to a fresh session.NewStopSignal().
func TestFlow_NilOptionsStopSignal_DefaultsAndDoesNotPanic(t *testing.T) {
	m := newFlowModel(domain.RunOutcome{Status: domain.RunCompleted})
	m.progressScreen = newProgressScreen(m)
	m.screen = screenProgress

	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}})
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})

	if !m.progressScreen.GracefulStop() {
		t.Error("GracefulStop() = false after a confirmed graceful stop; want true")
	}
}

// TestFlow_CancelledConfirmation_DoesNotSignalStopSignal asserts that
// cancelling a pending confirmation ('s' then 'n') never signals the
// StopSignal and leaves the run running (GracefulStop() stays false).
func TestFlow_CancelledConfirmation_DoesNotSignalStopSignal(t *testing.T) {
	stopSignal := session.NewStopSignal()
	m := newFlowModelWithStopSignal(domain.RunOutcome{Status: domain.RunCompleted}, stopSignal)
	m.progressScreen = newProgressScreen(m)
	m.screen = screenProgress

	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}})
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})

	if stopSignal.Requested() {
		t.Error("stopSignal.Requested() = true after a cancelled confirmation; want false")
	}
	if m.progressScreen.GracefulStop() {
		t.Error("GracefulStop() = true after a cancelled confirmation; want false")
	}
	if m.ctx.Err() != nil {
		t.Errorf("m.ctx.Err() = %v after a cancelled confirmation; want nil", m.ctx.Err())
	}
}
