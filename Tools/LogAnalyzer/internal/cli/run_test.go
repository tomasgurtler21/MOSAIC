package cli_test

// Tests for source-path flag handling and the Execute command dispatch.
//
// Coverage:
//   - A logs-root path supplied via --path is accepted and leads to analysis
//   - A single-run-folder path supplied via --path is accepted and leads to analysis
//   - An absent source with no --path flag produces a clean, non-hanging outcome
//   - The `pricing path` subcommand prints the config file location
//   - Invalid/unknown flags produce a usage error, not a panic

import (
	"bytes"
	"strings"
	"testing"

	"mosaic-log-analyzer/internal/app"
	"mosaic-log-analyzer/internal/cli"
	"mosaic-log-analyzer/internal/domain"
)

// ---------------------------------------------------------------------------
// Source-path flag: logs-root path accepted
// ---------------------------------------------------------------------------

func TestExecute_LogsRootPathAccepted(t *testing.T) {
	// When --path points to a logs-root directory, the CLI must classify it as
	// SourceLogsRoot and proceed with analysis rather than returning ExitUsage.
	logsRoot := domain.Source{Kind: domain.SourceLogsRoot, Path: "/my/logs"}

	logSource := &fakeLogSource{
		classifyFunc: func(path string) domain.Source {
			if path == "/my/logs" {
				return logsRoot
			}
			return domain.Source{Kind: domain.SourceNotFound}
		},
		enumerateFunc: func(s domain.Source) (domain.Inventory, []domain.Finding) {
			// Empty inventory: no runs. Produces ExitNoData, which proves the path
			// was accepted (ExitUsage would mean the path was rejected).
			return domain.Inventory{Source: s}, nil
		},
	}

	opts := cli.Options{
		Service: buildService(logSource),
		Stdout:  &bytes.Buffer{},
		Stderr:  &bytes.Buffer{},
	}
	exitCode := cli.Execute(opts, []string{"report", "--path", "/my/logs"})

	// ExitNoData means the source was accepted — analysis proceeded.
	// ExitUsage would mean the path was rejected without attempting analysis.
	if exitCode == cli.ExitUsage {
		t.Errorf("Execute exit code = ExitUsage (%d); logs-root path was rejected", exitCode)
	}
}

// ---------------------------------------------------------------------------
// Source-path flag: single-run-folder path accepted
// ---------------------------------------------------------------------------

func TestExecute_SingleRunFolderPathAccepted(t *testing.T) {
	// When --path points to a single run folder, the CLI must classify it as
	// SourceSingleRun and proceed with analysis rather than returning ExitUsage.
	singleRunSource := domain.Source{Kind: domain.SourceSingleRun, Path: "/my/logs/" + cliTestRunID}

	logSource := &fakeLogSource{
		classifyFunc: func(path string) domain.Source {
			if strings.HasSuffix(path, cliTestRunID) {
				return singleRunSource
			}
			return domain.Source{Kind: domain.SourceNotFound}
		},
		enumerateFunc: func(s domain.Source) (domain.Inventory, []domain.Finding) {
			return domain.Inventory{Source: s}, nil
		},
	}

	opts := cli.Options{
		Service: buildService(logSource),
		Stdout:  &bytes.Buffer{},
		Stderr:  &bytes.Buffer{},
	}
	exitCode := cli.Execute(opts, []string{"report", "--path", "/my/logs/" + cliTestRunID})

	if exitCode == cli.ExitUsage {
		t.Errorf("Execute exit code = ExitUsage (%d); single-run-folder path was rejected", exitCode)
	}
}

// ---------------------------------------------------------------------------
// Absent source: clean non-hanging outcome
// ---------------------------------------------------------------------------

func TestExecute_AbsentSourceWithNoFlagIsCleanOutcome(t *testing.T) {
	// With no --path flag and no default source, the CLI must return ExitUsage —
	// the documented "clean usage-style outcome" for an unresolvable source —
	// and must not hang. LogSource.Default must be called to confirm the
	// source-resolution code path was exercised and not bypassed by a stub return.
	defaultCalled := false
	logSource := &fakeLogSource{
		defaultFunc: func(workDir string) domain.Source {
			defaultCalled = true
			return domain.Source{Kind: domain.SourceNotFound}
		},
	}

	opts := cli.Options{
		Service: buildService(logSource),
		Stdout:  &bytes.Buffer{},
		Stderr:  &bytes.Buffer{},
	}
	exitCode := cli.Execute(opts, []string{"report"})

	// ExitUsage is the specific documented outcome for a missing source with no flag.
	if exitCode != cli.ExitUsage {
		t.Errorf("Execute exit code = %d, want ExitUsage (%d) — missing source with no flag is a usage-style outcome", exitCode, cli.ExitUsage)
	}
	if !defaultCalled {
		t.Errorf("LogSource.Default was not called; source resolution code path was not exercised")
	}
}

// ---------------------------------------------------------------------------
// `pricing path` subcommand
// ---------------------------------------------------------------------------

func TestExecute_PricingPathSubcommand_PrintsConfigPath(t *testing.T) {
	// The `pricing path` subcommand must write the config file path to stdout
	// and return ExitSuccess. It does not require a log source.
	const pricingPath = "/root/MosaicLogAnalyzer/config/pricing.yaml"
	store := &fakePricingStore{filePath: pricingPath, table: domain.EmptyPricingTable()}

	svc := app.New(app.Deps{
		Source:      &fakeLogSource{},
		Reader:      newFakeEventReader(),
		Pricing:     store,
		Clock:       newFakeClock(),
		Interaction: &alwaysSkippingInteraction{},
		WorkDir:     "/workdir",
	})

	stdout := &bytes.Buffer{}
	opts := cli.Options{
		Service: svc,
		Stdout:  stdout,
		Stderr:  &bytes.Buffer{},
	}
	exitCode := cli.Execute(opts, []string{"pricing", "path"})

	if exitCode != cli.ExitSuccess {
		t.Errorf("Execute `pricing path` exit code = %d, want ExitSuccess (%d)", exitCode, cli.ExitSuccess)
	}
	if !strings.Contains(stdout.String(), pricingPath) {
		t.Errorf("stdout does not contain pricing path %q; got: %s", pricingPath, stdout.String())
	}
}

// ---------------------------------------------------------------------------
// Unknown flags: usage error, no panic
// ---------------------------------------------------------------------------

func TestExecute_UnknownFlagProducesUsageError(t *testing.T) {
	// An unknown flag must produce ExitUsage AND write a flag-rejection message to
	// stderr, so the caller can distinguish cobra's flag rejection from other
	// ExitUsage paths. Without the stderr check this test cannot distinguish a real
	// cobra rejection from the stub's unconditional ExitUsage return.
	stderr := &bytes.Buffer{}
	opts := cli.Options{
		Service: buildService(&fakeLogSource{}),
		Stdout:  &bytes.Buffer{},
		Stderr:  stderr,
	}
	exitCode := cli.Execute(opts, []string{"report", "--nonexistent-flag", "value"})

	if exitCode != cli.ExitUsage {
		t.Errorf("Execute exit code = %d for unknown flag, want ExitUsage (%d)", exitCode, cli.ExitUsage)
	}
	// Cobra must write a flag-error message (e.g. "unknown flag: --nonexistent-flag")
	// to stderr. Without this, the test passes even if flag parsing is never wired.
	if !strings.Contains(stderr.String(), "unknown flag") {
		t.Errorf("stderr does not contain %q; cobra flag rejection must be reported to stderr\nstderr: %s", "unknown flag", stderr.String())
	}
}

// ---------------------------------------------------------------------------
// Output goes to Stdout, not Stderr
// ---------------------------------------------------------------------------

func TestExecute_ReportWithFormatTextWritesPlainTextToStdout(t *testing.T) {
	// When report is invoked with --format text, Execute must write human-readable
	// text output (not JSON) to stdout and return ExitSuccess. This verifies that
	// the --format flag is wired through Execute to the EncodeText encoder rather
	// than always dispatching to the JSON encoder.
	src := domain.Source{Kind: domain.SourceLogsRoot, Path: "/logs"}
	reader := newFakeEventReader()
	reader.addEvents(cliTestOrchFile,
		cliTestTurnEvent("model-a", domain.TokenUsage{Input: domain.Tokens(100)}),
		cliTestRunEndEvent(),
	)
	logSource := &fakeLogSource{
		classifyFunc: func(_ string) domain.Source { return src },
		enumerateFunc: func(s domain.Source) (domain.Inventory, []domain.Finding) {
			return domain.Inventory{
				Source: s,
				Runs: []domain.RunEntry{
					{
						Run:              cliTestRunRef,
						Dir:              "/logs/" + cliTestRunID,
						OrchestratorFile: cliTestOrchFile,
					},
				},
			}, nil
		},
	}
	svc := app.New(app.Deps{
		Source:      logSource,
		Reader:      reader,
		Pricing:     newFakePricingStore(),
		Clock:       newFakeClock(),
		Interaction: &alwaysSkippingInteraction{},
		WorkDir:     "/workdir",
	})

	stdout := &bytes.Buffer{}
	opts := cli.Options{
		Service: svc,
		Stdout:  stdout,
		Stderr:  &bytes.Buffer{},
	}
	exitCode := cli.Execute(opts, []string{"report", "--path", "/logs", "--format", "text"})

	if exitCode != cli.ExitSuccess {
		t.Errorf("Execute exit code = %d for report --format text, want ExitSuccess (%d)", exitCode, cli.ExitSuccess)
	}
	if stdout.Len() == 0 {
		t.Errorf("stdout is empty for report --format text; text output must be written to stdout")
	}
	// Text output must not begin with '{' (which would indicate JSON was produced
	// instead of the expected plain-text encoding).
	if strings.HasPrefix(strings.TrimSpace(stdout.String()), "{") {
		t.Errorf("report --format text produced JSON-shaped output; expected plain text from EncodeText")
	}
}

func TestExecute_ReportOutputWrittenToStdout(t *testing.T) {
	// When a report is produced, the JSON output must appear on stdout, not stderr.
	src := domain.Source{Kind: domain.SourceLogsRoot, Path: "/logs"}

	reader := newFakeEventReader()
	reader.addEvents(cliTestOrchFile,
		cliTestTurnEvent("model-a", domain.TokenUsage{Input: domain.Tokens(100)}),
		cliTestRunEndEvent(),
	)

	logSource := &fakeLogSource{
		classifyFunc: func(_ string) domain.Source { return src },
		enumerateFunc: func(s domain.Source) (domain.Inventory, []domain.Finding) {
			return domain.Inventory{
				Source: s,
				Runs: []domain.RunEntry{
					{
						Run:              cliTestRunRef,
						Dir:              "/logs/" + cliTestRunID,
						OrchestratorFile: cliTestOrchFile,
					},
				},
			}, nil
		},
	}

	svc := app.New(app.Deps{
		Source:      logSource,
		Reader:      reader,
		Pricing:     newFakePricingStore(),
		Clock:       newFakeClock(),
		Interaction: &alwaysSkippingInteraction{},
		WorkDir:     "/workdir",
	})

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	opts := cli.Options{
		Service: svc,
		Stdout:  stdout,
		Stderr:  stderr,
	}
	cli.Execute(opts, []string{"report", "--path", "/logs", "--format", "json"})

	if stdout.Len() == 0 {
		t.Errorf("stdout is empty; report output must be written to stdout")
	}
}
