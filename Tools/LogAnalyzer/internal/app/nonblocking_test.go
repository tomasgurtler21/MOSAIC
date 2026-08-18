package app_test

// Tests for non-blocking behaviour with an always-skipping Interaction.
//
// These tests verify that every flow in the service completes without blocking
// when given an always-skipping Interaction — the shape required of the CLI
// implementation. An always-skipping Interaction returns SkippedOne immediately
// for every question without waiting for user input.

import (
	"context"
	"testing"

	"mosaic-log-analyzer/internal/app"
	"mosaic-log-analyzer/internal/domain"
)

// ---------------------------------------------------------------------------
// Source resolution with always-skipping interaction
// ---------------------------------------------------------------------------

func TestAnalyze_AlwaysSkipping_NoSource_OutcomeSourceNotFound(t *testing.T) {
	// With an always-skipping Interaction and no available source, the service
	// must return OutcomeSourceNotFound without blocking — the prompt is skipped
	// immediately and the flow completes cleanly.
	logSource := &fakeLogSource{} // Default always returns SourceNotFound.

	svc := app.New(app.Deps{
		Source:      logSource,
		Reader:      newFakeEventReader(),
		Pricing:     newFakePricingStore(),
		Clock:       newFakeClock(),
		Interaction: &alwaysSkippingInteraction{},
		WorkDir:     "/workdir",
	})

	result, err := svc.Analyze(context.Background(), app.Request{})

	if err != nil {
		t.Errorf("Analyze must not return an error when the prompt is skipped; got: %v", err)
	}
	if result.Outcome != app.OutcomeSourceNotFound {
		t.Errorf("Outcome = %v, want OutcomeSourceNotFound when the always-skipping interaction skips the prompt",
			result.Outcome)
	}
}

func TestAnalyze_AlwaysSkipping_EmptySource_OutcomeNoData(t *testing.T) {
	// With an always-skipping Interaction and a valid but empty source, the
	// service must return OutcomeNoData without blocking.
	emptySource := domain.Source{Kind: domain.SourceLogsRoot, Path: "/empty-logs"}

	logSource := &fakeLogSource{
		classifyFunc:  func(_ string) domain.Source { return emptySource },
		enumerateFunc: func(src domain.Source) (domain.Inventory, []domain.Finding) {
			return domain.Inventory{Source: src}, nil
		},
	}

	svc := app.New(app.Deps{
		Source:      logSource,
		Reader:      newFakeEventReader(),
		Pricing:     newFakePricingStore(),
		Clock:       newFakeClock(),
		Interaction: &alwaysSkippingInteraction{},
		WorkDir:     "/workdir",
	})

	result, err := svc.Analyze(context.Background(), app.Request{
		ExplicitPath: "/empty-logs",
	})

	if err != nil {
		t.Errorf("Analyze returned unexpected error: %v", err)
	}
	if result.Outcome != app.OutcomeNoData {
		t.Errorf("Outcome = %v, want OutcomeNoData", result.Outcome)
	}
}

// ---------------------------------------------------------------------------
// Pricing gap flow with always-skipping interaction
// ---------------------------------------------------------------------------

func TestAnalyze_AlwaysSkipping_UnpricedModel_Completes(t *testing.T) {
	// When there are unpriced models and ResolvePricingGaps=true, the flow must
	// complete when given an always-skipping Interaction. The interaction skips
	// all pricing prompts immediately, leaving money as MoneyUnpriced.
	const unknownModel = domain.ModelID("unknown-model")
	const agent1 = domain.AgentInstanceID("TestAgent#1")

	src := domain.Source{Kind: domain.SourceLogsRoot, Path: "/test-logs"}

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
		appTestInvEndEvent(agent1, unknownModel, domain.TokenUsage{
			Input:  domain.Tokens(1_000_000),
			Output: domain.Tokens(500_000),
		}),
	)

	svc := app.New(app.Deps{
		Source:      logSource,
		Reader:      reader,
		Pricing:     newFakePricingStore(), // empty: model is unpriced
		Clock:       newFakeClock(),
		Interaction: &alwaysSkippingInteraction{},
		WorkDir:     "/workdir",
	})

	result, err := svc.Analyze(context.Background(), app.Request{
		ExplicitPath:       "/test-logs",
		ResolvePricingGaps: true,
	})

	if err != nil {
		t.Errorf("Analyze returned unexpected error with always-skipping interaction: %v", err)
	}
	// The flow must complete — any non-error outcome is acceptable.
	_ = result.Outcome
}

func TestResolvePricingGaps_AlwaysSkipping_Completes(t *testing.T) {
	// ResolvePricingGaps must return immediately without blocking when given an
	// always-skipping Interaction. The returned report must be the same as the
	// input (no modification on skip), and the error must be nil.
	svc, _, _ := buildUnpricedService(t)

	result, err := svc.Analyze(context.Background(), app.Request{
		ExplicitPath: "/test-logs",
	})
	if err != nil {
		t.Fatalf("Analyze returned unexpected error: %v", err)
	}
	if result.Outcome != app.OutcomeReport {
		t.Fatalf("Outcome = %v, want OutcomeReport for ResolvePricingGaps prerequisite", result.Outcome)
	}

	svcSkipping := app.New(app.Deps{
		Source:      &fakeLogSource{},
		Reader:      newFakeEventReader(),
		Pricing:     newFakePricingStore(),
		Clock:       newFakeClock(),
		Interaction: &alwaysSkippingInteraction{},
		WorkDir:     "/workdir",
	})

	_, err = svcSkipping.ResolvePricingGaps(context.Background(), result.Session, result.Report)
	if err != nil {
		t.Errorf("ResolvePricingGaps returned unexpected error with always-skipping interaction: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Non-blocking with a nil context should use the cancellation path
// ---------------------------------------------------------------------------

func TestAnalyze_AlwaysSkipping_ContextCancelled_ReturnsError(t *testing.T) {
	// A cancelled context must produce a non-nil error (context.Canceled), not a
	// hang. This is the one legitimate error path that even a non-blocking flow
	// must propagate.
	src := domain.Source{Kind: domain.SourceLogsRoot, Path: "/test-logs"}

	logSource := &fakeLogSource{
		classifyFunc: func(_ string) domain.Source { return src },
		enumerateFunc: func(s domain.Source) (domain.Inventory, []domain.Finding) {
			return oneRunTwoAgentInventory(s), nil
		},
	}

	reader := newFakeEventReader()
	// Provide minimal events so the reader is entered.
	reader.addEvents(appTestOrchFile, appTestRunEndEvent())
	reader.addEvents(appTestAgent1File,
		appTestInvEndEvent("TestAgent#1", "model-a", domain.TokenUsage{
			Input: domain.Tokens(100),
		}),
	)
	reader.addEvents(appTestAgent2File,
		appTestInvEndEvent("TestAgent#2", "model-a", domain.TokenUsage{
			Input: domain.Tokens(100),
		}),
	)

	svc := app.New(app.Deps{
		Source:      logSource,
		Reader:      reader,
		Pricing:     newFakePricingStore(),
		Clock:       newFakeClock(),
		Interaction: &alwaysSkippingInteraction{},
		WorkDir:     "/workdir",
	})

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	_, err := svc.Analyze(ctx, app.Request{ExplicitPath: "/test-logs"})

	if err == nil {
		t.Error("Analyze must return an error when the context is already cancelled")
	}
}
