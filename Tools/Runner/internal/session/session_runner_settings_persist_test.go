package session_test

// Tests for the Runner-owned run settings (mode, pre-consultation, manual
// resolution) and the review loop limit: a Runner-created run records them at
// creation, the first Runner resume of a native-created artifact adopts the
// supplied values once, and every later resume reuses the stored values and
// refuses a conflicting supplied one.

import (
	"context"
	"strings"
	"testing"

	"mosaic-run/internal/domain"
	"mosaic-run/internal/harness"
	"mosaic-run/internal/session"
)

// nativeCreatedState is the artifact of an interrupted linear run that native
// orchestration created: it records no runner settings.
func nativeCreatedState() domain.ArtifactState {
	return domain.ArtifactState{
		Type:            "orchestration-artifact",
		Workflow:        "linear",
		WorkflowVersion: "1.0",
		Task:            "existing task",
		GlobalSequence:  1,
		CurrentState: domain.CurrentState{
			Phase:      "PLANNING",
			LastStatus: domain.StatusSUCCESS,
			LastAgent:  "agent-a#1",
		},
		ExecutionLog: []domain.ExecutionLogEntry{
			{Seq: 1, Agent: "agent-a#1", Phase: "PLANNING", Status: domain.StatusSUCCESS},
		},
	}
}

// runnerCreatedState is the same interrupted run, created by the Runner with
// the given settings.
func runnerCreatedState(settings domain.RunSettings) domain.ArtifactState {
	st := nativeCreatedState()
	st.RunSettings = settings
	return st
}

// newResumeOfLinear returns a linear session over the given stored artifact,
// with agent-b queued as the remaining step, and a resume config.
func newResumeOfLinear(t *testing.T, stored domain.ArtifactState) (session.Session, *harness.MockAdapter, *memStore, domain.RunConfig) {
	t.Helper()
	ses, f, store, orchPath := newLinearSession(t)
	store.state = stored
	store.exists = true
	f.Queue("agent-b", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-b#2", StatusCode: domain.StatusSUCCESS, StatusMessage: "done",
	}})
	cfg := baseLinearConfig(orchPath)
	markResume(&cfg)
	return ses, f, store, cfg
}

func requireNoDispatchOrWrite(t *testing.T, f *harness.MockAdapter, store *memStore) {
	t.Helper()
	if n := len(f.Invocations()); n != 0 {
		t.Errorf("want zero invocations, got %d", n)
	}
	if store.AdoptWrites != 0 {
		t.Errorf("want no adoption write, got %d", store.AdoptWrites)
	}
	if len(store.Applied) != 0 {
		t.Errorf("want no recorded step, got %d", len(store.Applied))
	}
}

// ===== Runner-created run =====

// Guard tests: the creation write path already stores whatever RunSettings it
// is given, so the two new-run tests below are green from the start and protect
// against regressions. The RED signal for creation-time persistence is the
// round-trip tests further down, which resume from the artifact a new run wrote.

// A Runner-created run records all three runner settings and the review loop
// limit at creation and never goes through adoption.
func TestSession_Start_NewRun_RecordsRunnerSettingsAndReviewLoopLimit(t *testing.T) {
	dir := t.TempDir()
	orchPath := copyOrchestratorFile(t, dir, "linear-orch.md")
	writeAgentFile(t, dir, "agent-a")
	writeAgentFile(t, dir, "agent-b")
	f := harness.NewMockAdapter()
	store := &memStore{}
	ses := session.New(session.Deps{
		Harness:    f,
		Store:      store,
		Clock:      fixedClock{t: epoch},
		Interact:   &noopInteraction{},
		PreConsult: &scriptedPreConsultant{},
	})
	f.Queue("agent-a", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-a#1", StatusCode: domain.StatusSUCCESS, StatusMessage: "done",
	}})
	f.Queue("agent-b", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-b#2", StatusCode: domain.StatusSUCCESS, StatusMessage: "done",
	}})
	cfg := baseLinearConfig(orchPath)
	cfg.Mode = domain.ExecutionModeAutoReview
	cfg.PreConsultation = true
	cfg.ManualResolution = true
	cfg.ReviewLoopLimit = 4

	got, err := ses.Start(context.Background(), cfg)

	requireRunStatus(t, got, err, domain.RunCompleted)
	if store.state.Mode != domain.ExecutionModeAutoReview || !store.state.PreConsultation || !store.state.ManualResolution {
		t.Errorf("artifact runner settings = (%q, %v, %v), want (auto-review, true, true)",
			store.state.Mode, store.state.PreConsultation, store.state.ManualResolution)
	}
	if store.state.ReviewLoopLimit != 4 {
		t.Errorf("artifact review_loop_limit = %d, want 4", store.state.ReviewLoopLimit)
	}
	if len(store.AdoptCalls) != 0 {
		t.Errorf("a Runner-created run must not adopt settings, got %d adoption calls", len(store.AdoptCalls))
	}
}

// No limit is recorded as the absent value.
func TestSession_Start_NewRun_NoReviewLoopLimit_RecordsNone(t *testing.T) {
	ses, f, store, orchPath := newLinearSession(t)
	f.Queue("agent-a", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-a#1", StatusCode: domain.StatusSUCCESS, StatusMessage: "done",
	}})
	f.Queue("agent-b", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-b#2", StatusCode: domain.StatusSUCCESS, StatusMessage: "done",
	}})
	cfg := baseLinearConfig(orchPath)
	cfg.ReviewLoopLimit = 0

	got, err := ses.Start(context.Background(), cfg)

	requireRunStatus(t, got, err, domain.RunCompleted)
	if store.state.ReviewLoopLimit != 0 {
		t.Errorf("artifact review_loop_limit = %d, want 0 (no limit)", store.state.ReviewLoopLimit)
	}
}

// The settings a new run wrote are the ones a later resume enforces: every
// runner setting and the limit refuse a conflicting supplied value.
func TestSession_NewRunSettings_RoundTripToResume_ConflictsRefused(t *testing.T) {
	created := createdRunSettings(t, 4)
	tests := []struct {
		name      string
		supply    func(cfg *domain.RunConfig)
		wantField string
	}{
		{"mode", func(c *domain.RunConfig) {
			c.Mode = domain.ExecutionModeOrchestrated
			c.Supplied.Mode = true
		}, "runner_mode"},
		{"manual resolution", func(c *domain.RunConfig) {
			c.ManualResolution = false
			c.Supplied.ManualResolution = true
		}, "runner_manual_resolution"},
		{"review loop limit", func(c *domain.RunConfig) {
			c.ReviewLoopLimit = 2
			c.Supplied.ReviewLoopLimit = true
		}, "review_loop_limit"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ses, f, store, cfg := newResumeOfLinear(t, runnerCreatedState(created))
			cfg.RunSettings = created
			tc.supply(&cfg)

			got, err := ses.Start(context.Background(), cfg)

			msg := requireRefused(t, got, err)
			if !strings.Contains(msg, tc.wantField) {
				t.Errorf("refusal %q must name %s", msg, tc.wantField)
			}
			requireNoDispatchOrWrite(t, f, store)
		})
	}
}

// A run created with no limit records none, so a later resume refuses a limit
// supplied for it.
func TestSession_NewRunWithoutLimit_RoundTripToResume_SuppliedLimitRefused(t *testing.T) {
	created := createdRunSettings(t, 0)
	ses, f, store, cfg := newResumeOfLinear(t, runnerCreatedState(created))
	cfg.RunSettings = created
	cfg.ReviewLoopLimit = 3
	cfg.Supplied.ReviewLoopLimit = true

	got, err := ses.Start(context.Background(), cfg)

	msg := requireRefused(t, got, err)
	if !strings.Contains(msg, "review_loop_limit") {
		t.Errorf("refusal %q must name review_loop_limit", msg)
	}
	requireNoDispatchOrWrite(t, f, store)
}

// createdRunSettings runs a new linear run and returns the runner settings the
// artifact recorded for it.
func createdRunSettings(t *testing.T, limit int) domain.RunSettings {
	t.Helper()
	ses, f, store, orchPath := newLinearSession(t)
	f.Queue("agent-a", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-a#1", StatusCode: domain.StatusSUCCESS, StatusMessage: "done",
	}})
	f.Queue("agent-b", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-b#2", StatusCode: domain.StatusSUCCESS, StatusMessage: "done",
	}})
	cfg := baseLinearConfig(orchPath)
	cfg.Mode = domain.ExecutionModeAuto
	cfg.ManualResolution = true
	cfg.ReviewLoopLimit = limit
	got, err := ses.Start(context.Background(), cfg)
	requireRunStatus(t, got, err, domain.RunCompleted)
	return store.state.RunSettings
}

// ===== First Runner resume of a native-created artifact =====

// The supplied values are recorded once, before any dispatch, with a single
// artifact write.
func TestSession_Start_Resume_NativeArtifact_AdoptsSuppliedSettingsOnceBeforeDispatch(t *testing.T) {
	ses, f, store, cfg := newResumeOfLinear(t, nativeCreatedState())
	adoptedBeforeFirstInvoke := -1
	ses = session.New(session.Deps{
		Harness: &beforeInvokeHarness{delegate: f, before: func(string) {
			if adoptedBeforeFirstInvoke < 0 {
				adoptedBeforeFirstInvoke = len(store.AdoptCalls)
			}
		}},
		Store:    store,
		Clock:    fixedClock{t: epoch},
		Interact: &noopInteraction{},
	})
	cfg.Mode = domain.ExecutionModeAuto
	cfg.PreConsultation = false
	cfg.ManualResolution = true
	cfg.Supplied = domain.SuppliedSettings{Mode: true, PreConsultation: true, ManualResolution: true}

	got, err := ses.Start(context.Background(), cfg)

	requireRunStatus(t, got, err, domain.RunCompleted)
	if len(store.AdoptCalls) != 1 || store.AdoptWrites != 1 {
		t.Fatalf("want exactly one adoption (one write), got %d calls / %d writes", len(store.AdoptCalls), store.AdoptWrites)
	}
	want := adoptCall{Mode: domain.ExecutionModeAuto, PreConsultation: false, ManualResolution: true}
	if store.AdoptCalls[0] != want {
		t.Errorf("adopted %+v, want %+v", store.AdoptCalls[0], want)
	}
	if adoptedBeforeFirstInvoke != 1 {
		t.Errorf("adoption calls seen at the first dispatch = %d, want 1 (adopt before any dispatch)", adoptedBeforeFirstInvoke)
	}
	if store.state.Mode != domain.ExecutionModeAuto || !store.state.ManualResolution {
		t.Errorf("artifact settings after adoption = (%q, manual=%v), want (auto, true)", store.state.Mode, store.state.ManualResolution)
	}
}

// Without a supplied mode the artifact cannot be adopted: refused before any
// dispatch, nothing written, even when the config happens to carry a mode.
func TestSession_Start_Resume_NativeArtifact_ModeNotSupplied_Refused(t *testing.T) {
	for _, cfgMode := range []domain.ExecutionMode{domain.ExecutionModeUnset, domain.ExecutionModeAuto} {
		ses, f, store, cfg := newResumeOfLinear(t, nativeCreatedState())
		cfg.Mode = cfgMode

		got, err := ses.Start(context.Background(), cfg)

		msg := requireRefused(t, got, err)
		if !strings.Contains(msg, "runner_mode") {
			t.Errorf("cfg mode %q: refusal %q must name runner_mode", cfgMode, msg)
		}
		requireNoDispatchOrWrite(t, f, store)
		if len(store.AdoptCalls) != 0 {
			t.Errorf("cfg mode %q: no adoption may be attempted, got %d calls", cfgMode, len(store.AdoptCalls))
		}
	}
}

// Adoption never writes the review loop limit: the native artifact's absent
// value stays effective.
func TestSession_Start_Resume_NativeArtifact_AdoptionLeavesReviewLoopLimitUntouched(t *testing.T) {
	ses, _, store, cfg := newResumeOfLinear(t, nativeCreatedState())
	cfg.Mode = domain.ExecutionModeAuto
	cfg.ReviewLoopLimit = 3 // not supplied: ignored
	cfg.Supplied = domain.SuppliedSettings{Mode: true, PreConsultation: true, ManualResolution: true}

	got, err := ses.Start(context.Background(), cfg)

	requireRunStatus(t, got, err, domain.RunCompleted)
	if store.state.ReviewLoopLimit != 0 {
		t.Errorf("artifact review_loop_limit = %d after adoption, want 0 (adoption never writes it)", store.state.ReviewLoopLimit)
	}
}

// A limit supplied for a native-created artifact that records none is a
// conflict: the artifact is used as it is.
func TestSession_Start_Resume_NativeArtifact_SuppliedReviewLoopLimit_Refused(t *testing.T) {
	ses, f, store, cfg := newResumeOfLinear(t, nativeCreatedState())
	cfg.Mode = domain.ExecutionModeAuto
	cfg.ReviewLoopLimit = 3
	cfg.Supplied = domain.SuppliedSettings{Mode: true, PreConsultation: true, ManualResolution: true, ReviewLoopLimit: true}

	got, err := ses.Start(context.Background(), cfg)

	msg := requireRefused(t, got, err)
	if !strings.Contains(msg, "review_loop_limit") {
		t.Errorf("refusal %q must name review_loop_limit", msg)
	}
	requireNoDispatchOrWrite(t, f, store)
}

// ===== Later resumes =====

// Guard tests: the reuse and preservation tests below assert that a resume does
// not overwrite stored values. They are green until the adoption logic exists
// and protect against that logic overwriting recorded settings.

// Recorded settings are reused: nothing is adopted and unflagged config values
// that differ are ignored.
func TestSession_Start_Resume_RecordedSettings_ReusedWithoutAdoption(t *testing.T) {
	ses, _, store, cfg := newResumeOfLinear(t, runnerCreatedState(domain.RunSettings{
		Mode: domain.ExecutionModeAuto, PreConsultation: false, ManualResolution: true, ReviewLoopLimit: 5,
	}))
	cfg.Mode = domain.ExecutionModeOrchestrated
	cfg.PreConsultation = true
	cfg.ManualResolution = false
	cfg.ReviewLoopLimit = 2

	got, err := ses.Start(context.Background(), cfg)

	requireRunStatus(t, got, err, domain.RunCompleted)
	if len(store.AdoptCalls) != 0 {
		t.Errorf("recorded settings must not be adopted again, got %d calls", len(store.AdoptCalls))
	}
	if store.state.Mode != domain.ExecutionModeAuto || store.state.PreConsultation || !store.state.ManualResolution || store.state.ReviewLoopLimit != 5 {
		t.Errorf("stored settings changed on resume: mode=%q pre=%v manual=%v limit=%d",
			store.state.Mode, store.state.PreConsultation, store.state.ManualResolution, store.state.ReviewLoopLimit)
	}
}

// A supplied value equal to the recorded one is not a conflict.
func TestSession_Start_Resume_RecordedSettings_EqualSuppliedValuesAccepted(t *testing.T) {
	recorded := domain.RunSettings{Mode: domain.ExecutionModeAuto, ManualResolution: true, ReviewLoopLimit: 5}
	ses, _, store, cfg := newResumeOfLinear(t, runnerCreatedState(recorded))
	cfg.RunSettings = recorded
	cfg.Supplied = domain.SuppliedSettings{Mode: true, PreConsultation: true, ManualResolution: true, ReviewLoopLimit: true}

	got, err := ses.Start(context.Background(), cfg)

	requireRunStatus(t, got, err, domain.RunCompleted)
	if len(store.AdoptCalls) != 0 {
		t.Errorf("want no adoption, got %d calls", len(store.AdoptCalls))
	}
}

// A conflicting supplied value is refused, names the field, and leaves the
// artifact and the harness untouched.
func TestSession_Start_Resume_RecordedSettings_ConflictingSuppliedValue_Refused(t *testing.T) {
	recorded := domain.RunSettings{Mode: domain.ExecutionModeAuto, PreConsultation: false, ManualResolution: false, ReviewLoopLimit: 5}
	tests := []struct {
		name      string
		supply    func(cfg *domain.RunConfig)
		wantField string
	}{
		{"mode", func(c *domain.RunConfig) {
			c.Mode = domain.ExecutionModeOrchestrated
			c.Supplied.Mode = true
		}, "runner_mode"},
		{"pre-consultation", func(c *domain.RunConfig) {
			c.PreConsultation = true
			c.Supplied.PreConsultation = true
		}, "runner_pre_consultation"},
		{"manual resolution", func(c *domain.RunConfig) {
			c.ManualResolution = true
			c.Supplied.ManualResolution = true
		}, "runner_manual_resolution"},
		{"review loop limit", func(c *domain.RunConfig) {
			c.ReviewLoopLimit = 2
			c.Supplied.ReviewLoopLimit = true
		}, "review_loop_limit"},
		{"review loop limit to none", func(c *domain.RunConfig) {
			c.ReviewLoopLimit = 0
			c.Supplied.ReviewLoopLimit = true
		}, "review_loop_limit"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ses, f, store, cfg := newResumeOfLinear(t, runnerCreatedState(recorded))
			cfg.RunSettings = recorded
			tc.supply(&cfg)

			got, err := ses.Start(context.Background(), cfg)

			msg := requireRefused(t, got, err)
			if !strings.Contains(msg, tc.wantField) {
				t.Errorf("refusal %q must name %s", msg, tc.wantField)
			}
			requireNoDispatchOrWrite(t, f, store)
			if store.state.RunSettings.Mode != recorded.Mode || store.state.ReviewLoopLimit != recorded.ReviewLoopLimit {
				t.Errorf("stored settings changed by a refused resume: %+v", store.state.RunSettings)
			}
		})
	}
}

// The values recorded by an adoption are immutable for every resume after it.
func TestSession_Start_Resume_AdoptedSettings_ImmutableOnLaterResume(t *testing.T) {
	ses, f, store, cfg := newResumeOfLinear(t, nativeCreatedState())
	cfg.Mode = domain.ExecutionModeAuto
	cfg.Supplied = domain.SuppliedSettings{Mode: true, PreConsultation: true, ManualResolution: true}
	got, err := ses.Start(context.Background(), cfg)
	requireRunStatus(t, got, err, domain.RunCompleted)

	f.Queue("agent-b", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-b#3", StatusCode: domain.StatusSUCCESS, StatusMessage: "done",
	}})
	again := cfg
	again.Mode = domain.ExecutionModeOrchestrated
	again.Supplied = domain.SuppliedSettings{Mode: true}
	got2, err2 := ses.Start(context.Background(), again)

	msg := requireRefused(t, got2, err2)
	if !strings.Contains(msg, "runner_mode") {
		t.Errorf("refusal %q must name runner_mode", msg)
	}
	if store.state.Mode != domain.ExecutionModeAuto {
		t.Errorf("stored mode = %q after a refused change, want auto", store.state.Mode)
	}
	if len(store.AdoptCalls) != 1 {
		t.Errorf("want the single original adoption only, got %d calls", len(store.AdoptCalls))
	}
}

// ===== Review loop limit on resume =====

// A native-created artifact with a limit is used as it is; a Runner-created
// artifact's limit is preserved unchanged across a resume.
func TestSession_Start_Resume_ReviewLoopLimit_PreservedUnchanged(t *testing.T) {
	ses, _, store, cfg := newResumeOfLinear(t, runnerCreatedState(domain.RunSettings{
		Mode: domain.ExecutionModeAuto, ReviewLoopLimit: 7,
	}))
	cfg.ReviewLoopLimit = 1 // not supplied: ignored

	got, err := ses.Start(context.Background(), cfg)

	requireRunStatus(t, got, err, domain.RunCompleted)
	if store.state.ReviewLoopLimit != 7 {
		t.Errorf("review_loop_limit = %d after resume, want 7", store.state.ReviewLoopLimit)
	}
}
