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
// exactly the property AC15.4 requires be tested.
func scriptedSequence() []domain.ProgressEvent {
	return []domain.ProgressEvent{
		{Kind: domain.ProgressSuiteStarted, SuiteID: "orchestrator-routing", TotalTests: 1},
		{Kind: domain.ProgressTestStarted, TestID: "happy-path", Repetition: 1, Repetitions: 1},
		{
			Kind:     domain.ProgressInvocation,
			Seq:      1,
			Identity: domain.CollaboratorIdentity{ToolName: "Task", AgentIdentity: "researcher"},
			Outcome:  domain.OutcomeRewritePrompt,
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
		},
		{
			Kind:      domain.ProgressSuiteFinished,
			Counts:    map[domain.Verdict]int{domain.VerdictFail: 1},
			TotalCost: domain.CostReport{TotalUSD: 0.10, Attribution: domain.AttributionAttributed},
		},
	}
}

// TestNewLineSink_DrivenPurelyByEventStream feeds a scripted sequence
// straight into the sink with no suite running at all, and checks the
// resulting line output matches what the events alone determine — proving
// the renderer is driven purely by the stream (AC15.4), not by anything a
// live run happens to produce.
func TestNewLineSink_DrivenPurelyByEventStream(t *testing.T) {
	var buf bytes.Buffer
	sink := cli.NewLineSink(&buf)

	events := scriptedSequence()
	for _, ev := range events {
		sink.Emit(ev)
	}

	got := buf.String()

	wantLines := []string{
		cli.FormatEvent(events[0]),
		cli.FormatEvent(events[1]),
		cli.FormatEvent(events[2]),
		cli.FormatEvent(events[3]),
		"  - " + string(domain.ClassFinalStatus),
		"  - " + string(domain.ClassArtifactCreated),
		cli.FormatEvent(events[4]),
	}
	want := strings.Join(wantLines, "\n") + "\n"

	if got != want {
		t.Errorf("NewLineSink output =\n%q\nwant\n%q", got, want)
	}
}

// TestNewLineSink_PreservesEmissionOrder checks that lines appear in
// exactly the order Emit was called, regardless of event kind.
func TestNewLineSink_PreservesEmissionOrder(t *testing.T) {
	var buf bytes.Buffer
	sink := cli.NewLineSink(&buf)

	ids := []string{"a", "b", "c", "d", "e"}
	for _, id := range ids {
		sink.Emit(domain.ProgressEvent{Kind: domain.ProgressTestStarted, TestID: id, Repetition: 1, Repetitions: 1})
	}

	lines := strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")
	if len(lines) != len(ids) {
		t.Fatalf("got %d lines, want %d: %q", len(lines), len(ids), buf.String())
	}
	for i, id := range ids {
		want := "test " + id + " [1/1] started"
		if lines[i] != want {
			t.Errorf("line %d = %q, want %q", i, lines[i], want)
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
