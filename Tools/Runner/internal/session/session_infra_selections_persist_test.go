package session_test

// Tests for persisted infrastructure selections: a new run records the
// gated-class selections in the artifact, and a resumed run reads them back,
// validates them and builds its active-agent filter from them without any
// caller input. Absent, stale and wrong-class selections are refused and never
// replaced by declaration order.

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"mosaic-run/internal/domain"
	"mosaic-run/internal/harness"
	"mosaic-run/internal/session"
)

// resumedMixedState returns the artifact of an interrupted run of the linear
// workflow that has completed agent-a and carries the given persisted
// infrastructure selections. The runner settings are recorded, as they are for
// every run the Runner created.
func resumedMixedState(selections map[string]string) domain.ArtifactState {
	return domain.ArtifactState{
		Type:            "orchestration-artifact",
		Workflow:        "linear",
		WorkflowVersion: "1.0",
		Task:            "existing task",
		GlobalSequence:  1,
		RunSettings: domain.RunSettings{
			Mode:                 domain.ExecutionModeAuto,
			Checkpoints:          true,
			InfraClassSelections: selections,
		},
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

// queueResumedMixedRemainder queues the responses for the rest of the resumed
// run: agent-b, then the infrastructure agents that may fire after it.
func queueResumedMixedRemainder(f *harness.MockAdapter) {
	f.Queue("agent-b", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-b#2", StatusCode: domain.StatusSUCCESS, StatusMessage: "done",
	}})
	f.Queue("checkpoint-manager-git", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "checkpoint-manager-git#3", StatusCode: domain.StatusSUCCESS, StatusMessage: "checkpoint taken",
	}})
	f.Queue("checkpoint-manager-alt", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "checkpoint-manager-alt#3", StatusCode: domain.StatusSUCCESS, StatusMessage: "checkpoint taken",
	}})
	f.Queue("review-agent", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "review-agent#4", StatusCode: domain.StatusSUCCESS, StatusMessage: "review done",
	}})
}

// dispatched reports whether the harness was invoked for the named agent.
func dispatched(f *harness.MockAdapter, name string) bool {
	for _, inv := range f.Invocations() {
		if inv.Agent.Identifier == name {
			return true
		}
	}
	return false
}

// Guard tests: the creation write path already stores the given selections, so
// the two new-run tests below are green from the start. The RED signal for
// persistence is the round-trip test that resumes from the artifact a new run
// wrote.

// A new run records the selections it was configured with.
func TestSession_Start_NewRun_RecordsInfraSelectionsInArtifact(t *testing.T) {
	ses, f, store, orchPath := newMultiClassMixedSession(t)
	f.Queue("agent-a", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-a#1", StatusCode: domain.StatusSUCCESS, StatusMessage: "done",
	}})
	f.Queue("checkpoint-manager-alt", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "checkpoint-manager-alt#2", StatusCode: domain.StatusSUCCESS, StatusMessage: "checkpoint taken",
	}})
	f.Queue("review-agent", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "review-agent#3", StatusCode: domain.StatusSUCCESS, StatusMessage: "review done",
	}})
	f.Queue("agent-b", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-b#4", StatusCode: domain.StatusSUCCESS, StatusMessage: "done",
	}})
	f.Queue("checkpoint-manager-alt", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "checkpoint-manager-alt#5", StatusCode: domain.StatusSUCCESS, StatusMessage: "checkpoint taken",
	}})
	f.Queue("review-agent", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "review-agent#6", StatusCode: domain.StatusSUCCESS, StatusMessage: "review done",
	}})
	cfg := baseLinearConfig(orchPath)
	cfg.Checkpoints = true
	cfg.InfraClassSelections = map[string]string{"checkpoint": "checkpoint-manager-alt"}

	got, err := ses.Start(context.Background(), cfg)

	requireRunStatus(t, got, err, domain.RunCompleted)
	want := map[string]string{"checkpoint": "checkpoint-manager-alt"}
	if !reflect.DeepEqual(store.state.InfraClassSelections, want) {
		t.Errorf("artifact infrastructure_selections = %v, want %v", store.state.InfraClassSelections, want)
	}
}

// A gated class with a single declaration needs no entry: the map stays empty
// and the agent is still active.
func TestSession_Start_NewRun_SingleAgentGatedClass_OmittedFromSelectionsAndActive(t *testing.T) {
	ses, f, store, orchPath := newIntervalAgentSession(t)
	f.Queue("agent-a", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-a#1", StatusCode: domain.StatusSUCCESS, StatusMessage: "done",
	}})
	f.Queue("checkpoint-manager-git", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "checkpoint-manager-git#2", StatusCode: domain.StatusSUCCESS, StatusMessage: "checkpoint taken",
	}})
	f.Queue("agent-b", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-b#3", StatusCode: domain.StatusSUCCESS, StatusMessage: "done",
	}})
	f.Queue("checkpoint-manager-git", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "checkpoint-manager-git#4", StatusCode: domain.StatusSUCCESS, StatusMessage: "checkpoint taken",
	}})
	cfg := baseLinearConfig(orchPath)
	cfg.Checkpoints = true

	got, err := ses.Start(context.Background(), cfg)

	requireRunStatus(t, got, err, domain.RunCompleted)
	if len(store.state.InfraClassSelections) != 0 {
		t.Errorf("artifact infrastructure_selections = %v, want none for a single-agent class", store.state.InfraClassSelections)
	}
	if !dispatched(f, "checkpoint-manager-git") {
		t.Error("the only declared checkpoint agent must stay active without a selection entry")
	}
}

// The selections a new run wrote drive the resume that follows, with no caller
// input: only the selected gated agent is dispatched.
func TestSession_NewRunSelections_RoundTripToResume_OnlySelectedAgentDispatched(t *testing.T) {
	ses, f, store, orchPath := newMultiClassMixedSession(t)
	for i, name := range []string{"agent-a", "checkpoint-manager-alt", "review-agent", "agent-b", "checkpoint-manager-alt", "review-agent"} {
		f.Queue(name, harness.ScriptedEntry{Response: &domain.ProtocolResponse{
			AgentInstanceID: name + "#" + string(rune('1'+i)), StatusCode: domain.StatusSUCCESS, StatusMessage: "ok",
		}})
	}
	newCfg := baseLinearConfig(orchPath)
	newCfg.Checkpoints = true
	newCfg.InfraClassSelections = map[string]string{"checkpoint": "checkpoint-manager-alt"}
	got, err := ses.Start(context.Background(), newCfg)
	requireRunStatus(t, got, err, domain.RunCompleted)
	written := store.state.RunSettings

	resumeSes, rf, rstore, _ := newMultiClassMixedSession(t)
	interrupted := resumedMixedState(nil)
	interrupted.RunSettings = written
	rstore.state = interrupted
	rstore.exists = true
	queueResumedMixedRemainder(rf)
	cfg := baseLinearConfig(orchPath)
	markResume(&cfg)
	cfg.InfraClassSelections = nil

	got2, err2 := resumeSes.Start(context.Background(), cfg)

	requireRunStatus(t, got2, err2, domain.RunCompleted)
	if !dispatched(rf, "checkpoint-manager-alt") {
		t.Error("the selection written by the new run (checkpoint-manager-alt) must be active on resume")
	}
	if dispatched(rf, "checkpoint-manager-git") {
		t.Error("checkpoint-manager-git was not selected and must not be dispatched on resume")
	}
}

// A resumed run uses the persisted selection with no caller input, and only the
// selected gated agent plus every non-gated agent are eligible.
func TestSession_Start_Resume_UsesPersistedInfraSelections_WithoutCallerInput(t *testing.T) {
	ses, f, store, orchPath := newMultiClassMixedSession(t)
	store.state = resumedMixedState(map[string]string{"checkpoint": "checkpoint-manager-git"})
	store.exists = true
	queueResumedMixedRemainder(f)
	cfg := baseLinearConfig(orchPath)
	markResume(&cfg)
	cfg.InfraClassSelections = nil

	got, err := ses.Start(context.Background(), cfg)

	requireRunStatus(t, got, err, domain.RunCompleted)
	if !dispatched(f, "checkpoint-manager-git") {
		t.Error("the persisted selection (checkpoint-manager-git) must be active on resume")
	}
	if dispatched(f, "checkpoint-manager-alt") {
		t.Error("checkpoint-manager-alt was not selected and must not be dispatched on resume")
	}
	if !dispatched(f, "review-agent") {
		t.Error("the non-gated review-agent must stay active on resume")
	}
}

// The persisted selection wins over an unflagged caller value, so the second
// declared agent can be the one that runs.
func TestSession_Start_Resume_PersistedSelectionWinsOverUnsuppliedCallerValue(t *testing.T) {
	ses, f, store, orchPath := newMultiClassMixedSession(t)
	store.state = resumedMixedState(map[string]string{"checkpoint": "checkpoint-manager-alt"})
	store.exists = true
	queueResumedMixedRemainder(f)
	cfg := baseLinearConfig(orchPath)
	markResume(&cfg)
	cfg.InfraClassSelections = map[string]string{"checkpoint": "checkpoint-manager-git"}

	got, err := ses.Start(context.Background(), cfg)

	requireRunStatus(t, got, err, domain.RunCompleted)
	if !dispatched(f, "checkpoint-manager-alt") {
		t.Error("the persisted selection (checkpoint-manager-alt) must be active on resume")
	}
	if dispatched(f, "checkpoint-manager-git") {
		t.Error("an unflagged caller selection must not override the persisted one")
	}
}

// A caller value explicitly supplied on resume that differs from the persisted
// selections is a conflict, and the run is refused before any dispatch.
func TestSession_Start_Resume_SuppliedInfraSelectionsConflict_Refused(t *testing.T) {
	ses, f, store, orchPath := newMultiClassMixedSession(t)
	store.state = resumedMixedState(map[string]string{"checkpoint": "checkpoint-manager-git"})
	store.exists = true
	queueResumedMixedRemainder(f)
	cfg := baseLinearConfig(orchPath)
	markResume(&cfg)
	cfg.InfraClassSelections = map[string]string{"checkpoint": "checkpoint-manager-alt"}
	cfg.Supplied.InfraClassSelections = true

	got, err := ses.Start(context.Background(), cfg)

	msg := requireRefused(t, got, err)
	if !strings.Contains(msg, "infrastructure_selections") {
		t.Errorf("refusal %q must name infrastructure_selections", msg)
	}
	if n := len(f.Invocations()); n != 0 {
		t.Errorf("want zero invocations on a refused resume, got %d", n)
	}
}

// A supplied value equal to the persisted one is not a conflict.
func TestSession_Start_Resume_SuppliedInfraSelectionsEqualToPersisted_Accepted(t *testing.T) {
	ses, f, store, orchPath := newMultiClassMixedSession(t)
	store.state = resumedMixedState(map[string]string{"checkpoint": "checkpoint-manager-git"})
	store.exists = true
	queueResumedMixedRemainder(f)
	cfg := baseLinearConfig(orchPath)
	markResume(&cfg)
	cfg.InfraClassSelections = map[string]string{"checkpoint": "checkpoint-manager-git"}
	cfg.Supplied.InfraClassSelections = true

	got, err := ses.Start(context.Background(), cfg)

	requireRunStatus(t, got, err, domain.RunCompleted)
}

// Persisted selections that fail validation are refused before any dispatch.
// None of them may fall back to declaration order (which would pick
// checkpoint-manager-git).
func TestSession_Start_Resume_InvalidPersistedInfraSelections_Refused(t *testing.T) {
	tests := []struct {
		name       string
		selections map[string]string
		wantNamed  string
	}{
		{"missing selection for a class with several declarations", nil, "checkpoint"},
		{"stale agent name", map[string]string{"checkpoint": "checkpoint-manager-retired"}, "checkpoint-manager-retired"},
		{"agent of another class", map[string]string{"checkpoint": "review-agent"}, "review-agent"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ses, f, store, orchPath := newMultiClassMixedSession(t)
			store.state = resumedMixedState(tc.selections)
			store.exists = true
			queueResumedMixedRemainder(f)
			cfg := baseLinearConfig(orchPath)
			markResume(&cfg)

			got, err := ses.Start(context.Background(), cfg)

			msg := requireRefused(t, got, err)
			if !strings.Contains(msg, tc.wantNamed) {
				t.Errorf("refusal %q must name %q", msg, tc.wantNamed)
			}
			if n := len(f.Invocations()); n != 0 {
				t.Errorf("want zero invocations (no declaration-order fallback), got %d", n)
			}
		})
	}
}

// A caller-supplied map cannot stand in for a missing persisted selection
// unless it is flagged as supplied, and even then it conflicts with the
// artifact rather than filling the gap.
func TestSession_Start_Resume_MissingPersistedSelection_CallerMapDoesNotFillGap(t *testing.T) {
	for _, supplied := range []bool{false, true} {
		ses, f, store, orchPath := newMultiClassMixedSession(t)
		store.state = resumedMixedState(nil)
		store.exists = true
		queueResumedMixedRemainder(f)
		cfg := baseLinearConfig(orchPath)
		markResume(&cfg)
		cfg.InfraClassSelections = map[string]string{"checkpoint": "checkpoint-manager-git"}
		cfg.Supplied.InfraClassSelections = supplied

		got, err := ses.Start(context.Background(), cfg)

		requireRefused(t, got, err)
		if n := len(f.Invocations()); n != 0 {
			t.Errorf("supplied=%v: want zero invocations, got %d", supplied, n)
		}
	}
}

// A resumed run with a single-agent gated class needs no persisted entry.
func TestSession_Start_Resume_SingleAgentGatedClass_NoPersistedEntryNeeded(t *testing.T) {
	ses, f, store, orchPath := newIntervalAgentSession(t)
	state := resumedMixedState(nil)
	store.state = state
	store.exists = true
	f.Queue("agent-b", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-b#2", StatusCode: domain.StatusSUCCESS, StatusMessage: "done",
	}})
	f.Queue("checkpoint-manager-git", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "checkpoint-manager-git#3", StatusCode: domain.StatusSUCCESS, StatusMessage: "checkpoint taken",
	}})
	cfg := baseLinearConfig(orchPath)
	markResume(&cfg)

	got, err := ses.Start(context.Background(), cfg)

	requireRunStatus(t, got, err, domain.RunCompleted)
	if !dispatched(f, "checkpoint-manager-git") {
		t.Error("the single declared checkpoint agent must stay active on resume")
	}
}

// The persisted selections govern the consultation route too: an orchestrated
// resume must not dispatch the non-selected agent when its trigger comes due.
func TestSession_Start_Resume_Orchestrated_PersistedSelectionFiltersInfraAgents(t *testing.T) {
	store := &memStore{}
	dir := t.TempDir()
	orchPath := copyOrchestratorFile(t, dir, "multi-class-mixed-orch.md")
	writeAgentFile(t, dir, "agent-a")
	writeAgentFile(t, dir, "agent-b")
	writeAgentFile(t, dir, "checkpoint-manager-git")
	writeAgentFile(t, dir, "checkpoint-manager-alt")
	writeAgentFile(t, dir, "review-agent")
	f := harness.NewMockAdapter()
	consultant := &scriptedRoutingConsultant{}
	consultant.queueDispatch("agent-b", "finish the work", 1)
	consultant.queueStop("done")
	ses := session.New(session.Deps{
		Harness:  f,
		Store:    store,
		Clock:    fixedClock{t: epoch},
		Interact: &noopInteraction{},
		Routing:  consultant,
	})
	state := resumedMixedState(map[string]string{"checkpoint": "checkpoint-manager-git"})
	state.RunSettings.Mode = domain.ExecutionModeOrchestrated
	store.state = state
	store.exists = true
	queueResumedMixedRemainder(f)
	cfg := baseLinearConfig(orchPath)
	markResume(&cfg)

	got, err := ses.Start(context.Background(), cfg)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Status == domain.RunRefused {
		t.Fatalf("resume was refused (%q); the persisted selection must make it startable", got.Message)
	}
	if !dispatched(f, "agent-b") || !dispatched(f, "checkpoint-manager-git") {
		t.Error("want agent-b and the selected checkpoint-manager-git dispatched in the orchestrated resume")
	}
	if dispatched(f, "checkpoint-manager-alt") {
		t.Error("checkpoint-manager-alt was not selected and must not be dispatched in an orchestrated resume")
	}
}
