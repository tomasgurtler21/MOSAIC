package screens_test

// retention_test.go verifies the RetentionScreen behavioral contract:
// Space cycles through retention policy values, Enter confirms without cycling,
// Esc sets Back, Reset clears the Done and Back flags, and Reset discards
// any transient cycling state so the screen behaves as if freshly entered.

import (
	"testing"

	"mosaic-agent-test/internal/domain"
	"mosaic-agent-test/internal/tui/screens"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func newRetentionScreen(initial domain.RetentionPolicy) *screens.RetentionScreen {
	return screens.NewRetentionScreen(initial, 80, plainStyles())
}

// ---------------------------------------------------------------------------
// Space key — cycles the policy value
// ---------------------------------------------------------------------------

// TestRetentionScreen_Space_CyclesPolicy_NeverToOnFailure verifies that pressing
// Space once from RetainNever advances to RetainOnFailure without setting Done.
func TestRetentionScreen_Space_CyclesPolicy_NeverToOnFailure(t *testing.T) {
	// Arrange
	s := newRetentionScreen(domain.RetainNever)

	// Act
	s.Update(spaceKey())

	// Assert
	if s.Policy() != domain.RetainOnFailure {
		t.Errorf("Policy() = %q after one Space from RetainNever; want %q", s.Policy(), domain.RetainOnFailure)
	}
	if s.Done() {
		t.Error("Done() = true after Space; Space must cycle only, not confirm")
	}
}

// TestRetentionScreen_Space_CyclesPolicy_OnFailureToAlways verifies that pressing
// Space twice from RetainNever advances to RetainAlways.
func TestRetentionScreen_Space_CyclesPolicy_OnFailureToAlways(t *testing.T) {
	// Arrange
	s := newRetentionScreen(domain.RetainNever)

	// Act
	s.Update(spaceKey())
	s.Update(spaceKey())

	// Assert
	if s.Policy() != domain.RetainAlways {
		t.Errorf("Policy() = %q after two Space presses from RetainNever; want %q", s.Policy(), domain.RetainAlways)
	}
}

// TestRetentionScreen_Space_CyclesPolicy_OnFailureToAlways_DirectStart verifies
// that pressing Space once when the initial policy is RetainOnFailure advances
// directly to RetainAlways. This exercises a cycle started mid-sequence (not
// from RetainNever) to ensure no off-by-one in the cycle order.
func TestRetentionScreen_Space_CyclesPolicy_OnFailureToAlways_DirectStart(t *testing.T) {
	// Arrange — start directly at RetainOnFailure, not at RetainNever
	s := newRetentionScreen(domain.RetainOnFailure)

	// Act
	s.Update(spaceKey())

	// Assert
	if s.Policy() != domain.RetainAlways {
		t.Errorf("Policy() = %q after one Space from RetainOnFailure; want %q", s.Policy(), domain.RetainAlways)
	}
}

// TestRetentionScreen_Space_CyclesPolicy_AlwaysToNever verifies that pressing
// Space three times from RetainNever wraps back to RetainNever.
func TestRetentionScreen_Space_CyclesPolicy_AlwaysWrapsToNever(t *testing.T) {
	// Arrange
	s := newRetentionScreen(domain.RetainNever)

	// Act
	s.Update(spaceKey())
	s.Update(spaceKey())
	s.Update(spaceKey())

	// Assert
	if s.Policy() != domain.RetainNever {
		t.Errorf("Policy() = %q after three Space presses from RetainNever; want %q (cycle must wrap)", s.Policy(), domain.RetainNever)
	}
}

// ---------------------------------------------------------------------------
// Enter key — confirms without cycling
// ---------------------------------------------------------------------------

// TestRetentionScreen_Enter_SetsDone verifies that pressing Enter sets Done
// so the root model can advance to the next screen.
func TestRetentionScreen_Enter_SetsDone(t *testing.T) {
	// Arrange
	s := newRetentionScreen(domain.RetainNever)

	// Act
	s.Update(enterKey())

	// Assert
	if !s.Done() {
		t.Error("Done() = false after Enter; Enter must confirm the current policy and set Done")
	}
}

// TestRetentionScreen_Enter_DoesNotCyclePolicy verifies that pressing Enter
// accepts the currently displayed value without changing it. Enter is the
// universal confirm key and must not act as a cycle key.
func TestRetentionScreen_Enter_DoesNotCyclePolicy(t *testing.T) {
	// Arrange — start at RetainOnFailure so a cycle to RetainAlways would be detectable
	s := newRetentionScreen(domain.RetainOnFailure)

	// Act
	s.Update(enterKey())

	// Assert
	if s.Policy() != domain.RetainOnFailure {
		t.Errorf("Policy() = %q after Enter from RetainOnFailure; Enter must not cycle, want %q", s.Policy(), domain.RetainOnFailure)
	}
}

// ---------------------------------------------------------------------------
// Esc key — back navigation
// ---------------------------------------------------------------------------

// TestRetentionScreen_Esc_SetsBack verifies that pressing Esc sets Back so
// the root model can navigate to the previous screen.
func TestRetentionScreen_Esc_SetsBack(t *testing.T) {
	// Arrange
	s := newRetentionScreen(domain.RetainNever)

	// Act
	s.Update(escKey())

	// Assert
	if !s.Back() {
		t.Error("Back() = false after Esc; Esc must signal back-navigation")
	}
}

// TestRetentionScreen_Esc_DoesNotChangePolicyValue verifies that pressing Esc
// does not alter the retention policy.
func TestRetentionScreen_Esc_DoesNotChangePolicyValue(t *testing.T) {
	// Arrange
	s := newRetentionScreen(domain.RetainAlways)

	// Act
	s.Update(escKey())

	// Assert
	if s.Policy() != domain.RetainAlways {
		t.Errorf("Policy() = %q after Esc; Esc must not change the value, want %q", s.Policy(), domain.RetainAlways)
	}
}

// ---------------------------------------------------------------------------
// Reset — clears Done and Back flags
// ---------------------------------------------------------------------------

// TestRetentionScreen_Reset_ClearsDone verifies that Reset clears the Done flag
// so the screen can be re-entered by the root model.
func TestRetentionScreen_Reset_ClearsDone(t *testing.T) {
	// Arrange
	s := newRetentionScreen(domain.RetainNever)
	s.Update(enterKey())
	if !s.Done() {
		t.Fatal("precondition: Done() must be true after Enter before Reset can be verified")
	}

	// Act
	s.Reset()

	// Assert
	if s.Done() {
		t.Error("Done() = true after Reset; Reset must clear the Done flag")
	}
}

// TestRetentionScreen_Reset_ClearsBack verifies that Reset clears the Back flag
// so the screen can be re-entered cleanly.
func TestRetentionScreen_Reset_ClearsBack(t *testing.T) {
	// Arrange
	s := newRetentionScreen(domain.RetainNever)
	s.Update(escKey())
	if !s.Back() {
		t.Fatal("precondition: Back() must be true after Esc before Reset can be verified")
	}

	// Act
	s.Reset()

	// Assert
	if s.Back() {
		t.Error("Back() = true after Reset; Reset must clear the Back flag")
	}
}

// TestRetentionScreen_Reset_ClearsDraftState verifies that Reset discards any
// policy cycling done before Reset so the screen behaves as if freshly entered.
// An implementation that clears only the Done/Back flags but retains the cycled
// policy would accumulate the pre-Reset cycle with the post-Reset cycle,
// producing the wrong final policy.
func TestRetentionScreen_Reset_ClearsDraftState(t *testing.T) {
	// Arrange — start at RetainNever and cycle twice (reaching RetainAlways)
	// without confirming, to accumulate transient state.
	s := newRetentionScreen(domain.RetainNever)
	s.Update(spaceKey()) // RetainNever -> RetainOnFailure
	s.Update(spaceKey()) // RetainOnFailure -> RetainAlways
	// Precondition: verify the pre-reset cycle took effect.
	if s.Policy() != domain.RetainAlways {
		t.Fatal("precondition: Policy() must be RetainAlways after two Space presses before Reset can be verified")
	}

	// Act
	s.Reset()

	// Act (post-reset) — press Space once and confirm.
	// If Reset correctly cleared the draft, cycling resumes from the initial
	// RetainNever, so one Space reaches RetainOnFailure.
	// If Reset did NOT clear the draft (RetainAlways still active), one Space
	// would wrap to RetainNever instead.
	s.Update(spaceKey())
	s.Update(enterKey())
	if !s.Done() {
		t.Fatal("precondition: Done() must be true after post-Reset Enter")
	}

	// Assert
	if s.Policy() != domain.RetainOnFailure {
		t.Errorf("Policy() = %q after Reset + one Space + Enter; want %q — Reset must discard pre-Reset cycles so post-Reset cycling starts fresh", s.Policy(), domain.RetainOnFailure)
	}
}

// ---------------------------------------------------------------------------
// Resize — does not corrupt state
// ---------------------------------------------------------------------------

// TestRetentionScreen_Resize_DoesNotTriggerDoneOrBack verifies that a terminal
// resize event does not accidentally signal confirmation or back-navigation.
func TestRetentionScreen_Resize_DoesNotTriggerDoneOrBack(t *testing.T) {
	// Arrange
	s := newRetentionScreen(domain.RetainNever)

	// Act
	s.Resize(120)

	// Assert
	if s.Done() {
		t.Error("Done() = true after Resize; Resize must not trigger Done")
	}
	if s.Back() {
		t.Error("Back() = true after Resize; Resize must not trigger Back")
	}
}

// ---------------------------------------------------------------------------
// View — renders content
// ---------------------------------------------------------------------------

// TestRetentionScreen_View_ReturnsNonEmptyString verifies that View returns a
// non-empty rendered string so the root model has content to display.
func TestRetentionScreen_View_ReturnsNonEmptyString(t *testing.T) {
	// Arrange
	s := newRetentionScreen(domain.RetainNever)

	// Act
	view := s.View()

	// Assert
	if len(view) == 0 {
		t.Error("View() returned an empty string; the retention screen must render visible content")
	}
}
