package cli_test

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"mosaic-agent-test/internal/cli"
	"mosaic-agent-test/internal/preflight"
	"mosaic-agent-test/internal/report"
)

// TestJSONFormat_StdoutCarriesOnlyTheReport asserts that in --format json,
// standard output carries the machine-readable report and nothing else:
// progress lines, diagnostics and errors all go to standard error, so the
// output pipes into a parser without filtering.
func TestJSONFormat_StdoutCarriesOnlyTheReport(t *testing.T) {
	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	want := passingResult("orchestrator-routing")
	runner := &fakeSuiteRunner{
		events: scriptedSequence(),
		result: want,
	}
	opts := baseOptions(stdout, stderr, scriptedPreflight(preflight.Plan{}, cleanReport(), nil), runner)

	got := cli.Execute(context.Background(), []string{"run", "suite.yaml", "--format", "json"}, opts)

	if got != cli.ExitSuccess {
		t.Fatalf("exit code = %d, want ExitSuccess (%d)", got, cli.ExitSuccess)
	}

	var wantBuf bytes.Buffer
	if err := report.RenderJSON(&wantBuf, want); err != nil {
		t.Fatalf("report.RenderJSON: %v", err)
	}

	if stdout.String() != wantBuf.String() {
		t.Errorf("stdout =\n%s\nwant exactly\n%s", stdout.String(), wantBuf.String())
	}

	for _, ev := range scriptedSequence() {
		line := cli.FormatEvent(ev)
		if line == "" {
			continue
		}
		if strings.Contains(stdout.String(), line) {
			t.Errorf("stdout contains a progress line %q; progress must go to stderr in JSON mode", line)
		}
	}
}

// TestJSONFormat_ProgressAndDiagnosticsGoToStderr asserts the complementary
// half of stream separation: progress lines are actually observable, just
// not on stdout.
func TestJSONFormat_ProgressAndDiagnosticsGoToStderr(t *testing.T) {
	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	events := scriptedSequence()
	runner := &fakeSuiteRunner{events: events, result: passingResult("orchestrator-routing")}
	opts := baseOptions(stdout, stderr, scriptedPreflight(preflight.Plan{}, cleanReport(), nil), runner)

	cli.Execute(context.Background(), []string{"run", "suite.yaml", "--format", "json"}, opts)

	firstLine := cli.FormatEvent(events[0])
	if firstLine == "" {
		t.Fatal("test fixture produced an empty formatted line; fix the fixture")
	}
	if !strings.Contains(stderr.String(), firstLine) {
		t.Errorf("stderr does not contain progress line %q; got %q", firstLine, stderr.String())
	}
}

// TestJSONFormat_EmptySuite is the case that matters most for a caller
// parsing this output: no tests ran, and the document must still be
// well-formed rather than a special case the caller has to branch on.
func TestJSONFormat_EmptySuite(t *testing.T) {
	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	empty := report.Build("empty-suite", time.Time{}, time.Time{}, nil, "")
	runner := &fakeSuiteRunner{result: empty}
	opts := baseOptions(stdout, stderr, scriptedPreflight(preflight.Plan{}, cleanReport(), nil), runner)

	got := cli.Execute(context.Background(), []string{"run", "suite.yaml", "--format", "json"}, opts)

	if got != cli.ExitSuccess {
		t.Errorf("exit code = %d, want ExitSuccess (%d) for a suite with no tests", got, cli.ExitSuccess)
	}

	var wantBuf bytes.Buffer
	if err := report.RenderJSON(&wantBuf, empty); err != nil {
		t.Fatalf("report.RenderJSON: %v", err)
	}
	if stdout.String() != wantBuf.String() {
		t.Errorf("stdout =\n%s\nwant exactly\n%s", stdout.String(), wantBuf.String())
	}
}

// TestTextFormat_StdoutCarriesTheHumanReadableReport is the counterpart for
// the default, human-readable format.
func TestTextFormat_StdoutCarriesTheHumanReadableReport(t *testing.T) {
	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	want := failedTestResult("orchestrator-routing")
	runner := &fakeSuiteRunner{result: want}
	opts := baseOptions(stdout, stderr, scriptedPreflight(preflight.Plan{}, cleanReport(), nil), runner)

	got := cli.Execute(context.Background(), []string{"run", "suite.yaml", "--format", "text"}, opts)

	if got != cli.ExitTestsFailed {
		t.Fatalf("exit code = %d, want ExitTestsFailed (%d)", got, cli.ExitTestsFailed)
	}

	var wantBuf bytes.Buffer
	if err := report.RenderText(&wantBuf, want); err != nil {
		t.Fatalf("report.RenderText: %v", err)
	}
	if stdout.String() != wantBuf.String() {
		t.Errorf("stdout =\n%s\nwant exactly\n%s", stdout.String(), wantBuf.String())
	}
}

// TestFormat_EqualsAndSpaceForms_AreEquivalent closes the flag-form
// equivalence gap for --format: every other test in this file exercises
// only the space-separated form (--format json), but AC15.2 requires
// --format=json to behave identically.
func TestFormat_EqualsAndSpaceForms_AreEquivalent(t *testing.T) {
	want := passingResult("orchestrator-routing")

	run := func(args []string) string {
		stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
		runner := &fakeSuiteRunner{result: want}
		opts := baseOptions(stdout, stderr, scriptedPreflight(preflight.Plan{}, cleanReport(), nil), runner)
		cli.Execute(context.Background(), args, opts)
		return stdout.String()
	}

	space := run([]string{"run", "suite.yaml", "--format", "json"})
	equals := run([]string{"run", "suite.yaml", "--format=json"})

	if space != equals {
		t.Errorf("--format json and --format=json produced different stdout:\nspace form:\n%s\nequals form:\n%s", space, equals)
	}

	var wantBuf bytes.Buffer
	if err := report.RenderJSON(&wantBuf, want); err != nil {
		t.Fatalf("report.RenderJSON: %v", err)
	}
	if equals != wantBuf.String() {
		t.Errorf("--format=json stdout =\n%s\nwant exactly\n%s", equals, wantBuf.String())
	}
}

// TestFormat_UnrecognizedValue_IsUsageError asserts a format value outside
// the documented text|json surface is rejected as a usage error rather than
// silently accepted or defaulted.
func TestFormat_UnrecognizedValue_IsUsageError(t *testing.T) {
	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	runner := &fakeSuiteRunner{}
	opts := baseOptions(stdout, stderr, scriptedPreflight(preflight.Plan{}, cleanReport(), nil), runner)

	got := cli.Execute(context.Background(), []string{"run", "suite.yaml", "--format", "xml"}, opts)

	if got != cli.ExitUsage {
		t.Errorf("exit code = %d, want ExitUsage (%d) for an unrecognized --format value", got, cli.ExitUsage)
	}
	if runner.wasCalled() {
		t.Error("suite runner was invoked for an unrecognized --format value")
	}
}

// TestFormat_DefaultsToText: with no --format flag, output must be the
// human-readable rendering, not the machine-readable one.
func TestFormat_DefaultsToText(t *testing.T) {
	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	want := passingResult("orchestrator-routing")
	runner := &fakeSuiteRunner{result: want}
	opts := baseOptions(stdout, stderr, scriptedPreflight(preflight.Plan{}, cleanReport(), nil), runner)

	cli.Execute(context.Background(), []string{"run", "suite.yaml"}, opts)

	var textBuf, jsonBuf bytes.Buffer
	_ = report.RenderText(&textBuf, want)
	_ = report.RenderJSON(&jsonBuf, want)

	if stdout.String() != textBuf.String() {
		t.Errorf("stdout with no --format flag =\n%s\nwant the text rendering\n%s", stdout.String(), textBuf.String())
	}
	if stdout.String() == jsonBuf.String() {
		t.Error("stdout with no --format flag matched the JSON rendering; default must be text")
	}
}
