package concurrency_test

// Tests for concurrency.Peaks: reconstructing peak simultaneous in-flight
// invocations per declared parallel group by replaying the invocation log's
// start/end records as intervals.
//
// Coverage follows T7.3: a fully sequential run yields a peak of one; a run
// with genuinely overlapping intervals yields the true overlap count
// (AC7.2, the direct regression guard for the first-generation defect,
// which always reported zero); intervals that merely abut do not count as
// concurrent; a start record with no end record is handled without
// crashing or inflating the peak (AC7.4); and peaks are computed per
// declared parallel group, so invocations outside a group never contribute
// to it (AC7.3).
//
// Records mirror the shape internal/intercept actually produces: a start
// record carries Group, Seq and CorrelationToken; the corresponding end
// record carries the same Seq and CorrelationToken but not Group — Peaks
// must therefore correlate an end record to its group through its start
// record, not through a Group field of its own.

import (
	"testing"
	"time"

	"mosaic-agent-test/internal/concurrency"
	"mosaic-agent-test/internal/domain"
)

func at(offsetSeconds float64) time.Time {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	return base.Add(time.Duration(offsetSeconds * float64(time.Second)))
}

func start(seq int, token, group string, ts time.Time) domain.LogRecord {
	return domain.LogRecord{
		Kind:             domain.RecordStart,
		TestID:           "example",
		RunNumber:        1,
		Timestamp:        ts,
		Seq:              seq,
		Group:            group,
		CorrelationToken: token,
	}
}

func end(seq int, token string, ts time.Time) domain.LogRecord {
	return domain.LogRecord{
		Kind:             domain.RecordEnd,
		TestID:           "example",
		RunNumber:        1,
		Timestamp:        ts,
		Seq:              seq,
		CorrelationToken: token,
	}
}

func oneGroup(name string) []domain.ParallelGroup {
	return []domain.ParallelGroup{
		{Name: name, Members: []domain.CollaboratorIdentity{
			{ToolName: "Task", AgentIdentity: "researcher"},
			{ToolName: "Task", AgentIdentity: "library-researcher"},
		}},
	}
}

// --- Sequential run yields a peak of one ---

func TestPeaks_FullySequentialRun_YieldsPeakOfOne(t *testing.T) {
	records := []domain.LogRecord{
		start(1, "t1", "G1", at(0)),
		end(1, "t1", at(1)),
		start(2, "t2", "G1", at(2)),
		end(2, "t2", at(3)),
		start(3, "t3", "G1", at(4)),
		end(3, "t3", at(5)),
	}

	peaks, report := concurrency.Peaks(records, oneGroup("G1"))

	if got := peaks["G1"]; got != 1 {
		t.Errorf(`peaks["G1"] = %d, want 1 for a fully sequential run`, got)
	}
	if len(report.UnterminatedSeqs) != 0 {
		t.Errorf("report.UnterminatedSeqs = %v, want none", report.UnterminatedSeqs)
	}
}

// --- Overlapping intervals yield the true overlap count (AC7.2) ---

// TestPeaks_OverlappingIntervals_YieldsPeakGreaterThanOne is the direct
// regression guard named in AC7.2: the first-generation implementation
// always reported zero here because it computed concurrency at interception
// time against an always-empty parallel-group table.
func TestPeaks_OverlappingIntervals_YieldsPeakGreaterThanOne(t *testing.T) {
	records := []domain.LogRecord{
		start(1, "t1", "G1", at(0)),
		start(2, "t2", "G1", at(1)), // starts before t1 ends
		end(1, "t1", at(3)),
		end(2, "t2", at(4)),
	}

	peaks, _ := concurrency.Peaks(records, oneGroup("G1"))

	if got := peaks["G1"]; got <= 1 {
		t.Fatalf(`peaks["G1"] = %d, want > 1 — this is the first-generation defect (always zero) demonstrably fixed`, got)
	}
	if got := peaks["G1"]; got != 2 {
		t.Errorf(`peaks["G1"] = %d, want exactly 2`, got)
	}
}

func TestPeaks_ThreeMutuallyOverlappingIntervals_YieldsPeakOfThree(t *testing.T) {
	records := []domain.LogRecord{
		start(1, "t1", "G1", at(0)),
		start(2, "t2", "G1", at(1)),
		start(3, "t3", "G1", at(2)),
		end(1, "t1", at(5)),
		end(2, "t2", at(6)),
		end(3, "t3", at(7)),
	}

	peaks, _ := concurrency.Peaks(records, oneGroup("G1"))

	if got := peaks["G1"]; got != 3 {
		t.Errorf(`peaks["G1"] = %d, want 3`, got)
	}
}

// --- Abutting intervals do not count as concurrent ---

func TestPeaks_AbuttingIntervals_DoNotCountAsConcurrent(t *testing.T) {
	// The second invocation starts at the exact instant the first ends —
	// intervals are half-open [start, end), so this is abutment, not
	// overlap.
	shared := at(1)
	records := []domain.LogRecord{
		start(1, "t1", "G1", at(0)),
		end(1, "t1", shared),
		start(2, "t2", "G1", shared),
		end(2, "t2", at(2)),
	}

	peaks, _ := concurrency.Peaks(records, oneGroup("G1"))

	if got := peaks["G1"]; got != 1 {
		t.Errorf(`peaks["G1"] = %d, want 1 — abutting intervals must not be counted as concurrent`, got)
	}
}

// --- Unterminated invocations neither crash nor inflate the peak (AC7.4) ---

func TestPeaks_UnterminatedInvocation_IsExcludedAndReported(t *testing.T) {
	records := []domain.LogRecord{
		start(1, "t1", "G1", at(0)), // never ends — crashed or timed out
		start(2, "t2", "G1", at(1)),
		end(2, "t2", at(2)),
	}

	peaks, report := concurrency.Peaks(records, oneGroup("G1"))

	found := false
	for _, seq := range report.UnterminatedSeqs {
		if seq == 1 {
			found = true
		}
	}
	if !found {
		t.Errorf("report.UnterminatedSeqs = %v, want it to contain seq 1", report.UnterminatedSeqs)
	}
	// The unterminated invocation must not be silently folded into the
	// overlap computation: if it were treated as still running for the rest
	// of the log, the peak would be inflated to 2.
	if got := peaks["G1"]; got != 1 {
		t.Errorf(`peaks["G1"] = %d, want 1 — an unterminated invocation must not inflate the peak`, got)
	}
}

func TestPeaks_UnterminatedInvocation_DoesNotPanic(t *testing.T) {
	records := []domain.LogRecord{
		start(1, "t1", "G1", at(0)),
	}

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("Peaks panicked on an unterminated invocation: %v", r)
		}
	}()

	concurrency.Peaks(records, oneGroup("G1"))
}

// --- Per-group computation (AC7.3) ---

func TestPeaks_ComputesEachDeclaredGroupIndependently(t *testing.T) {
	groups := []domain.ParallelGroup{
		{Name: "G1", Members: []domain.CollaboratorIdentity{{ToolName: "Task", AgentIdentity: "a"}}},
		{Name: "G2", Members: []domain.CollaboratorIdentity{{ToolName: "Task", AgentIdentity: "b"}}},
	}
	records := []domain.LogRecord{
		// G1: sequential, peak 1.
		start(1, "g1-t1", "G1", at(0)),
		end(1, "g1-t1", at(1)),
		start(2, "g1-t2", "G1", at(2)),
		end(2, "g1-t2", at(3)),
		// G2: overlapping, peak 2, running over the exact same wall-clock
		// window as G1's invocations — a group that leaked into another
		// group's count would show up here.
		start(3, "g2-t1", "G2", at(0)),
		start(4, "g2-t2", "G2", at(1)),
		end(3, "g2-t1", at(2)),
		end(4, "g2-t2", at(3)),
	}

	peaks, _ := concurrency.Peaks(records, groups)

	if got := peaks["G1"]; got != 1 {
		t.Errorf(`peaks["G1"] = %d, want 1`, got)
	}
	if got := peaks["G2"]; got != 2 {
		t.Errorf(`peaks["G2"] = %d, want 2 — G1's lower concurrency must not leak into G2's count`, got)
	}
}

func TestPeaks_InvocationsOutsideAnyDeclaredGroup_DoNotContributeToAGroupsPeak(t *testing.T) {
	records := []domain.LogRecord{
		// Overlapping, but ungrouped — must not appear under any group key.
		start(1, "u1", "", at(0)),
		start(2, "u2", "", at(1)),
		end(1, "u1", at(2)),
		end(2, "u2", at(3)),
	}

	peaks, report := concurrency.Peaks(records, oneGroup("G1"))

	if got, ok := peaks["G1"]; ok && got != 0 {
		t.Errorf(`peaks["G1"] = %d, want 0 or absent — ungrouped invocations must not contribute to a declared group's peak`, got)
	}
	if report.Ungrouped != 2 {
		t.Errorf("report.Ungrouped = %d, want 2", report.Ungrouped)
	}
}

// --- Non-interval records are ignored, not fatal ---

func TestPeaks_IgnoresRunAndErrorRecordsWithoutCrashing(t *testing.T) {
	records := []domain.LogRecord{
		start(1, "t1", "G1", at(0)),
		end(1, "t1", at(1)),
		{Kind: domain.RecordRun, TestID: "example", RunNumber: 1, Timestamp: at(0.5), Event: domain.RunEventLockReclaimed},
		{Kind: domain.RecordError, TestID: "example", RunNumber: 1, Timestamp: at(0.5), Seq: 99},
	}

	peaks, _ := concurrency.Peaks(records, oneGroup("G1"))

	if got := peaks["G1"]; got != 1 {
		t.Errorf(`peaks["G1"] = %d, want 1 — run/error records must not be mistaken for interval endpoints`, got)
	}
}
