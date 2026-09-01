package interceptor_test

// Coverage: T12.4/AC12.4/AC12.5 — ordering and lock discipline: side effects
// and log appends happen only after the lock-guarded state update has
// released, and the reply is emitted only after both, so a subject acting
// immediately on the reply cannot outrun the files its stub declared or the
// record describing what happened.
//
// This is asserted by wrapping the real collaborators in recording
// decorators that append to one shared, ordered event log, rather than by
// timing the lock hold directly — the observable property is relative
// ordering, not duration.

import (
	"bytes"
	"context"
	"encoding/json"
	"sync"
	"testing"

	"mosaic-agent-test/internal/domain"
	"mosaic-agent-test/internal/interceptor"
	"mosaic-agent-test/internal/invlog"
	"mosaic-agent-test/internal/runstate"
	"mosaic-agent-test/internal/sideeffects"
)

type eventLog struct {
	mu     sync.Mutex
	events []string
}

func (e *eventLog) add(name string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.events = append(e.events, name)
}

func (e *eventLog) indexOf(name string) int {
	e.mu.Lock()
	defer e.mu.Unlock()
	for i, ev := range e.events {
		if ev == name {
			return i
		}
	}
	return -1
}

func (e *eventLog) snapshot() []string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]string{}, e.events...)
}

type recordingStore struct {
	inner runstate.Store
	log   *eventLog
}

func (s recordingStore) Initialize(st domain.RunState) error { return s.inner.Initialize(st) }
func (s recordingStore) Read() (domain.RunState, error)      { return s.inner.Read() }
func (s recordingStore) Update(mutate func(domain.RunState) (domain.RunState, error)) (runstate.UpdateResult, error) {
	s.log.add("store.Update:start")
	result, err := s.inner.Update(mutate)
	s.log.add("store.Update:end")
	return result, err
}

type recordingApplier struct {
	inner sideeffects.Applier
	log   *eventLog
}

func (a recordingApplier) Apply(subjectDir string, effects []domain.FileEffect, runID string) (domain.EffectLedger, error) {
	a.log.add("effects.Apply")
	return a.inner.Apply(subjectDir, effects, runID)
}

type recordingLog struct {
	inner invlog.Log
	log   *eventLog
}

func (l recordingLog) Append(records ...domain.LogRecord) error {
	l.log.add("log.Append")
	return l.inner.Append(records...)
}
func (l recordingLog) Read() ([]domain.LogRecord, invlog.ReadReport, error) { return l.inner.Read() }

type recordingWriter struct {
	inner *bytes.Buffer
	log   *eventLog
	name  string
}

func (w recordingWriter) Write(p []byte) (int, error) {
	w.log.add(w.name)
	return w.inner.Write(p)
}

func TestRun_PreInvocation_OrdersEffectsAndLogAppendsAfterTheLockAndBeforeTheReply(t *testing.T) {
	id := domain.CollaboratorIdentity{ToolName: "Task", AgentIdentity: "researcher"}
	registry := domain.StubRegistry{
		TestID:      "stage12-example",
		OnUnmatched: domain.UnmatchedHalt,
		Stubs: []domain.Stub{{
			Match:       domain.StubMatch{Identity: id, Invocation: 1},
			Response:    json.RawMessage(`{"status_code":"SUCCESS"}`),
			SideEffects: []domain.FileEffect{{Path: "artifact.md", Content: "hello"}},
		}},
	}
	h := newHarness(t, baseState(), registry, domain.HarnessCapabilities{SupportsDirectSubstitution: true}, nil)

	events := &eventLog{}
	cfg := h.Config
	cfg.State = recordingStore{inner: h.Store, log: events}
	cfg.Log = recordingLog{inner: h.Log, log: events}
	cfg.Effects = recordingApplier{inner: cfg.Effects, log: events}
	cfg.Phase = domain.PhasePre

	native := encodePre(t, id, "corr-1", "researcher#1", "do work")
	var outBuf, diagBuf bytes.Buffer
	cfg.In = bytes.NewReader(native)
	cfg.Out = recordingWriter{inner: &outBuf, log: events, name: "reply.write"}
	cfg.Diag = &diagBuf

	exitCode := interceptor.Run(context.Background(), cfg)

	if exitCode != 0 {
		t.Fatalf("Run returned exit code %d, want 0", exitCode)
	}

	updateEnd := events.indexOf("store.Update:end")
	effectsApply := events.indexOf("effects.Apply")
	logAppend := events.indexOf("log.Append")
	replyWrite := events.indexOf("reply.write")

	if updateEnd < 0 || effectsApply < 0 || logAppend < 0 || replyWrite < 0 {
		t.Fatalf("expected all four events to occur, got %v", events.snapshot())
	}
	if effectsApply < updateEnd {
		t.Errorf("effects.Apply happened before the lock-guarded update released: %v", events.snapshot())
	}
	if logAppend < updateEnd {
		t.Errorf("log.Append happened before the lock-guarded update released: %v", events.snapshot())
	}
	if replyWrite < effectsApply {
		t.Errorf("the reply was emitted before side effects were applied: %v", events.snapshot())
	}
	if replyWrite < logAppend {
		t.Errorf("the reply was emitted before the log record was appended: %v", events.snapshot())
	}
}
