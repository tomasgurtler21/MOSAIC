package invlog_test

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"mosaic-agent-test/internal/domain"
	"mosaic-agent-test/internal/invlog"
)

func TestAppendThenRead_RoundTripsEveryRecordKind(t *testing.T) {
	// Arrange
	path := filepath.Join(t.TempDir(), "invocations.jsonl")
	log := invlog.NewLog(path)
	start := domain.LogRecord{Kind: domain.RecordStart, TestID: "t", RunNumber: 1, Seq: 1, Outcome: domain.OutcomePassthrough}
	end := domain.LogRecord{Kind: domain.RecordEnd, TestID: "t", RunNumber: 1, Seq: 1, Echo: &domain.EchoOutcome{Match: true}}
	errRec := domain.LogRecord{Kind: domain.RecordError, TestID: "t", RunNumber: 1, Seq: 2, Detail: "boom"}
	run := domain.LogRecord{Kind: domain.RecordRun, TestID: "t", RunNumber: 1, Event: domain.RunEventLockReclaimed}

	// Act
	if err := log.Append(start, end, errRec, run); err != nil {
		t.Fatalf("Append returned unexpected error: %v", err)
	}
	records, report, err := log.Read()

	// Assert
	if err != nil {
		t.Fatalf("Read returned unexpected error: %v", err)
	}
	if report.TrailingPartialRecord {
		t.Fatalf("expected no trailing partial record, got report %+v", report)
	}
	if len(records) != 4 {
		t.Fatalf("expected 4 records, got %d: %+v", len(records), records)
	}
	kinds := map[domain.RecordKind]bool{}
	for _, r := range records {
		kinds[r.Kind] = true
	}
	for _, want := range []domain.RecordKind{domain.RecordStart, domain.RecordEnd, domain.RecordError, domain.RecordRun} {
		if !kinds[want] {
			t.Fatalf("expected a record of kind %q in %+v", want, records)
		}
	}
}

func TestAppend_ConcurrentWritersProduceNoInterleavedOrTruncatedRecords(t *testing.T) {
	// Arrange
	path := filepath.Join(t.TempDir(), "invocations.jsonl")
	log := invlog.NewLog(path)

	const writers = 25
	var wg sync.WaitGroup

	// Act: many short-lived interceptor-process-like writers appending
	// concurrently to the same file.
	for i := 0; i < writers; i++ {
		wg.Add(1)
		go func(seq int) {
			defer wg.Done()
			rec := domain.LogRecord{Kind: domain.RecordStart, TestID: "t", RunNumber: 1, Seq: seq, Timestamp: time.Now()}
			if err := log.Append(rec); err != nil {
				t.Errorf("Append returned unexpected error: %v", err)
			}
		}(i)
	}
	wg.Wait()

	// Assert
	records, report, err := log.Read()
	if err != nil {
		t.Fatalf("Read returned unexpected error: %v", err)
	}
	if len(report.MalformedLines) != 0 {
		t.Fatalf("expected no malformed (interleaved/truncated) lines, got %v", report.MalformedLines)
	}
	if len(records) != writers {
		t.Fatalf("expected %d records with none lost to interleaving, got %d", writers, len(records))
	}
	seen := map[int]bool{}
	for _, r := range records {
		if seen[r.Seq] {
			t.Fatalf("duplicate seq %d: a torn write likely duplicated a record", r.Seq)
		}
		seen[r.Seq] = true
	}
}

func TestRead_ToleratesATrailingPartialRecordLeftByACrashMidAppend(t *testing.T) {
	// Arrange: one well-formed line, then a line that was cut off mid-write.
	path := filepath.Join(t.TempDir(), "invocations.jsonl")
	whole := `{"kind":"start","test_id":"t","run_number":1,"seq":1}` + "\n"
	partial := `{"kind":"start","test_id":"t","run_number":1,"se`
	if err := os.WriteFile(path, []byte(whole+partial), 0o644); err != nil {
		t.Fatalf("failed to seed the log file: %v", err)
	}
	log := invlog.NewLog(path)

	// Act
	records, report, err := log.Read()

	// Assert
	if err != nil {
		t.Fatalf("a trailing partial record must be tolerated, not returned as an error: %v", err)
	}
	if !report.TrailingPartialRecord {
		t.Fatal("expected TrailingPartialRecord to be true")
	}
	if len(records) != 1 {
		t.Fatalf("expected the one well-formed record to still be readable, got %d: %+v", len(records), records)
	}
}

func TestRead_SkipsAndReportsAMalformedInteriorLine(t *testing.T) {
	// Arrange: a malformed line in the middle of the file, not the trailing
	// line, so this exercises ReadReport.MalformedLines rather than the
	// trailing-partial-record tolerance covered elsewhere.
	path := filepath.Join(t.TempDir(), "invocations.jsonl")
	line1 := `{"kind":"start","test_id":"t","run_number":1,"seq":1}` + "\n"
	malformed := `not valid json at all` + "\n"
	line3 := `{"kind":"end","test_id":"t","run_number":1,"seq":2}` + "\n"
	if err := os.WriteFile(path, []byte(line1+malformed+line3), 0o644); err != nil {
		t.Fatalf("failed to seed the log file: %v", err)
	}
	log := invlog.NewLog(path)

	// Act
	records, report, err := log.Read()

	// Assert
	if err != nil {
		t.Fatalf("a malformed interior line must be skipped and reported, not returned as an error: %v", err)
	}
	if len(report.MalformedLines) != 1 || report.MalformedLines[0] != 2 {
		t.Fatalf("expected MalformedLines to report the 1-based line number [2], got %v", report.MalformedLines)
	}
	if report.TrailingPartialRecord {
		t.Fatal("an interior malformed line is not a trailing partial record")
	}
	if len(records) != 2 {
		t.Fatalf("expected the two well-formed records to still be readable, got %d: %+v", len(records), records)
	}
}

func TestFilter_SelectsOnlyTheRecordsForOneTestAttempt(t *testing.T) {
	// Arrange
	target := domain.LogRecord{Kind: domain.RecordStart, TestID: "target", RunNumber: 2, Seq: 1}
	otherTest := domain.LogRecord{Kind: domain.RecordStart, TestID: "other", RunNumber: 2, Seq: 1}
	otherRun := domain.LogRecord{Kind: domain.RecordStart, TestID: "target", RunNumber: 1, Seq: 1}
	all := []domain.LogRecord{target, otherTest, otherRun}

	// Act
	filtered := invlog.Filter(all, "target", 2)

	// Assert
	if len(filtered) != 1 {
		t.Fatalf("expected exactly 1 matching record, got %d: %+v", len(filtered), filtered)
	}
	if filtered[0].TestID != "target" || filtered[0].RunNumber != 2 {
		t.Fatalf("Filter returned a non-matching record: %+v", filtered[0])
	}
}

func TestFilter_ReturnsEmptyWhenNothingMatches(t *testing.T) {
	// Arrange
	all := []domain.LogRecord{{Kind: domain.RecordStart, TestID: "other", RunNumber: 1, Seq: 1}}

	// Act
	filtered := invlog.Filter(all, "target", 1)

	// Assert
	if len(filtered) != 0 {
		t.Fatalf("expected no matches, got %+v", filtered)
	}
}
