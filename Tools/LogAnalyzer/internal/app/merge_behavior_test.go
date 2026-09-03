package app_test

// Behavioral tests for the unknown-run merge path in TotalForRun.
//
// These tests drive the public API (TotalForRun, Analyze) to verify the
// merge behavior described in the Stage 3 design:
//
//   - When TotalForRun is called against a single-run-isolated source
//     containing an unknown-run/ sibling bucket, the unknown-run records
//     are included in the returned RunTotal's token and money totals.
//   - Duplicate records (same record_id) that appear in both the named run
//     and the unknown-run bucket are counted only once.
//   - RunTotal.UnknownRunMerged reflects the number of unknown-run records
//     successfully absorbed into the named run's totals.
//   - RunTotal.UnknownRunResidual is 0 for a single-run-isolated source.
//   - When no unknown-run bucket exists, both fields are 0 and behavior
//     is identical to the current implementation (backward compatible).
//   - When the source has multiple named runs, the merge is skipped
//     (UnknownRunMerged == 0, UnknownRunResidual == 0).
//   - Calling Analyze (the general path) with a single-run source containing
//     an unknown-run bucket still produces Report.Unattributable != nil: the
//     general path is NOT affected by the merge.

import (
	"context"
	"testing"

	"mosaic-log-analyzer/internal/app"
	"mosaic-log-analyzer/internal/domain"
)

// ---------------------------------------------------------------------------
// Test constants for merge behavior tests
// ---------------------------------------------------------------------------

const (
	mergeTestRunID    = "20260101T000000Z-abcd"
	mergeTestOrchFile = "/logs/20260101T000000Z-abcd/00_orchestrator_events.jsonl"

	// Unknown-run bucket paths
	mergeTestUnknownOrchFile   = "/logs/unknown-run/00_orchestrator_events.jsonl"
	mergeTestUnknownAgent1File = "/logs/unknown-run/Agent#1/03_events.jsonl"
)

var mergeTestRunRef = domain.NamedRun(mergeTestRunID)

// mergeTestUsageRecordEvent constructs a usage_record event with the given
// record_id, model, and token usage. Used to test record-id deduplication.
func mergeTestUsageRecordEvent(recordID string, model domain.ModelID, usage domain.TokenUsage) domain.Event {
	return domain.Event{
		Type: domain.EventUsageRecord,
		UsageRecord: &domain.UsageRecordFields{
			RecordID: recordID,
			Model:    model,
			Usage:    usage,
			HasUsage: !usage.IsEmpty(),
		},
	}
}

// mergeTestInventoryOneRunWithUnknown returns an Inventory with one named run
// and an unattributable bucket that has an orchestrator event file.
func mergeTestInventoryOneRunWithUnknown(src domain.Source) domain.Inventory {
	return domain.Inventory{
		Source: src,
		Runs: []domain.RunEntry{
			{
				Run:              mergeTestRunRef,
				Dir:              "/logs/20260101T000000Z-abcd",
				OrchestratorFile: mergeTestOrchFile,
			},
		},
		Unattributable: &domain.RunEntry{
			Run:              domain.UnattributableRun(),
			Dir:              "/logs/unknown-run",
			OrchestratorFile: mergeTestUnknownOrchFile,
		},
	}
}

// ---------------------------------------------------------------------------
// TotalForRun: unknown-run records included in totals (basic merge)
// ---------------------------------------------------------------------------

func TestTotalForRun_UnknownRunRecordsIncludedInTotal(t *testing.T) {
	// When the source has exactly one named run and an unknown-run bucket,
	// TotalForRun must include the unknown-run records in the returned total.
	//
	// Setup:
	//   Named run: run_end event only (no usage).
	//   Unknown-run: one usage_record with known token amounts.
	//
	// After merge, the run total must reflect those token amounts.
	const modelA = domain.ModelID("model-a")
	const wantInput = int64(2_000_000)

	src := domain.Source{Kind: domain.SourceLogsRoot, Path: "/logs"}

	logSource := &fakeLogSource{
		classifyFunc: func(_ string) domain.Source { return src },
		enumerateFunc: func(s domain.Source) (domain.Inventory, []domain.Finding) {
			return mergeTestInventoryOneRunWithUnknown(s), nil
		},
	}

	reader := newFakeEventReader()
	reader.addEvents(mergeTestOrchFile, appTestRunEndEvent())
	reader.addEvents(mergeTestUnknownOrchFile,
		mergeTestUsageRecordEvent("r1", modelA, domain.TokenUsage{
			Input: domain.Tokens(wantInput),
		}),
	)

	store := newFakePricingStore()
	store.table = domain.NewPricingTable([]domain.ModelPricing{flatPricing(modelA)})

	svc := app.New(app.Deps{
		Source:      logSource,
		Reader:      reader,
		Pricing:     store,
		Clock:       newFakeClock(),
		Interaction: &alwaysSkippingInteraction{},
		WorkDir:     "/workdir",
	})

	total, outcome, err := svc.TotalForRun(context.Background(), app.Request{
		ExplicitPath: "/logs",
	}, mergeTestRunID)

	if err != nil {
		t.Fatalf("TotalForRun returned unexpected error: %v", err)
	}
	if outcome != app.OutcomeReport {
		t.Fatalf("outcome = %v, want OutcomeReport", outcome)
	}
	if total.RunID != mergeTestRunID {
		t.Errorf("total.RunID = %q, want %q", total.RunID, mergeTestRunID)
	}

	// The unknown-run usage_record must be reflected in the input token count.
	inputVal, present := total.Tokens.Input.Value()
	if !present {
		t.Error("total.Tokens.Input is absent; unknown-run usage_record tokens must be included after merge")
	} else if inputVal != wantInput {
		t.Errorf("total.Tokens.Input = %d, want %d; unknown-run records must be merged into the run total", inputVal, wantInput)
	}
}

// ---------------------------------------------------------------------------
// TotalForRun: unknown-run merge signal (UnknownRunMerged, UnknownRunResidual)
// ---------------------------------------------------------------------------

func TestTotalForRun_UnknownRunMergedCount_NonZeroAfterMerge(t *testing.T) {
	// After a successful merge, RunTotal.UnknownRunMerged must be greater than
	// zero to signal that records from the unknown-run bucket were absorbed.
	const modelA = domain.ModelID("model-a")

	src := domain.Source{Kind: domain.SourceLogsRoot, Path: "/logs"}

	logSource := &fakeLogSource{
		classifyFunc: func(_ string) domain.Source { return src },
		enumerateFunc: func(s domain.Source) (domain.Inventory, []domain.Finding) {
			return mergeTestInventoryOneRunWithUnknown(s), nil
		},
	}

	reader := newFakeEventReader()
	reader.addEvents(mergeTestOrchFile, appTestRunEndEvent())
	reader.addEvents(mergeTestUnknownOrchFile,
		mergeTestUsageRecordEvent("r1", modelA, domain.TokenUsage{
			Input: domain.Tokens(500_000),
		}),
		mergeTestUsageRecordEvent("r2", modelA, domain.TokenUsage{
			Input: domain.Tokens(300_000),
		}),
	)

	store := newFakePricingStore()
	store.table = domain.NewPricingTable([]domain.ModelPricing{flatPricing(modelA)})

	svc := app.New(app.Deps{
		Source:      logSource,
		Reader:      reader,
		Pricing:     store,
		Clock:       newFakeClock(),
		Interaction: &alwaysSkippingInteraction{},
		WorkDir:     "/workdir",
	})

	total, outcome, err := svc.TotalForRun(context.Background(), app.Request{
		ExplicitPath: "/logs",
	}, mergeTestRunID)

	if err != nil {
		t.Fatalf("TotalForRun returned unexpected error: %v", err)
	}
	if outcome != app.OutcomeReport {
		t.Fatalf("outcome = %v, want OutcomeReport", outcome)
	}
	if total.UnknownRunMerged != 2 {
		t.Errorf("total.UnknownRunMerged = %d, want 2 (one for each of the two unknown-run records supplied); a partial-merge implementation that sets this to 1 would pass the old != 0 check but fail here", total.UnknownRunMerged)
	}
}

func TestTotalForRun_UnknownRunResidual_ZeroForSingleRunSource(t *testing.T) {
	// For a single-run-isolated source, all unknown-run records belong to the
	// one run by construction. UnknownRunResidual must be 0.
	const modelA = domain.ModelID("model-a")

	src := domain.Source{Kind: domain.SourceLogsRoot, Path: "/logs"}

	logSource := &fakeLogSource{
		classifyFunc: func(_ string) domain.Source { return src },
		enumerateFunc: func(s domain.Source) (domain.Inventory, []domain.Finding) {
			return mergeTestInventoryOneRunWithUnknown(s), nil
		},
	}

	reader := newFakeEventReader()
	reader.addEvents(mergeTestOrchFile, appTestRunEndEvent())
	reader.addEvents(mergeTestUnknownOrchFile,
		mergeTestUsageRecordEvent("r1", modelA, domain.TokenUsage{
			Input: domain.Tokens(400_000),
		}),
	)

	store := newFakePricingStore()
	store.table = domain.NewPricingTable([]domain.ModelPricing{flatPricing(modelA)})

	svc := app.New(app.Deps{
		Source:      logSource,
		Reader:      reader,
		Pricing:     store,
		Clock:       newFakeClock(),
		Interaction: &alwaysSkippingInteraction{},
		WorkDir:     "/workdir",
	})

	total, _, err := svc.TotalForRun(context.Background(), app.Request{
		ExplicitPath: "/logs",
	}, mergeTestRunID)

	if err != nil {
		t.Fatalf("TotalForRun returned unexpected error: %v", err)
	}
	// Guard: confirm the merge actually ran before checking the residual.
	// Without this, a zero residual from a no-merge path is indistinguishable
	// from a zero residual from a successful merge.
	if total.UnknownRunMerged == 0 {
		t.Error("total.UnknownRunMerged is 0; the merge must have run and absorbed at least one record before the residual check is meaningful")
	}
	if total.UnknownRunResidual != 0 {
		t.Errorf("total.UnknownRunResidual = %d, want 0 for a single-run-isolated source", total.UnknownRunResidual)
	}
}

func TestTotalForRun_NoUnknownRunBucket_MergedFieldsZero(t *testing.T) {
	// When the source has no unknown-run bucket at all, both merge fields must
	// be zero to preserve backward compatibility.
	const modelA = domain.ModelID("model-a")
	const agent1 = domain.AgentInstanceID("TestAgent#1")

	src := domain.Source{Kind: domain.SourceLogsRoot, Path: "/logs"}

	logSource := &fakeLogSource{
		classifyFunc: func(_ string) domain.Source { return src },
		enumerateFunc: func(s domain.Source) (domain.Inventory, []domain.Finding) {
			return domain.Inventory{
				Source: s,
				Runs: []domain.RunEntry{
					{
						Run:              mergeTestRunRef,
						Dir:              "/logs/20260101T000000Z-abcd",
						OrchestratorFile: mergeTestOrchFile,
					},
				},
				// Unattributable: nil — no unknown-run bucket
			}, nil
		},
	}

	reader := newFakeEventReader()
	reader.addEvents(mergeTestOrchFile,
		appTestInvEndEvent(agent1, modelA, domain.TokenUsage{Input: domain.Tokens(100_000)}),
		appTestRunEndEvent(),
	)

	store := newFakePricingStore()
	store.table = domain.NewPricingTable([]domain.ModelPricing{flatPricing(modelA)})

	svc := app.New(app.Deps{
		Source:      logSource,
		Reader:      reader,
		Pricing:     store,
		Clock:       newFakeClock(),
		Interaction: &alwaysSkippingInteraction{},
		WorkDir:     "/workdir",
	})

	total, outcome, err := svc.TotalForRun(context.Background(), app.Request{
		ExplicitPath: "/logs",
	}, mergeTestRunID)

	if err != nil {
		t.Fatalf("TotalForRun returned unexpected error: %v", err)
	}
	if outcome != app.OutcomeReport {
		t.Fatalf("outcome = %v, want OutcomeReport", outcome)
	}
	if total.UnknownRunMerged != 0 {
		t.Errorf("total.UnknownRunMerged = %d, want 0 when no unknown-run bucket exists", total.UnknownRunMerged)
	}
	if total.UnknownRunResidual != 0 {
		t.Errorf("total.UnknownRunResidual = %d, want 0 when no unknown-run bucket exists", total.UnknownRunResidual)
	}
}

// ---------------------------------------------------------------------------
// TotalForRun: deduplication by record_id
// ---------------------------------------------------------------------------

func TestTotalForRun_DuplicateRecordID_CountedOnlyOnce(t *testing.T) {
	// When the unknown-run bucket contains the same record_id on two different
	// actor streams (orchestrator and agent), the aggregator must count it only
	// once after merge and re-attribution.
	//
	// Setup:
	//   Named run: run_end only (no usage records of its own).
	//   Unknown-run orchestrator stream: record_id "r1" with perRecordInput tokens.
	//   Unknown-run agent stream: record_id "r1" with perRecordInput tokens
	//     (same record_id, different actor -- cross-stream collision).
	//
	// After merge, both unknown-run streams are re-attributed to the named run.
	// The orchestrator actor claims "r1" first. When the agent actor presents "r1",
	// the aggregator detects a cross-stream record collision and rejects it.
	// The final total must equal perRecordInput (1M), not 2M.
	//
	// TDD RED phase behavior: mergeEligible() stub returns false, so no merge
	// occurs. The named run has no usage records, so total.Tokens.Input is absent.
	// This test FAILS in RED for the correct reason: the merge implementation is
	// missing, so the duplicate record cannot be presented to the aggregator at all.
	const modelA = domain.ModelID("model-a")
	const perRecordInput = int64(1_000_000)

	src := domain.Source{Kind: domain.SourceLogsRoot, Path: "/logs"}

	logSource := &fakeLogSource{
		classifyFunc: func(_ string) domain.Source { return src },
		enumerateFunc: func(s domain.Source) (domain.Inventory, []domain.Finding) {
			return domain.Inventory{
				Source: s,
				Runs: []domain.RunEntry{
					{
						Run:              mergeTestRunRef,
						Dir:              "/logs/20260101T000000Z-abcd",
						OrchestratorFile: mergeTestOrchFile,
					},
				},
				Unattributable: &domain.RunEntry{
					Run:              domain.UnattributableRun(),
					Dir:              "/logs/unknown-run",
					OrchestratorFile: mergeTestUnknownOrchFile,
					Agents: []domain.AgentEntry{
						{
							InstanceHint: "Agent#1",
							EventFile:    mergeTestUnknownAgent1File,
						},
					},
				},
			}, nil
		},
	}

	reader := newFakeEventReader()
	// Named run: run_end only -- no usage records of its own.
	reader.addEvents(mergeTestOrchFile, appTestRunEndEvent())
	reader.addEvents(mergeTestUnknownOrchFile,
		// Unknown-run orchestrator: record_id "r1" (first occurrence, orchestrator actor).
		mergeTestUsageRecordEvent("r1", modelA, domain.TokenUsage{
			Input: domain.Tokens(perRecordInput),
		}),
	)
	reader.addEvents(mergeTestUnknownAgent1File,
		// Unknown-run agent: record_id "r1" (duplicate -- same ID, agent actor).
		// After re-attribution, the aggregator detects this as a cross-stream
		// collision (different actor, same record_id) and rejects it.
		mergeTestUsageRecordEvent("r1", modelA, domain.TokenUsage{
			Input: domain.Tokens(perRecordInput),
		}),
	)

	store := newFakePricingStore()
	store.table = domain.NewPricingTable([]domain.ModelPricing{flatPricing(modelA)})

	svc := app.New(app.Deps{
		Source:      logSource,
		Reader:      reader,
		Pricing:     store,
		Clock:       newFakeClock(),
		Interaction: &alwaysSkippingInteraction{},
		WorkDir:     "/workdir",
	})

	total, outcome, err := svc.TotalForRun(context.Background(), app.Request{
		ExplicitPath: "/logs",
	}, mergeTestRunID)

	if err != nil {
		t.Fatalf("TotalForRun returned unexpected error: %v", err)
	}
	if outcome != app.OutcomeReport {
		t.Fatalf("outcome = %v, want OutcomeReport", outcome)
	}

	inputVal, present := total.Tokens.Input.Value()
	if !present {
		t.Fatal("total.Tokens.Input is absent; unknown-run records must be merged and the cross-stream duplicate record_id counted only once (perRecordInput tokens expected)")
	}
	if inputVal != perRecordInput {
		t.Errorf("total.Tokens.Input = %d, want %d; cross-stream duplicate record_id must be rejected (not counted twice to give %d)",
			inputVal, perRecordInput, perRecordInput*2)
	}
}

func TestTotalForRun_UniqueRecordIDs_BothCounted(t *testing.T) {
	// When the named run and unknown-run bucket have records with DIFFERENT
	// record_ids, both must be counted in the final total.
	const modelA = domain.ModelID("model-a")
	const namedRunInput = int64(1_000_000)
	const unknownRunInput = int64(600_000)
	const wantTotal = namedRunInput + unknownRunInput

	src := domain.Source{Kind: domain.SourceLogsRoot, Path: "/logs"}

	logSource := &fakeLogSource{
		classifyFunc: func(_ string) domain.Source { return src },
		enumerateFunc: func(s domain.Source) (domain.Inventory, []domain.Finding) {
			return mergeTestInventoryOneRunWithUnknown(s), nil
		},
	}

	reader := newFakeEventReader()
	reader.addEvents(mergeTestOrchFile,
		mergeTestUsageRecordEvent("r1", modelA, domain.TokenUsage{
			Input: domain.Tokens(namedRunInput),
		}),
		appTestRunEndEvent(),
	)
	reader.addEvents(mergeTestUnknownOrchFile,
		mergeTestUsageRecordEvent("r2", modelA, domain.TokenUsage{
			Input: domain.Tokens(unknownRunInput),
		}),
	)

	store := newFakePricingStore()
	store.table = domain.NewPricingTable([]domain.ModelPricing{flatPricing(modelA)})

	svc := app.New(app.Deps{
		Source:      logSource,
		Reader:      reader,
		Pricing:     store,
		Clock:       newFakeClock(),
		Interaction: &alwaysSkippingInteraction{},
		WorkDir:     "/workdir",
	})

	total, outcome, err := svc.TotalForRun(context.Background(), app.Request{
		ExplicitPath: "/logs",
	}, mergeTestRunID)

	if err != nil {
		t.Fatalf("TotalForRun returned unexpected error: %v", err)
	}
	if outcome != app.OutcomeReport {
		t.Fatalf("outcome = %v, want OutcomeReport", outcome)
	}

	inputVal, present := total.Tokens.Input.Value()
	if !present {
		t.Fatal("total.Tokens.Input is absent; want a known value when both named-run and unknown-run records are present")
	}
	if inputVal != wantTotal {
		t.Errorf("total.Tokens.Input = %d, want %d (namedRun %d + unknownRun %d)",
			inputVal, wantTotal, namedRunInput, unknownRunInput)
	}
}

// ---------------------------------------------------------------------------
// Multi-run guard: merge is skipped when source has 2+ named runs
// ---------------------------------------------------------------------------

func TestTotalForRun_MultipleNamedRuns_MergeSkipped(t *testing.T) {
	// When the source has two or more named runs, the merge guard must reject
	// the merge. UnknownRunMerged and UnknownRunResidual must both be 0.
	const modelA = domain.ModelID("model-a")
	const runIDA = "20260101T000000Z-aaaa"
	const runIDB = "20260101T000000Z-bbbb"
	const orchFileA = "/logs/20260101T000000Z-aaaa/00_orchestrator_events.jsonl"
	const orchFileB = "/logs/20260101T000000Z-bbbb/00_orchestrator_events.jsonl"

	src := domain.Source{Kind: domain.SourceLogsRoot, Path: "/logs"}

	logSource := &fakeLogSource{
		classifyFunc: func(_ string) domain.Source { return src },
		enumerateFunc: func(s domain.Source) (domain.Inventory, []domain.Finding) {
			return domain.Inventory{
				Source: s,
				Runs: []domain.RunEntry{
					{
						Run:              domain.NamedRun(runIDA),
						Dir:              "/logs/20260101T000000Z-aaaa",
						OrchestratorFile: orchFileA,
					},
					{
						Run:              domain.NamedRun(runIDB),
						Dir:              "/logs/20260101T000000Z-bbbb",
						OrchestratorFile: orchFileB,
					},
				},
				Unattributable: &domain.RunEntry{
					Run:              domain.UnattributableRun(),
					Dir:              "/logs/unknown-run",
					OrchestratorFile: mergeTestUnknownOrchFile,
				},
			}, nil
		},
	}

	reader := newFakeEventReader()
	reader.addEvents(orchFileA, appTestRunEndEvent())
	reader.addEvents(orchFileB, appTestRunEndEvent())
	reader.addEvents(mergeTestUnknownOrchFile,
		mergeTestUsageRecordEvent("r1", modelA, domain.TokenUsage{Input: domain.Tokens(999_000)}),
	)

	svc := app.New(app.Deps{
		Source:      logSource,
		Reader:      reader,
		Pricing:     newFakePricingStore(),
		Clock:       newFakeClock(),
		Interaction: &alwaysSkippingInteraction{},
		WorkDir:     "/workdir",
	})

	total, outcome, err := svc.TotalForRun(context.Background(), app.Request{
		ExplicitPath: "/logs",
	}, runIDA)

	if err != nil {
		t.Fatalf("TotalForRun returned unexpected error: %v", err)
	}
	if outcome != app.OutcomeReport {
		t.Fatalf("outcome = %v, want OutcomeReport", outcome)
	}
	if total.UnknownRunMerged != 0 {
		t.Errorf("total.UnknownRunMerged = %d, want 0; merge must be skipped for multi-run sources", total.UnknownRunMerged)
	}
	if total.UnknownRunResidual != 0 {
		t.Errorf("total.UnknownRunResidual = %d, want 0; merge must be skipped for multi-run sources", total.UnknownRunResidual)
	}
}

// ---------------------------------------------------------------------------
// Analyze: invariant preservation (general path must NOT merge)
// ---------------------------------------------------------------------------

func TestAnalyze_WithUnknownRunBucket_UnattributableHeldSeparately(t *testing.T) {
	// Calling Analyze (the general path, not TotalForRun) with a single-run
	// source that has an unknown-run bucket must NOT merge those records into
	// the named run. The general path's invariant is that Unattributable is
	// NEVER folded into named runs. Report.Unattributable must be non-nil
	// and contain the unknown-run data as a separate bucket.
	const modelA = domain.ModelID("model-a")

	src := domain.Source{Kind: domain.SourceLogsRoot, Path: "/logs"}

	logSource := &fakeLogSource{
		classifyFunc: func(_ string) domain.Source { return src },
		enumerateFunc: func(s domain.Source) (domain.Inventory, []domain.Finding) {
			return mergeTestInventoryOneRunWithUnknown(s), nil
		},
	}

	reader := newFakeEventReader()
	reader.addEvents(mergeTestOrchFile, appTestRunEndEvent())
	reader.addEvents(mergeTestUnknownOrchFile,
		mergeTestUsageRecordEvent("r1", modelA, domain.TokenUsage{Input: domain.Tokens(750_000)}),
	)

	store := newFakePricingStore()
	store.table = domain.NewPricingTable([]domain.ModelPricing{flatPricing(modelA)})

	svc := app.New(app.Deps{
		Source:      logSource,
		Reader:      reader,
		Pricing:     store,
		Clock:       newFakeClock(),
		Interaction: &alwaysSkippingInteraction{},
		WorkDir:     "/workdir",
	})

	// Call the general Analyze path (not TotalForRun).
	result, err := svc.Analyze(context.Background(), app.Request{
		ExplicitPath: "/logs",
	})

	if err != nil {
		t.Fatalf("Analyze returned unexpected error: %v", err)
	}
	if result.Outcome != app.OutcomeReport {
		t.Fatalf("outcome = %v, want OutcomeReport", result.Outcome)
	}

	// The general path must preserve the invariant: unknown-run data is held
	// in Report.Unattributable, NEVER folded into Report.Runs.
	if result.Report.Unattributable == nil {
		t.Error("Report.Unattributable is nil when a single-run source with an unknown-run bucket was analyzed via Analyze; the general path must hold it separately")
	}
}

func TestAnalyze_WithUnknownRunBucket_NamedRunTotalsNotInflated(t *testing.T) {
	// When Analyze (general path) is called with a single-run source containing
	// unknown-run records, those records must NOT appear in the named run's
	// totals. Only TotalForRun merges; Analyze does not.
	const modelA = domain.ModelID("model-a")

	src := domain.Source{Kind: domain.SourceLogsRoot, Path: "/logs"}

	logSource := &fakeLogSource{
		classifyFunc: func(_ string) domain.Source { return src },
		enumerateFunc: func(s domain.Source) (domain.Inventory, []domain.Finding) {
			return mergeTestInventoryOneRunWithUnknown(s), nil
		},
	}

	reader := newFakeEventReader()
	// Named run: one usage_record.
	reader.addEvents(mergeTestOrchFile,
		mergeTestUsageRecordEvent("r1", modelA, domain.TokenUsage{Input: domain.Tokens(100_000)}),
		appTestRunEndEvent(),
	)
	// Unknown-run: a separate usage_record that must NOT appear in named run.
	reader.addEvents(mergeTestUnknownOrchFile,
		mergeTestUsageRecordEvent("r2", modelA, domain.TokenUsage{Input: domain.Tokens(999_999)}),
	)

	store := newFakePricingStore()
	store.table = domain.NewPricingTable([]domain.ModelPricing{flatPricing(modelA)})

	svc := app.New(app.Deps{
		Source:      logSource,
		Reader:      reader,
		Pricing:     store,
		Clock:       newFakeClock(),
		Interaction: &alwaysSkippingInteraction{},
		WorkDir:     "/workdir",
	})

	result, err := svc.Analyze(context.Background(), app.Request{
		ExplicitPath: "/logs",
	})

	if err != nil {
		t.Fatalf("Analyze returned unexpected error: %v", err)
	}
	if result.Outcome != app.OutcomeReport {
		t.Fatalf("outcome = %v, want OutcomeReport", result.Outcome)
	}
	if len(result.Report.Runs) != 1 {
		t.Fatalf("len(Report.Runs) = %d, want 1", len(result.Report.Runs))
	}

	// The named run must have only 100_000 input tokens (its own record).
	runTokens := result.Report.Runs[0].Totals.Tokens
	inputVal, present := runTokens.Input.Value()
	if !present {
		t.Fatal("named run Totals.Tokens.Input is absent")
	}
	if inputVal != 100_000 {
		t.Errorf("named run Totals.Tokens.Input = %d, want 100_000; Analyze must NOT merge unknown-run records (got %d extra tokens from unknown-run)",
			inputVal, inputVal-100_000)
	}
}
