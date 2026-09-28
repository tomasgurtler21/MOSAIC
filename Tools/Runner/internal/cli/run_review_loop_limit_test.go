package cli_test

// Tests for the --review-loop-limit flag and for how the run subcommand
// reports run-configuration values on a resume: values the user did not give
// are never marked as supplied, values the user did give are marked so the
// session can compare them with the artifact, and a native-created artifact
// (no runner settings recorded) needs --mode to be adopted.

import (
	"os"
	"strings"
	"testing"

	"mosaic-run/internal/cli"
	"mosaic-run/internal/domain"
)

// newRunArgs returns the arguments of a new run with the given extra flags.
func newRunArgs(extra ...string) []string {
	args := []string{"run", "--workflow", "w1", "--task", "do work", "--mode", "auto", "--new-run"}
	return append(args, extra...)
}

// chdirTemp moves the process into a fresh temporary directory for the test.
func chdirTemp(t *testing.T) string {
	t.Helper()
	rootDir := t.TempDir()
	origDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("os.Getwd: %v", err)
	}
	if err := os.Chdir(rootDir); err != nil {
		t.Fatalf("os.Chdir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(origDir) })
	return rootDir
}

// writeRunnerRecordedRunArtifact creates a resumable run whose artifact
// records the three runner settings (as every Runner-created run does), plus
// any extra frontmatter lines.
func writeRunnerRecordedRunArtifact(t *testing.T, rootDir, runID, extraLines string) {
	t.Helper()
	path := writeResumableRunArtifact(t, rootDir, runID)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read artifact: %v", err)
	}
	recorded := "runner_mode: auto\nrunner_pre_consultation: true\nrunner_manual_resolution: false\n" + extraLines
	edited := strings.Replace(string(data), "current_state:", recorded+"current_state:", 1)
	if edited == string(data) {
		t.Fatal("current_state key not found in the artifact fixture")
	}
	if err := os.WriteFile(path, []byte(edited), 0o600); err != nil {
		t.Fatalf("write artifact: %v", err)
	}
}

func startedSession() *scriptedSession {
	return &scriptedSession{outcome: domain.RunOutcome{Status: domain.RunCompleted}}
}

// ===== New run =====

func TestReviewLoopLimitFlag_ValidValues_ReachRunSettings(t *testing.T) {
	tests := []struct {
		value string
		want  int
	}{
		{"5", 5},
		{"1", 1},
		{"none", 0},
		{"NONE", 0},
		{"no limit", 0},
	}
	for _, tc := range tests {
		t.Run(tc.value, func(t *testing.T) {
			sess := startedSession()

			code, _, errOut := runCLI(t, newRunArgs("--review-loop-limit", tc.value), sess)

			if code != cli.ExitSuccess {
				t.Fatalf("exit code = %d, want ExitSuccess; stderr: %q", code, errOut)
			}
			if !sess.called {
				t.Fatal("session.Start was not called")
			}
			if sess.config.ReviewLoopLimit != tc.want {
				t.Errorf("ReviewLoopLimit = %d, want %d", sess.config.ReviewLoopLimit, tc.want)
			}
		})
	}
}

func TestReviewLoopLimitFlag_AbsentOnNewRun_RefusedWithAcceptedForms(t *testing.T) {
	sess := startedSession()

	code, _, errOut := runCLI(t, newRunArgs(), sess)

	if code != cli.ExitUsage {
		t.Errorf("exit code = %d, want ExitUsage when --review-loop-limit is absent on a new run", code)
	}
	if sess.called {
		t.Error("session.Start must not be called when --review-loop-limit is absent on a new run")
	}
	if !strings.Contains(errOut, "--review-loop-limit") {
		t.Errorf("stderr %q does not name --review-loop-limit", errOut)
	}
	if !strings.Contains(errOut, "none") {
		t.Errorf("stderr %q does not list the accepted forms (a positive integer or none)", errOut)
	}
}

func TestReviewLoopLimitFlag_InvalidValues_Refused(t *testing.T) {
	for _, value := range []string{"0", "-1", "abc", "2.5", ""} {
		t.Run("value_"+value, func(t *testing.T) {
			sess := startedSession()

			code, _, errOut := runCLI(t, newRunArgs("--review-loop-limit", value), sess)

			if code != cli.ExitUsage {
				t.Errorf("exit code = %d, want ExitUsage for --review-loop-limit %q", code, value)
			}
			if sess.called {
				t.Errorf("session.Start must not be called for --review-loop-limit %q", value)
			}
			if !strings.Contains(errOut, "--review-loop-limit") {
				t.Errorf("stderr %q does not name --review-loop-limit", errOut)
			}
		})
	}
}

// ===== Resume of a Runner-created run =====

// Nothing the user did not give is reported as supplied, and no run setting
// flag is needed at all: a resume reads the stored values.
func TestResume_RecordedSettings_NoSettingFlags_NothingSupplied(t *testing.T) {
	rootDir := chdirTemp(t)
	writeRunnerRecordedRunArtifact(t, rootDir, testRunID, "review_loop_limit: 5\n")
	sess := startedSession()

	code, _, errOut := runCLI(t, []string{"run", "--workflow", "w1", "--task", "do work", "--run", testRunID}, sess)

	if code != cli.ExitSuccess {
		t.Fatalf("exit code = %d, want ExitSuccess (no mode or limit flag is needed to resume a recorded run); stderr: %q", code, errOut)
	}
	if !sess.called {
		t.Fatal("session.Start was not called")
	}
	if sess.config.IsNewRun {
		t.Error("IsNewRun = true, want false")
	}
	if sess.config.Supplied != (domain.SuppliedSettings{}) {
		t.Errorf("Supplied = %+v, want nothing supplied (flag defaults must never count as supplied values)", sess.config.Supplied)
	}
}

// An explicit limit on resume is passed through as a supplied value, so the
// session can refuse a change; it is never silently dropped or applied.
func TestResume_ExplicitReviewLoopLimit_IsMarkedSupplied(t *testing.T) {
	tests := []struct {
		value string
		want  int
	}{
		{"2", 2},
		{"none", 0},
	}
	for _, tc := range tests {
		t.Run(tc.value, func(t *testing.T) {
			rootDir := chdirTemp(t)
			writeRunnerRecordedRunArtifact(t, rootDir, testRunID, "review_loop_limit: 5\n")
			sess := startedSession()

			code, _, errOut := runCLI(t, []string{"run", "--workflow", "w1", "--task", "do work", "--run", testRunID,
				"--review-loop-limit", tc.value}, sess)

			if code != cli.ExitSuccess {
				t.Fatalf("exit code = %d, want ExitSuccess; stderr: %q", code, errOut)
			}
			if !sess.config.Supplied.ReviewLoopLimit || sess.config.ReviewLoopLimit != tc.want {
				t.Errorf("limit = %d supplied=%v, want %d supplied=true",
					sess.config.ReviewLoopLimit, sess.config.Supplied.ReviewLoopLimit, tc.want)
			}
		})
	}
}

func TestResume_InvalidReviewLoopLimit_Refused(t *testing.T) {
	rootDir := chdirTemp(t)
	writeRunnerRecordedRunArtifact(t, rootDir, testRunID, "")
	sess := startedSession()

	code, _, errOut := runCLI(t, []string{"run", "--workflow", "w1", "--task", "do work", "--run", testRunID,
		"--review-loop-limit", "abc"}, sess)

	if code != cli.ExitUsage {
		t.Errorf("exit code = %d, want ExitUsage", code)
	}
	if sess.called {
		t.Error("session.Start must not be called for an invalid --review-loop-limit")
	}
	if !strings.Contains(errOut, "--review-loop-limit") {
		t.Errorf("stderr %q does not name --review-loop-limit", errOut)
	}
}

// Each setting flag is marked supplied only when it was actually given.
func TestResume_ExplicitSettingFlags_AreMarkedSupplied(t *testing.T) {
	rootDir := chdirTemp(t)
	writeRunnerRecordedRunArtifact(t, rootDir, testRunID, "")
	sess := startedSession()

	code, _, errOut := runCLI(t, []string{"run", "--workflow", "w1", "--task", "do work", "--run", testRunID,
		"--mode", "orchestrated", "--pre-consult=false", "--manual-resolution", "--infra-class", "checkpoint=checkpoint-manager-git"}, sess)

	if code != cli.ExitSuccess {
		t.Fatalf("exit code = %d, want ExitSuccess; stderr: %q", code, errOut)
	}
	want := domain.SuppliedSettings{Mode: true, PreConsultation: true, ManualResolution: true, InfraClassSelections: true}
	if sess.config.Supplied != want {
		t.Errorf("Supplied = %+v, want %+v", sess.config.Supplied, want)
	}
	if sess.config.Mode != domain.ExecutionModeOrchestrated || sess.config.PreConsultation || !sess.config.ManualResolution {
		t.Errorf("supplied values = (%q, pre=%v, manual=%v), want (orchestrated, false, true)",
			sess.config.Mode, sess.config.PreConsultation, sess.config.ManualResolution)
	}
	if sess.config.InfraClassSelections["checkpoint"] != "checkpoint-manager-git" {
		t.Errorf("InfraClassSelections = %v, want the supplied mapping", sess.config.InfraClassSelections)
	}
}

// ===== First resume of a native-created artifact =====

func TestResume_NativeArtifact_WithoutMode_Refused(t *testing.T) {
	rootDir := chdirTemp(t)
	writeResumableRunArtifact(t, rootDir, testRunID) // records no runner settings
	sess := startedSession()

	code, _, errOut := runCLI(t, []string{"run", "--workflow", "w1", "--task", "do work", "--run", testRunID}, sess)

	if code != cli.ExitUsage {
		t.Errorf("exit code = %d, want ExitUsage when adopting a native artifact without --mode", code)
	}
	if sess.called {
		t.Error("session.Start must not be called when --mode is missing for an adoption")
	}
	if !strings.Contains(errOut, "--mode") {
		t.Errorf("stderr %q does not name --mode", errOut)
	}
	for _, m := range domain.ExecutionModes() {
		if !strings.Contains(errOut, string(m)) {
			t.Errorf("stderr %q does not list valid mode %q", errOut, m)
		}
	}
}

// The flags supply the three runner settings; the review loop limit is not
// needed, because a native-created artifact is used as it is.
func TestResume_NativeArtifact_ModeAndSettingFlags_SuppliedForAdoption(t *testing.T) {
	rootDir := chdirTemp(t)
	writeResumableRunArtifact(t, rootDir, testRunID)
	sess := startedSession()

	code, _, errOut := runCLI(t, []string{"run", "--workflow", "w1", "--task", "do work", "--run", testRunID,
		"--mode", "auto-review", "--pre-consult=false", "--manual-resolution"}, sess)

	if code != cli.ExitSuccess {
		t.Fatalf("exit code = %d, want ExitSuccess (no --review-loop-limit is needed to adopt); stderr: %q", code, errOut)
	}
	want := domain.SuppliedSettings{Mode: true, PreConsultation: true, ManualResolution: true}
	if sess.config.Supplied != want {
		t.Errorf("Supplied = %+v, want %+v", sess.config.Supplied, want)
	}
	if sess.config.Mode != domain.ExecutionModeAutoReview || sess.config.PreConsultation || !sess.config.ManualResolution {
		t.Errorf("adoption values = (%q, pre=%v, manual=%v), want (auto-review, false, true)",
			sess.config.Mode, sess.config.PreConsultation, sess.config.ManualResolution)
	}
	if sess.config.Supplied.ReviewLoopLimit {
		t.Error("a review loop limit was never given and must not be marked supplied")
	}
}

// With only --mode given, the other two settings are adopted at their flag
// defaults (pre-consultation on, manual resolution off).
func TestResume_NativeArtifact_OnlyMode_AdoptsDefaultsForTheOtherSettings(t *testing.T) {
	rootDir := chdirTemp(t)
	writeResumableRunArtifact(t, rootDir, testRunID)
	sess := startedSession()

	code, _, errOut := runCLI(t, []string{"run", "--workflow", "w1", "--task", "do work", "--run", testRunID, "--mode", "auto"}, sess)

	if code != cli.ExitSuccess {
		t.Fatalf("exit code = %d, want ExitSuccess; stderr: %q", code, errOut)
	}
	if !sess.config.Supplied.Mode {
		t.Error("Supplied.Mode = false, want true (--mode was given)")
	}
	if !sess.config.PreConsultation || sess.config.ManualResolution {
		t.Errorf("defaults = (pre=%v, manual=%v), want (true, false)", sess.config.PreConsultation, sess.config.ManualResolution)
	}
}
