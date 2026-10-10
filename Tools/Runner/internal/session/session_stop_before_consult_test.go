package session_test

// Tests that a graceful stop requested before a routing consultation begins
// takes effect before the consultation: no consultant (orchestrator or
// manual) is called, nothing is written, and the stop is logged at the
// consultation-entry checkpoint. The consultation writes nothing to the
// artifact, so a resume re-derives the same decision and no work is lost.
//
// Every entry point is covered: a deviation after a dispatch, an
// orchestrated-mode consultation, a harness failure, a review consultation
// after infrastructure agents, and a manual dispatch.

import (
	"context"
	"errors"
	"testing"

	"mosaic-run/internal/domain"
	"mosaic-run/internal/harness"
	"mosaic-run/internal/session"
)

// stopBeforeConsultRun is one session run with a stop flag, a routing
// consultant that must not be called, and the doubles needed to inspect it.
type stopBeforeConsultRun struct {
	flag       *stopFlag
	f          *harness.MockAdapter
	store      *memStore
	consultant *scriptedRoutingConsultant
	logger     *sessionRecordingLogger
	notices    *noticeCapturingInteraction
	orchPath   string
	ses        session.Session
}

// newStopBeforeConsultRun builds a session over fixture whose agents are
// agentNames. The stop is armed by the harness after armAgent is invoked
// (never armed when armAgent is empty); the consultant is scripted by script.
func newStopBeforeConsultRun(t *testing.T, fixture, armAgent string, script func(c *scriptedRoutingConsultant), agentNames ...string) *stopBeforeConsultRun {
	t.Helper()
	dir := t.TempDir()
	r := &stopBeforeConsultRun{
		flag:       &stopFlag{},
		f:          harness.NewMockAdapter(),
		store:      &memStore{},
		consultant: &scriptedRoutingConsultant{},
		logger:     &sessionRecordingLogger{},
		notices:    &noticeCapturingInteraction{},
		orchPath:   copyOrchestratorFile(t, dir, fixture),
	}
	for _, name := range agentNames {
		writeAgentFile(t, dir, name)
	}
	if script != nil {
		script(r.consultant)
	}
	var h domain.HarnessAdapter = r.f
	if armAgent != "" {
		h = &armAfterInvokeHarness{delegate: r.f, flag: r.flag, agent: armAgent}
	}
	r.ses = session.New(session.Deps{
		Harness:       h,
		Store:         r.store,
		Routing:       r.consultant,
		Clock:         fixedClock{t: epoch},
		Interact:      r.notices,
		Debug:         r.logger,
		StopRequested: r.flag.requested,
	})
	return r
}

// A dispatch that ends in a deviation, with a stop confirmed during that
// dispatch, stops before the deviation is put to the consultant.
func TestSession_StopArmedDuringDispatch_DeviationIsNotConsulted(t *testing.T) {
	r := newStopBeforeConsultRun(t, "linear-orch.md", "agent-a", nil, "agent-a", "agent-b")
	r.f.Queue("agent-a", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-a#1",
		StatusCode:      domain.StatusBLOCKED,
		ErrorCode:       domain.ErrorINVALID_INVOCATION,
		StatusMessage:   "cannot proceed",
	}})

	got, err := r.ses.Start(context.Background(), baseLinearConfig(r.orchPath))

	requireStopObservedAt(t, got, err, r.logger, session.StopCheckpointConsultEntry)
	requireNoDiscardSurfaced(t, got, r.logger, r.notices)
	if r.consultant.CallCount != 0 {
		t.Errorf("consultant called %d times, want 0: the stop was requested before the consultation", r.consultant.CallCount)
	}
	if len(r.store.Applied) != 1 {
		t.Errorf("applied steps = %d, want 1 (only the blocked dispatch; the stop must leave the artifact as the step left it)", len(r.store.Applied))
	}
	if n := invocationCount(r.f, "agent-b"); n != 0 {
		t.Errorf("agent-b invoked %d times, want 0", n)
	}
}

// In orchestrated mode every step is a consultation, so a stop confirmed
// while a routed step ran stops before the next consultation.
func TestSession_StopArmedDuringRoutedStep_NextConsultationIsNotMade(t *testing.T) {
	r := newStopBeforeConsultRun(t, "linear-orch.md", "agent-a",
		func(c *scriptedRoutingConsultant) { c.queueDispatch("agent-a", "do the work", 0) },
		"agent-a", "agent-b")
	queueStopTestSuccess(r.f, "agent-a", "agent-a#1")

	got, err := r.ses.Start(context.Background(), baseOrchestratedConfig(r.orchPath))

	requireStopObservedAt(t, got, err, r.logger, session.StopCheckpointConsultEntry)
	requireNoDiscardSurfaced(t, got, r.logger, r.notices)
	if r.consultant.CallCount != 1 {
		t.Errorf("consultant called %d times, want 1 (the consultation before the stop only)", r.consultant.CallCount)
	}
	if len(r.store.Applied) != 1 {
		t.Errorf("applied steps = %d, want 1 (agent-a only)", len(r.store.Applied))
	}
}

// A stop already in force when the run reaches its first consultation stops
// the run without consulting.
func TestSession_StopRequestedBeforeFirstConsultation_NoConsultation(t *testing.T) {
	r := newStopBeforeConsultRun(t, "linear-orch.md", "",
		func(c *scriptedRoutingConsultant) { c.queueDispatch("agent-a", "do the work", 0) },
		"agent-a", "agent-b")
	r.flag.arm()

	got, err := r.ses.Start(context.Background(), baseOrchestratedConfig(r.orchPath))

	requireStopObservedAt(t, got, err, r.logger, session.StopCheckpointConsultEntry)
	requireNoDiscardSurfaced(t, got, r.logger, r.notices)
	if r.consultant.CallCount != 0 {
		t.Errorf("consultant called %d times, want 0", r.consultant.CallCount)
	}
	if len(r.store.Applied) != 0 || r.store.state.GlobalSequence != 0 {
		t.Errorf("artifact changed: %d applied steps, global_sequence %d; want untouched",
			len(r.store.Applied), r.store.state.GlobalSequence)
	}
}

// A harness failure is followed by a consultation in orchestrated mode; a stop
// confirmed while the failing step ran stops before that consultation.
func TestSession_StopArmedDuringFailingDispatch_HarnessErrorIsNotConsulted(t *testing.T) {
	r := newStopBeforeConsultRun(t, "linear-orch.md", "agent-a",
		func(c *scriptedRoutingConsultant) { c.queueDispatch("agent-a", "do the work", 0) },
		"agent-a", "agent-b")
	r.f.Queue("agent-a", harness.ScriptedEntry{Err: errors.New("harness exploded")})

	got, err := r.ses.Start(context.Background(), baseOrchestratedConfig(r.orchPath))

	requireStopObservedAt(t, got, err, r.logger, session.StopCheckpointConsultEntry)
	requireNoDiscardSurfaced(t, got, r.logger, r.notices)
	if r.consultant.CallCount != 1 {
		t.Errorf("consultant called %d times, want 1 (the routing of the failing dispatch only)", r.consultant.CallCount)
	}
	if n := invocationCount(r.f, "agent-a"); n != 1 {
		t.Errorf("agent-a invoked %d times, want exactly 1", n)
	}
	if n := invocationCount(r.f, "agent-b"); n != 0 {
		t.Errorf("agent-b invoked %d times, want 0", n)
	}
}

// The consultation after a successful review-class infrastructure pass is
// one more entry point.
func TestSession_StopArmedDuringReviewPass_ReviewConsultationIsNotMade(t *testing.T) {
	r := newStopBeforeConsultRun(t, "review-class-orch.md", "review-agent-b", nil,
		"agent-a", "agent-b", "review-agent-a", "review-agent-b")
	queueStopTestSuccess(r.f, "agent-a", "agent-a#1")
	queueStopTestSuccess(r.f, "review-agent-a", "review-agent-a#2")
	queueStopTestSuccess(r.f, "review-agent-b", "review-agent-b#3")

	got, err := r.ses.Start(context.Background(), baseLinearConfig(r.orchPath))

	requireStopObservedAt(t, got, err, r.logger, session.StopCheckpointConsultEntry)
	requireNoDiscardSurfaced(t, got, r.logger, r.notices)
	if r.consultant.CallCount != 0 {
		t.Errorf("consultant called %d times, want 0: the review consultation must not start after a stop", r.consultant.CallCount)
	}
	if n := invocationCount(r.f, "agent-b"); n != 0 {
		t.Errorf("agent-b invoked %d times, want 0", n)
	}
}

// The manual resolver is a consultant too: a stop requested before a manual
// dispatch prompt prevents the prompt.
func TestSession_StopRequestedBeforeManualDispatch_ManualResolverNotCalled(t *testing.T) {
	dir := t.TempDir()
	orchPath := copyOrchestratorFile(t, dir, "linear-orch.md")
	writeAgentFile(t, dir, "agent-a")
	writeAgentFile(t, dir, "agent-b")
	flag := &stopFlag{}
	flag.arm()
	logger := &sessionRecordingLogger{}
	routing := &scriptedRoutingConsultant{}
	manual := &scriptedRoutingConsultant{}
	manual.queueDispatch("agent-a", "do the work", 0)
	ses := session.New(session.Deps{
		Harness:       harness.NewMockAdapter(),
		Store:         &memStore{},
		Routing:       routing,
		Manual:        manual,
		Clock:         fixedClock{t: epoch},
		Interact:      &noopInteraction{},
		Debug:         logger,
		StopRequested: flag.requested,
	})
	cfg := baseOrchestratedConfig(orchPath)
	cfg.ManualDispatch = true

	got, err := ses.Start(context.Background(), cfg)

	requireStopObservedAt(t, got, err, logger, session.StopCheckpointConsultEntry)
	if manual.CallCount != 0 || routing.CallCount != 0 {
		t.Errorf("consultants called (manual %d, orchestrator %d), want neither", manual.CallCount, routing.CallCount)
	}
}

// After the stop, resuming derives the same decision again, so the skipped
// consultation costs nothing but the wait.
func TestSession_StopBeforeConsultation_ResumeConsultsAndContinues(t *testing.T) {
	r := newStopBeforeConsultRun(t, "linear-orch.md", "agent-a",
		func(c *scriptedRoutingConsultant) {
			c.queueDispatch("agent-a", "do the work", 0)
			c.queueStop("resumed and consulted")
		},
		"agent-a", "agent-b")
	queueStopTestSuccess(r.f, "agent-a", "agent-a#1")
	first, err := r.ses.Start(context.Background(), baseOrchestratedConfig(r.orchPath))
	if err != nil || first.Status != domain.RunStopped {
		t.Fatalf("precondition: first run = %q (%v), want RunStopped", first.Status, err)
	}

	r.flag.armed.Store(false)
	cfg := baseOrchestratedConfig(r.orchPath)
	markResume(&cfg)
	second, err := r.ses.Start(context.Background(), cfg)

	if err != nil {
		t.Fatalf("resume: want nil error, got %v", err)
	}
	if second.Status != domain.RunStoppedByConsultant {
		t.Errorf("resume status = %q (message %q), want the consultation to be made and stop the run", second.Status, second.Message)
	}
	if r.consultant.CallCount != 2 {
		t.Errorf("consultant called %d times in total, want 2 (one before the stop, one on resume)", r.consultant.CallCount)
	}
}
