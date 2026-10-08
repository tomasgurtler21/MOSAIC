package session_test

// Doubles and assertions shared by the tests that pin when a graceful stop
// takes effect relative to a routing consultation: a stop armed before the
// consultation prevents it, and a decision completed after a stop request is
// discarded visibly.

import (
	"context"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"mosaic-common/interaction"
	"mosaic-run/internal/domain"
	"mosaic-run/internal/harness"
	"mosaic-run/internal/session"
)

// stopFlag is a stop request a test arms at a chosen moment.
type stopFlag struct{ armed atomic.Bool }

func (f *stopFlag) arm()            { f.armed.Store(true) }
func (f *stopFlag) requested() bool { return f.armed.Load() }

// armAfterInvokeHarness delegates to a harness and arms flag after every
// invocation of agent (of any agent when agent is empty), whether the
// invocation succeeded or failed. It models a stop confirmed while that
// agent's step was running.
type armAfterInvokeHarness struct {
	delegate domain.HarnessAdapter
	flag     *stopFlag
	agent    string
}

func (h *armAfterInvokeHarness) Invoke(ctx context.Context, agent domain.AgentReference, request domain.ProtocolRequest) (domain.ProtocolResponse, error) {
	resp, err := h.delegate.Invoke(ctx, agent, request)
	if h.agent == "" || h.agent == agent.Identifier {
		h.flag.arm()
	}
	return resp, err
}

// stopArmingConsultant arms flag when it is first asked to decide, then
// answers like the scripted consultant it wraps. It models a stop confirmed
// while the first consultation was in progress; later consultations (after
// the test disarms the flag for a resume) do not arm it again.
type stopArmingConsultant struct {
	inner *scriptedRoutingConsultant
	flag  *stopFlag
	once  sync.Once
}

func (c *stopArmingConsultant) ConsultRouting(ctx context.Context, req domain.ConsultationRequest) (domain.RoutingInstruction, error) {
	c.once.Do(c.flag.arm)
	return c.inner.ConsultRouting(ctx, req)
}

// queueStopTestSuccess queues one SUCCESS response for agent under instance.
func queueStopTestSuccess(f *harness.MockAdapter, agent, instance string) {
	f.Queue(agent, harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: instance,
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "done",
	}})
}

// invocationCount returns how many times the mock adapter was asked to run
// agent.
func invocationCount(f *harness.MockAdapter, agent string) int {
	n := 0
	for _, inv := range f.Invocations() {
		if inv.Agent.Identifier == agent {
			n++
		}
	}
	return n
}

// requireStopObservedAt asserts the run stopped gracefully and that the debug
// log holds exactly the given stop checkpoints, in order.
func requireStopObservedAt(t *testing.T, got domain.RunOutcome, err error, logger *sessionRecordingLogger, want ...string) {
	t.Helper()
	if err != nil {
		t.Fatalf("want nil error, got %v", err)
	}
	if got.Status != domain.RunStopped {
		t.Fatalf("want RunStopped, got %q (message: %q)", got.Status, got.Message)
	}
	checkpoints := logger.stopObservedCheckpoints()
	if strings.Join(checkpoints, ",") != strings.Join(want, ",") {
		t.Errorf("stop checkpoints observed = %v, want %v (events: %v)", checkpoints, want, logger.allEvents())
	}
}

// requireNoDiscardSurfaced asserts nothing claims a routing decision was
// discarded: a stop before the consultation discards nothing.
func requireNoDiscardSurfaced(t *testing.T, got domain.RunOutcome, logger *sessionRecordingLogger, notices *noticeCapturingInteraction) {
	t.Helper()
	if strings.Contains(got.Message, session.DiscardedRoutingDecisionNote) {
		t.Errorf("outcome message %q mentions a discarded decision, but no decision was made", got.Message)
	}
	if logger.eventLogged(domain.EventSessionConsultDiscarded) {
		t.Errorf("%s was logged, but no decision was made", domain.EventSessionConsultDiscarded)
	}
	for _, n := range notices.allNotices() {
		if n.Level == interaction.NoticeWarning {
			t.Errorf("warning notice %q emitted, but no decision was discarded", n.Message)
		}
	}
}
