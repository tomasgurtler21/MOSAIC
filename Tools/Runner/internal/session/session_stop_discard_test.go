package session_test

// Tests for a stop confirmed during a routing consultation. The user asked the
// run to stop, so the completed decision is not dispatched; it is discarded
// visibly rather than silently: a warning notice naming the agent and the
// workflow row, a debug log entry, and a stop outcome message that says a
// completed routing decision was discarded. The consultation wrote nothing,
// so the artifact is unchanged and a resume derives the decision again.

import (
	"context"
	"strings"
	"testing"

	"mosaic-common/interaction"
	"mosaic-run/internal/domain"
	"mosaic-run/internal/harness"
	"mosaic-run/internal/session"
)

type discardRun struct {
	flag    *stopFlag
	f       *harness.MockAdapter
	store   *memStore
	logger  *sessionRecordingLogger
	notices *noticeCapturingInteraction
	inner   *scriptedRoutingConsultant
	ses     session.Session
}

func newDiscardRun(t *testing.T, ses func(d *discardRun, routing domain.RoutingConsultant) session.Session) *discardRun {
	t.Helper()
	d := &discardRun{
		flag:    &stopFlag{},
		f:       harness.NewMockAdapter(),
		store:   &memStore{},
		logger:  &sessionRecordingLogger{},
		notices: &noticeCapturingInteraction{},
		inner:   &scriptedRoutingConsultant{},
	}
	d.ses = ses(d, &stopArmingConsultant{inner: d.inner, flag: d.flag})
	return d
}

func (d *discardRun) deps(routing domain.RoutingConsultant) session.Deps {
	return session.Deps{
		Harness:       d.f,
		Store:         d.store,
		Routing:       routing,
		Clock:         fixedClock{t: epoch},
		Interact:      d.notices,
		Debug:         d.logger,
		StopRequested: d.flag.requested,
	}
}

// warningNotices returns the warning-level notices emitted so far.
func (d *discardRun) warningNotices() []interaction.Notice {
	var out []interaction.Notice
	for _, n := range d.notices.allNotices() {
		if n.Level == interaction.NoticeWarning {
			out = append(out, n)
		}
	}
	return out
}

func newLinearDiscardRun(t *testing.T) (*discardRun, string) {
	t.Helper()
	dir := t.TempDir()
	orchPath := copyOrchestratorFile(t, dir, "linear-orch.md")
	writeAgentFile(t, dir, "agent-a")
	writeAgentFile(t, dir, "agent-b")
	d := newDiscardRun(t, func(d *discardRun, routing domain.RoutingConsultant) session.Session {
		return session.New(d.deps(routing))
	})
	return d, orchPath
}

func TestSession_StopDuringConsultation_DecisionIsDiscardedNotDispatched(t *testing.T) {
	d, orchPath := newLinearDiscardRun(t)
	d.inner.queueDispatch("agent-a", "do the work", 0)

	got, err := d.ses.Start(context.Background(), baseOrchestratedConfig(orchPath))

	if err != nil {
		t.Fatalf("want nil error, got %v", err)
	}
	if got.Status != domain.RunStopped {
		t.Fatalf("status = %q (message %q), want RunStopped: a graceful stop, not a consultant stop", got.Status, got.Message)
	}
	if n := invocationCount(d.f, "agent-a"); n != 0 {
		t.Errorf("agent-a invoked %d times, want 0: the decision completed after the stop request", n)
	}
	if len(d.store.Applied) != 0 || d.store.state.GlobalSequence != 0 {
		t.Errorf("artifact changed: %d applied steps, global_sequence %d; want untouched by the discarded decision",
			len(d.store.Applied), d.store.state.GlobalSequence)
	}
	for _, n := range d.notices.allNotices() {
		if strings.Contains(n.Message, "status=running") {
			t.Errorf("notice %q announces a running step, but nothing was dispatched", n.Message)
		}
	}
}

func TestSession_StopDuringConsultation_OutcomeMessageSaysDecisionWasDiscarded(t *testing.T) {
	d, orchPath := newLinearDiscardRun(t)
	d.inner.queueDispatch("agent-a", "do the work", 0)

	got, _ := d.ses.Start(context.Background(), baseOrchestratedConfig(orchPath))

	for _, want := range []string{session.DiscardedRoutingDecisionNote, "agent-a", "workflow row 1", "re-derived on resume"} {
		if !strings.Contains(got.Message, want) {
			t.Errorf("outcome message %q does not contain %q", got.Message, want)
		}
	}
	if !strings.Contains(got.Message, "graceful stop") {
		t.Errorf("outcome message %q does not identify the graceful stop", got.Message)
	}
}

func TestSession_StopDuringConsultation_SurfacesWarningNoticeNamingAgentAndRow(t *testing.T) {
	d, orchPath := newLinearDiscardRun(t)
	d.inner.queueDispatch("agent-b", "do the work", 1)

	d.ses.Start(context.Background(), baseOrchestratedConfig(orchPath))

	warnings := d.warningNotices()
	if len(warnings) != 1 {
		t.Fatalf("warning notices = %+v, want exactly one", warnings)
	}
	w := warnings[0]
	if w.Title != "Routing decision discarded" {
		t.Errorf("notice title = %q, want %q", w.Title, "Routing decision discarded")
	}
	for _, want := range []string{"agent-b", "workflow row 2", "discarded", "re-derived on resume"} {
		if !strings.Contains(w.Message, want) {
			t.Errorf("notice message %q does not contain %q", w.Message, want)
		}
	}
}

func TestSession_StopDuringConsultation_LogsDiscardedDecision(t *testing.T) {
	d, orchPath := newLinearDiscardRun(t)
	d.inner.queueDispatch("agent-b", "do the work", 1)

	d.ses.Start(context.Background(), baseOrchestratedConfig(orchPath))

	if !d.logger.eventLogged(domain.EventSessionConsultDiscarded) {
		t.Fatalf("%s not logged (events: %v)", domain.EventSessionConsultDiscarded, d.logger.allEvents())
	}
	for key, want := range map[string]string{"agent": "agent-b", "row": "2", "stage": ""} {
		got, ok := d.logger.fieldValue(domain.EventSessionConsultDiscarded, key)
		if !ok || got != want {
			t.Errorf("%s field %q = %q (present %v), want %q", domain.EventSessionConsultDiscarded, key, got, ok, want)
		}
	}
	got := d.logger.stopObservedCheckpoints()
	if len(got) != 1 || got[0] != session.StopCheckpointConsultDispatch {
		t.Errorf("stop checkpoints = %v, want exactly [%s]", got, session.StopCheckpointConsultDispatch)
	}
}

// A decision for a staged row names the recorded stage too.
func TestSession_StopDuringConsultation_StagedRowNamesTheStage(t *testing.T) {
	dir := scopedTempDir(t)
	orchPath := copyOrchestratorFile(t, dir, "consult-staged-orch.md")
	writeAgentFile(t, dir, "agent-a")
	writeAgentFile(t, dir, "agent-b")
	writeConsultStagedPlan(t, dir)
	d := newDiscardRun(t, func(d *discardRun, routing domain.RoutingConsultant) session.Session {
		return session.New(d.deps(routing))
	})
	d.store.state = consultStagedStage2State()
	d.store.exists = true
	d.inner.queueStagedDispatch("agent-b", "complete stage 2", 1, 2)

	got, err := d.ses.Start(context.Background(), baseConsultStagedConfig(orchPath, dir))

	if err != nil || got.Status != domain.RunStopped {
		t.Fatalf("run = %q (%v), want RunStopped", got.Status, err)
	}
	for _, want := range []string{"agent-b", "workflow row 2", "stage 2"} {
		if !strings.Contains(got.Message, want) {
			t.Errorf("outcome message %q does not contain %q", got.Message, want)
		}
	}
	warnings := d.warningNotices()
	if len(warnings) != 1 || !strings.Contains(warnings[0].Message, "stage 2") {
		t.Errorf("warning notices = %+v, want one naming stage 2", warnings)
	}
	if got, _ := d.logger.fieldValue(domain.EventSessionConsultDiscarded, "stage"); got != "2" {
		t.Errorf("logged stage = %q, want %q", got, "2")
	}
	if len(d.store.Applied) != 0 {
		t.Errorf("applied steps = %d, want 0", len(d.store.Applied))
	}
}

// A consultant stop instruction or failure is not a discarded decision, even
// when a stop is also requested: they end as they always did.
func TestSession_StopDuringConsultation_ConsultantStopInstructionEndsAsConsultantStop(t *testing.T) {
	d, orchPath := newLinearDiscardRun(t)
	d.inner.queueStop("nothing more to do")

	got, err := d.ses.Start(context.Background(), baseOrchestratedConfig(orchPath))

	if err != nil {
		t.Fatalf("want nil error, got %v", err)
	}
	if got.Status != domain.RunStoppedByConsultant {
		t.Errorf("status = %q (message %q), want RunStoppedByConsultant", got.Status, got.Message)
	}
	if len(d.warningNotices()) != 0 || d.logger.eventLogged(domain.EventSessionConsultDiscarded) {
		t.Error("a consultant stop instruction was reported as a discarded decision")
	}
}

func TestSession_StopDuringConsultation_ConsultationFailureEndsAsConsultantStop(t *testing.T) {
	d, orchPath := newLinearDiscardRun(t)
	d.inner.queueError(domain.ConsultFailTransport)

	got, err := d.ses.Start(context.Background(), baseOrchestratedConfig(orchPath))

	if err != nil {
		t.Fatalf("want nil error, got %v", err)
	}
	if got.Status != domain.RunStoppedByConsultant {
		t.Errorf("status = %q (message %q), want RunStoppedByConsultant", got.Status, got.Message)
	}
	if len(d.warningNotices()) != 0 || d.logger.eventLogged(domain.EventSessionConsultDiscarded) {
		t.Error("a consultation failure was reported as a discarded decision")
	}
}

// The discarded decision is re-derived on resume: the same consultation is
// made again and its dispatch runs.
func TestSession_StopDuringConsultation_ResumeDerivesTheDecisionAgain(t *testing.T) {
	d, orchPath := newLinearDiscardRun(t)
	d.inner.queueDispatch("agent-a", "do the work", 0)
	d.inner.queueDispatch("agent-a", "do the work", 0)
	d.inner.queueStop("done for now")
	queueStopTestSuccess(d.f, "agent-a", "agent-a#1")
	first, err := d.ses.Start(context.Background(), baseOrchestratedConfig(orchPath))
	if err != nil || first.Status != domain.RunStopped {
		t.Fatalf("precondition: first run = %q (%v), want RunStopped", first.Status, err)
	}

	d.flag.armed.Store(false)
	cfg := baseOrchestratedConfig(orchPath)
	markResume(&cfg)
	second, err := d.ses.Start(context.Background(), cfg)

	if err != nil {
		t.Fatalf("resume: want nil error, got %v", err)
	}
	if second.Status != domain.RunStoppedByConsultant {
		t.Errorf("resume status = %q (message %q), want the run to continue to the consultant's stop", second.Status, second.Message)
	}
	if n := invocationCount(d.f, "agent-a"); n != 1 {
		t.Errorf("agent-a invoked %d times in total, want 1 (only on resume)", n)
	}
	if len(d.store.Applied) != 1 {
		t.Errorf("applied steps = %d, want 1 (agent-a on resume)", len(d.store.Applied))
	}
}
