package cli_test

// Tests for stable exit codes.
//
// Coverage:
//   - ExitSuccess, ExitFailure, ExitNoData, ExitUsage, ExitUnusable have the
//     documented stable values
//   - No two exit codes share the same value
//   - Execute returns ExitSuccess when analysis produces a report
//   - Execute returns ExitNoData when the source contains no runs
//   - Execute returns ExitUsage when no source can be resolved
//   - Execute returns ExitUnusable when the supplied path is unusable

import (
	"bytes"
	"errors"
	"testing"

	"mosaic-log-analyzer/internal/app"
	"mosaic-log-analyzer/internal/cli"
	"mosaic-log-analyzer/internal/domain"
)

// ---------------------------------------------------------------------------
// Constant values
// ---------------------------------------------------------------------------

func TestExitCodes_StableValues(t *testing.T) {
	// These values are stable, documented, and shared with mosaic-deploy and
	// mosaic-run. Changing them is a breaking change.
	cases := []struct {
		name string
		got  int
		want int
	}{
		{"ExitSuccess", cli.ExitSuccess, 0},
		{"ExitFailure", cli.ExitFailure, 1},
		{"ExitNoData", cli.ExitNoData, 2},
		{"ExitUsage", cli.ExitUsage, 3},
		{"ExitUnusable", cli.ExitUnusable, 4},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.got != tc.want {
				t.Errorf("%s = %d, want %d", tc.name, tc.got, tc.want)
			}
		})
	}
}

func TestExitCodes_AllDistinct(t *testing.T) {
	codes := map[int]string{}
	check := func(name string, value int) {
		if prev, dup := codes[value]; dup {
			t.Errorf("exit code %d is shared by %s and %s", value, prev, name)
		}
		codes[value] = name
	}
	check("ExitSuccess", cli.ExitSuccess)
	check("ExitFailure", cli.ExitFailure)
	check("ExitNoData", cli.ExitNoData)
	check("ExitUsage", cli.ExitUsage)
	check("ExitUnusable", cli.ExitUnusable)
}

// ---------------------------------------------------------------------------
// Execute: exit code mapping by outcome
// ---------------------------------------------------------------------------

// buildService constructs an app.Service with the supplied LogSource and
// an always-skipping Interaction (CLI shape — non-blocking).
func buildService(src domain.LogSource) *app.Service {
	return app.New(app.Deps{
		Source:      src,
		Reader:      newFakeEventReader(),
		Pricing:     newFakePricingStore(),
		Clock:       newFakeClock(),
		Interaction: &alwaysSkippingInteraction{},
		WorkDir:     "/workdir",
	})
}

func TestExecute_ReturnsExitSuccessWhenReportProduced(t *testing.T) {
	// A source with at least one named run that contains events must produce a
	// report, and Execute must return ExitSuccess.
	src := domain.Source{Kind: domain.SourceLogsRoot, Path: "/logs"}

	reader := newFakeEventReader()
	reader.addEvents(cliTestOrchFile,
		cliTestTurnEvent("model-a", domain.TokenUsage{Input: domain.Tokens(1000)}),
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

	opts := cli.Options{
		Service: svc,
		Stdout:  &bytes.Buffer{},
		Stderr:  &bytes.Buffer{},
	}
	// Use `report` subcommand with an explicit path.
	exitCode := cli.Execute(opts, []string{"report", "--path", "/logs"})

	if exitCode != cli.ExitSuccess {
		t.Errorf("Execute exit code = %d, want ExitSuccess (%d)", exitCode, cli.ExitSuccess)
	}
}

func TestExecute_ReturnsExitNoDataWhenSourceHasNoRuns(t *testing.T) {
	// A source that is usable but contains no runs must produce ExitNoData.
	emptySource := domain.Source{Kind: domain.SourceLogsRoot, Path: "/empty"}

	logSource := &fakeLogSource{
		classifyFunc: func(_ string) domain.Source { return emptySource },
		enumerateFunc: func(s domain.Source) (domain.Inventory, []domain.Finding) {
			return domain.Inventory{Source: s}, nil // no runs
		},
	}

	opts := cli.Options{
		Service: buildService(logSource),
		Stdout:  &bytes.Buffer{},
		Stderr:  &bytes.Buffer{},
	}
	exitCode := cli.Execute(opts, []string{"report", "--path", "/empty"})

	if exitCode != cli.ExitNoData {
		t.Errorf("Execute exit code = %d, want ExitNoData (%d)", exitCode, cli.ExitNoData)
	}
}

func TestExecute_ReturnsExitUsageWhenNoSourceResolved(t *testing.T) {
	// With no --path flag and no default source, Execute must return ExitUsage
	// without hanging. A missing source is a usage-style outcome, not an error.
	// LogSource.Default must be called to confirm the source-resolution code path
	// was traversed — not merely that the stub coincidentally returned ExitUsage.
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
	// No --path flag supplied, no positional args.
	exitCode := cli.Execute(opts, []string{"report"})

	if exitCode != cli.ExitUsage {
		t.Errorf("Execute exit code = %d, want ExitUsage (%d)", exitCode, cli.ExitUsage)
	}
	if !defaultCalled {
		t.Errorf("LogSource.Default was not called; source resolution was not exercised (ExitUsage may be a stub coincidence, not the no-source code path)")
	}
}

func TestExecute_ReturnsExitUnusableWhenPathIsUnusable(t *testing.T) {
	// When the supplied --path exists but is not a logs tree (SourceUnusable),
	// Execute must return ExitUnusable.
	unusableSource := domain.Source{
		Kind:   domain.SourceUnusable,
		Path:   "/not-a-logs-tree",
		Reason: "no run folders found",
	}

	logSource := &fakeLogSource{
		classifyFunc: func(_ string) domain.Source { return unusableSource },
	}

	opts := cli.Options{
		Service: buildService(logSource),
		Stdout:  &bytes.Buffer{},
		Stderr:  &bytes.Buffer{},
	}
	exitCode := cli.Execute(opts, []string{"report", "--path", "/not-a-logs-tree"})

	if exitCode != cli.ExitUnusable {
		t.Errorf("Execute exit code = %d, want ExitUnusable (%d)", exitCode, cli.ExitUnusable)
	}
}

func TestExecute_ReturnsExitFailureWhenPricingStoreLoadFails(t *testing.T) {
	// When PricingStore.Load returns an infrastructure error (not a data-quality
	// finding), Execute must return ExitFailure — the code for unexpected
	// infrastructure errors. This distinguishes genuine infrastructure failures
	// from usage errors, no-data outcomes, and unusable-path outcomes.
	src := domain.Source{Kind: domain.SourceLogsRoot, Path: "/logs"}
	logSource := &fakeLogSource{
		classifyFunc: func(_ string) domain.Source { return src },
		enumerateFunc: func(s domain.Source) (domain.Inventory, []domain.Finding) {
			return domain.Inventory{
				Source: s,
				Runs: []domain.RunEntry{
					{
						Run: cliTestRunRef,
						Dir: "/logs/" + cliTestRunID,
					},
				},
			}, nil
		},
	}
	store := &fakePricingStore{
		filePath: "/pricing.yaml",
		table:    domain.EmptyPricingTable(),
		loadErr:  errors.New("disk read error"),
	}
	svc := app.New(app.Deps{
		Source:      logSource,
		Reader:      newFakeEventReader(),
		Pricing:     store,
		Clock:       newFakeClock(),
		Interaction: &alwaysSkippingInteraction{},
		WorkDir:     "/workdir",
	})
	opts := cli.Options{
		Service: svc,
		Stdout:  &bytes.Buffer{},
		Stderr:  &bytes.Buffer{},
	}
	exitCode := cli.Execute(opts, []string{"report", "--path", "/logs"})

	if exitCode != cli.ExitFailure {
		t.Errorf("Execute exit code = %d for pricing-store load failure, want ExitFailure (%d)", exitCode, cli.ExitFailure)
	}
}

func TestExecute_TotalCommand_ReturnsExitSuccessWhenRunFound(t *testing.T) {
	// The `total --run <id>` command must return ExitSuccess when the run is found.
	src := domain.Source{Kind: domain.SourceLogsRoot, Path: "/logs"}

	reader := newFakeEventReader()
	reader.addEvents(cliTestOrchFile,
		cliTestTurnEvent("model-a", domain.TokenUsage{Input: domain.Tokens(500)}),
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

	opts := cli.Options{
		Service: svc,
		Stdout:  &bytes.Buffer{},
		Stderr:  &bytes.Buffer{},
	}
	exitCode := cli.Execute(opts, []string{"total", "--path", "/logs", "--run", cliTestRunID})

	if exitCode != cli.ExitSuccess {
		t.Errorf("Execute `total` exit code = %d, want ExitSuccess (%d)", exitCode, cli.ExitSuccess)
	}
}

func TestExecute_NeverHangsWithAlwaysSkippingInteraction(t *testing.T) {
	// Under every flag combination the CLI explores, Execute must complete
	// without blocking when the Interaction always skips. This is verified by
	// running the command synchronously — if it hangs the test will time out.
	logSource := &fakeLogSource{}

	opts := cli.Options{
		Service: buildService(logSource),
		Stdout:  &bytes.Buffer{},
		Stderr:  &bytes.Buffer{},
	}
	// This call must return (not hang). The exact exit code is secondary.
	_ = cli.Execute(opts, []string{"report"})
}
