package cli_test

// Tests for Stage 4: CLI Store & Summary Commands.
//
// These tests cover the storeCommand and summaryCommand implementations:
// request field mapping, stdout output, exit code contract, independence from
// the run path, and nil-func handling. Per-command flag scoping, flag
// acceptance/rejection, and --help group rendering are covered by
// percommand_flags_test.go (Stage 3).

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"mosaic-agent-test/internal/authoring"
	"mosaic-agent-test/internal/cli"
	"mosaic-agent-test/internal/preflight"
	"mosaic-agent-test/internal/resultstore"
	"mosaic-agent-test/internal/resultsummary"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// captureStoreFunc returns a StoreFunc that captures the request it receives,
// records whether it was called, and returns the supplied result and error.
func captureStoreFunc(req *resultstore.StoreFromPathsRequest, result resultstore.StoreResult, err error) cli.StoreFunc {
	return func(r resultstore.StoreFromPathsRequest) (resultstore.StoreResult, error) {
		if req != nil {
			*req = r
		}
		return result, err
	}
}

// captureSummaryFunc returns a SummaryFunc that captures the request it
// receives and returns the supplied result and error.
func captureSummaryFunc(req *resultsummary.SummaryRequest, result resultsummary.SummaryResult, err error) cli.SummaryFunc {
	return func(r resultsummary.SummaryRequest) (resultsummary.SummaryResult, error) {
		if req != nil {
			*req = r
		}
		return result, err
	}
}

// recordingPreflight returns a PreflightFunc that sets *called = true when
// invoked. Used to verify that store/summary commands never trigger preflight.
func recordingPreflight(called *bool) cli.PreflightFunc {
	return func(in preflight.Input) (preflight.Plan, authoring.Report) {
		*called = true
		return preflight.Plan{}, authoring.Report{}
	}
}

// ---------------------------------------------------------------------------
// T4.1: store command — request field mapping
// ---------------------------------------------------------------------------

// TestStoreCommand_PositionalFiles_AreMappedToRequestFiles verifies that
// positional file arguments given to `store` are passed as the Files field
// of the StoreFromPathsRequest, not as Dir or any other field.
func TestStoreCommand_PositionalFiles_AreMappedToRequestFiles(t *testing.T) {
	var got resultstore.StoreFromPathsRequest
	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	opts := storeBaseOpts(stdout, stderr, captureStoreFunc(&got, resultstore.StoreResult{}, nil), nil)

	code := cli.Execute(context.Background(),
		[]string{"store", "report-a.json", "report-b.json"},
		opts)

	if code != cli.ExitSuccess {
		t.Fatalf("exit code = %d, want ExitSuccess (%d); stderr=%q",
			code, cli.ExitSuccess, stderr.String())
	}

	want := []string{"report-a.json", "report-b.json"}
	if len(got.Files) != len(want) {
		t.Fatalf("StoreFromPathsRequest.Files = %v, want %v", got.Files, want)
	}
	for i, f := range want {
		if got.Files[i] != f {
			t.Errorf("StoreFromPathsRequest.Files[%d] = %q, want %q", i, got.Files[i], f)
		}
	}
	if got.Dir != "" {
		t.Errorf("StoreFromPathsRequest.Dir = %q, want empty (positional files, not directory)", got.Dir)
	}
}

// TestStoreCommand_SinglePositionalFile_IsMappedToRequestFiles verifies the
// single-file positional case.
func TestStoreCommand_SinglePositionalFile_IsMappedToRequestFiles(t *testing.T) {
	var got resultstore.StoreFromPathsRequest
	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	opts := storeBaseOpts(stdout, stderr, captureStoreFunc(&got, resultstore.StoreResult{Stored: 1}, nil), nil)

	code := cli.Execute(context.Background(), []string{"store", "single-report.json"}, opts)

	if code != cli.ExitSuccess {
		t.Fatalf("exit code = %d, want ExitSuccess (%d); stderr=%q",
			code, cli.ExitSuccess, stderr.String())
	}
	if len(got.Files) != 1 || got.Files[0] != "single-report.json" {
		t.Errorf("StoreFromPathsRequest.Files = %v, want [\"single-report.json\"]", got.Files)
	}
	if got.Dir != "" {
		t.Errorf("StoreFromPathsRequest.Dir = %q, want empty", got.Dir)
	}
}

// TestStoreCommand_DirFlag_IsMappedToRequestDir verifies that --dir is passed
// as the Dir field of StoreFromPathsRequest, not in Files.
func TestStoreCommand_DirFlag_IsMappedToRequestDir(t *testing.T) {
	var got resultstore.StoreFromPathsRequest
	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	opts := storeBaseOpts(stdout, stderr, captureStoreFunc(&got, resultstore.StoreResult{}, nil), nil)

	code := cli.Execute(context.Background(), []string{"store", "--dir", "/path/to/reports"}, opts)

	if code != cli.ExitSuccess {
		t.Fatalf("exit code = %d, want ExitSuccess (%d); stderr=%q",
			code, cli.ExitSuccess, stderr.String())
	}
	if got.Dir != "/path/to/reports" {
		t.Errorf("StoreFromPathsRequest.Dir = %q, want \"/path/to/reports\"", got.Dir)
	}
	if len(got.Files) != 0 {
		t.Errorf("StoreFromPathsRequest.Files = %v, want empty (directory mode, not files)", got.Files)
	}
}

// TestStoreCommand_DirFlagEqualsForm_IsMappedToRequestDir verifies that the
// equals form of --dir is handled identically to the space form.
func TestStoreCommand_DirFlagEqualsForm_IsMappedToRequestDir(t *testing.T) {
	var got resultstore.StoreFromPathsRequest
	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	opts := storeBaseOpts(stdout, stderr, captureStoreFunc(&got, resultstore.StoreResult{}, nil), nil)

	code := cli.Execute(context.Background(), []string{"store", "--dir=/reports"}, opts)

	if code != cli.ExitSuccess {
		t.Fatalf("exit code = %d, want ExitSuccess (%d); stderr=%q",
			code, cli.ExitSuccess, stderr.String())
	}
	if got.Dir != "/reports" {
		t.Errorf("StoreFromPathsRequest.Dir = %q, want \"/reports\"", got.Dir)
	}
}

// TestStoreCommand_TestResultsRoot_IsForwardedToRequest verifies that the
// TestResultsRoot field on Options is passed through to
// StoreFromPathsRequest.TestResultsRoot. If the implementation omits this
// field from the request, all other store tests would still pass because they
// do not set or inspect TestResultsRoot.
func TestStoreCommand_TestResultsRoot_IsForwardedToRequest(t *testing.T) {
	var got resultstore.StoreFromPathsRequest
	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	opts := storeBaseOpts(stdout, stderr, captureStoreFunc(&got, resultstore.StoreResult{}, nil), nil)
	opts.TestResultsRoot = "/some/root"

	code := cli.Execute(context.Background(), []string{"store", "report.json"}, opts)

	if code != cli.ExitSuccess {
		t.Fatalf("exit code = %d, want ExitSuccess (%d); stderr=%q",
			code, cli.ExitSuccess, stderr.String())
	}
	if got.TestResultsRoot != opts.TestResultsRoot {
		t.Errorf("StoreFromPathsRequest.TestResultsRoot = %q, want %q",
			got.TestResultsRoot, opts.TestResultsRoot)
	}
}

// ---------------------------------------------------------------------------
// T4.1: store command — stdout output
// ---------------------------------------------------------------------------

// TestStoreCommand_PrintsSummaryLineToStdout verifies that the store command
// writes the StoreResult.SummaryLine() to stdout on success. The summary line
// is the primary user-facing output for the store command.
func TestStoreCommand_PrintsSummaryLineToStdout(t *testing.T) {
	result := resultstore.StoreResult{Stored: 5}
	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	opts := storeBaseOpts(stdout, stderr, captureStoreFunc(nil, result, nil), nil)

	code := cli.Execute(context.Background(), []string{"store", "report.json"}, opts)

	if code != cli.ExitSuccess {
		t.Fatalf("exit code = %d, want ExitSuccess (%d); stderr=%q",
			code, cli.ExitSuccess, stderr.String())
	}

	wantLine := result.SummaryLine()
	if !strings.Contains(stdout.String(), wantLine) {
		t.Errorf("stdout = %q, want it to contain the summary line %q", stdout.String(), wantLine)
	}
}

// TestStoreCommand_PrintsSummaryLineWithSkips verifies that the summary line
// is rendered when some reports were skipped, and reflects the skip counts.
func TestStoreCommand_PrintsSummaryLineWithSkips(t *testing.T) {
	result := resultstore.StoreResult{
		Stored:           3,
		SkippedNonReport: 1,
		SkippedUnknown:   2,
		SkippedDuplicate: 1,
	}
	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	opts := storeBaseOpts(stdout, stderr, captureStoreFunc(nil, result, nil), nil)

	code := cli.Execute(context.Background(), []string{"store", "a.json", "b.json"}, opts)

	if code != cli.ExitSuccess {
		t.Fatalf("exit code = %d, want ExitSuccess (%d); stderr=%q",
			code, cli.ExitSuccess, stderr.String())
	}
	wantLine := result.SummaryLine()
	if !strings.Contains(stdout.String(), wantLine) {
		t.Errorf("stdout = %q, want it to contain the summary line %q", stdout.String(), wantLine)
	}
}

// ---------------------------------------------------------------------------
// T4.1: store command — exit codes
// ---------------------------------------------------------------------------

// TestStoreCommand_AllReportsSkipped_ReturnsExitSuccess verifies that when all
// reports are skipped (refused), the exit code is still 0 (ExitSuccess).
// Skip/refuse conditions are not infrastructure errors; they are expected
// outcomes recorded in StoreResult.
func TestStoreCommand_AllReportsSkipped_ReturnsExitSuccess(t *testing.T) {
	result := resultstore.StoreResult{
		Stored:           0,
		SkippedNonReport: 1,
		SkippedUnknown:   1,
		Reports: []resultstore.StoredReport{
			{SourcePath: "a.json", Skipped: true, SkipReason: resultstore.SkipNotReport},
			{SourcePath: "b.json", Skipped: true, SkipReason: resultstore.SkipUnknownVersion},
		},
	}
	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	opts := storeBaseOpts(stdout, stderr, captureStoreFunc(nil, result, nil), nil)

	code := cli.Execute(context.Background(), []string{"store", "a.json", "b.json"}, opts)

	if code != cli.ExitSuccess {
		t.Errorf("exit code = %d, want ExitSuccess (%d) even when all reports skipped; stderr=%q",
			code, cli.ExitSuccess, stderr.String())
	}
}

// TestStoreCommand_InfrastructureError_ReturnsExitFailure verifies that when
// Store returns an error (infrastructure failure), the exit code is 1
// (ExitFailure), not ExitUsage or any other code.
func TestStoreCommand_InfrastructureError_ReturnsExitFailure(t *testing.T) {
	infraErr := errors.New("cannot read file: permission denied")
	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	opts := storeBaseOpts(stdout, stderr,
		captureStoreFunc(nil, resultstore.StoreResult{}, infraErr), nil)

	code := cli.Execute(context.Background(), []string{"store", "report.json"}, opts)

	if code != cli.ExitFailure {
		t.Errorf("exit code = %d, want ExitFailure (%d) for infrastructure error; stderr=%q",
			code, cli.ExitFailure, stderr.String())
	}
}

// TestStoreCommand_InfrastructureError_PrintsErrorToStderr verifies that the
// error message from an infrastructure failure reaches stderr.
func TestStoreCommand_InfrastructureError_PrintsErrorToStderr(t *testing.T) {
	infraErr := errors.New("cannot write to TestResults: disk full")
	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	opts := storeBaseOpts(stdout, stderr,
		captureStoreFunc(nil, resultstore.StoreResult{}, infraErr), nil)

	cli.Execute(context.Background(), []string{"store", "report.json"}, opts)

	if !strings.Contains(stderr.String(), infraErr.Error()) {
		t.Errorf("stderr = %q, want it to mention the error %q", stderr.String(), infraErr.Error())
	}
}

// TestStoreCommand_NilStoreFunc_ReturnsExitFailure verifies that when the Store
// func is nil (wiring omission), the exit code is ExitFailure. A nil func
// must produce a visible error, not a silent no-op or a panic.
func TestStoreCommand_NilStoreFunc_ReturnsExitFailure(t *testing.T) {
	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	opts := storeBaseOpts(stdout, stderr, nil, nil)

	code := cli.Execute(context.Background(), []string{"store", "report.json"}, opts)

	if code != cli.ExitFailure {
		t.Errorf("exit code = %d, want ExitFailure (%d) when Store func is nil; stderr=%q",
			code, cli.ExitFailure, stderr.String())
	}
}

// ---------------------------------------------------------------------------
// T4.2: summary command — request field mapping
// ---------------------------------------------------------------------------

// TestSummaryCommand_ForVersionFlag_IsMappedToVersionFilter verifies that the
// --for-version flag value is passed as SummaryRequest.VersionFilter.
func TestSummaryCommand_ForVersionFlag_IsMappedToVersionFilter(t *testing.T) {
	var got resultsummary.SummaryRequest
	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	opts := storeBaseOpts(stdout, stderr, nil,
		captureSummaryFunc(&got, resultsummary.SummaryResult{}, nil))

	code := cli.Execute(context.Background(),
		[]string{"summary", "--for-version", "v1.6.2"}, opts)

	if code != cli.ExitSuccess {
		t.Fatalf("exit code = %d, want ExitSuccess (%d); stderr=%q",
			code, cli.ExitSuccess, stderr.String())
	}
	if got.VersionFilter != "v1.6.2" {
		t.Errorf("SummaryRequest.VersionFilter = %q, want \"v1.6.2\"", got.VersionFilter)
	}
}

// TestSummaryCommand_ForVersionFlagEqualsForm_IsMappedToVersionFilter verifies
// the equals form of --for-version is handled identically to the space form.
func TestSummaryCommand_ForVersionFlagEqualsForm_IsMappedToVersionFilter(t *testing.T) {
	var got resultsummary.SummaryRequest
	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	opts := storeBaseOpts(stdout, stderr, nil,
		captureSummaryFunc(&got, resultsummary.SummaryResult{}, nil))

	code := cli.Execute(context.Background(),
		[]string{"summary", "--for-version=v2.0.0"}, opts)

	if code != cli.ExitSuccess {
		t.Fatalf("exit code = %d, want ExitSuccess (%d); stderr=%q",
			code, cli.ExitSuccess, stderr.String())
	}
	if got.VersionFilter != "v2.0.0" {
		t.Errorf("SummaryRequest.VersionFilter = %q, want \"v2.0.0\"", got.VersionFilter)
	}
}

// TestSummaryCommand_NoVersionFlag_PassesEmptyVersionFilter verifies that when
// --for-version is not given, SummaryRequest.VersionFilter is the empty string,
// meaning "scan all versions".
func TestSummaryCommand_NoVersionFlag_PassesEmptyVersionFilter(t *testing.T) {
	var got resultsummary.SummaryRequest
	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	opts := storeBaseOpts(stdout, stderr, nil,
		captureSummaryFunc(&got, resultsummary.SummaryResult{}, nil))

	code := cli.Execute(context.Background(), []string{"summary"}, opts)

	if code != cli.ExitSuccess {
		t.Fatalf("exit code = %d, want ExitSuccess (%d); stderr=%q",
			code, cli.ExitSuccess, stderr.String())
	}
	if got.VersionFilter != "" {
		t.Errorf("SummaryRequest.VersionFilter = %q, want empty string (scan all versions)",
			got.VersionFilter)
	}
}

// TestSummaryCommand_TestResultsRoot_IsForwardedToRequest verifies that the
// TestResultsRoot field on Options is passed through to
// SummaryRequest.TestResultsRoot. If the implementation omits this field from
// the request, all other summary tests would still pass because they do not
// set or inspect TestResultsRoot.
func TestSummaryCommand_TestResultsRoot_IsForwardedToRequest(t *testing.T) {
	var got resultsummary.SummaryRequest
	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	opts := storeBaseOpts(stdout, stderr, nil,
		captureSummaryFunc(&got, resultsummary.SummaryResult{}, nil))
	opts.TestResultsRoot = "/some/root"

	code := cli.Execute(context.Background(), []string{"summary"}, opts)

	if code != cli.ExitSuccess {
		t.Fatalf("exit code = %d, want ExitSuccess (%d); stderr=%q",
			code, cli.ExitSuccess, stderr.String())
	}
	if got.TestResultsRoot != opts.TestResultsRoot {
		t.Errorf("SummaryRequest.TestResultsRoot = %q, want %q",
			got.TestResultsRoot, opts.TestResultsRoot)
	}
}

// ---------------------------------------------------------------------------
// T4.2: summary command — stdout output
// ---------------------------------------------------------------------------

// TestSummaryCommand_ListsWrittenFilesOnStdout verifies that newly written
// summary.md files appear in stdout output with a "wrote " prefix, so the user
// can identify which files were created.
func TestSummaryCommand_ListsWrittenFilesOnStdout(t *testing.T) {
	result := resultsummary.SummaryResult{
		FilesWritten: []string{
			"TestResults/v1.0.0/summary.md",
			"TestResults/summary.md",
		},
	}
	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	opts := storeBaseOpts(stdout, stderr, nil,
		captureSummaryFunc(nil, result, nil))

	code := cli.Execute(context.Background(), []string{"summary"}, opts)

	if code != cli.ExitSuccess {
		t.Fatalf("exit code = %d, want ExitSuccess (%d); stderr=%q",
			code, cli.ExitSuccess, stderr.String())
	}

	out := stdout.String()
	for _, f := range result.FilesWritten {
		wantLine := "wrote " + f
		if !strings.Contains(out, wantLine) {
			t.Errorf("stdout = %q, want it to contain %q", out, wantLine)
		}
	}
}

// TestSummaryCommand_ListsUpdatedFilesOnStdout verifies that existing
// summary.md files whose generated blocks were refreshed appear in stdout
// with an "updated " prefix.
func TestSummaryCommand_ListsUpdatedFilesOnStdout(t *testing.T) {
	result := resultsummary.SummaryResult{
		FilesUpdated: []string{
			"TestResults/v1.0.0/summary.md",
			"TestResults/summary.md",
		},
	}
	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	opts := storeBaseOpts(stdout, stderr, nil,
		captureSummaryFunc(nil, result, nil))

	code := cli.Execute(context.Background(), []string{"summary"}, opts)

	if code != cli.ExitSuccess {
		t.Fatalf("exit code = %d, want ExitSuccess (%d); stderr=%q",
			code, cli.ExitSuccess, stderr.String())
	}

	out := stdout.String()
	for _, f := range result.FilesUpdated {
		wantLine := "updated " + f
		if !strings.Contains(out, wantLine) {
			t.Errorf("stdout = %q, want it to contain %q", out, wantLine)
		}
	}
}

// TestSummaryCommand_ListsWrittenAndUpdatedFilesOnStdout verifies that both
// written and updated files are listed when both sets are non-empty.
func TestSummaryCommand_ListsWrittenAndUpdatedFilesOnStdout(t *testing.T) {
	result := resultsummary.SummaryResult{
		FilesWritten: []string{"TestResults/v2.0.0/summary.md"},
		FilesUpdated: []string{"TestResults/summary.md"},
	}
	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	opts := storeBaseOpts(stdout, stderr, nil,
		captureSummaryFunc(nil, result, nil))

	code := cli.Execute(context.Background(), []string{"summary"}, opts)

	if code != cli.ExitSuccess {
		t.Fatalf("exit code = %d, want ExitSuccess (%d); stderr=%q",
			code, cli.ExitSuccess, stderr.String())
	}

	out := stdout.String()
	if !strings.Contains(out, "wrote TestResults/v2.0.0/summary.md") {
		t.Errorf("stdout = %q, want \"wrote TestResults/v2.0.0/summary.md\"", out)
	}
	if !strings.Contains(out, "updated TestResults/summary.md") {
		t.Errorf("stdout = %q, want \"updated TestResults/summary.md\"", out)
	}
}

// ---------------------------------------------------------------------------
// T4.2: summary command — exit codes
// ---------------------------------------------------------------------------

// TestSummaryCommand_EmptyResult_ReturnsExitSuccess verifies that when
// Generate completes with no files written or updated (empty TestResults tree),
// the exit code is 0 (ExitSuccess). An empty result is not an error.
func TestSummaryCommand_EmptyResult_ReturnsExitSuccess(t *testing.T) {
	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	opts := storeBaseOpts(stdout, stderr, nil,
		captureSummaryFunc(nil, resultsummary.SummaryResult{}, nil))

	code := cli.Execute(context.Background(), []string{"summary"}, opts)

	if code != cli.ExitSuccess {
		t.Errorf("exit code = %d, want ExitSuccess (%d) for empty summary result; stderr=%q",
			code, cli.ExitSuccess, stderr.String())
	}
}

// TestSummaryCommand_WithVersionFilter_EmptyResult_ReturnsExitSuccess verifies
// that filtering by version with no matching reports still returns ExitSuccess.
func TestSummaryCommand_WithVersionFilter_EmptyResult_ReturnsExitSuccess(t *testing.T) {
	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	opts := storeBaseOpts(stdout, stderr, nil,
		captureSummaryFunc(nil, resultsummary.SummaryResult{}, nil))

	code := cli.Execute(context.Background(),
		[]string{"summary", "--for-version", "v99.0.0"}, opts)

	if code != cli.ExitSuccess {
		t.Errorf("exit code = %d, want ExitSuccess (%d) when filter matches nothing; stderr=%q",
			code, cli.ExitSuccess, stderr.String())
	}
}

// TestSummaryCommand_InfrastructureError_ReturnsExitFailure verifies that when
// Summary returns an error (infrastructure failure), the exit code is 1
// (ExitFailure).
func TestSummaryCommand_InfrastructureError_ReturnsExitFailure(t *testing.T) {
	infraErr := errors.New("cannot read TestResults/: no such file or directory")
	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	opts := storeBaseOpts(stdout, stderr, nil,
		captureSummaryFunc(nil, resultsummary.SummaryResult{}, infraErr))

	code := cli.Execute(context.Background(), []string{"summary"}, opts)

	if code != cli.ExitFailure {
		t.Errorf("exit code = %d, want ExitFailure (%d) for infrastructure error; stderr=%q",
			code, cli.ExitFailure, stderr.String())
	}
}

// TestSummaryCommand_InfrastructureError_PrintsErrorToStderr verifies that the
// error message from an infrastructure failure reaches stderr.
func TestSummaryCommand_InfrastructureError_PrintsErrorToStderr(t *testing.T) {
	infraErr := errors.New("cannot write summary.md: permission denied")
	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	opts := storeBaseOpts(stdout, stderr, nil,
		captureSummaryFunc(nil, resultsummary.SummaryResult{}, infraErr))

	cli.Execute(context.Background(), []string{"summary"}, opts)

	if !strings.Contains(stderr.String(), infraErr.Error()) {
		t.Errorf("stderr = %q, want it to mention the error %q", stderr.String(), infraErr.Error())
	}
}

// TestSummaryCommand_NilSummaryFunc_ReturnsExitFailure verifies that when the
// Summary func is nil (wiring omission), the exit code is ExitFailure. A nil
// func must not panic or silently succeed.
func TestSummaryCommand_NilSummaryFunc_ReturnsExitFailure(t *testing.T) {
	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	opts := storeBaseOpts(stdout, stderr, nil, nil)

	code := cli.Execute(context.Background(), []string{"summary"}, opts)

	if code != cli.ExitFailure {
		t.Errorf("exit code = %d, want ExitFailure (%d) when Summary func is nil; stderr=%q",
			code, cli.ExitFailure, stderr.String())
	}
}

// ---------------------------------------------------------------------------
// T4.1 / T4.2: independence from the run path
// ---------------------------------------------------------------------------

// TestStoreCommand_NeverCallsPreflight verifies that the store command does not
// invoke Options.Preflight. Store is fully independent of the run path.
func TestStoreCommand_NeverCallsPreflight(t *testing.T) {
	var preflightCalled bool
	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	opts := storeBaseOpts(stdout, stderr, captureStoreFunc(nil, resultstore.StoreResult{}, nil), nil)
	opts.Preflight = recordingPreflight(&preflightCalled)

	cli.Execute(context.Background(), []string{"store", "report.json"}, opts)

	if preflightCalled {
		t.Error("store command invoked Options.Preflight; it must be independent of the run path")
	}
}

// TestSummaryCommand_NeverCallsPreflight verifies that the summary command
// does not invoke Options.Preflight.
func TestSummaryCommand_NeverCallsPreflight(t *testing.T) {
	var preflightCalled bool
	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	opts := storeBaseOpts(stdout, stderr, nil,
		captureSummaryFunc(nil, resultsummary.SummaryResult{}, nil))
	opts.Preflight = recordingPreflight(&preflightCalled)

	cli.Execute(context.Background(), []string{"summary"}, opts)

	if preflightCalled {
		t.Error("summary command invoked Options.Preflight; it must be independent of the run path")
	}
}

// TestStoreCommand_NeverCallsSuiteFactory verifies that the store command does
// not invoke Options.Suite (the SuiteFactory). Store is fully independent of
// the run path.
func TestStoreCommand_NeverCallsSuiteFactory(t *testing.T) {
	factoryCalled := false
	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	opts := storeBaseOpts(stdout, stderr, captureStoreFunc(nil, resultstore.StoreResult{}, nil), nil)
	opts.Suite = func(cfg cli.RunConfig) (cli.SuiteRunner, error) {
		factoryCalled = true
		return &fakeSuiteRunner{}, nil
	}

	cli.Execute(context.Background(), []string{"store", "report.json"}, opts)

	if factoryCalled {
		t.Error("store command invoked Options.Suite (SuiteFactory); it must be independent of the run path")
	}
}

// TestSummaryCommand_NeverCallsSuiteFactory verifies that the summary command
// does not invoke Options.Suite.
func TestSummaryCommand_NeverCallsSuiteFactory(t *testing.T) {
	factoryCalled := false
	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	opts := storeBaseOpts(stdout, stderr, nil,
		captureSummaryFunc(nil, resultsummary.SummaryResult{}, nil))
	opts.Suite = func(cfg cli.RunConfig) (cli.SuiteRunner, error) {
		factoryCalled = true
		return &fakeSuiteRunner{}, nil
	}

	cli.Execute(context.Background(), []string{"summary"}, opts)

	if factoryCalled {
		t.Error("summary command invoked Options.Suite (SuiteFactory); it must be independent of the run path")
	}
}

// ---------------------------------------------------------------------------
// T4.3: execute switch — dispatch and unknown command error
// ---------------------------------------------------------------------------

// TestExecute_StoreCommand_IsDispatchedAlongsideRunAndValidate verifies that
// "store" is recognised by the execute switch and dispatched correctly
// alongside "run" and "validate", without producing an unknown-command error.
func TestExecute_StoreCommand_IsDispatchedAlongsideRunAndValidate(t *testing.T) {
	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	var storeCalled bool
	opts := storeBaseOpts(stdout, stderr, fakeStoreFunc(&storeCalled), nil)

	code := cli.Execute(context.Background(), []string{"store", "file.json"}, opts)

	// ExitUsage with "unknown command" in stderr means the switch did not dispatch.
	if code == cli.ExitUsage && strings.Contains(stderr.String(), "unknown command") {
		t.Errorf("store was rejected as unknown command; stderr=%q", stderr.String())
	}
	if !storeCalled {
		t.Error("execute switch did not dispatch to store command (Store func was not called)")
	}
}

// TestExecute_SummaryCommand_IsDispatchedAlongsideRunAndValidate verifies that
// "summary" is recognised and dispatched by the execute switch.
func TestExecute_SummaryCommand_IsDispatchedAlongsideRunAndValidate(t *testing.T) {
	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	var summaryCalled bool
	opts := storeBaseOpts(stdout, stderr, nil, fakeSummaryFunc(&summaryCalled))

	code := cli.Execute(context.Background(), []string{"summary"}, opts)

	if code == cli.ExitUsage && strings.Contains(stderr.String(), "unknown command") {
		t.Errorf("summary was rejected as unknown command; stderr=%q", stderr.String())
	}
	if !summaryCalled {
		t.Error("execute switch did not dispatch to summary command (Summary func was not called)")
	}
}

// TestExecute_UnknownCommand_ErrorMentionsAllFourCommands verifies that an
// unknown command produces an error message that lists all four recognised
// commands: run, validate, store, and summary. The error message and the
// switch statement must be kept in sync.
func TestExecute_UnknownCommand_ErrorMentionsAllFourCommands(t *testing.T) {
	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	opts := storeBaseOpts(stdout, stderr, nil, nil)

	code := cli.Execute(context.Background(), []string{"frobnicate"}, opts)

	if code != cli.ExitUsage {
		t.Fatalf("exit code = %d, want ExitUsage (%d) for unknown command", code, cli.ExitUsage)
	}

	errOut := stderr.String()
	for _, cmd := range []string{"run", "validate", "store", "summary"} {
		if !strings.Contains(errOut, cmd) {
			t.Errorf("unknown command error does not mention %q; stderr=%q", cmd, errOut)
		}
	}
}

// TestExecute_UnknownCommand_WithFlags_IsUsageError verifies that an
// unrecognised command name paired with flags still produces ExitUsage, not
// ExitFailure or a panic.
func TestExecute_UnknownCommand_WithFlags_IsUsageError(t *testing.T) {
	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	opts := storeBaseOpts(stdout, stderr, nil, nil)

	code := cli.Execute(context.Background(), []string{"unknown-cmd", "--some-flag", "value"}, opts)

	if code != cli.ExitUsage {
		t.Errorf("exit code = %d, want ExitUsage (%d) for unrecognised command; stderr=%q",
			code, cli.ExitUsage, stderr.String())
	}
}
