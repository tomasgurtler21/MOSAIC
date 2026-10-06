package main

// Resume regression tests through the shared dependency builder. A resumed run
// takes its settings from the artifact, whatever the frontend supplied, so the
// capabilities the recorded settings enable must be available to the session
// regardless of the settings the frontend handed to the builder side.
//
//   - TUI-style: the dependencies come from a configuration selection that holds
//     no settings (the wizard skips those steps on resume).
//   - CLI-style: the setting flags are omitted, so the frontend supplies only
//     flag defaults and marks nothing as supplied.

import (
	"strings"
	"testing"

	"mosaic-run/internal/domain"
	"mosaic-run/internal/session"
	"mosaic-run/internal/tui/screens/runconfig"
)

const resumeAdviceSentinel = "resume-recorded-preconsult-advice-k4w"

// resumeAdviceResponse is a pre-consultation reply carrying the sentinel advice.
const resumeAdviceResponse = `{"task_description":"` + resumeAdviceSentinel + `","constraints":""}`

// TestResume_TUIStyle_RecordedPreConsultation_ReachesRawInvokerWithoutPanic: the
// artifact records auto + pre-consultation, the frontend supplied no settings.
func TestResume_TUIStyle_RecordedPreConsultation_ReachesRawInvokerWithoutPanic(t *testing.T) {
	sc := newResumeScenario(t,
		domain.RunSettings{Mode: domain.ExecutionModeAuto, PreConsultation: true},
		resumeAdviceResponse)
	sc.queueAgentBSuccess()
	cfg := resumeConfig(sc.orchPath, domain.RunSettings{})

	got, err := sc.start(t, cfg)

	if err != nil || got.Status != domain.RunCompleted {
		t.Fatalf("want the resumed run completed, got status %q, err %v (message: %q)", got.Status, err, got.Message)
	}
	sc.invoker.mu.Lock()
	calls := sc.invoker.callCount
	sc.invoker.mu.Unlock()
	if calls == 0 {
		t.Error("want the pre-consultation request to reach the raw invoker, got no InvokeRaw call")
	}
	invs := sc.harness.Invocations()
	if len(invs) == 0 || !strings.Contains(invs[0].Request.TaskDescription, resumeAdviceSentinel) {
		t.Errorf("want the pre-consultation advice applied to the first dispatch, got %+v", invs)
	}
}

// TestResume_CLIStyle_FlagsOmitted_HonorsRecordedPreConsultationAndManualResolution:
// all setting flags are omitted (pre-consult defaults to true, manual
// resolution to false, nothing marked supplied) while the artifact records both
// capabilities enabled.
func TestResume_CLIStyle_FlagsOmitted_HonorsRecordedPreConsultationAndManualResolution(t *testing.T) {
	sc := newResumeScenario(t,
		domain.RunSettings{Mode: domain.ExecutionModeAuto, PreConsultation: true, ManualResolution: true},
		resumeAdviceResponse)
	sc.harness.Queue("agent-b", failingHarnessEntry())
	cfg := resumeConfig(sc.orchPath, domain.RunSettings{PreConsultation: true})

	_, err := sc.start(t, cfg)

	if err != nil {
		t.Fatalf("want a nil error from the resumed run, got %v", err)
	}
	invs := sc.harness.Invocations()
	if len(invs) == 0 || !strings.Contains(invs[0].Request.TaskDescription, resumeAdviceSentinel) {
		t.Errorf("want the recorded pre-consultation honored (advice on the first dispatch), got %+v", invs)
	}
	if sc.interact.selectOneCalls() == 0 {
		t.Error("want the recorded manual resolution honored: the manual resolver should be offered after the consultation failed, but it was never reached")
	}
}

// TestResume_RecordedManualResolution_NothingSupplied_OffersManualFallback: the
// artifact records manual resolution; the frontend supplied no settings; the
// consultation fails, so the manual resolver must be reached.
func TestResume_RecordedManualResolution_NothingSupplied_OffersManualFallback(t *testing.T) {
	sc := newResumeScenario(t,
		domain.RunSettings{Mode: domain.ExecutionModeAuto, ManualResolution: true})
	sc.harness.Queue("agent-b", failingHarnessEntry())
	cfg := resumeConfig(sc.orchPath, domain.RunSettings{})

	_, err := sc.start(t, cfg)

	if err != nil {
		t.Fatalf("want a nil error from the resumed run, got %v", err)
	}
	if sc.interact.selectOneCalls() == 0 {
		t.Error("want the manual resolver reached through the Interaction after the consultation failure, but it was skipped")
	}
}

// TestResume_RecordedManualResolutionDisabled_ConsultationFailure_NoManualFallback
// pins that the manual resolver being available does not make it used: the
// effective (recorded) setting is off, so a consultation failure must stop the
// run without asking the user.
func TestResume_RecordedManualResolutionDisabled_ConsultationFailure_NoManualFallback(t *testing.T) {
	sc := newResumeScenario(t, domain.RunSettings{Mode: domain.ExecutionModeAuto})
	sc.harness.Queue("agent-b", failingHarnessEntry())
	cfg := resumeConfig(sc.orchPath, domain.RunSettings{})

	got, err := sc.start(t, cfg)

	if err != nil {
		t.Fatalf("want a nil error from the resumed run, got %v", err)
	}
	if got.Status != domain.RunStoppedByConsultant {
		t.Errorf("want RunStoppedByConsultant when the consultation fails with manual resolution off, got %q", got.Status)
	}
	if n := sc.interact.selectOneCalls(); n != 0 {
		t.Errorf("want no manual resolution offered when the recorded setting is off, got %d questions", n)
	}
}

// TestInteractiveWiring_NewDeps_EmptySelection_WiresCapabilityPorts: deps built
// from a configuration selection that holds no settings still carry the
// pre-consultation and manual-resolution ports, because availability follows
// the transports, not the selection.
func TestInteractiveWiring_NewDeps_EmptySelection_WiresCapabilityPorts(t *testing.T) {
	w := buildInteractiveWiring(testWiringInput(t, session.NewStopSignal(), resolvedTestIdentity(t)))
	cfg := runconfig.ConfigSelection{Harness: "claude-code"}

	deps := w.NewDeps(testRunFolder(t, "20260101T000000Z-0011"), false, "", cfg)

	if deps.PreConsult == nil {
		t.Error("Deps.PreConsult is nil for a harness with a raw invoker and an empty selection: a resumed run recording pre-consultation would crash")
	}
	if deps.Manual == nil {
		t.Error("Deps.Manual is nil for an empty selection: a resumed run recording manual resolution would lose its fallback")
	}
}

// TestInteractiveWiring_NewDeps_HarnessWithoutRawInvoker_LeavesPreConsultNil:
// without a raw invoker transport there is nothing to consult through, so the
// pre-consultation port stays unwired while the manual resolver (which needs
// only the interaction channel) is wired.
func TestInteractiveWiring_NewDeps_HarnessWithoutRawInvoker_LeavesPreConsultNil(t *testing.T) {
	w := buildInteractiveWiring(testWiringInput(t, session.NewStopSignal(), resolvedTestIdentity(t)))
	cfg := runconfig.ConfigSelection{Harness: "fake"}

	deps := w.NewDeps(testRunFolder(t, "20260101T000000Z-0012"), false, "", cfg)

	if deps.PreConsult != nil {
		t.Errorf("Deps.PreConsult = %T, want nil when the harness adapter has no raw invoker", deps.PreConsult)
	}
	if deps.Manual == nil {
		t.Error("Deps.Manual is nil: the manual resolver needs only the interaction channel")
	}
}
