package runstate_test

import (
	"path/filepath"
	"testing"

	"mosaic-agent-test/internal/domain"
	"mosaic-agent-test/internal/invlog"
	"mosaic-agent-test/internal/runstate"
)

// TestReclamation_ProducesADistinctRunLevelInvocationLogRecord exercises the
// consequence that actually matters: reclaiming a
// lock means a state update may have been lost, so every verdict computed
// afterwards rests on possibly-wrong state. runstate.Store.Update reports
// reclamation to its caller and never writes to the invocation log itself
// (runstate must not import invlog); this test plays the caller's role, the
// way the interceptor shell eventually will, and asserts the resulting
// record is a distinct run-level kind that later evaluation can treat as a
// state-integrity failure rather than an ordinary assertion failure.
func TestReclamation_ProducesADistinctRunLevelInvocationLogRecord(t *testing.T) {
	// Arrange
	dir := t.TempDir()
	clock := newFakeClock()
	store := runstate.NewStore(dir, clock)
	if err := store.Initialize(newState()); err != nil {
		t.Fatalf("Initialize returned unexpected error: %v", err)
	}
	deadHolder := domain.LockInfo{PID: 999999994, Host: "nowhere", AcquiredAt: clock.Now(), ControlDir: dir}
	writeLockFile(t, dir, deadHolder)

	log := invlog.NewLog(filepath.Join(dir, domain.InvocationLogName))

	// Act
	result, err := store.Update(func(s domain.RunState) (domain.RunState, error) {
		return s, nil
	})
	if err != nil {
		t.Fatalf("Update returned unexpected error: %v", err)
	}
	if result.LockReclaimed {
		if appendErr := log.Append(domain.LogRecord{
			Kind:   domain.RecordRun,
			TestID: result.State.TestID,
			Event:  domain.RunEventLockReclaimed,
			Detail: "reclaimed from a verifiably dead holder",
		}); appendErr != nil {
			t.Fatalf("Append returned unexpected error: %v", appendErr)
		}
	}

	// Assert
	if !result.LockReclaimed {
		t.Fatal("expected the dead holder's lock to be reclaimed")
	}
	records, _, err := log.Read()
	if err != nil {
		t.Fatalf("Read returned unexpected error: %v", err)
	}
	var runRecords []domain.LogRecord
	for _, r := range records {
		if r.Kind == domain.RecordRun {
			runRecords = append(runRecords, r)
		}
	}
	if len(runRecords) != 1 {
		t.Fatalf("expected exactly one run-level record, got %d: %+v", len(runRecords), records)
	}
	if runRecords[0].Event != domain.RunEventLockReclaimed {
		t.Fatalf("expected the run-level record to carry RunEventLockReclaimed, got %q", runRecords[0].Event)
	}
}
