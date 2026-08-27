package cli_test

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"mosaic-agent-test/internal/cli"
	"mosaic-agent-test/internal/domain"
)

// TestFormatEvent_Grammar checks each event kind against the fixed line
// grammar (ContractsDesign.md, Frontend Models): the cross-frontend
// equivalence test needs this exact surface to compare against.
func TestFormatEvent_Grammar(t *testing.T) {
	tests := []struct {
		name string
		ev   domain.ProgressEvent
		want string
	}{
		{
			name: "suite started",
			ev: domain.ProgressEvent{
				Kind:       domain.ProgressSuiteStarted,
				SuiteID:    "orchestrator-routing",
				TotalTests: 4,
			},
			want: "suite orchestrator-routing started: 4 tests",
		},
		{
			name: "test started",
			ev: domain.ProgressEvent{
				Kind:        domain.ProgressTestStarted,
				TestID:      "happy-path",
				Repetition:  2,
				Repetitions: 3,
			},
			want: "test happy-path [2/3] started",
		},
		{
			name: "invocation",
			ev: domain.ProgressEvent{
				Kind:     domain.ProgressInvocation,
				Seq:      5,
				Identity: domain.CollaboratorIdentity{ToolName: "Task", AgentIdentity: "researcher"},
				Outcome:  domain.OutcomeRewritePrompt,
			},
			want: "  #5 Task/researcher -> rewrite_prompt",
		},
		{
			name: "test finished, pass",
			ev: domain.ProgressEvent{
				Kind:        domain.ProgressTestFinished,
				TestID:      "happy-path",
				Repetition:  1,
				Repetitions: 1,
				Verdict:     domain.VerdictPass,
				Duration:    1500 * time.Millisecond,
				Cost:        domain.CostReport{TotalUSD: 0.42, Attribution: domain.AttributionAttributed},
			},
			want: "test happy-path [1/1] PASS (1.5s, $0.42)",
		},
		{
			name: "test finished, cost not attributed",
			ev: domain.ProgressEvent{
				Kind:        domain.ProgressTestFinished,
				TestID:      "happy-path",
				Repetition:  1,
				Repetitions: 1,
				Verdict:     domain.VerdictFail,
				Duration:    time.Second,
				Cost:        domain.CostReport{Attribution: domain.AttributionUnavailable},
			},
			want: "test happy-path [1/1] FAIL (1s, unavailable)",
		},
		{
			name: "suite finished",
			ev: domain.ProgressEvent{
				Kind:      domain.ProgressSuiteFinished,
				Counts:    map[domain.Verdict]int{domain.VerdictPass: 3, domain.VerdictFail: 1},
				TotalCost: domain.CostReport{TotalUSD: 1.23, Attribution: domain.AttributionAttributed},
			},
			want: "suite finished: FAIL=1 PASS=3 ($1.23)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := cli.FormatEvent(tt.ev)
			if got != tt.want {
				t.Errorf("FormatEvent() = %q, want %q", got, tt.want)
			}
			if strings.HasSuffix(got, "\n") {
				t.Error("FormatEvent must not include a trailing newline")
			}
		})
	}
}

// TestFormatEvent_CostNeverPrintsCurrencyForUnattributed guards the specific
// claim: an unattributed cost renders as its attribution word, never as a
// dollar amount that would read as a genuine (and misleadingly free) zero.
func TestFormatEvent_CostNeverPrintsCurrencyForUnattributed(t *testing.T) {
	for _, attribution := range []domain.CostAttribution{domain.AttributionUnavailable, domain.AttributionUnknownBucket} {
		ev := domain.ProgressEvent{
			Kind:    domain.ProgressTestFinished,
			TestID:  "t",
			Verdict: domain.VerdictPass,
			Cost:    domain.CostReport{TotalUSD: 0, Attribution: attribution},
		}
		got := cli.FormatEvent(ev)
		if strings.Contains(got, "$") {
			t.Errorf("FormatEvent(%s) = %q, must not contain a currency amount", attribution, got)
		}
		if !strings.Contains(got, string(attribution)) {
			t.Errorf("FormatEvent(%s) = %q, must name the attribution", attribution, got)
		}
	}
}

// scriptedSequence is a fixed, self-contained event sequence: it needs no
// suite, no sandbox and no harness to produce line output from — which is
// exactly the property the cross-frontend equivalence test requires.
//
// Per-run events carry a Run field so both the CLI formatter and the TUI
// fold are driven from events with the same shape suite.Suite emits. This
// is the cross-frontend equivalence update for the run-attributed event
// shape: the sequence is the same, the event shape is the new one.
func scriptedSequence() []domain.ProgressEvent {
	run := domain.RunKey{
		RunID:     "20260807T120000Z-0001",
		TestName:  "happy-path",
		RunNumber: 1,
	}
	return []domain.ProgressEvent{
		{Kind: domain.ProgressSuiteStarted, SuiteID: "orchestrator-routing", TotalTests: 1, TotalRuns: 1},
		{Kind: domain.ProgressTestStarted, TestID: "happy-path", Repetition: 1, Repetitions: 1, Run: run},
		{
			Kind:     domain.ProgressInvocation,
			Seq:      1,
			Identity: domain.CollaboratorIdentity{ToolName: "Task", AgentIdentity: "researcher"},
			Outcome:  domain.OutcomeRewritePrompt,
			Run:      run,
		},
		{
			Kind:             domain.ProgressTestFinished,
			TestID:           "happy-path",
			Repetition:       1,
			Repetitions:      1,
			Verdict:          domain.VerdictFail,
			Duration:         2 * time.Second,
			Cost:             domain.CostReport{TotalUSD: 0.10, Attribution: domain.AttributionAttributed},
			FailedAssertions: []string{string(domain.ClassFinalStatus), string(domain.ClassArtifactCreated)},
			Run:              run,
		},
		{
			Kind:      domain.ProgressSuiteFinished,
			Counts:    map[domain.Verdict]int{domain.VerdictFail: 1},
			TotalCost: domain.CostReport{TotalUSD: 0.10, Attribution: domain.AttributionAttributed},
		},
	}
}

// TestFormatEvent_AttributedGrammar verifies the run-attribution prefix for
// per-run events. Every rendered line for a per-run event begins with the
// run identity in square brackets, making lines from interleaved runs
// individually readable. Suite-level events carry no prefix because they
// belong to no single run.
//
// This test references domain.ProgressEvent.Run, a field added additively
// by the implementation phase. Until that field exists the test fails to
// compile. Once the field exists but FormatEvent does not emit the prefix,
// the want strings will not match — the expected RED failure for this test.
func TestFormatEvent_AttributedGrammar(t *testing.T) {
	run := domain.RunKey{
		RunID:     "20260807T120000Z-0001",
		TestName:  "happy-path",
		RunNumber: 1,
	}
	tests := []struct {
		name string
		ev   domain.ProgressEvent
		want string
	}{
		{
			name: "test started with run attribution",
			ev: domain.ProgressEvent{
				Kind:        domain.ProgressTestStarted,
				TestID:      "happy-path",
				Repetition:  1,
				Repetitions: 3,
				Run:         run,
			},
			want: "[20260807T120000Z-0001] test happy-path [1/3] started",
		},
		{
			name: "invocation with run attribution",
			ev: domain.ProgressEvent{
				Kind:     domain.ProgressInvocation,
				Seq:      3,
				Identity: domain.CollaboratorIdentity{ToolName: "Task", AgentIdentity: "planner"},
				Outcome:  domain.OutcomePassthrough,
				Run:      run,
			},
			want: "[20260807T120000Z-0001]   #3 Task/planner -> passthrough",
		},
		{
			name: "test finished with run attribution",
			ev: domain.ProgressEvent{
				Kind:        domain.ProgressTestFinished,
				TestID:      "happy-path",
				Repetition:  1,
				Repetitions: 3,
				Verdict:     domain.VerdictPass,
				Duration:    500 * time.Millisecond,
				Cost:        domain.CostReport{TotalUSD: 0.05, Attribution: domain.AttributionAttributed},
				Run:         run,
			},
			want: "[20260807T120000Z-0001] test happy-path [1/3] PASS (500ms, $0.05)",
		},
		{
			name: "suite started has no run attribution prefix",
			ev: domain.ProgressEvent{
				Kind:       domain.ProgressSuiteStarted,
				SuiteID:    "orchestrator-routing",
				TotalTests: 2,
				TotalRuns:  2,
			},
			want: "suite orchestrator-routing started: 2 tests",
		},
		{
			name: "suite finished has no run attribution prefix",
			ev: domain.ProgressEvent{
				Kind:      domain.ProgressSuiteFinished,
				Counts:    map[domain.Verdict]int{domain.VerdictPass: 2},
				TotalCost: domain.CostReport{TotalUSD: 0.10, Attribution: domain.AttributionAttributed},
			},
			want: "suite finished: PASS=2 ($0.10)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := cli.FormatEvent(tt.ev)
			if got != tt.want {
				t.Errorf("FormatEvent() = %q, want %q", got, tt.want)
			}
			if strings.HasSuffix(got, "\n") {
				t.Error("FormatEvent must not include a trailing newline")
			}
		})
	}
}

// TestFormatEvent_InterleavedRunLines verifies that lines from events
// produced by two different runs carry distinct prefixes, making them
// individually readable when they appear interleaved in the same output
// stream. This is the readability property the run attribution prefix exists
// to provide.
func TestFormatEvent_InterleavedRunLines(t *testing.T) {
	runA := domain.RunKey{RunID: "20260807T120000Z-0001", TestName: "test-a", RunNumber: 1}
	runB := domain.RunKey{RunID: "20260807T120000Z-0002", TestName: "test-b", RunNumber: 1}

	events := []domain.ProgressEvent{
		{Kind: domain.ProgressTestStarted, TestID: "test-a", Repetition: 1, Repetitions: 1, Run: runA},
		{Kind: domain.ProgressTestStarted, TestID: "test-b", Repetition: 1, Repetitions: 1, Run: runB},
		{Kind: domain.ProgressInvocation, Seq: 1, Identity: domain.CollaboratorIdentity{ToolName: "Task", AgentIdentity: "worker"}, Outcome: domain.OutcomePassthrough, Run: runA},
		{Kind: domain.ProgressInvocation, Seq: 1, Identity: domain.CollaboratorIdentity{ToolName: "Task", AgentIdentity: "worker"}, Outcome: domain.OutcomePassthrough, Run: runB},
	}

	const prefixA = "[20260807T120000Z-0001]"
	const prefixB = "[20260807T120000Z-0002]"

	for _, ev := range events {
		line := cli.FormatEvent(ev)
		switch ev.Run.RunID {
		case runA.RunID:
			if !strings.HasPrefix(line, prefixA) {
				t.Errorf("FormatEvent for run %s = %q; want line beginning with %q so it is attributable among interleaved output", runA.RunID, line, prefixA)
			}
		case runB.RunID:
			if !strings.HasPrefix(line, prefixB) {
				t.Errorf("FormatEvent for run %s = %q; want line beginning with %q so it is attributable among interleaved output", runB.RunID, line, prefixB)
			}
		}
	}
}

// TestNewLineSink_DrivenPurelyByEventStream feeds a scripted sequence
// straight into the sink with no suite running at all, and checks the
// resulting line output matches what the events alone determine — proving
// the renderer is driven purely by the stream (AC15.4), not by anything a
// live run happens to produce.
//
// Each lifecycle event (ProgressTestStarted, ProgressTestFinished) is
// followed by a tally line so the running/finished/remaining counts are
// visible in the live stream without waiting for the final report.
func TestNewLineSink_DrivenPurelyByEventStream(t *testing.T) {
	var buf bytes.Buffer
	sink := cli.NewLineSink(&buf)

	events := scriptedSequence()
	for _, ev := range events {
		sink.Emit(ev)
	}

	got := buf.String()

	// scriptedSequence has TotalRuns: 1, so after the single test starts
	// the tally is Running:1 Finished:0 Remaining:0, and after it finishes
	// the tally is Running:0 Finished:1 Remaining:0.
	wantLines := []string{
		cli.FormatEvent(events[0]),                        // suite started
		cli.FormatEvent(events[1]),                        // test started
		"Running: 1 | Finished: 0 | Remaining: 0",        // tally after test started
		cli.FormatEvent(events[2]),                        // invocation (no tally)
		cli.FormatEvent(events[3]),                        // test finished
		"  - " + string(domain.ClassFinalStatus),          // failed assertion
		"  - " + string(domain.ClassArtifactCreated),      // failed assertion
		"Running: 0 | Finished: 1 | Remaining: 0",        // tally after test finished
		cli.FormatEvent(events[4]),                        // suite finished (no tally)
	}
	want := strings.Join(wantLines, "\n") + "\n"

	if got != want {
		t.Errorf("NewLineSink output =\n%q\nwant\n%q", got, want)
	}
}

// TestNewLineSink_PreservesEmissionOrder checks that lines appear in
// exactly the order Emit was called, regardless of event kind.
//
// Each ProgressTestStarted event produces two lines: the event line and a
// tally line immediately after. The test verifies that the event lines are
// interleaved correctly — each event line precedes its tally line, and the
// event lines appear in the same order as the Emit calls.
func TestNewLineSink_PreservesEmissionOrder(t *testing.T) {
	var buf bytes.Buffer
	sink := cli.NewLineSink(&buf)

	ids := []string{"a", "b", "c", "d", "e"}
	for _, id := range ids {
		sink.Emit(domain.ProgressEvent{Kind: domain.ProgressTestStarted, TestID: id, Repetition: 1, Repetitions: 1})
	}

	lines := strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")
	// Each lifecycle event produces an event line plus a tally line, so
	// len(ids) events yield 2*len(ids) lines in total.
	wantTotal := len(ids) * 2
	if len(lines) != wantTotal {
		t.Fatalf("got %d lines, want %d: %q", len(lines), wantTotal, buf.String())
	}
	for i, id := range ids {
		wantEvent := "test " + id + " [1/1] started"
		if lines[i*2] != wantEvent {
			t.Errorf("event line %d = %q, want %q", i*2, lines[i*2], wantEvent)
		}
		if !strings.HasPrefix(lines[i*2+1], "Running:") {
			t.Errorf("tally line %d = %q, want a line beginning with \"Running:\"", i*2+1, lines[i*2+1])
		}
	}
}

// TestNewLineSink_WritesEachEventBeforeTheNext asserts that emission is not
// deferred to a batch flush: after each individual Emit, the writer already
// carries that event's line. A sink that buffers everything until Close (or
// until the stream ends) makes a long-running suite look like a hang.
func TestNewLineSink_WritesEachEventBeforeTheNext(t *testing.T) {
	var buf bytes.Buffer
	sink := cli.NewLineSink(&buf)

	sink.Emit(domain.ProgressEvent{Kind: domain.ProgressSuiteStarted, SuiteID: "s", TotalTests: 1})
	afterFirst := buf.String()
	if !strings.Contains(afterFirst, "started") {
		t.Errorf("after the first Emit, buffer = %q; want the suite-started line already present", afterFirst)
	}

	sink.Emit(domain.ProgressEvent{Kind: domain.ProgressSuiteFinished, Counts: map[domain.Verdict]int{}})
	afterSecond := buf.String()
	if len(afterSecond) <= len(afterFirst) {
		t.Error("second Emit produced no additional output")
	}
}
