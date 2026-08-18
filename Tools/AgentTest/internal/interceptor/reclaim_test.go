package interceptor_test

// Coverage: T12.5/AC12.6 — when this shell reclaims a lock from a dead or
// stale holder, it appends the distinct run-level record that later
// evaluation treats as a state-integrity failure, rather than swallowing the
// signal and continuing as if nothing happened. The reclamation mechanics
// themselves are already tested upstream (internal/runstate); this test
// exercises only that the shell surfaces the consequence.

import (
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"mosaic-agent-test/internal/domain"
)

func TestRun_PreInvocation_ReclaimingADeadHoldersLock_AppendsTheDistinctRunLevelRecord(t *testing.T) {
	id := domain.CollaboratorIdentity{ToolName: "Task", AgentIdentity: "researcher"}
	registry := domain.StubRegistry{TestID: "stage12-example", OnUnmatched: domain.UnmatchedPassthrough}
	h := newHarness(t, baseState(), registry, domain.HarnessCapabilities{SupportsDirectSubstitution: true}, nil)

	deadPID := 999999990
	writeLockFile(t, h.ControlDir, domain.LockInfo{
		PID: deadPID, Host: "nowhere", AcquiredAt: h.Clock.Now(), ControlDir: h.ControlDir,
	})

	native := encodePre(t, id, "corr-1", "researcher#1", "do work")
	reply, exitCode, _ := run(t, h.Config, domain.PhasePre, native)

	if exitCode != 0 {
		t.Fatalf("Run returned exit code %d, want 0", exitCode)
	}
	got := decodeReply(t, reply)
	if got.Kind == "" {
		t.Fatal("expected a valid reply even while reclaiming a dead holder's lock")
	}

	records := readLog(t, h.Log)
	var runRecords []domain.LogRecord
	for _, r := range records {
		if r.Kind == domain.RecordRun && r.Event == domain.RunEventLockReclaimed {
			runRecords = append(runRecords, r)
		}
	}
	if len(runRecords) != 1 {
		t.Fatalf("expected exactly one RunEventLockReclaimed record, got %d: %+v", len(runRecords), records)
	}
	if !strings.Contains(runRecords[0].Detail, strconv.Itoa(deadPID)) {
		t.Errorf("Detail = %q, want it to name the prior holder (PID %d)", runRecords[0].Detail, deadPID)
	}

	// The call decided on top of the reclaimed lock must still have been
	// carried out: reclamation is surfaced as evidence, not treated as a
	// reason to refuse the call.
	state := readState(t, h.Store)
	if state.SequenceCounter != 1 {
		t.Errorf("reclaiming the lock must not prevent the call from being decided: SequenceCounter = %d, want 1", state.SequenceCounter)
	}
}

func TestRun_PreInvocation_ReclaimingAStaleHoldersLock_AppendsTheDistinctRunLevelRecord(t *testing.T) {
	id := domain.CollaboratorIdentity{ToolName: "Task", AgentIdentity: "researcher"}
	registry := domain.StubRegistry{TestID: "stage12-example", OnUnmatched: domain.UnmatchedPassthrough}
	h := newHarness(t, baseState(), registry, domain.HarnessCapabilities{SupportsDirectSubstitution: true}, nil)

	// A live-looking holder (this process's own PID) whose lock has simply
	// aged past the staleness threshold must still be reclaimable —
	// staleness is a standalone reclamation condition, not merely a proxy
	// for liveness.
	writeLockFile(t, h.ControlDir, domain.LockInfo{
		PID: os.Getpid(), Host: "here", AcquiredAt: h.Clock.Now(), ControlDir: h.ControlDir,
	})
	h.Clock.Advance(31 * time.Second) // past the 30s staleness threshold

	native := encodePre(t, id, "corr-1", "researcher#1", "do work")
	_, exitCode, _ := run(t, h.Config, domain.PhasePre, native)

	if exitCode != 0 {
		t.Fatalf("Run returned exit code %d, want 0", exitCode)
	}

	records := readLog(t, h.Log)
	found := false
	for _, r := range records {
		if r.Kind == domain.RecordRun && r.Event == domain.RunEventLockReclaimed {
			found = true
		}
	}
	if !found {
		t.Error("expected a RunEventLockReclaimed record for a lock reclaimed on staleness alone")
	}
}
