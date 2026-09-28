package runconfig

// config_versiondrift_test.go verifies the ConfigScreen version-drift step.

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"mosaic-run/internal/tui/screens"
)

// ---------------------------------------------------------------------------
// Version-drift screen: resume awareness
//
// These tests verify that ConfigScreen conditionally skips the version-drift
// confirmation screen based on whether this is a new run or a resumed run, and
// whether the workflow version recorded for the run differs from the version
// the current orchestrator file declares.
// ---------------------------------------------------------------------------

// TestConfigScreen_VersionDrift_NewRun_SkipsVersionDriftStep verifies that when
// isNewRun=true, the version-drift step is skipped and the wizard jumps from
// harness-timeout straight to checkpoints.
func TestConfigScreen_VersionDrift_NewRun_SkipsVersionDriftStep(t *testing.T) {
	// Arrange
	s := NewConfigScreen(80, 24, screens.Styles{})
	s.SetIsNewRun(true)
	s.SetVersionDriftInfo("", "")

	// Act
	driveConfigScreenPastModeAndHarness(s)

	// Assert
	if s.step != configStepCheckpoints {
		t.Errorf("after timeout with isNewRun=true, step=%v, want configStepCheckpoints; "+
			"version-drift screen must be skipped for new runs", s.step)
	}
}

// TestConfigScreen_VersionDrift_ResumedRun_MatchingVersions_SkipsVersionDriftStep verifies
// that when isNewRun=false and the recorded version matches the current version, the
// version-drift step is skipped and the wizard goes directly to checkpoints.
func TestConfigScreen_VersionDrift_ResumedRun_MatchingVersions_SkipsVersionDriftStep(t *testing.T) {
	// Arrange
	s := NewConfigScreen(80, 24, screens.Styles{})
	s.SetIsNewRun(false)
	s.SetNeedsRunnerAdoption(true) // a resumed native artifact still asks the mode first
	s.SetVersionDriftInfo("1.0", "1.0")

	// Act
	driveConfigScreenPastModeAndHarness(s)

	// Assert
	if s.step != configStepCheckpoints {
		t.Errorf("after timeout with matching versions, step=%v, want configStepCheckpoints; "+
			"version-drift screen must be skipped when versions match", s.step)
	}
}

// TestConfigScreen_VersionDrift_ResumedRun_DifferentVersions_AllowingDriftRecordsTrue verifies
// that on a resumed run with drifted versions the drift prompt is presented, and that
// choosing "Yes" records AllowVersionDrift=true and continues to checkpoints.
//
// Asserting the recorded choice (not merely the step value) is what makes this test
// discriminating: the step assertion alone is satisfied by unconditional advancement.
func TestConfigScreen_VersionDrift_ResumedRun_DifferentVersions_AllowingDriftRecordsTrue(t *testing.T) {
	// Arrange
	s := NewConfigScreen(80, 24, screens.Styles{})
	s.SetIsNewRun(false)
	s.SetNeedsRunnerAdoption(true) // a resumed native artifact still asks the mode first
	s.SetVersionDriftInfo("1.0", "2.0")

	// Act
	driveConfigScreenPastModeAndHarness(s)

	// Assert: the drift step is reached and actually renders its prompt
	if s.step != configStepVersionDrift {
		t.Fatalf("after timeout with different versions, step=%v, want configStepVersionDrift; "+
			"version-drift screen must be shown when versions differ on resumed runs", s.step)
	}
	if view := s.View(); !strings.Contains(view, "Allow workflow version drift") {
		t.Errorf("View() at the version-drift step on a resumed drifted run did not render the "+
			"drift prompt; the user must be asked before the choice is recorded. View:\n%s", view)
	}

	// Act: choose "Yes" (allow drift)
	driveConfigScreenVersionDriftChoice(s, 0)

	// Assert
	if !s.Selection().AllowVersionDrift {
		t.Errorf("AllowVersionDrift = false after selecting Yes on the drift prompt, want true; "+
			"the user's consent to run against a drifted workflow must be captured")
	}
	if s.step != configStepCheckpoints {
		t.Errorf("after answering the drift prompt, step=%v, want configStepCheckpoints", s.step)
	}
}

// TestConfigScreen_VersionDrift_ResumedRun_DifferentVersions_RefusingDriftRecordsFalse verifies
// that choosing "No" on the drift prompt records AllowVersionDrift=false and still continues
// to checkpoints (the refusal is enforced later, at run start).
func TestConfigScreen_VersionDrift_ResumedRun_DifferentVersions_RefusingDriftRecordsFalse(t *testing.T) {
	// Arrange
	s := NewConfigScreen(80, 24, screens.Styles{})
	s.SetIsNewRun(false)
	s.SetNeedsRunnerAdoption(true) // a resumed native artifact still asks the mode first
	s.SetVersionDriftInfo("1.0", "2.0")
	driveConfigScreenPastModeAndHarness(s)
	if s.step != configStepVersionDrift {
		t.Fatalf("precondition: step=%v, want configStepVersionDrift", s.step)
	}

	// Act: choose "No" (refuse drift)
	driveConfigScreenVersionDriftChoice(s, 1)

	// Assert
	if s.Selection().AllowVersionDrift {
		t.Errorf("AllowVersionDrift = true after selecting No on the drift prompt, want false; "+
			"refusing drift must not be recorded as consent")
	}
	if s.step != configStepCheckpoints {
		t.Errorf("after answering the drift prompt, step=%v, want configStepCheckpoints", s.step)
	}
}

// TestConfigScreen_VersionDrift_NewRun_ViewNeverRendersPrompt verifies that when
// isNewRun=true, the View() method never renders the version-drift prompt text,
// even if the step is somehow forced to configStepVersionDrift (defense in depth).
func TestConfigScreen_VersionDrift_NewRun_ViewNeverRendersPrompt(t *testing.T) {
	// Arrange
	s := NewConfigScreen(80, 24, screens.Styles{})
	s.SetIsNewRun(true)
	s.SetVersionDriftInfo("", "")
	s.step = configStepVersionDrift
	s.cursor = 0

	// Act
	view := s.View()

	// Assert
	if strings.Contains(view, "Allow workflow version drift") {
		t.Errorf("View() rendered version-drift prompt when isNewRun=true; "+
			"defense in depth: prompt must never be rendered when isNewRun=true, regardless of step state")
	}
}

// TestConfigScreen_VersionDrift_NewRun_BackFromCheckpointsSkipsVersionDriftStep verifies
// that the skipped step is skipped in the backward direction too: pressing Esc on the
// checkpoints step of a new run returns to the harness-timeout step, never landing on
// the version-drift step the forward pass jumped over.
func TestConfigScreen_VersionDrift_NewRun_BackFromCheckpointsSkipsVersionDriftStep(t *testing.T) {
	// Arrange
	s := NewConfigScreen(80, 24, screens.Styles{})
	s.SetIsNewRun(true)
	s.SetVersionDriftInfo("", "")
	driveConfigScreenPastModeAndHarness(s)
	if s.step != configStepCheckpoints {
		t.Fatalf("precondition: step=%v, want configStepCheckpoints", s.step)
	}

	// Act
	pressKey(s, tea.KeyEsc)

	// Assert
	if s.step == configStepVersionDrift {
		t.Fatalf("Esc from checkpoints on a new run landed on configStepVersionDrift; " +
			"a step skipped going forward must also be skipped going back, otherwise the user " +
			"reaches an empty prompt that still records a drift answer")
	}
	if s.step != configStepHarnessTimeout {
		t.Errorf("Esc from checkpoints on a new run: step=%v, want configStepHarnessTimeout", s.step)
	}
}

// TestConfigScreen_VersionDrift_EmptyVersions_TreatsAsMatching verifies that when
// both recorded and current versions are empty strings, they are treated as matching
// and the version-drift step is skipped.
func TestConfigScreen_VersionDrift_EmptyVersions_TreatsAsMatching(t *testing.T) {
	// Arrange
	s := NewConfigScreen(80, 24, screens.Styles{})
	s.SetIsNewRun(false)
	s.SetNeedsRunnerAdoption(true) // a resumed native artifact still asks the mode first
	s.SetVersionDriftInfo("", "")

	// Act
	driveConfigScreenPastModeAndHarness(s)

	// Assert
	if s.step != configStepCheckpoints {
		t.Errorf("after timeout with both versions empty, step=%v, want configStepCheckpoints; "+
			"empty versions must be treated as matching", s.step)
	}
}

// TestConfigScreen_VersionDrift_RecordedVersionEmpty_TreatsAsMismatch verifies the
// conservative comparison rule when only the recorded version is missing: the drift
// prompt is shown and the user's answer is recorded.
func TestConfigScreen_VersionDrift_RecordedVersionEmpty_TreatsAsMismatch(t *testing.T) {
	// Arrange
	s := NewConfigScreen(80, 24, screens.Styles{})
	s.SetIsNewRun(false)
	s.SetNeedsRunnerAdoption(true) // a resumed native artifact still asks the mode first
	s.SetVersionDriftInfo("", "1.0")

	// Act
	driveConfigScreenPastModeAndHarness(s)

	// Assert
	if s.step != configStepVersionDrift {
		t.Fatalf("after timeout with an empty recorded version, step=%v, want configStepVersionDrift; "+
			"one version present and one absent must be treated as a mismatch for safety", s.step)
	}

	// Act: answer the prompt
	driveConfigScreenVersionDriftChoice(s, 0)

	// Assert
	if !s.Selection().AllowVersionDrift {
		t.Errorf("AllowVersionDrift = false after selecting Yes, want true")
	}
	if s.step != configStepCheckpoints {
		t.Errorf("after answering the drift prompt, step=%v, want configStepCheckpoints", s.step)
	}
}

// TestConfigScreen_VersionDrift_CurrentVersionEmpty_TreatsAsMismatch verifies the
// symmetric case: only the current version is missing. The comparison must not be
// short-circuited on the recorded value alone.
func TestConfigScreen_VersionDrift_CurrentVersionEmpty_TreatsAsMismatch(t *testing.T) {
	// Arrange
	s := NewConfigScreen(80, 24, screens.Styles{})
	s.SetIsNewRun(false)
	s.SetNeedsRunnerAdoption(true) // a resumed native artifact still asks the mode first
	s.SetVersionDriftInfo("1.0", "")

	// Act
	driveConfigScreenPastModeAndHarness(s)

	// Assert
	if s.step != configStepVersionDrift {
		t.Fatalf("after timeout with an empty current version, step=%v, want configStepVersionDrift; "+
			"one version present and one absent must be treated as a mismatch for safety", s.step)
	}

	// Act: answer the prompt
	driveConfigScreenVersionDriftChoice(s, 1)

	// Assert
	if s.Selection().AllowVersionDrift {
		t.Errorf("AllowVersionDrift = true after selecting No, want false")
	}
	if s.step != configStepCheckpoints {
		t.Errorf("after answering the drift prompt, step=%v, want configStepCheckpoints", s.step)
	}
}
