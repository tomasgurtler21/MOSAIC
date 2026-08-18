package evaluate_test

// Tests proving the degraded-extraction condition is raised from records
// that actually crossed the invocation-log file boundary, not from values
// hand-built in memory. Every other test in this package constructs a
// domain.TaskMessage directly, which is exactly the gap that let
// TaskMessage.Raw and .Extraction arrive zero-valued in production while
// every in-memory unit test still passed: these tests round-trip through
// invlog first, the way the interceptor process and the runner actually
// communicate.

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mosaic-agent-test/internal/domain"
	"mosaic-agent-test/internal/evaluate"
	"mosaic-agent-test/internal/invlog"
)

// readBackRecords appends records through invlog and reads them back,
// returning what a caller downstream of the log actually observes.
func readBackRecords(t *testing.T, records ...domain.LogRecord) []domain.LogRecord {
	t.Helper()
	path := filepath.Join(t.TempDir(), "invocations.jsonl")
	log := invlog.NewLog(path)
	if err := log.Append(records...); err != nil {
		t.Fatalf("Append returned unexpected error: %v", err)
	}
	got, report, err := log.Read()
	if err != nil {
		t.Fatalf("Read returned unexpected error: %v", err)
	}
	if report.TrailingPartialRecord || len(report.MalformedLines) != 0 {
		t.Fatalf("Read reported an anomaly for a clean write: %+v", report)
	}
	return got
}

func TestEvaluate_DegradedExtractionCondition_FiresFromLogReadRecords(t *testing.T) {
	// Arrange: a start record whose message could not be parsed, appended
	// to and read back off the invocation log — not hand-built in memory.
	start := time.Date(2026, 8, 7, 12, 0, 0, 0, time.UTC)
	degradedStart := domain.LogRecord{
		Kind:      domain.RecordStart,
		TestID:    "example",
		RunNumber: 1,
		Seq:       1,
		Ordinal:   1,
		Identity:  researcher(),
		Timestamp: start,
		Outcome:   domain.OutcomeRewritePrompt,
		Message:   &domain.TaskMessage{Raw: "not valid json", Extraction: domain.ExtractionDegraded},
	}
	end := domain.LogRecord{
		Kind: domain.RecordEnd, TestID: "example", RunNumber: 1, Seq: 1, Ordinal: 1,
		Identity: researcher(), Timestamp: start.Add(time.Second), Echo: &domain.EchoOutcome{Match: true},
	}

	ev := baseEvidence()
	ev.Records = readBackRecords(t, degradedStart, end)

	// Act
	got := evaluate.Evaluate(ev)

	// Assert
	if !hasConditionKind(got.Conditions, domain.ConditionExtractionDegraded) {
		t.Errorf("Conditions = %+v, want ConditionExtractionDegraded — the log-read record's message was only degraded-recovered, not cleanly parsed", got.Conditions)
	}
}

func TestEvaluate_CleanlyParsedExtraction_StaysSilentAfterLogReadRoundTrip(t *testing.T) {
	// Arrange: a start record whose message parsed cleanly, put through the
	// same append-and-read cycle as the degraded case above, so a false
	// positive on every read-back record (rather than only on degraded
	// ones) would be caught here.
	start := time.Date(2026, 8, 7, 12, 0, 0, 0, time.UTC)
	msg := plainMessage("researcher#1")
	msg.Extraction = domain.ExtractionParsed
	cleanStart := domain.LogRecord{
		Kind:      domain.RecordStart,
		TestID:    "example",
		RunNumber: 1,
		Seq:       1,
		Ordinal:   1,
		Identity:  researcher(),
		Timestamp: start,
		Outcome:   domain.OutcomeRewritePrompt,
		Message:   &msg,
	}
	end := domain.LogRecord{
		Kind: domain.RecordEnd, TestID: "example", RunNumber: 1, Seq: 1, Ordinal: 1,
		Identity: researcher(), Timestamp: start.Add(time.Second), Echo: &domain.EchoOutcome{Match: true},
	}

	ev := baseEvidence()
	ev.Records = readBackRecords(t, cleanStart, end)

	// Act
	got := evaluate.Evaluate(ev)

	// Assert
	if hasConditionKind(got.Conditions, domain.ConditionExtractionDegraded) {
		t.Errorf("Conditions = %+v, a cleanly parsed message must not raise ConditionExtractionDegraded even after crossing the log", got.Conditions)
	}
}

// --- T10.4 — the no-logs signal ---
//
// A run that started but whose queried log root held no logs raises
// domain.ConditionNoLogsProduced, distinct from ConditionCostUnattributed
// (about the cost delegate) and from ConditionRunNotStarted (raised outside
// evaluate entirely, on the path where no evidence exists at all — see the
// suite package's tests for that path).

func TestEvaluate_NoLogsProducedCondition_FiresWhenTheRunProducedNoLogs(t *testing.T) {
	ev := baseEvidence()
	ev.LogsProduced = false
	ev.LogRoot = "/sandbox/subject/OrchestrationLogs/20260809T171229Z-79ca"

	got := evaluate.Evaluate(ev)

	if !hasConditionKind(got.Conditions, domain.ConditionNoLogsProduced) {
		t.Fatalf("Conditions = %+v, want ConditionNoLogsProduced for a run that produced no logs", got.Conditions)
	}
	cond := findCondition(t, got.Conditions, domain.ConditionNoLogsProduced)
	if !strings.Contains(cond.Detail, ev.LogRoot) {
		t.Errorf("ConditionNoLogsProduced.Detail = %q, want it to name the queried log root %q", cond.Detail, ev.LogRoot)
	}
}

func TestEvaluate_LogsProducedTrue_StaysSilentOnTheNoLogsCondition(t *testing.T) {
	ev := baseEvidence()
	ev.LogsProduced = true

	got := evaluate.Evaluate(ev)

	if hasConditionKind(got.Conditions, domain.ConditionNoLogsProduced) {
		t.Errorf("Conditions = %+v, a run that produced logs must not raise ConditionNoLogsProduced", got.Conditions)
	}
}

// TestEvaluate_NoLogsProducedAndCostUnattributed_AreDistinctConditions pins
// that the two conditions are independent: a run that produced no logs (this
// stage's diagnosis) is not silently folded into "the cost delegate could not
// attribute cost" (Stage 9's diagnosis), even though both leave cost
// unattributed on the same result.
func TestEvaluate_NoLogsProducedAndCostUnattributed_AreDistinctConditions(t *testing.T) {
	ev := baseEvidence()
	ev.LogsProduced = false
	ev.LogRoot = "/sandbox/subject/OrchestrationLogs/20260809T171229Z-79ca"
	ev.Cost = domain.CostReport{Attribution: domain.AttributionUnavailable, Detail: "cost: delegate could not be invoked"}

	got := evaluate.Evaluate(ev)

	if !hasConditionKind(got.Conditions, domain.ConditionNoLogsProduced) {
		t.Errorf("Conditions = %+v, want ConditionNoLogsProduced reported independently of the cost delegate's own failure", got.Conditions)
	}
	if !hasConditionKind(got.Conditions, domain.ConditionCostUnattributed) {
		t.Errorf("Conditions = %+v, want ConditionCostUnattributed still reported alongside it", got.Conditions)
	}
}
