package app_test

// Tests for data-quality summary assembly.
//
// These tests verify that:
//   - Findings produced by EventReader.Read appear in the report's QualitySummary.
//   - Findings produced by LogSource.Enumerate appear in the report's QualitySummary.
//   - Findings from analysis (e.g. missing token usage) appear in the report's QualitySummary.
//   - A report that includes data-loss findings (malformed line, unreadable file, etc.)
//     is marked as incomplete via QualitySummary.Incomplete().
//   - A report with only informational findings (unknown schema version, unrecognised
//     harness) is NOT marked incomplete.
//   - A frontend can always read QualitySummary directly from the report without
//     extra calls, ensuring partial data cannot be presented as complete.

import (
	"context"
	"testing"

	"mosaic-log-analyzer/internal/app"
	"mosaic-log-analyzer/internal/domain"
)

// ---------------------------------------------------------------------------
// Findings from EventReader.Read present in report
// ---------------------------------------------------------------------------

func TestAnalyze_ReadFindings_AppearsInReportQuality(t *testing.T) {
	// A finding emitted by EventReader.Read (e.g. a malformed line) must appear
	// in the final report's QualitySummary.Findings slice.
	src := domain.Source{Kind: domain.SourceLogsRoot, Path: "/test-logs"}
	const agent1 = domain.AgentInstanceID("TestAgent#1")

	logSource := &fakeLogSource{
		classifyFunc: func(_ string) domain.Source { return src },
		enumerateFunc: func(s domain.Source) (domain.Inventory, []domain.Finding) {
			return domain.Inventory{
				Source: s,
				Runs: []domain.RunEntry{
					{
						Run:              appTestRunRef,
						Dir:              "/test-logs/20260101T000000Z-abcd",
						OrchestratorFile: appTestOrchFile,
						Agents: []domain.AgentEntry{
							{
								Dir:          "/test-logs/20260101T000000Z-abcd/TestAgent#1",
								InstanceHint: agent1,
								EventFile:    appTestAgent1File,
							},
						},
					},
				},
			}, nil
		},
	}

	reader := newFakeEventReader()
	reader.addEvents(appTestOrchFile, appTestRunEndEvent())
	// The agent file has events AND a malformed-line finding from the reader.
	reader.addEvents(appTestAgent1File,
		appTestInvEndEvent(agent1, "model-a", domain.TokenUsage{Input: domain.Tokens(100)}),
	)
	reader.addFindings(appTestAgent1File,
		domain.Finding{
			Kind:     domain.FindingMalformedLine,
			Severity: domain.SeverityWarning,
			Path:     appTestAgent1File,
			Line:     7,
			Run:      appTestRunRef,
		},
	)

	store := newFakePricingStore()
	store.table = domain.NewPricingTable([]domain.ModelPricing{flatPricing("model-a")})

	svc := app.New(app.Deps{
		Source:      logSource,
		Reader:      reader,
		Pricing:     store,
		Clock:       newFakeClock(),
		Interaction: &alwaysSkippingInteraction{},
		WorkDir:     "/workdir",
	})

	result, err := svc.Analyze(context.Background(), app.Request{
		ExplicitPath: "/test-logs",
	})

	if err != nil {
		t.Fatalf("Analyze returned unexpected error: %v", err)
	}
	if result.Outcome != app.OutcomeReport {
		t.Fatalf("Outcome = %v, want OutcomeReport", result.Outcome)
	}

	if !hasFindingKindApp(result.Report.Quality.Findings, domain.FindingMalformedLine) {
		t.Errorf("FindingMalformedLine from EventReader must appear in Report.Quality.Findings; got: %v",
			result.Report.Quality.Findings)
	}
}

// ---------------------------------------------------------------------------
// Findings from LogSource.Enumerate present in report
// ---------------------------------------------------------------------------

func TestAnalyze_EnumerateFindings_AppearsInReportQuality(t *testing.T) {
	// A finding emitted by LogSource.Enumerate (e.g. unrecognised folder) must
	// appear in the final report's QualitySummary.Findings slice alongside any
	// read-time findings.
	src := domain.Source{Kind: domain.SourceLogsRoot, Path: "/test-logs"}

	logSource := &fakeLogSource{
		classifyFunc: func(_ string) domain.Source { return src },
		enumerateFunc: func(s domain.Source) (domain.Inventory, []domain.Finding) {
			// Return a finding from the scanner alongside the inventory.
			findings := []domain.Finding{
				{
					Kind:     domain.FindingUnrecognisedFolder,
					Severity: domain.SeverityWarning,
					Path:     "/test-logs/some-garbage-folder",
				},
			}
			// Return a minimal inventory with one run so analysis proceeds.
			return domain.Inventory{
				Source: s,
				Runs: []domain.RunEntry{
					{
						Run:              appTestRunRef,
						Dir:              "/test-logs/20260101T000000Z-abcd",
						OrchestratorFile: appTestOrchFile,
						Agents:           nil,
					},
				},
			}, findings
		},
	}

	reader := newFakeEventReader()
	reader.addEvents(appTestOrchFile, appTestRunEndEvent())

	svc := app.New(app.Deps{
		Source:      logSource,
		Reader:      reader,
		Pricing:     newFakePricingStore(),
		Clock:       newFakeClock(),
		Interaction: &alwaysSkippingInteraction{},
		WorkDir:     "/workdir",
	})

	result, err := svc.Analyze(context.Background(), app.Request{
		ExplicitPath: "/test-logs",
	})

	if err != nil {
		t.Fatalf("Analyze returned unexpected error: %v", err)
	}
	if result.Outcome != app.OutcomeReport {
		t.Fatalf("Outcome = %v, want OutcomeReport", result.Outcome)
	}

	if !hasFindingKindApp(result.Report.Quality.Findings, domain.FindingUnrecognisedFolder) {
		t.Errorf("FindingUnrecognisedFolder from Enumerate must appear in Report.Quality.Findings; got: %v",
			result.Report.Quality.Findings)
	}
}

// ---------------------------------------------------------------------------
// Findings from analysis (missing token usage) present in report
// ---------------------------------------------------------------------------

func TestAnalyze_MissingTokenUsage_FindingInReportQuality(t *testing.T) {
	// An invocation_end event with HasUsage=false must produce a
	// FindingMissingTokenUsage in the analysis layer, and that finding must
	// appear in the final report's QualitySummary.
	src := domain.Source{Kind: domain.SourceLogsRoot, Path: "/test-logs"}
	const agent1 = domain.AgentInstanceID("TestAgent#1")

	logSource := &fakeLogSource{
		classifyFunc: func(_ string) domain.Source { return src },
		enumerateFunc: func(s domain.Source) (domain.Inventory, []domain.Finding) {
			return domain.Inventory{
				Source: s,
				Runs: []domain.RunEntry{
					{
						Run:              appTestRunRef,
						Dir:              "/test-logs/20260101T000000Z-abcd",
						OrchestratorFile: appTestOrchFile,
						Agents: []domain.AgentEntry{
							{
								Dir:          "/test-logs/20260101T000000Z-abcd/TestAgent#1",
								InstanceHint: agent1,
								EventFile:    appTestAgent1File,
							},
						},
					},
				},
			}, nil
		},
	}

	reader := newFakeEventReader()
	reader.addEvents(appTestOrchFile, appTestRunEndEvent())
	// Invocation_end event with no token usage — HasUsage=false.
	reader.addEvents(appTestAgent1File,
		domain.Event{
			Type: domain.EventInvocationEnd,
			InvocationEnd: &domain.InvocationEndFields{
				AgentInstanceID: agent1,
				Model:           "model-a",
				Usage:           domain.TokenUsage{},
				HasUsage:        false,
			},
		},
	)

	svc := app.New(app.Deps{
		Source:      logSource,
		Reader:      reader,
		Pricing:     newFakePricingStore(),
		Clock:       newFakeClock(),
		Interaction: &alwaysSkippingInteraction{},
		WorkDir:     "/workdir",
	})

	result, err := svc.Analyze(context.Background(), app.Request{
		ExplicitPath: "/test-logs",
	})

	if err != nil {
		t.Fatalf("Analyze returned unexpected error: %v", err)
	}
	if result.Outcome != app.OutcomeReport {
		t.Fatalf("Outcome = %v, want OutcomeReport", result.Outcome)
	}

	if !hasFindingKindApp(result.Report.Quality.Findings, domain.FindingMissingTokenUsage) {
		t.Errorf("FindingMissingTokenUsage must appear in Report.Quality.Findings when an "+
			"invocation_end carries no token data; got: %v", result.Report.Quality.Findings)
	}
}

// ---------------------------------------------------------------------------
// Data-loss findings mark report as incomplete
// ---------------------------------------------------------------------------

func TestAnalyze_MalformedLineReading_ReportMarkedIncomplete(t *testing.T) {
	// A malformed line finding is one of the data-loss kinds that must cause
	// QualitySummary.Incomplete() to return true. Partial data must never be
	// silently presented as complete.
	src := domain.Source{Kind: domain.SourceLogsRoot, Path: "/test-logs"}
	const agent1 = domain.AgentInstanceID("TestAgent#1")

	logSource := &fakeLogSource{
		classifyFunc: func(_ string) domain.Source { return src },
		enumerateFunc: func(s domain.Source) (domain.Inventory, []domain.Finding) {
			return domain.Inventory{
				Source: s,
				Runs: []domain.RunEntry{
					{
						Run:              appTestRunRef,
						Dir:              "/test-logs/20260101T000000Z-abcd",
						OrchestratorFile: appTestOrchFile,
						Agents: []domain.AgentEntry{
							{
								Dir:          "/test-logs/20260101T000000Z-abcd/TestAgent#1",
								InstanceHint: agent1,
								EventFile:    appTestAgent1File,
							},
						},
					},
				},
			}, nil
		},
	}

	reader := newFakeEventReader()
	reader.addEvents(appTestOrchFile, appTestRunEndEvent())
	reader.addEvents(appTestAgent1File,
		appTestInvEndEvent(agent1, "model-a", domain.TokenUsage{Input: domain.Tokens(100)}),
	)
	reader.addFindings(appTestAgent1File,
		domain.Finding{
			Kind:     domain.FindingMalformedLine,
			Severity: domain.SeverityWarning,
			Path:     appTestAgent1File,
			Line:     3,
		},
	)

	svc := app.New(app.Deps{
		Source:      logSource,
		Reader:      reader,
		Pricing:     newFakePricingStore(),
		Clock:       newFakeClock(),
		Interaction: &alwaysSkippingInteraction{},
		WorkDir:     "/workdir",
	})

	result, err := svc.Analyze(context.Background(), app.Request{
		ExplicitPath: "/test-logs",
	})

	if err != nil {
		t.Fatalf("Analyze returned unexpected error: %v", err)
	}
	if result.Outcome != app.OutcomeReport {
		t.Fatalf("Outcome = %v, want OutcomeReport", result.Outcome)
	}

	if !result.Report.Quality.Incomplete() {
		t.Error("QualitySummary.Incomplete() must be true when a malformed-line finding is present")
	}
}

func TestAnalyze_UnrecognisedFolderFromScan_ReportMarkedIncomplete(t *testing.T) {
	// An unrecognised-folder finding from Enumerate is a data-loss kind and must
	// also mark the report as incomplete.
	src := domain.Source{Kind: domain.SourceLogsRoot, Path: "/test-logs"}

	logSource := &fakeLogSource{
		classifyFunc: func(_ string) domain.Source { return src },
		enumerateFunc: func(s domain.Source) (domain.Inventory, []domain.Finding) {
			findings := []domain.Finding{
				{
					Kind:     domain.FindingUnrecognisedFolder,
					Severity: domain.SeverityWarning,
					Path:     "/test-logs/garbage",
				},
			}
			return domain.Inventory{
				Source: s,
				Runs: []domain.RunEntry{
					{
						Run:              appTestRunRef,
						Dir:              "/test-logs/20260101T000000Z-abcd",
						OrchestratorFile: appTestOrchFile,
					},
				},
			}, findings
		},
	}

	reader := newFakeEventReader()
	reader.addEvents(appTestOrchFile, appTestRunEndEvent())

	svc := app.New(app.Deps{
		Source:      logSource,
		Reader:      reader,
		Pricing:     newFakePricingStore(),
		Clock:       newFakeClock(),
		Interaction: &alwaysSkippingInteraction{},
		WorkDir:     "/workdir",
	})

	result, err := svc.Analyze(context.Background(), app.Request{
		ExplicitPath: "/test-logs",
	})

	if err != nil {
		t.Fatalf("Analyze returned unexpected error: %v", err)
	}
	if result.Outcome != app.OutcomeReport {
		t.Fatalf("Outcome = %v, want OutcomeReport", result.Outcome)
	}

	if !result.Report.Quality.Incomplete() {
		t.Error("QualitySummary.Incomplete() must be true when an unrecognised-folder finding is present")
	}
}

func TestAnalyze_OnlyNonDataLossFindings_NotMarkedIncomplete(t *testing.T) {
	// A finding that is informational (e.g. FindingMissingTokenUsage) does not
	// constitute data loss and must not cause Incomplete() to return true.
	// Incomplete() is true only for data-loss finding kinds defined in quality.go.
	src := domain.Source{Kind: domain.SourceLogsRoot, Path: "/test-logs"}
	const agent1 = domain.AgentInstanceID("TestAgent#1")

	logSource := &fakeLogSource{
		classifyFunc: func(_ string) domain.Source { return src },
		enumerateFunc: func(s domain.Source) (domain.Inventory, []domain.Finding) {
			return domain.Inventory{
				Source: s,
				Runs: []domain.RunEntry{
					{
						Run:              appTestRunRef,
						Dir:              "/test-logs/20260101T000000Z-abcd",
						OrchestratorFile: appTestOrchFile,
						Agents: []domain.AgentEntry{
							{
								Dir:          "/test-logs/20260101T000000Z-abcd/TestAgent#1",
								InstanceHint: agent1,
								EventFile:    appTestAgent1File,
							},
						},
					},
				},
			}, nil
		},
	}

	reader := newFakeEventReader()
	reader.addEvents(appTestOrchFile, appTestRunEndEvent())
	// HasUsage=false → FindingMissingTokenUsage (not a data-loss kind).
	reader.addEvents(appTestAgent1File,
		domain.Event{
			Type: domain.EventInvocationEnd,
			InvocationEnd: &domain.InvocationEndFields{
				AgentInstanceID: agent1,
				Model:           "model-a",
				HasUsage:        false,
			},
		},
	)

	svc := app.New(app.Deps{
		Source:      logSource,
		Reader:      reader,
		Pricing:     newFakePricingStore(),
		Clock:       newFakeClock(),
		Interaction: &alwaysSkippingInteraction{},
		WorkDir:     "/workdir",
	})

	result, err := svc.Analyze(context.Background(), app.Request{
		ExplicitPath: "/test-logs",
	})

	if err != nil {
		t.Fatalf("Analyze returned unexpected error: %v", err)
	}
	if result.Outcome != app.OutcomeReport {
		t.Fatalf("Outcome = %v, want OutcomeReport", result.Outcome)
	}

	if result.Report.Quality.Incomplete() {
		t.Errorf("QualitySummary.Incomplete() must be false when only non-data-loss findings are present; "+
			"findings: %v", result.Report.Quality.Findings)
	}
}

// ---------------------------------------------------------------------------
// Helper: finding kind search
// ---------------------------------------------------------------------------

// hasFindingKindApp reports whether the findings slice contains a finding with
// the given kind.
func hasFindingKindApp(findings []domain.Finding, kind domain.FindingKind) bool {
	for _, f := range findings {
		if f.Kind == kind {
			return true
		}
	}
	return false
}
